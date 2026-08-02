// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
)

const (
	deployPhaseNew    = "new"
	deployPhaseUpdate = "update"
	deployPhaseStop   = "stop"
)

func executeAllocationEventHooks(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job, event string,
	workerIPs []string,
	batchCtx hooks.BatchContext,
) error {
	if len(workerIPs) == 0 {
		return nil
	}
	commands, err := data.GetHooks(tx, job, event)
	if err != nil {
		return err
	}
	if len(commands) == 0 {
		return nil
	}

	batchCtx.Job = job
	extraEnv := hooks.BatchEnv(batchCtx)
	concurrency := len(workerIPs)
	if concurrency < 1 {
		concurrency = 1
	}

	for _, command := range commands {
		if err := hooks.RunHookOnWorkers(
			ctx, tx, rt, job, command, event, workerIPs, concurrency, true, extraEnv, nil,
		); err != nil {
			return err
		}
	}
	return nil
}

func executeAfterAllocationStarted(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	workerIPs []string,
	batchCtx hooks.BatchContext,
) error {
	return executeAllocationEventHooks(ctx, tx, rt, job, "after_allocation_started", workerIPs, batchCtx)
}

func executeAfterAllocationRestarted(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	workerIPs []string,
	batchCtx hooks.BatchContext,
) error {
	return executeAllocationEventHooks(ctx, tx, rt, job, "after_allocation_restarted", workerIPs, batchCtx)
}

func executeAfterAllocationStopped(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	workerIPs []string,
	batchCtx hooks.BatchContext,
) error {
	return executeAllocationEventHooks(ctx, tx, rt, job, "after_allocation_stopped", workerIPs, batchCtx)
}
