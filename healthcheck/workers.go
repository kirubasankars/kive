// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package healthcheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"kive/bucket"
	"kive/data"
)

type workerDialResult struct {
	row  data.WorkerHealthStatusRow
	fail error
}

// CheckWorkers verifies SSH port reachability on every worker in the catalog.
// Every worker is dialed in parallel even when some fail. Results are persisted
// when runID is set. Catalog reads stay on tx; dials do not hold it.
func CheckWorkers(ctx context.Context, tx *sql.Tx, runID string, persistDB *sql.DB) error {
	if ctx == nil {
		ctx = context.Background()
	}

	workers, sshPort, err := catalogWorkers(tx)
	if err != nil {
		return err
	}
	if len(workers) == 0 {
		fmt.Println("worker health check skipped: no workers")
		return nil
	}

	rows, workerErr := dialWorkers(ctx, workers, sshPort, runID)
	if err := persistWorkerRows(tx, persistDB, runID, rows); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return workerErr
}

func catalogWorkers(tx *sql.Tx) ([]string, int, error) {
	workers, err := data.GetWorkers(tx, nil)
	if err != nil {
		return nil, 0, err
	}
	if len(workers) == 0 {
		return nil, 0, nil
	}
	sshPort, err := bucket.SSHPort()
	if err != nil {
		return nil, 0, err
	}
	return workers, sshPort, nil
}

func dialWorkers(ctx context.Context, workers []string, sshPort int, runID string) ([]data.WorkerHealthStatusRow, error) {
	if len(workers) == 0 {
		return nil, nil
	}
	if testHooks != nil && testHooks.HoldWorkerDials != nil {
		testHooks.HoldWorkerDials()
	}
	now := time.Now().UTC()
	results := make([]workerDialResult, len(workers))
	var wg sync.WaitGroup
	for i, workerIP := range workers {
		wg.Add(1)
		go func(i int, workerIP string) {
			defer wg.Done()
			row := data.WorkerHealthStatusRow{
				WorkerIP:  workerIP,
				Status:    data.HealthStatusHealthy,
				CheckedAt: now,
				RunID:     runID,
			}
			if err := probeTCP(ctx, workerIP, sshPort, defaultProbeTimeout); err != nil {
				probeErr := fmt.Errorf("worker %s ssh port %d: %w", workerIP, sshPort, err)
				fmt.Printf("worker health check failed: %v\n", probeErr)
				row.Status = data.HealthStatusUnhealthy
				row.Detail = err.Error()
				results[i] = workerDialResult{row: row, fail: probeErr}
				return
			}
			results[i] = workerDialResult{row: row}
		}(i, workerIP)
	}
	wg.Wait()

	rows := make([]data.WorkerHealthStatusRow, 0, len(results))
	var fails []error
	for _, item := range results {
		if item.row.WorkerIP == "" {
			continue
		}
		rows = append(rows, item.row)
		if item.fail != nil {
			fails = append(fails, item.fail)
		}
	}
	if len(fails) == 0 {
		fmt.Println("worker health check passed")
		return rows, nil
	}
	return rows, &WorkerHealthCheckError{Err: errors.Join(fails...)}
}

func persistWorkerRows(tx *sql.Tx, persistDB *sql.DB, runID string, rows []data.WorkerHealthStatusRow) error {
	if runID == "" || len(rows) == 0 {
		return nil
	}
	return persistWith(tx, persistDB, func(wtx *sql.Tx) error {
		return persistWorkerHealthResults(wtx, rows)
	})
}

func persistWorkerHealthResults(tx *sql.Tx, rows []data.WorkerHealthStatusRow) error {
	if err := data.UpsertWorkerHealthStatus(tx, rows...); err != nil {
		return err
	}
	return data.AppendWorkerHealthHistory(tx, rows...)
}
