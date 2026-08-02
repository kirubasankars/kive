// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package healthcheck runs built-in manifest probes and health hooks.
package healthcheck

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
	"kive/kv"
	"kive/rollout"
	"kive/utils"
	"kive/workspace"
)

const waitRetryInterval = time.Second

// PrepareRuntime loads the KV store and starts the hook HTTP server on tx.
// Call the returned cancel function when finished.
func PrepareRuntime(tx *sql.Tx) (context.CancelFunc, error) {
	if err := kv.Initialize(tx); err != nil {
		return nil, err
	}
	return hooks.StartRuntimeAPI(tx), nil
}

// DeployHealthCheck runs health checks during deploy (deployed active allocations only).
func DeployHealthCheck(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, wait bool, job string, verbose bool) error {
	if testHooks != nil && testHooks.DeployHealthCheckInterceptor != nil {
		return testHooks.DeployHealthCheckInterceptor(ctx, tx, rt, wait, job, verbose)
	}
	return HealthCheckWithOptions(ctx, tx, rt, wait, job, verbose, CheckOptions{DeployMode: true})
}

// HealthCheck runs manifest probes and health commands for a job.
func HealthCheck(tx *sql.Tx, rt *bucket.Runtime, wait bool, job string, verbose bool) error {
	return HealthCheckWithOptions(context.Background(), tx, rt, wait, job, verbose, CheckOptions{})
}

// HealthCheckWithOptions runs health checks with optional manifest probe scoping.
// Liveness runs first (if configured); on failure readiness is skipped.
// Each configured kind has its own wait budget.
func HealthCheckWithOptions(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	wait bool,
	job string,
	verbose bool,
	opts CheckOptions,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	hasAllocations, err := jobHasHealthCheckTargets(tx, job, opts.DeployMode)
	if err != nil {
		return err
	}
	if !hasAllocations {
		logHealthCheckSkipped(job, "no allocations")
		return nil
	}

	spec, err := data.GetJobHealthCheck(tx, job)
	if err != nil {
		return err
	}

	livenessHooks, err := data.GetHooks(tx, job, workspace.HealthKindLiveness)
	if err != nil {
		return err
	}
	readinessHooks, err := data.GetHooks(tx, job, workspace.HealthKindReadiness)
	if err != nil {
		return err
	}

	runLiveness, runReadiness, err := kindFilter(opts.Kind)
	if err != nil {
		return err
	}

	hasLiveness := runLiveness && (spec.HasLivenessProbes() || len(livenessHooks) > 0)
	hasReadiness := runReadiness && (spec.HasReadinessProbes() || len(readinessHooks) > 0)
	if !hasLiveness && !hasReadiness {
		logHealthCheckSkipped(job, "no health_check config or commands")
		if opts.PersistResults && opts.RunID != "" {
			if err := persistWith(tx, opts.PersistDB, func(wtx *sql.Tx) error {
				return persistSkippedJob(wtx, job, opts.RunID, "no health_check config or commands")
			}); err != nil {
				return err
			}
		}
		return nil
	}

	if opts.PersistResults && opts.recorder == nil {
		opts.recorder = newStatusRecorder()
	}

	logPasses := func() error {
		if !verbose {
			logHealthCheckPassed(job, "")
			return nil
		}
		workerIPs, err := resolveHealthCheckWorkers(tx, job, opts.DeployMode)
		if err != nil {
			return err
		}
		logHealthCheckPasses(job, workerIPs)
		return nil
	}

	if hasLiveness {
		if err := runHealthKind(ctx, tx, rt, job, wait, verbose, opts, workspace.HealthKindLiveness,
			spec, spec.LivenessChecks(), livenessHooks); err != nil {
			if opts.PersistResults && opts.RunID != "" {
				if perr := persistWith(tx, opts.PersistDB, func(wtx *sql.Tx) error {
					return persistJobHealthResults(wtx, job, opts.RunID, opts.recorder, err)
				}); perr != nil {
					return perr
				}
			}
			return err
		}
	}
	if hasReadiness {
		if err := runHealthKind(ctx, tx, rt, job, wait, verbose, opts, workspace.HealthKindReadiness,
			spec, spec.ReadinessChecks(), readinessHooks); err != nil {
			if opts.PersistResults && opts.RunID != "" {
				if perr := persistWith(tx, opts.PersistDB, func(wtx *sql.Tx) error {
					return persistJobHealthResults(wtx, job, opts.RunID, opts.recorder, err)
				}); perr != nil {
					return perr
				}
			}
			return err
		}
	}

	if err := logPasses(); err != nil {
		return err
	}
	if opts.PersistResults && opts.RunID != "" {
		if err := persistWith(tx, opts.PersistDB, func(wtx *sql.Tx) error {
			return persistJobHealthResults(wtx, job, opts.RunID, opts.recorder, nil)
		}); err != nil {
			return err
		}
	}
	return nil
}

func kindFilter(kind string) (liveness, readiness bool, err error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "all":
		return true, true, nil
	case workspace.HealthKindLiveness:
		return true, false, nil
	case workspace.HealthKindReadiness:
		return false, true, nil
	default:
		return false, false, fmt.Errorf("invalid health check kind %q (want liveness, readiness, or all)", kind)
	}
}

func runHealthKind(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	wait bool,
	verbose bool,
	opts CheckOptions,
	kind string,
	spec *workspace.ManifestHealthCheck,
	probes []workspace.HealthCheckProbe,
	commands []string,
) error {
	if opts.recorder != nil {
		opts.recorder.noteKind(kind)
	}
	runOnce := func() error {
		var errs []error
		if len(probes) > 0 {
			if err := runManifestProbes(ctx, tx, rt, job, probes, probeTimeout(spec), opts); err != nil {
				errs = append(errs, err)
			}
		}
		if len(commands) > 0 {
			if err := runHealthHooks(ctx, tx, rt, job, kind, commands, verbose, opts.DeployMode); err != nil {
				errs = append(errs, err)
			}
		}
		return joinProbeErrors(errs)
	}

	if !wait {
		if err := runOnce(); err != nil {
			logKindFailures(kind, job, err)
			if opts.recorder != nil {
				for ip := range failedWorkerIPs(err) {
					opts.recorder.failWorker(ip, kind, allocationFailureDetail(err, ip))
				}
			}
			return &HealthCheckError{Job: job, Err: err}
		}
		if opts.recorder != nil {
			workers, werr := resolveHealthCheckWorkers(tx, job, opts.DeployMode)
			if werr == nil {
				for _, ip := range workers {
					opts.recorder.passWorker(ip, kind)
				}
			}
		}
		return nil
	}

	attempts, interval, err := waitConfig(spec, kind)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = runOnce()
		if lastErr == nil {
			if opts.recorder != nil {
				workers, werr := resolveHealthCheckWorkers(tx, job, opts.DeployMode)
				if werr == nil {
					for _, ip := range workers {
						opts.recorder.passWorker(ip, kind)
					}
				}
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		logKindFailedRetrying(kind, job, healthCheckFailureWorkerIP(lastErr), attempt, attempts)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	logKindFailures(kind, job, lastErr)
	if opts.recorder != nil {
		for ip := range failedWorkerIPs(lastErr) {
			opts.recorder.failWorker(ip, kind, allocationFailureDetail(lastErr, ip))
		}
	}
	return &HealthCheckError{Job: job, Err: lastErr}
}

func runHealthHooks(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job, event string,
	commands []string,
	verbose bool,
	deployMode bool,
) error {
	workerIPs, err := resolveHealthCheckWorkers(tx, job, deployMode)
	if err != nil {
		return err
	}
	resolved, err := rollout.ResolveOrder(tx, job, workerIPs)
	if err != nil {
		return err
	}
	batchSize, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return err
	}
	baseCtx := hooks.BatchContext{
		Phase:        event,
		RolloutOrder: resolved.FullOrder,
		OrderSource:  resolved.Source,
	}
	var errs []error
	for _, commandName := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := hooks.RunHookInBatches(
			ctx, tx, rt, job, commandName, event,
			resolved.Ordered, batchSize, baseCtx, verbose, nil, nil,
			hooks.WithContinueOnFailure(),
		); err != nil {
			errs = append(errs, err)
		}
	}
	return joinProbeErrors(errs)
}

// RunJobs health-checks multiple jobs using the same transaction and runtime.
// Waiting retries for per-job checks honor ctx cancellation. Worker SSH is a
// single pass and never blocks job checks.
func RunJobs(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, wait, verbose bool, jobNames []string) error {
	return RunJobsWithOptions(ctx, tx, rt, wait, verbose, jobNames, CheckOptions{})
}

// RunJobsWithOptions is like RunJobs with CheckOptions (e.g. Kind filter).
// Catalog worker SSH runs in parallel with job checks so it does not delay them.
func RunJobsWithOptions(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, wait, verbose bool, jobNames []string, opts CheckOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	workers, sshPort, catalogErr := catalogWorkers(tx)
	type workerOutcome struct {
		rows []data.WorkerHealthStatusRow
		err  error
	}
	workerCh := make(chan workerOutcome, 1)
	go func() {
		if catalogErr != nil {
			workerCh <- workerOutcome{err: catalogErr}
			return
		}
		if len(workers) == 0 {
			fmt.Println("worker health check skipped: no workers")
			workerCh <- workerOutcome{}
			return
		}
		rows, err := dialWorkers(ctx, workers, sshPort, opts.RunID)
		if opts.PersistDB != nil {
			if perr := persistWorkerRows(nil, opts.PersistDB, opts.RunID, rows); perr != nil && err == nil {
				err = perr
			}
			rows = nil
		}
		workerCh <- workerOutcome{rows: rows, err: err}
	}()

	jobNames = utils.Unique(jobNames)
	failures := make([]HealthCheckError, 0)
	for _, jobName := range jobNames {
		if err := ctx.Err(); err != nil {
			break
		}
		if err := HealthCheckWithOptions(ctx, tx, rt, wait, jobName, verbose, opts); err != nil {
			if hcErr, ok := err.(*HealthCheckError); ok {
				failures = append(failures, *hcErr)
			} else {
				failures = append(failures, HealthCheckError{Job: jobName, Err: err})
			}
		}
	}
	jobErr := newBatchHealthCheckError(failures)

	outcome := <-workerCh
	if err := persistWorkerRows(tx, opts.PersistDB, opts.RunID, outcome.rows); err != nil && outcome.err == nil {
		outcome.err = err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if catalogErr != nil && jobErr == nil {
		return catalogErr
	}
	return joinHealthRunErrors(outcome.err, jobErr)
}

func joinHealthRunErrors(workerErr, jobErr error) error {
	if workerErr == nil {
		return jobErr
	}
	if jobErr == nil {
		return workerErr
	}
	return fmt.Errorf("%w; %v", workerErr, jobErr)
}

// Execute runs health checks for jobs in the bucket (optionally filtered by name).
func Execute(wait, verbose bool, jobsComma string) error {
	return ExecuteContext(context.Background(), wait, verbose, jobsComma, "", "")
}

// ExecuteContext is like Execute but stops wait retries when ctx is cancelled
// (e.g. serve Activity Cancel). kind filters liveness/readiness/all.
// Results are persisted to health_status tables; when runID is empty a new ID is generated.
func ExecuteContext(ctx context.Context, wait, verbose bool, jobsComma, kind, runID string) error {
	persistDB, err := data.OpenDatabase(true)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	persistDB.SetMaxOpenConns(1)
	persistDB.SetMaxIdleConns(1)
	defer func() {
		_ = persistDB.Close()
	}()

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

	cancel, err := PrepareRuntime(tx)
	if err != nil {
		return err
	}
	defer cancel()

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	rt, err := bucket.SetupRuntime(bucketID, bucket.NewRunContext("healthcheck", 0))
	if err != nil {
		return err
	}
	defer func() {
		_ = rt.Stop()
	}()

	jobNames, err := data.GetDeployedJobs(tx)
	if err != nil {
		return err
	}

	jobFilter := parseJobFilter(jobsComma)
	if len(jobFilter) > 0 {
		unknown := utils.Difference(jobFilter, jobNames)
		if len(unknown) > 0 {
			return fmt.Errorf("jobs not deployed: %v", unknown)
		}
		jobNames = jobFilter
	}

	opts := CheckOptions{Kind: kind, PersistDB: persistDB}
	if strings.TrimSpace(runID) == "" {
		runID = uuid.NewString()
	}
	opts.PersistResults = true
	opts.RunID = runID
	return RunJobsWithOptions(ctx, tx, rt, wait, verbose, jobNames, opts)
}

func jobHasHealthCheckTargets(tx *sql.Tx, job string, deployMode bool) (bool, error) {
	workers, err := resolveHealthCheckWorkers(tx, job, deployMode)
	if err != nil {
		return false, err
	}
	return len(workers) > 0, nil
}

func resolveHealthCheckWorkers(tx *sql.Tx, job string, deployMode bool) ([]string, error) {
	if deployMode {
		return data.GetDeployedActiveAllocations(tx, job)
	}
	return data.GetHealthCheckActiveAllocations(tx, job)
}

func parseJobFilter(jobsComma string) []string {
	if strings.TrimSpace(jobsComma) == "" {
		return nil
	}

	parts := strings.Split(jobsComma, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			names = append(names, part)
		}
	}
	return names
}
