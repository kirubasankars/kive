// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"sync"
)

// runParallelWorkers runs fn for each workerIP with at most parallelism concurrent calls.
// fn must not use a shared *sql.Tx; database access from callbacks must be synchronized externally.
func runParallelWorkers(ctx context.Context, workerIPs []string, parallelism int, fn func(workerIP string) error) error {
	if len(workerIPs) == 0 {
		return nil
	}
	if parallelism < 1 {
		parallelism = 1
	}
	if parallelism > len(workerIPs) {
		parallelism = len(workerIPs)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
		sem  = make(chan struct{}, parallelism)
	)

	for _, workerIP := range workerIPs {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		select {
		case <-ctx.Done():
			wg.Done()
			continue
		case sem <- struct{}{}:
		}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := fn(ip); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(workerIP)
	}

	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return joinErrors("", errs)
}

// runWorkerBatches processes workers in contiguous batches of batchSize (parallel within each batch).
func runWorkerBatches(ctx context.Context, workerIPs []string, batchSize int, fn func(workerIP string) error) error {
	if batchSize < 1 {
		batchSize = 1
	}
	for i := 0; i < len(workerIPs); i += batchSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		end := i + batchSize
		if end > len(workerIPs) {
			end = len(workerIPs)
		}
		if err := runParallelWorkers(ctx, workerIPs[i:end], end-i, fn); err != nil {
			return err
		}
	}
	return nil
}
