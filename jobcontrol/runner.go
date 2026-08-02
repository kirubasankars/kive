// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package jobcontrol

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"kive/bucket"
	"kive/data"
	"kive/healthcheck"
	"kive/worker"
)

func runnerCommand(bucketID string, target Target, job string) string {
	return fmt.Sprintf(
		"python3 %s/bin/runner.py %s %s --jobs %s",
		bucket.WorkerBucketPath(bucketID), bucketID, target, job,
	)
}

// runRunnerTarget runs the default runner.py target on the given workers in parallel batches.
func runRunnerTarget(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID, job string,
	target Target,
	healthCheck bool,
	selected []string,
) error {
	if len(selected) == 0 {
		return nil
	}

	parallelBatchCount, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return &JobRunError{Job: job, Target: target, Err: err}
	}
	if parallelBatchCount < 1 {
		parallelBatchCount = 1
	}

	command := runnerCommand(bucketID, target, job)
	workerCount := len(selected)

	for i := 0; i < workerCount; i += parallelBatchCount {
		if err := ctx.Err(); err != nil {
			return &JobRunError{Job: job, Target: target, Err: err}
		}
		end := i + parallelBatchCount
		if end > workerCount {
			end = workerCount
		}
		batch := selected[i:end]

		if err := runWorkerBatch(ctx, rt, job, target, batch, command); err != nil {
			return &JobRunError{Job: job, Target: target, Failures: err}
		}

		if healthCheck {
			if err := healthcheck.HealthCheckWithOptions(ctx, tx, rt, true, job, true, healthcheck.CheckOptions{}); err != nil {
				return &JobRunError{Job: job, Target: target, Err: err}
			}
		}
	}

	return nil
}

func filterWorkers(workers, workerFilter []string) []string {
	if len(workerFilter) == 0 {
		out := make([]string, len(workers))
		copy(out, workers)
		return out
	}
	selected := make([]string, 0, len(workers))
	for _, workerIP := range workers {
		if workerSelected(workerIP, workerFilter) {
			selected = append(selected, workerIP)
		}
	}
	return selected
}

func runWorkerBatch(ctx context.Context, rt *bucket.Runtime, job string, target Target, workerIPs []string, command string) []WorkerFailure {
	var (
		wg       sync.WaitGroup
		failures []WorkerFailure
		mu       sync.Mutex
	)

	cmdCtx := bucket.CommandContext{
		Job:    job,
		Phase:  "job_control",
		Action: string(target),
		Cmd:    command,
	}

	for _, workerIP := range workerIPs {
		if err := ctx.Err(); err != nil {
			mu.Lock()
			failures = append(failures, WorkerFailure{WorkerIP: workerIP, Err: err})
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			if err := worker.ExecuteCommand(ctx, rt, ip, cmdCtx, []string{command}, nil); err != nil {
				mu.Lock()
				failures = append(failures, WorkerFailure{WorkerIP: ip, Err: err})
				mu.Unlock()
			}
		}(workerIP)
	}

	wg.Wait()
	return failures
}
