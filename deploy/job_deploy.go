// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
	"kive/rollout"
)

// deployJob runs the lifecycle rollout for one job on the current deployment sequence.
func deployJob(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, bucketID, job string, opts Options) error {
	commands, err := data.GetHooks(tx, job, "job_control")
	if err != nil {
		return &JobError{Job: job, Err: err}
	}

	if len(commands) > 0 {
		return deployJobWithCommands(ctx, tx, rt, bucketID, job, commands, opts)
	}

	needsRollout, err := JobNeedsRollout(tx, job)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}

	if needsRollout || opts.Force {
		if err := handleNewAllocations(ctx, tx, rt, bucketID, job, opts.Force); err != nil {
			return &JobError{Job: job, Err: fmt.Errorf("start new allocations: %w", err)}
		}
		if err := handleUpdatedAllocations(ctx, tx, rt, bucketID, job, opts); err != nil {
			return &JobError{Job: job, Err: fmt.Errorf("restart updated allocations: %w", err)}
		}
		return finalizeJobDeploy(tx, job)
	}

	return nil
}

func deployJobWithCommands(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, bucketID, job string, commands []string, opts Options) error {
	needsRollout, err := JobNeedsRollout(tx, job)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}

	if !opts.Force && !needsRollout {
		return nil
	}

	if err := runJobControlRollout(ctx, tx, rt, bucketID, job, commands, opts); err != nil {
		return &JobError{Job: job, Err: err}
	}
	return finalizeJobDeploy(tx, job)
}

func runJobControlRollout(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, bucketID, job string, commands []string, opts Options) error {
	allocations, err := data.GetAllocatedWorkers(tx, job)
	if err != nil {
		return err
	}

	newAllocations, err := data.GetNewAllocations(tx, job)
	if err != nil {
		return err
	}
	updatedAllocations, err := allocationsNeedingRestart(tx, job, opts.Force)
	if err != nil {
		return err
	}

	extraEnv := []string{
		fmt.Sprintf("UPDATED_ALLOCATIONS=%s", strings.Join(updatedAllocations, ",")),
		fmt.Sprintf("NEW_ALLOCATIONS=%s", strings.Join(newAllocations, ",")),
	}

	resolved, err := rollout.ResolveOrder(tx, job, allocations)
	if err != nil {
		return err
	}
	batchSize, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return err
	}
	baseCtx := hooks.BatchContext{
		Phase:        "job_control",
		RolloutOrder: resolved.FullOrder,
		OrderSource:  resolved.Source,
	}

	batches := hooks.SplitWorkerBatches(resolved.Ordered, batchSize)
	for batchIndex, batch := range batches {
		if err := preBatchHealthCheck(ctx, tx, rt, job, opts.Force); err != nil {
			return err
		}
		for _, workerIP := range batch {
			if err := syncWorkerFiles(ctx, rt, bucketID, workerIP, []string{job}, true); err != nil {
				return err
			}
		}

		batchCtx := baseCtx
		batchCtx.Job = job
		batchCtx.BatchIndex = batchIndex
		batchCtx.BatchCount = len(batches)
		batchCtx.BatchAllocation = append([]string(nil), batch...)
		env := append(append([]string(nil), extraEnv...), hooks.BatchEnv(batchCtx)...)

		for _, command := range commands {
			if err := hooks.RunHookOnWorkers(
				ctx, tx, rt, job, command, "job_control", batch, len(batch), false, env, nil,
			); err != nil {
				return err
			}
		}

		if err := promoteThenHealth(ctx, tx, rt, job, batch); err != nil {
			return err
		}
	}
	return nil
}

func upgradePending(tx *sql.Tx, job string, opts Options) (bool, error) {
	updated, err := allocationsNeedingRestart(tx, job, opts.Force)
	if err != nil {
		return false, err
	}
	return len(updated) > 0, nil
}

// finalizeJobDeploy promotes any remaining allocation hashes.
func finalizeJobDeploy(tx *sql.Tx, job string) error {
	if err := promoteAllocationHash(tx, job); err != nil {
		return &JobError{Job: job, Err: err}
	}
	return nil
}
