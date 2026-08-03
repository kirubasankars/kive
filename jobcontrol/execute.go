// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package jobcontrol runs start/stop/restart/status across job allocations,
// using registered job_control commands when present or runner.py otherwise.
package jobcontrol

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"kive/bucket"
	"kive/data"
	"kive/healthcheck"
)

// Request configures a job control run.
type Request struct {
	JobsCSV     string
	WorkersCSV  string
	Target      Target
	HealthCheck bool
	// Forget clears promoted deploy state (applied_hash) after a successful stop
	// so the next deploy treats those allocations as start-pending and skips
	// pre-batch health on them. Only valid with TargetStop.
	Forget bool
}

// Execute runs job control for optional job and worker filters.
// jobsCSV and workersCSV are comma-separated; empty means all allocated jobs/workers.
func Execute(jobsCSV, workersCSV, target string, healthCheck bool) error {
	parsedTarget, err := ParseTarget(target)
	if err != nil {
		return err
	}
	return ExecuteRequest(Request{
		JobsCSV:     jobsCSV,
		WorkersCSV:  workersCSV,
		Target:      parsedTarget,
		HealthCheck: healthCheck,
	})
}

// ExecuteRequest is the structured entry point for job control.
func ExecuteRequest(req Request) error {
	return ExecuteRequestContext(context.Background(), req)
}

// ExecuteRequestContext is like ExecuteRequest but stops in-flight work when ctx is cancelled
// (e.g. serve Activity Cancel).
func ExecuteRequestContext(ctx context.Context, req Request) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if req.Forget && req.Target != TargetStop {
		return fmt.Errorf("--forget is only valid with stop")
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
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	err = runControl(ctx, tx, req)
	// Persist forget writes even when some jobs failed: successful stops may have
	// already cleared applied_hash and must not be rolled back.
	if req.Forget {
		if commitErr := tx.Commit(); commitErr != nil {
			if err != nil {
				return fmt.Errorf("%w; also failed to commit forgot deploy state: %v", err, commitErr)
			}
			return bucket.DatabaseError(commitErr)
		}
		committed = true
	}
	return err
}

func runControl(ctx context.Context, tx *sql.Tx, req Request) error {
	filters := ParseFilters(req.JobsCSV, req.WorkersCSV)

	allJobs, err := data.GetDeployableJobs(tx)
	if err != nil {
		return err
	}
	allWorkers, err := data.GetAllWorkers(tx)
	if err != nil {
		return err
	}
	if err := filters.validateAgainst(allJobs, allWorkers); err != nil {
		return err
	}

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	generation, err := data.GetBucketGeneration(tx)
	if err != nil {
		return err
	}

	rt, err := bucket.SetupRuntime(bucketID, bucket.NewRunContext("job", generation))
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

	cancel, err := healthcheck.PrepareRuntime(tx)
	if err != nil {
		return err
	}
	defer cancel()

	workers, err := data.GetWorkers(tx, nil)
	if err != nil {
		return err
	}
	if err := data.ValidateBucketGenerationContext(ctx, tx, rt, workers); err != nil {
		return err
	}

	maxDeploymentSequence, err := data.GetMaxDeploymentSeq(tx)
	if err != nil {
		return err
	}

	var jobFailures []JobRunError

	for deploymentSeq := 0; deploymentSeq <= maxDeploymentSequence; deploymentSeq++ {
		if err := ctx.Err(); err != nil {
			exitCode = 1
			return err
		}
		jobsAtSeq, err := data.GetJobsByDeploymentSeq(tx, deploymentSeq)
		if err != nil {
			return err
		}

		selectedJobs := selectJobs(jobsAtSeq, filters.Jobs)
		seqFailures := runJobsInParallel(ctx, tx, rt, bucketID, selectedJobs, req, filters.Workers)
		jobFailures = append(jobFailures, seqFailures...)
	}

	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}
	err = newControlError(jobFailures)
	if err != nil {
		exitCode = 1
	}
	return err
}

// runJobsInParallel runs controlJob for each job on the caller goroutine.
// Jobs are sequential so a shared *sql.Tx is never used concurrently; worker SSH
// parallelism inside each job (runWorkerBatch) is unchanged.
func runJobsInParallel(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID string,
	jobs []string,
	req Request,
	workerFilter []string,
) []JobRunError {
	if len(jobs) == 0 {
		return nil
	}

	var failures []JobRunError
	for _, jobName := range jobs {
		if err := ctx.Err(); err != nil {
			failures = append(failures, JobRunError{Job: jobName, Target: req.Target, Err: err})
			break
		}
		if err := controlJob(ctx, tx, rt, bucketID, jobName, req, workerFilter); err != nil {
			var jobErr *JobRunError
			if asJobRun(err, &jobErr) {
				failures = append(failures, *jobErr)
				continue
			}
			failures = append(failures, JobRunError{Job: jobName, Target: req.Target, Err: err})
		}
	}
	return failures
}

func controlJob(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID, job string,
	req Request,
	workerFilter []string,
) error {
	if err := ctx.Err(); err != nil {
		return &JobRunError{Job: job, Target: req.Target, Err: err}
	}
	selected, err := resolveControlWorkers(tx, job, workerFilter)
	if err != nil {
		return &JobRunError{Job: job, Target: req.Target, Err: err}
	}
	if err := errIfNoControlTargets(tx, job, workerFilter, selected); err != nil {
		return &JobRunError{Job: job, Target: req.Target, Err: err}
	}
	if len(selected) == 0 {
		return nil
	}

	log.Printf("jobcontrol: %s %s on %s", req.Target, job, strings.Join(selected, ", "))

	handled, err := runJobControlHooks(
		ctx,
		tx,
		rt,
		job,
		req.Target,
		req.HealthCheck,
		selected,
	)
	if err != nil {
		return err
	}
	if !handled {
		if err := runRunnerTarget(
			ctx,
			tx,
			rt,
			bucketID,
			job,
			req.Target,
			req.HealthCheck,
			selected,
		); err != nil {
			return err
		}
	}

	if req.Forget {
		if err := forgetDeployedState(tx, job, selected); err != nil {
			return &JobRunError{
				Job:    job,
				Target: req.Target,
				Err:    fmt.Errorf("job stopped but failed to forget deploy state: %w", err),
			}
		}
	}
	log.Printf("jobcontrol: %s %s completed", req.Target, job)
	return nil
}

func asJobRun(err error, target **JobRunError) bool {
	if err == nil {
		return false
	}
	if jobErr, ok := err.(*JobRunError); ok {
		*target = jobErr
		return true
	}
	return false
}
