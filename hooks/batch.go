// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"kive/bucket"
)

type batchConfig struct {
	continueOnFailure bool
	scriptArgs        []string
}

// BatchOption configures RunHookInBatches.
type BatchOption func(*batchConfig)

// WithContinueOnFailure keeps running later batches after a batch fails.
// afterBatch is skipped for failed batches. Used by health_check to finish every allocation.
func WithContinueOnFailure() BatchOption {
	return func(c *batchConfig) {
		c.continueOnFailure = true
	}
}

// WithScriptArgs appends positional arguments to the hook script argv (CLI hooks only).
func WithScriptArgs(args []string) BatchOption {
	return func(c *batchConfig) {
		c.scriptArgs = append([]string(nil), args...)
	}
}

// BatchContext describes one hook batch.
type BatchContext struct {
	Job             string
	Phase           string
	BatchIndex      int
	BatchCount      int
	BatchAllocation []string
	RolloutOrder    string
	OrderSource     string
}

// BatchCount returns the number of batches for total workers and batchSize.
func BatchCount(total, batchSize int) int {
	if total == 0 {
		return 0
	}
	if batchSize < 1 {
		batchSize = total
	}
	return (total + batchSize - 1) / batchSize
}

// SplitWorkerBatches splits ordered worker IPs into fixed-size batches.
func SplitWorkerBatches(orderedIPs []string, batchSize int) [][]string {
	if len(orderedIPs) == 0 {
		return nil
	}
	if batchSize < 1 {
		batchSize = len(orderedIPs)
	}
	totalBatches := BatchCount(len(orderedIPs), batchSize)
	batches := make([][]string, 0, totalBatches)
	for i := 0; i < len(orderedIPs); i += batchSize {
		end := i + batchSize
		if end > len(orderedIPs) {
			end = len(orderedIPs)
		}
		batches = append(batches, append([]string(nil), orderedIPs[i:end]...))
	}
	return batches
}

// BatchEnv returns BATCH_* and rollout metadata for one batch.
func BatchEnv(ctx BatchContext) []string {
	return []string{
		fmt.Sprintf("BATCH_ALLOCATIONS=%s", strings.Join(ctx.BatchAllocation, ",")),
		fmt.Sprintf("BATCH_INDEX=%d", ctx.BatchIndex),
		fmt.Sprintf("BATCH_COUNT=%d", ctx.BatchCount),
		fmt.Sprintf("DEPLOY_PHASE=%s", ctx.Phase),
		fmt.Sprintf("ROLLOUT_ORDER=%s", ctx.RolloutOrder),
		fmt.Sprintf("ROLLOUT_ORDER_SOURCE=%s", ctx.OrderSource),
		fmt.Sprintf("JOB=%s", ctx.Job),
	}
}

// RunHookInBatches runs hookName on orderedIPs in sequential batches.
// afterBatch, when non-nil, runs after each batch succeeds (e.g. promote + health).
// opts may include WithContinueOnFailure or WithScriptArgs (CLI argv passthrough).
func RunHookInBatches(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job, hookName, event string,
	orderedIPs []string,
	batchSize int,
	baseCtx BatchContext,
	verbose bool,
	extraEnv []string,
	afterBatch func(ctx BatchContext) error,
	opts ...BatchOption,
) error {
	cfg := batchConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(orderedIPs) == 0 {
		return nil
	}
	if batchSize < 1 {
		batchSize = 1
	}

	batches := SplitWorkerBatches(orderedIPs, batchSize)
	var errs []error
	for i, batchIPs := range batches {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		batchCtx := baseCtx
		batchCtx.Job = job
		batchCtx.BatchIndex = i
		batchCtx.BatchCount = len(batches)
		batchCtx.BatchAllocation = batchIPs

		logHookBatch(job, hookName, i, len(batches), verbose)
		env := append(append([]string(nil), extraEnv...), BatchEnv(batchCtx)...)
		concurrency := len(batchIPs)
		if concurrency < 1 {
			concurrency = 1
		}
		if err := RunHookOnWorkers(
			ctx, tx, rt, job, hookName, event, batchIPs, concurrency, verbose, env, cfg.scriptArgs,
		); err != nil {
			if !cfg.continueOnFailure {
				return err
			}
			errs = append(errs, err)
			continue
		}
		if afterBatch != nil {
			if err := afterBatch(batchCtx); err != nil {
				if !cfg.continueOnFailure {
					return err
				}
				errs = append(errs, err)
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}
