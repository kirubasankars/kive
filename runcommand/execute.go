// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package runcommand runs shell commands on bucket workers via the kive container.
package runcommand

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"kive/bucket"
	"kive/data"
	"kive/healthcheck"
	"kive/prereq"
	"kive/utils"
	"kive/worker"

	"github.com/google/uuid"
)

const healthCheckDelay = 10 * time.Second

// Execute runs a shell script on selected workers. Provide either shellCommand
// (inline text) or scriptName (workspace/commands/<name>.sh), not both.
//
// Workers are limited to batchSize (-c) concurrent SSH sessions. Without
// --health_check, workers start as slots free (streaming). With --health_check,
// workers run in fixed batches and jobs are health-checked after each batch.
func Execute(workerCSV, labelCSV string, batchSize int, shellCommand, scriptName string, runHealthChecks, ignoreFailure bool) error {
	return ExecuteContext(context.Background(), workerCSV, labelCSV, batchSize, shellCommand, scriptName, runHealthChecks, ignoreFailure)
}

// ExecuteContext is like Execute but stops in-flight SSH sessions when ctx is cancelled
// (e.g. serve Activity Cancel).
func ExecuteContext(ctx context.Context, workerCSV, labelCSV string, batchSize int, shellCommand, scriptName string, runHealthChecks, ignoreFailure bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if batchSize < 1 {
		return errConcurrencyTooLow()
	}
	if workerCSV != "" && labelCSV != "" {
		return errWorkersAndLabelsTogether()
	}

	db, err := data.OpenDatabase(true)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = db.Close()
	}()

	tx, err := db.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	targetWorkerIPs, err := resolveTargetWorkerIPs(tx, workerCSV, labelCSV)
	if err != nil {
		return err
	}
	if len(targetWorkerIPs) == 0 {
		return errNoTargetWorkers()
	}

	if err := prereq.CheckLocalRunCommand(); err != nil {
		return err
	}

	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}

	scriptContent, err := resolveScriptContent(shellCommand, scriptName)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(bucket.TempLocation, os.ModePerm); err != nil {
		return bucket.UnexpectedError(err)
	}
	defer bucket.PruneTempDir()

	generation, err := data.GetBucketGeneration(tx)
	if err != nil {
		return err
	}

	rt, err := bucket.SetupRuntime(bucketID, bucket.NewRunContext("run_command", generation))
	if err != nil {
		return err
	}
	runStarted := time.Now()
	exitCode := 0
	defer func() {
		_ = rt.LogRunEnd(exitCode, time.Since(runStarted), nil)
		_ = rt.Stop()
	}()
	if err := rt.LogRunBegin(nil); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	if !ignoreFailure {
		if err := worker.CheckRunCommandPrerequisitesWithRuntime(ctx, rt, targetWorkerIPs, conf.UseSUDO); err != nil {
			exitCode = 1
			return err
		}
	}

	var jobNames []string
	var cancelHealthCheck context.CancelFunc
	if runHealthChecks {
		jobNames, err = data.GetJobs(tx)
		if err != nil {
			return err
		}
		cancelHealthCheck, err = healthcheck.PrepareRuntime(tx)
		if err != nil {
			return err
		}
		defer cancelHealthCheck()
	}

	var onFailure func(string, error)
	if ignoreFailure {
		onFailure = printWorkerFailure
	}

	if runHealthChecks {
		batchIterator := utils.NewStringIterator(targetWorkerIPs, batchSize)
		var allFailures map[string]error
		for batchNumber := 1; ; batchNumber++ {
			if err := ctx.Err(); err != nil {
				exitCode = 1
				return err
			}
			workerBatch, hasMore := batchIterator()
			if !hasMore {
				break
			}

			failures := runScriptOnWorkers(ctx, rt, bucketID, scriptContent, workerBatch, len(workerBatch), onFailure)
			if ignoreFailure {
				allFailures = mergeWorkerFailures(allFailures, failures)
			} else if err := newRunCommandError(failures); err != nil {
				return err
			}

			if err := sleepContext(ctx, healthCheckDelay); err != nil {
				exitCode = 1
				return err
			}
			if err := healthcheck.RunJobs(ctx, tx, rt, true, true, jobNames); err != nil {
				return fmt.Errorf("after worker batch %d: %w", batchNumber, err)
			}
		}

		if ignoreFailure {
			return newRunCommandError(allFailures)
		}
		return nil
	}

	failures := runScriptOnWorkers(ctx, rt, bucketID, scriptContent, targetWorkerIPs, batchSize, onFailure)
	if err := ctx.Err(); err != nil {
		exitCode = 1
		if len(failures) == 0 {
			return err
		}
		// Prefer cancel when the run was interrupted; still surface worker errors if any.
		return err
	}
	err = newRunCommandError(failures)
	if err != nil {
		exitCode = 1
	}
	return err
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func resolveTargetWorkerIPs(tx *sql.Tx, workerCSV, labelCSV string) ([]string, error) {
	workerFilter := parseCSVList(workerCSV)
	labelFilter := parseCSVList(labelCSV)

	switch {
	case len(workerFilter) > 0:
		bucketWorkerIPs, err := data.GetWorkers(tx, nil)
		if err != nil {
			return nil, err
		}

		unknownWorkers := utils.Difference(workerFilter, bucketWorkerIPs)
		if len(unknownWorkers) > 0 {
			return nil, errUnknownWorkers(unknownWorkers)
		}
		return utils.Unique(workerFilter), nil

	case len(labelFilter) > 0:
		workerIPs, err := data.GetWorkers(tx, labelFilter)
		if err != nil {
			return nil, err
		}
		return utils.Unique(workerIPs), nil

	default:
		workerIPs, err := data.GetWorkers(tx, nil)
		if err != nil {
			return nil, err
		}
		return utils.Unique(workerIPs), nil
	}
}

func parseCSVList(csv string) []string {
	if csv == "" {
		return nil
	}

	parts := strings.Split(csv, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func resolveScriptContent(shellCommand, scriptName string) ([]byte, error) {
	inline := strings.TrimSpace(shellCommand)
	name := strings.TrimSpace(scriptName)
	switch {
	case inline != "" && name != "":
		return nil, errCommandAndScriptTogether()
	case inline != "":
		return []byte(inline), nil
	case name != "":
		return loadNamedScript(name)
	default:
		return nil, errCommandRequired()
	}
}

// ListScripts returns sorted script names from workspace/commands/*.sh.
// A missing directory yields an empty list.
func ListScripts() ([]string, error) {
	entries, err := os.ReadDir(bucket.CommandsLocation)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, bucket.UnexpectedError(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		if !strings.HasSuffix(fileName, ".sh") {
			continue
		}
		name := strings.TrimSuffix(fileName, ".sh")
		if err := data.ValidateCommandScriptName(name); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func loadNamedScript(name string) ([]byte, error) {
	if err := data.ValidateCommandScriptName(name); err != nil {
		return nil, err
	}
	commandFilePath := path.Join(bucket.CommandsLocation, name+".sh")
	content, err := os.ReadFile(commandFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errUnknownScript(name)
		}
		return nil, bucket.UnexpectedError(err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return nil, errEmptyCommand()
	}
	return content, nil
}

// runScriptOnWorkers runs the script on workerIPs with at most concurrency parallel
// SSH sessions. The next worker starts as soon as a slot frees. Cancelling ctx stops
// in-flight SSH (CommandContext) and skips workers not yet started.
func runScriptOnWorkers(
	ctx context.Context,
	rt *bucket.Runtime,
	bucketID string,
	scriptContent []byte,
	workerIPs []string,
	concurrency int,
	onFailure func(string, error),
) map[string]error {
	workerIPs = utils.Unique(workerIPs)
	if len(workerIPs) == 0 {
		return nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var waitGroup sync.WaitGroup
	var failureMu sync.Mutex
	failures := make(map[string]error, len(workerIPs))
	sem := make(chan struct{}, concurrency)

	for _, workerIP := range workerIPs {
		if err := ctx.Err(); err != nil {
			failureMu.Lock()
			failures[workerIP] = err
			failureMu.Unlock()
			continue
		}

		waitGroup.Add(1)
		select {
		case <-ctx.Done():
			waitGroup.Done()
			failureMu.Lock()
			failures[workerIP] = ctx.Err()
			failureMu.Unlock()
			continue
		case sem <- struct{}{}:
		}

		go func(targetWorkerIP string) {
			defer waitGroup.Done()
			defer func() { <-sem }()

			if err := ctx.Err(); err != nil {
				failureMu.Lock()
				failures[targetWorkerIP] = err
				failureMu.Unlock()
				return
			}

			if err := runScriptOnWorker(ctx, rt, bucketID, scriptContent, targetWorkerIP); err != nil {
				failureMu.Lock()
				failures[targetWorkerIP] = err
				failureMu.Unlock()
				if onFailure != nil {
					onFailure(targetWorkerIP, err)
				}
			}
		}(workerIP)
	}

	waitGroup.Wait()
	return failures
}

func runScriptOnWorker(ctx context.Context, rt *bucket.Runtime, bucketID string, scriptContent []byte, targetWorkerIP string) error {
	commandFilePath, err := writeWorkerCommandScript(bucketID, targetWorkerIP, scriptContent)
	if err != nil {
		return fmt.Errorf("create command script: %w", err)
	}
	defer func() {
		_ = os.Remove(commandFilePath)
	}()

	execEnv := []string{
		fmt.Sprintf("BUCKET_ID=%s", bucketID),
		fmt.Sprintf("WORKER_IP=%s", targetWorkerIP),
	}

	return worker.ExecuteFileCommand(ctx, rt, targetWorkerIP, bucket.CommandContext{
		Phase:  "run_command",
		Action: "ssh",
		Cmd:    commandFilePath,
	}, commandFilePath, execEnv)
}

func writeWorkerCommandScript(bucketID, workerIP string, scriptContent []byte) (string, error) {
	// Unique name per invocation so concurrent workers never clobber the same script file.
	scriptFileName := fmt.Sprintf("command-%s-%s.sh", sanitizeWorkerIPForFilename(workerIP), uuid.NewString())
	scriptFilePath := path.Join(bucket.TempLocation, scriptFileName)

	// Start in the worker's bucket directory so relative paths (jobs/<job>/...) resolve as
	// expected. Fall back to the SSH login directory when the bucket has not been deployed
	// yet (e.g. host bootstrap via run_command before the first kive deploy).
	// HUP/INT/TERM kill the whole process group so cancel (SSH disconnect) stops children
	// like vmstat, not only the outer bash.
	prefix := strings.Join([]string{
		fmt.Sprintf("export BUCKET_ID=%s", shellQuote(bucketID)),
		fmt.Sprintf("export WORKER_IP=%s", shellQuote(workerIP)),
		fmt.Sprintf("cd %q 2>/dev/null || true", bucket.WorkerBucketPath(bucketID)),
		"trap 'kill 0 2>/dev/null' HUP INT TERM",
	}, "\n")

	fileContent := prefix + "\n" + string(scriptContent)
	if err := os.WriteFile(scriptFilePath, []byte(fileContent), 0o644); err != nil {
		return "", bucket.UnexpectedError(err)
	}

	return scriptFilePath, nil
}

func sanitizeWorkerIPForFilename(workerIP string) string {
	replacer := strings.NewReplacer(".", "_", ":", "_", "/", "_", " ", "_")
	return replacer.Replace(workerIP)
}

// shellQuote wraps a value for a single-quoted POSIX shell argument so
// catalog-derived values (bucket id, worker ip) cannot break out of the
// generated script prefix.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func mergeWorkerFailures(into, from map[string]error) map[string]error {
	if len(from) == 0 {
		return into
	}
	if into == nil {
		into = make(map[string]error, len(from))
	}
	for workerIP, err := range from {
		into[workerIP] = err
	}
	return into
}

