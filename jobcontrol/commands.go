// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package jobcontrol

import (
	"context"
	"database/sql"
	"fmt"

	"kive/bucket"
	"kive/data"
	"kive/healthcheck"
	"kive/hooks"
	"kive/rollout"
)

const jobControlEvent = "job_control"

// runJobControlHooks executes manifest job_control commands when registered; otherwise false.
func runJobControlHooks(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	target Target,
	healthCheck bool,
	selected []string,
) (handled bool, err error) {
	commands, err := data.GetHooks(tx, job, jobControlEvent)
	if err != nil {
		return false, &JobRunError{Job: job, Target: target, Err: err}
	}
	if len(commands) == 0 {
		return false, nil
	}

	if len(selected) == 0 {
		return true, nil
	}

	resolved, err := rollout.ResolveOrder(tx, job, selected)
	if err != nil {
		return false, &JobRunError{Job: job, Target: target, Err: err}
	}
	batchSize, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return false, &JobRunError{Job: job, Target: target, Err: err}
	}

	extraEnv := []string{fmt.Sprintf("TARGET=%s", target)}
	baseCtx := hooks.BatchContext{
		Phase:        jobControlEvent,
		RolloutOrder: resolved.FullOrder,
		OrderSource:  resolved.Source,
	}

	for _, command := range commands {
		if err := ctx.Err(); err != nil {
			return true, &JobRunError{Job: job, Target: target, Err: err}
		}
		if err := hooks.RunHookInBatches(
			ctx, tx, rt, job, command, jobControlEvent,
			resolved.Ordered, batchSize, baseCtx, true, extraEnv, nil,
		); err != nil {
			return true, &JobRunError{Job: job, Target: target, Err: err}
		}
	}

	if healthCheck {
		if err := healthcheck.HealthCheckWithOptions(ctx, tx, rt, true, job, true, healthcheck.CheckOptions{}); err != nil {
			return true, &JobRunError{Job: job, Target: target, Err: err}
		}
	}

	return true, nil
}
