// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package gc

import (
	"context"
	"database/sql"
	"log"

	"kive/bucket"
	"kive/data"
	"kive/worker"
)

func purgeRemovedAllocationWorkerData(
	ctx context.Context,
	rt *bucket.Runtime,
	tx *sql.Tx,
	bucketID string,
	allocs []removedAllocation,
) error {
	catalog, err := data.LoadWorkerCatalog(tx)
	if err != nil {
		return err
	}

	for _, alloc := range allocs {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := bucket.WorkerJobPath(bucketID, alloc.Job)
		if catalog.Contains(alloc.WorkerIP) {
			log.Printf("gc: removing worker job tree worker=%s job=%s path=%s", alloc.WorkerIP, alloc.Job, path)
			if err := worker.RemoveJobTreeContext(ctx, rt, bucketID, alloc.WorkerIP, alloc.Job); err != nil {
				return err
			}
			log.Printf("gc: removed worker job tree worker=%s job=%s path=%s", alloc.WorkerIP, alloc.Job, path)
			continue
		}
		log.Printf("gc: removing worker job tree worker=%s job=%s path=%s (off-catalog)", alloc.WorkerIP, alloc.Job, path)
		if err := worker.RemoveJobTreeOrAssumeDeadContext(ctx, rt, bucketID, alloc.WorkerIP, alloc.Job); err != nil {
			return err
		}
		log.Printf("gc: removed worker job tree worker=%s job=%s path=%s", alloc.WorkerIP, alloc.Job, path)
	}
	return nil
}
