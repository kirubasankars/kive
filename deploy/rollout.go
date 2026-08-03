// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"
	"fmt"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
)

func effectiveBatchSize(requested, total, hardCap int) int {
	n := total
	if requested >= 1 {
		n = requested
		if n > total {
			n = total
		}
	}
	if hardCap >= 1 && n > hardCap {
		n = hardCap
	}
	return n
}

func rolloutStartBatches(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID, job string,
	candidates []string,
	force bool,
	startFn func(workerIP string) error,
	rsyncFn func(workerIP string) error,
) error {
	if len(candidates) == 0 {
		return nil
	}

	active, err := activeWorkers(candidates, tx, job)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		return nil
	}

	resolved, err := ResolveRolloutOrder(tx, job, active)
	if err != nil {
		return err
	}

	parallelism, err := data.GetMaxConcurrentStarts(tx, job)
	if err != nil {
		return err
	}
	hardCap, err := maxConcurrentSyncs()
	if err != nil {
		return err
	}
	batchSize := effectiveBatchSize(parallelism, len(resolved.Ordered), hardCap)
	totalBatches := hooks.BatchCount(len(resolved.Ordered), batchSize)

	for i := 0; i < len(resolved.Ordered); i += batchSize {
		end := i + batchSize
		if end > len(resolved.Ordered) {
			end = len(resolved.Ordered)
		}
		batch := resolved.Ordered[i:end]

		if err := preBatchHealthCheck(ctx, tx, rt, job, force); err != nil {
			return err
		}

		if err := runParallelWorkers(ctx, batch, len(batch), func(workerIP string) error {
			if err := rsyncFn(workerIP); err != nil {
				return err
			}
			return startFn(workerIP)
		}); err != nil {
			return err
		}

		batchCtx := hooks.BatchContext{
			Job:             job,
			Phase:           deployPhaseNew,
			BatchIndex:      i / batchSize,
			BatchCount:      totalBatches,
			BatchAllocation: append([]string(nil), batch...),
			RolloutOrder:    resolved.FullOrder,
			OrderSource:     resolved.Source,
		}
		if err := executeAfterAllocationStarted(ctx, tx, rt, job, batch, batchCtx); err != nil {
			return err
		}
		if err := promoteThenHealth(ctx, tx, rt, job, batch); err != nil {
			return err
		}
	}

	return nil
}

func rolloutRestartBatches(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID, job string,
	candidates []string,
	force bool,
	restartFn func(workerIP string) error,
	rsyncFn func(workerIP string) error,
) error {
	if len(candidates) == 0 {
		return nil
	}

	active, err := activeWorkers(candidates, tx, job)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		return nil
	}

	resolved, err := ResolveRolloutOrder(tx, job, active)
	if err != nil {
		return err
	}

	parallelism, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return err
	}
	if parallelism < 1 {
		parallelism = 1
	}
	hardCap, err := maxConcurrentSyncs()
	if err != nil {
		return err
	}
	parallelism = effectiveBatchSize(parallelism, len(resolved.Ordered), hardCap)
	totalBatches := hooks.BatchCount(len(resolved.Ordered), parallelism)

	for i := 0; i < len(resolved.Ordered); i += parallelism {
		end := i + parallelism
		if end > len(resolved.Ordered) {
			end = len(resolved.Ordered)
		}
		batch := resolved.Ordered[i:end]

		if err := preBatchHealthCheck(ctx, tx, rt, job, force); err != nil {
			return err
		}

		if err := runParallelWorkers(ctx, batch, len(batch), func(workerIP string) error {
			if err := rsyncFn(workerIP); err != nil {
				return err
			}
			return restartFn(workerIP)
		}); err != nil {
			return fmt.Errorf("restart updated allocations: %w", err)
		}

		batchCtx := hooks.BatchContext{
			Job:             job,
			Phase:           deployPhaseUpdate,
			BatchIndex:      i / parallelism,
			BatchCount:      totalBatches,
			BatchAllocation: append([]string(nil), batch...),
			RolloutOrder:    resolved.FullOrder,
			OrderSource:     resolved.Source,
		}
		if err := executeAfterAllocationRestarted(ctx, tx, rt, job, batch, batchCtx); err != nil {
			return err
		}
		if err := promoteThenHealth(ctx, tx, rt, job, batch); err != nil {
			return err
		}
	}

	return nil
}

// JobNeedsRollout reports whether the job still needs a deploy wave: new workers,
// staged content or versions not yet promoted, or missing hash rows on non-removed
// allocations (including disabled). Reconcile stop for disabled allocations runs
// at the start of every deploy even when this returns false.
func JobNeedsRollout(tx *sql.Tx, job string) (bool, error) {
	workers, err := data.GetNonRemovedAllocations(tx, job)
	if err != nil {
		return false, err
	}
	for _, workerIP := range workers {
		allocID, err := data.GetAllocationID(tx, workerIP, job)
		if err != nil {
			return false, err
		}
		var hashCount int
		err = tx.QueryRow(
			`SELECT count(*) FROM allocation_hashes WHERE alloc_id = ?`,
			allocID,
		).Scan(&hashCount)
		if err != nil {
			return false, bucket.DatabaseError(err)
		}
		if hashCount == 0 {
			return true, nil
		}
	}

	newAllocations, err := data.GetNewAllocations(tx, job)
	if err != nil {
		return false, err
	}
	if len(newAllocations) > 0 {
		return true, nil
	}
	updatedAllocations, err := data.GetUpdatedNonRemovedAllocations(tx, job)
	if err != nil {
		return false, err
	}
	if len(updatedAllocations) > 0 {
		return true, nil
	}

	versionPending, err := data.GetVersionPendingNonRemovedAllocations(tx, job)
	if err != nil {
		return false, err
	}
	return len(versionPending) > 0, nil
}
