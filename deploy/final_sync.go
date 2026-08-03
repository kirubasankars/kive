// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"

	"kive/bucket"
	"kive/data"
)

// finalSyncDeployedJobs rsyncs each successfully deployed job only to workers with an
// active allocation for that job. Worker metadata (jobs.json, bin/, and optionally
// worker.json) is refreshed separately by finalSyncWorkerMetadata.
func finalSyncDeployedJobs(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID string,
	deployedJobs []string,
) error {
	refreshedWorkers := make(map[string]bool, len(deployedJobs))

	for _, job := range deployedJobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		workers, err := data.GetActiveAllocations(tx, job)
		if err != nil {
			return err
		}
		if len(workers) == 0 {
			continue
		}

		for _, workerIP := range workers {
			if refreshedWorkers[workerIP] {
				continue
			}
			if err := prepareOneWorkerFiles(tx, workerIP); err != nil {
				return err
			}
			refreshedWorkers[workerIP] = true
		}

		if err := syncWorkers(ctx, rt, bucketID, workers, []string{job}, true); err != nil {
			return err
		}
	}
	return nil
}

// finalSyncWorkerMetadata refreshes jobs.json, bin/, and (when bucket.conf
// iptables=true) etc/iptables.rules on every worker after generation has been
// bumped. Job trees are not touched. When includeWorkerJSON is true, worker.json
// is included (only after all jobs in the run skipped or deployed successfully).
// When iptables is enabled, each worker then runs bin/apply_iptables.
func finalSyncWorkerMetadata(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID string,
	workers []string,
	includeWorkerJSON bool,
) error {
	if len(workers) == 0 {
		return nil
	}
	if err := prepareWorkersFiles(tx, workers); err != nil {
		return err
	}
	return syncWorkersMetadata(ctx, rt, bucketID, workers, includeWorkerJSON)
}
