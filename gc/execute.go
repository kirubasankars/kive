// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package gc removes stale database rows after builds and manual cleanup.
package gc

import (
	"context"
	"log"

	"kive/bucket"
	"kive/data"
	"kive/kv"
	"kive/logs"
)

// Options configures a GC run.
type Options struct {
	RetainDays         int
	SourceRevisionPins []string
}

// Execute purges removed allocations and stale key_value history.
// retainDays controls how long deleted KV rows are kept (0 = purge eligible rows immediately).
func Execute(retainDays int) error {
	return ExecuteOptions(context.Background(), Options{RetainDays: retainDays})
}

// ExecuteContext purges stale state and cancels active worker cleanup.
func ExecuteContext(ctx context.Context, retainDays int) error {
	return ExecuteOptions(ctx, Options{RetainDays: retainDays})
}

// ExecuteOptions runs GC with full options (including optional source-revision pins from server peers).
func ExecuteOptions(ctx context.Context, opts Options) error {
	retainDays := opts.RetainDays
	if err := ctx.Err(); err != nil {
		return err
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

	removedAllocs, err := listRemovedAllocations(tx)
	if err != nil {
		return err
	}

	log.Printf("gc: starting retain_days=%d removed_allocations=%d", retainDays, len(removedAllocs))

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	rt, err := bucket.SetupRuntime(bucketID, bucket.NewRunContext("gc", 0))
	if err != nil {
		return err
	}
	defer func() {
		_ = rt.Stop()
	}()

	if removed, err := logs.PruneRunLogsFromConfig(); err != nil {
		log.Printf("gc: prune run logs: %v", err)
	} else {
		log.Printf("gc: pruned run logs removed=%d", removed)
	}

	if len(removedAllocs) > 0 {
		if err := purgeRemovedAllocationWorkerData(ctx, rt, tx, bucketID, removedAllocs); err != nil {
			return err
		}
		log.Printf("gc: worker job trees cleaned allocations=%d", len(removedAllocs))
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := kv.RequireStore()
	if err != nil {
		return err
	}

	if len(removedAllocs) > 0 {
		if err := purgeRemovedAllocationReferences(tx, store, removedAllocs); err != nil {
			return err
		}
		log.Printf("gc: purged allocation references allocations=%d", len(removedAllocs))
	}

	if err := purgeRemovedAllocations(tx); err != nil {
		return err
	}
	log.Printf("gc: deleted removed allocations")

	if err := store.PurgeStaleVersions(tx, retainDays); err != nil {
		return err
	}
	log.Printf("gc: purged stale kv versions retain_days=%d", retainDays)

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := pruneHistoryForBucket(tx, HistoryOptions{SourceRevisionPins: opts.SourceRevisionPins}); err != nil {
		log.Printf("gc: history: %v", err)
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	log.Printf("gc: completed")
	return nil
}

// Collect runs GC using kv_retain_days from workspace/bucket.conf (default 7).
func Collect() error {
	retainDays, err := bucket.KVRetainDaysFromConfig()
	if err != nil {
		return err
	}
	return Execute(retainDays)
}
