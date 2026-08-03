// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package jobcontrol

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"kive/data"
)

func validateActiveWorkerFilter(tx *sql.Tx, job string, workerFilter []string) error {
	if len(workerFilter) == 0 {
		return nil
	}
	for _, workerIP := range workerFilter {
		removed, err := data.IsAllocationRemoved(tx, workerIP, job)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &InvalidFilterError{Kind: "worker", Values: []string{workerIP}}
			}
			return err
		}
		if removed == 1 {
			return &RemovedAllocationError{Job: job, WorkerIP: workerIP}
		}
		disabled, err := data.IsAllocationDisabled(tx, workerIP, job)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &InvalidFilterError{Kind: "worker", Values: []string{workerIP}}
			}
			return err
		}
		if disabled == 1 {
			return &DisabledAllocationError{Job: job, WorkerIP: workerIP}
		}
	}
	return nil
}

// resolveControlWorkers returns active worker IPs from kive.db for job control
// (removed=0, disabled=0), optionally filtered by --allocations. Disabled or
// removed explicit targets error (never silently no-op).
func resolveControlWorkers(tx *sql.Tx, job string, workerFilter []string) ([]string, error) {
	if err := validateActiveWorkerFilter(tx, job, workerFilter); err != nil {
		return nil, err
	}
	activeWorkers, err := data.GetActiveAllocations(tx, job)
	if err != nil {
		return nil, err
	}
	return filterWorkers(activeWorkers, workerFilter), nil
}

// errIfNoControlTargets reports when a job has non-removed allocations but none are active
// (fully disabled via disabled.conf jobs/workers entries).
func errIfNoControlTargets(tx *sql.Tx, job string, workerFilter []string, selected []string) error {
	if len(selected) > 0 || len(workerFilter) > 0 {
		return nil
	}
	hasNonRemoved, err := data.JobHasNonRemovedAllocations(tx, job)
	if err != nil {
		return err
	}
	if !hasNonRemoved {
		return nil
	}
	hasActive, err := data.JobHasActiveAllocations(tx, job)
	if err != nil {
		return err
	}
	if !hasActive {
		return &DisabledJobError{Job: job}
	}
	return nil
}

// forgetDeployedState clears applied_hash so deploy treats allocations as start-pending
// and excludes them from pre-batch DeployHealthCheck.
func forgetDeployedState(tx *sql.Tx, job string, workerIPs []string) error {
	if len(workerIPs) == 0 {
		return nil
	}

	var failed []string
	forgotten := make([]string, 0, len(workerIPs))
	for _, workerIP := range workerIPs {
		allocID, err := data.GetAllocationID(tx, workerIP, job)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", workerIP, err))
			continue
		}
		if err := data.MarkAllocationStartPending(tx, allocID); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", workerIP, err))
			continue
		}
		forgotten = append(forgotten, workerIP)
	}

	if len(forgotten) > 0 {
		log.Printf("jobcontrol: forgot deploy state for job %q on %s", job, strings.Join(forgotten, ", "))
	}
	if len(failed) > 0 {
		return fmt.Errorf("%s", strings.Join(failed, "; "))
	}
	return nil
}
