// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"path"

	"kive/bucket"
	"kive/data"
	"kive/healthcheck"
	"kive/utils"
	"kive/workspace"
)

func updateAllocationHash(tx *sql.Tx, jobs []string) error {
	for _, job := range jobs {
		if err := updateJobAllocationHashes(tx, job); err != nil {
			return err
		}
	}
	return nil
}

func updateJobAllocationHashes(tx *sql.Tx, job string) error {
	workers, err := data.GetNonRemovedAllocations(tx, job)
	if err != nil {
		return err
	}

	for _, workerIP := range workers {
		workerDirPath := bucket.GetTempWorkerPath(workerIP)
		jobDir := path.Join(workerDirPath, "jobs", job)

		allocID, err := data.GetAllocationID(tx, workerIP, job)
		if err != nil {
			return err
		}

		tree, err := utils.HashDirectoryTree(jobDir)
		if err != nil {
			return err
		}
		tree = utils.FilterDirectoryTree(tree, workspace.IsJobHostLocalPath)
		if err := data.UpdateAllocationPlan(tx, allocID, tree.Aggregate, data.FileManifest(tree.Files)); err != nil {
			return err
		}

		if err := clearWorkerVersionKVKeys(job, workerIP); err != nil {
			return err
		}
	}
	return nil
}

func promoteAllocationWorkers(tx *sql.Tx, job string, workerIPs []string) error {
	if len(workerIPs) == 0 {
		return nil
	}

	for _, workerIP := range workerIPs {
		allocID, err := data.GetAllocationID(tx, workerIP, job)
		if err != nil {
			return err
		}

		if err := data.PromoteAllocationState(tx, allocID); err != nil {
			return err
		}
		if err := clearWorkerVersionKVKeys(job, workerIP); err != nil {
			return err
		}
	}
	return nil
}

func markAllocationWorkersHealthFailed(tx *sql.Tx, job string, workerIPs []string) error {
	for _, workerIP := range workerIPs {
		allocID, err := data.GetAllocationID(tx, workerIP, job)
		if err != nil {
			return err
		}
		if err := data.MarkAllocationHealthFailed(tx, allocID); err != nil {
			return err
		}
	}
	return nil
}

// promoteThenHealth promotes the batch then runs the post-batch health gate.
// On health failure or cancel, only this batch is marked health_failed so the
// next deploy retries it; earlier batches that already passed health stay promoted.
func promoteThenHealth(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	batch []string,
) error {
	if err := promoteAllocationWorkers(tx, job, batch); err != nil {
		return err
	}
	if err := healthcheck.DeployHealthCheck(deployCancelCtx(ctx), tx, rt, true, job, true); err != nil {
		if markErr := markAllocationWorkersHealthFailed(tx, job, batch); markErr != nil {
			return fmt.Errorf("%v; mark health_failed: %w", err, markErr)
		}
		return err
	}
	return nil
}

func promoteAllocationHash(tx *sql.Tx, job string) error {
	workers, err := data.GetNonRemovedAllocations(tx, job)
	if err != nil {
		return err
	}
	return promoteAllocationWorkers(tx, job, workers)
}
