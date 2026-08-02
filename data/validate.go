// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"kive/bucket"
	"kive/worker"
)

// WorkerSyncError reports that a worker's worker.json does not match kive.db.
type WorkerSyncError struct {
	WorkerIP string
	Reason   string
}

func (e *WorkerSyncError) Error() string {
	return fmt.Sprintf("worker %s: %s (run kive deploy)", e.WorkerIP, e.Reason)
}

// ValidateBucketGeneration verifies each worker's worker.json matches bucket_id,
// worker_id, and generation in kive.db before manual job control.
func ValidateBucketGeneration(tx *sql.Tx, rt *bucket.Runtime, workers []string) error {
	return ValidateBucketGenerationContext(context.Background(), tx, rt, workers)
}

// ValidateBucketGenerationContext validates worker state with cancellation.
func ValidateBucketGenerationContext(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, workers []string) error {
	bucketID, err := GetBucketID(tx)
	if err != nil {
		return err
	}

	generation, err := GetBucketGeneration(tx)
	if err != nil {
		return err
	}

	var (
		errs = make(map[string]error)
		mu   sync.Mutex
		wg   sync.WaitGroup
	)

	for _, workerIP := range workers {
		if err := ctx.Err(); err != nil {
			wg.Wait()
			return err
		}
		workerID, err := GetWorkerID(tx, workerIP)
		if err != nil {
			return err
		}

		wg.Add(1)
		go func(tWorkerID, tWorkerIP string) {
			defer wg.Done()
			cmd := fmt.Sprintf(
				"python3 %s/bin/worker.py %s %s %d",
				bucket.WorkerBucketPath(bucketID), bucketID, tWorkerID, generation,
			)
			err := worker.ExecuteCommand(ctx, rt, tWorkerIP, bucket.CommandContext{
				Phase:  "validate",
				Action: "worker_sync",
				Cmd:    cmd,
			}, []string{cmd}, nil)
			if err != nil {
				mu.Lock()
				errs[tWorkerIP] = err
				mu.Unlock()
			}
		}(workerID, workerIP)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	var syncErrors []error
	for workerIP, err := range errs {
		if syncErr := workerSyncError(workerIP, err); syncErr != nil {
			syncErrors = append(syncErrors, syncErr)
		}
	}
	return errors.Join(syncErrors...)
}

func workerSyncError(workerIP string, err error) error {
	if code, ok := remoteExitCode(err); ok {
		switch code {
		case 1:
			return &WorkerSyncError{WorkerIP: workerIP, Reason: "bucket id mismatch"}
		case 2:
			return &WorkerSyncError{WorkerIP: workerIP, Reason: "worker id mismatch"}
		case 3:
			return &WorkerSyncError{WorkerIP: workerIP, Reason: "generation mismatch"}
		}
	}
	var remote *worker.RemoteCommandError
	if errors.As(err, &remote) {
		return err
	}
	return fmt.Errorf("worker %s: %w", workerIP, err)
}

func remoteExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	var coder interface{ ExitCode() int }
	if errors.As(err, &coder) {
		return coder.ExitCode(), true
	}
	return 0, false
}
