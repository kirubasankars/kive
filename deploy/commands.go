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
	"kive/rollout"
)

func executePreHooks(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, jobs []string) error {
	for _, job := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := executePreHooksForJob(ctx, tx, rt, job); err != nil {
			return err
		}
	}
	return nil
}

func executePreHooksForJob(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, job string) error {
	commands, err := data.GetHooks(tx, job, "pre_deploy")
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	if len(commands) == 0 {
		return nil
	}

	workerIPs, err := data.GetNonRemovedAllocations(tx, job)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	resolved, err := rollout.ResolveOrder(tx, job, workerIPs)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	batchSize, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	baseCtx := hooks.BatchContext{
		Phase:        "pre_deploy",
		RolloutOrder: resolved.FullOrder,
		OrderSource:  resolved.Source,
	}

	for _, command := range commands {
		if err := hooks.RunHookInBatches(
			ctx, tx, rt, job, command, "pre_deploy", resolved.Ordered, batchSize, baseCtx, true, nil, nil,
		); err != nil {
			return &PreDeployError{Job: job, Err: err}
		}
	}
	return nil
}

func executePostHooks(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, job string) error {
	commands, err := data.GetHooks(tx, job, "post_deploy")
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	if len(commands) == 0 {
		return nil
	}

	workerIPs, err := data.GetNonRemovedAllocations(tx, job)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	resolved, err := rollout.ResolveOrder(tx, job, workerIPs)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	batchSize, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return &JobError{Job: job, Err: err}
	}
	baseCtx := hooks.BatchContext{
		Phase:        "post_deploy",
		RolloutOrder: resolved.FullOrder,
		OrderSource:  resolved.Source,
	}

	for _, command := range commands {
		if err := hooks.RunHookInBatches(
			ctx, tx, rt, job, command, "post_deploy", resolved.Ordered, batchSize, baseCtx, true, nil, nil,
		); err != nil {
			return &PostDeployError{Job: job, Err: err}
		}
	}
	return nil
}
