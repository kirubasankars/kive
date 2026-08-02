// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package hooks runs Python, Bun, Ruby, Bash, or precompiled binary manifest hooks on the CLI host across worker allocations.
package hooks

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"kive/bucket"
	"kive/data"
	"kive/kv"
	"kive/rollout"
	"kive/utils"
)

//go:embed kive.py
var KivePy []byte

//go:embed kive.ts
var KiveTS []byte

//go:embed kive.rb
var KiveRB []byte

//go:embed kive.sh
var KiveSH []byte

// RunHook runs hookName for jobName on all non-removed allocations for event.
func RunHook(
	tx *sql.Tx,
	rt *bucket.Runtime,
	jobName, hookName, event string,
	concurrency int,
	verbose bool,
	extraEnv []string,
) error {
	workerIPs, err := data.GetNonRemovedAllocations(tx, jobName)
	if err != nil {
		return err
	}
	return RunHookOnWorkers(context.Background(), tx, rt, jobName, hookName, event, workerIPs, concurrency, verbose, extraEnv, nil)
}

// RunHookOnWorkers runs hookName on the given worker IPs for event.
// scriptArgs are positional arguments for the hook script (CLI hooks only; pass nil otherwise).
func RunHookOnWorkers(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	jobName, hookName, event string,
	workerIPs []string,
	concurrency int,
	verbose bool,
	extraEnv []string,
	scriptArgs []string,
) error {
	allowedHooks, err := data.GetHooks(tx, jobName, event)
	if err != nil {
		return err
	}
	if !hookAllowed(allowedHooks, hookName) {
		return &NotFoundError{Job: jobName, Hook: hookName, Event: event}
	}

	if len(workerIPs) == 0 {
		return nil
	}
	workerIPs = utils.Unique(workerIPs)

	if err := validateHostRuntime(jobName, hookName); err != nil {
		return err
	}

	logHookPreparing(jobName, len(workerIPs), verbose)
	if err := prepareWorkerWorkspaces(tx, jobName, workerIPs, event, hookName); err != nil {
		return err
	}

	if concurrency < 1 {
		concurrency = 1
	}

	return runHookOnWorkers(ctx, tx, rt, jobName, hookName, event, workerIPs, concurrency, verbose, extraEnv, scriptArgs)
}

func runHookOnWorkers(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	jobName, hookName, event string,
	workerIPs []string,
	concurrency int,
	verbose bool,
	extraEnv []string,
	scriptArgs []string,
) error {
	type allocation struct {
		id              string
		workerIP        string
		disabled        int
		allocationIndex string
		versionEnv      []string
	}

	allocations := make([]allocation, 0, len(workerIPs))
	for _, workerIP := range workerIPs {
		disabled, err := data.IsAllocationDisabled(tx, workerIP, jobName)
		if err != nil {
			return err
		}
		allocID, err := data.GetAllocationID(tx, workerIP, jobName)
		if err != nil {
			return err
		}
		allocationIndex, err := allocationIndexForHook(tx, jobName, workerIP)
		if err != nil {
			return err
		}
		versionEnv, err := allocationVersionEnvForHook(tx, jobName, workerIP)
		if err != nil {
			return err
		}
		allocations = append(allocations, allocation{
			id:              allocID,
			workerIP:        workerIP,
			disabled:        disabled,
			allocationIndex: allocationIndex,
			versionEnv:      versionEnv,
		})
	}

	var (
		waitGroup sync.WaitGroup
		failures  []WorkerFailure
		failureMu sync.Mutex
		semaphore = make(chan struct{}, concurrency)
	)

	for _, alloc := range allocations {
		if ctx.Err() != nil {
			break
		}

		waitGroup.Add(1)
		select {
		case <-ctx.Done():
			waitGroup.Done()
			continue
		case semaphore <- struct{}{}:
		}

		go func(alloc allocation) {
			defer waitGroup.Done()
			defer func() { <-semaphore }()

			workerEnv := append(append([]string(nil), extraEnv...), alloc.versionEnv...)

			err := runHookOnWorker(
				ctx,
				rt,
				alloc.id,
				jobName,
				alloc.workerIP,
				alloc.allocationIndex,
				alloc.disabled,
				hookName,
				event,
				verbose,
				workerEnv,
				scriptArgs,
			)
			if err != nil {
				failureMu.Lock()
				failures = append(failures, WorkerFailure{WorkerIP: alloc.workerIP, Err: err})
				failureMu.Unlock()
			}
		}(alloc)
	}

	waitGroup.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return newRunError(jobName, hookName, failures)
}

// Execute is the CLI entry point: open DB, start the hook HTTP server, and run the command.
// When jobName is empty, hookName runs on every job that registers it for the event.
// scriptArgs are passed as positional argv to the hook script (cli event only).
func Execute(hookName, jobName, event string, verbose bool, extraEnv, scriptArgs []string) error {
	return ExecuteContext(context.Background(), hookName, jobName, event, verbose, extraEnv, scriptArgs)
}

// ExecuteContext is like Execute but honors ctx cancellation between batches and workers.
func ExecuteContext(ctx context.Context, hookName, jobName, event string, verbose bool, extraEnv, scriptArgs []string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	db, err := data.OpenDatabase(true)
	if err != nil {
		return err
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

	if err := kv.Initialize(tx); err != nil {
		return err
	}

	jobs, err := resolveJobsForHook(tx, jobName, hookName, event)
	if err != nil {
		return err
	}
	if jobName == "" && len(jobs) == 0 {
		return nil
	}

	cancelServer := StartRuntimeAPI(tx)
	defer cancelServer()

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	rt, err := bucket.SetupRuntime(bucketID, bucket.NewRunContext("hooks", 0))
	if err != nil {
		return err
	}
	defer func() {
		_ = rt.Stop()
	}()

	if err := os.MkdirAll(bucket.TempLocation, 0o755); err != nil {
		return err
	}
	defer bucket.PruneTempDir()

	var batchOpts []BatchOption
	if len(scriptArgs) > 0 {
		batchOpts = append(batchOpts, WithScriptArgs(scriptArgs))
	}

	var runErrors []error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			runErrors = append(runErrors, err)
			break
		}
		if len(jobs) > 1 {
			log.Printf("hooks: %s", job)
		}
		workerIPs, err := data.GetNonRemovedAllocations(tx, job)
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("job %s: %w", job, err))
			continue
		}
		resolved, err := rollout.ResolveOrder(tx, job, workerIPs)
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("job %s: %w", job, err))
			continue
		}
		batchSize, err := data.GetMaxConcurrentRestarts(tx, job)
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("job %s: %w", job, err))
			continue
		}
		baseCtx := BatchContext{
			Phase:        event,
			RolloutOrder: resolved.FullOrder,
			OrderSource:  resolved.Source,
		}
		if err := RunHookInBatches(
			ctx, tx, rt, job, hookName, event,
			resolved.Ordered, batchSize, baseCtx, verbose, extraEnv, nil,
			batchOpts...,
		); err != nil {
			if jobName == "" {
				var notFound *NotFoundError
				if errors.As(err, &notFound) {
					continue
				}
			}
			runErrors = append(runErrors, fmt.Errorf("job %s: %w", job, err))
		}
	}

	if err := kv.PersistToSessionTransaction(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}

	return errors.Join(runErrors...)
}

func resolveJobsForHook(tx *sql.Tx, jobName, hookName, event string) ([]string, error) {
	if jobName != "" {
		allowed, err := data.GetHooks(tx, jobName, event)
		if err != nil {
			return nil, err
		}
		if !hookAllowed(allowed, hookName) {
			return nil, &NotFoundError{Job: jobName, Hook: hookName, Event: event}
		}
		return []string{jobName}, nil
	}

	return data.GetJobsWithHook(tx, hookName, event)
}
