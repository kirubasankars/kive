// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package healthcheck

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"kive/data"
)

const persistLockAttempts = 5

var persistMu sync.Mutex

// persistWith commits fn in a short write transaction on persistDB when set,
// so probes can run without holding kive.db. When persistDB is nil, fn uses tx.
func persistWith(tx *sql.Tx, persistDB *sql.DB, fn func(*sql.Tx) error) error {
	if persistDB != nil {
		persistMu.Lock()
		defer persistMu.Unlock()
		var last error
		for attempt := 0; attempt < persistLockAttempts; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
			}
			last = persistOnce(persistDB, fn)
			if last == nil || !isSQLiteLocked(last) {
				return last
			}
		}
		return last
	}
	if tx == nil {
		return fmt.Errorf("health persist: no database")
	}
	return fn(tx)
}

func persistOnce(persistDB *sql.DB, fn func(*sql.Tx) error) error {
	wtx, err := persistDB.Begin()
	if err != nil {
		return err
	}
	if err := fn(wtx); err != nil {
		_ = wtx.Rollback()
		return err
	}
	return wtx.Commit()
}

func isSQLiteLocked(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
}

func persistJobHealthResults(
	tx *sql.Tx,
	job, runID string,
	rec *statusRecorder,
	jobErr error,
) error {
	if runID == "" {
		return nil
	}
	workers, err := resolveHealthCheckWorkers(tx, job, false)
	if err != nil {
		return err
	}
	if len(workers) == 0 {
		return persistSkippedJob(tx, job, runID, "no allocations")
	}

	hasConfig, err := data.JobHasHealthConfig(tx, job)
	if err != nil {
		return err
	}
	if !hasConfig {
		return persistSkippedJob(tx, job, runID, "no health_check config or commands")
	}

	now := time.Now().UTC()
	rows := make([]data.HealthStatusRow, 0, len(workers))
	failedIPs := failedWorkerIPSet(jobErr)
	failSafeAll := jobErr != nil && len(failedIPs) == 0

	if jobErr == nil {
		if rec != nil {
			rec.finalizeSuccess(workers)
		}
	}

	for _, workerIP := range workers {
		allocID, err := data.GetAllocationID(tx, workerIP, job)
		if err != nil {
			return err
		}

		if jobErr == nil {
			liveness, readiness := data.HealthKindPass, data.HealthKindPass
			if rec != nil {
				if st, ok := rec.workerState(workerIP); ok {
					if st.liveness != "" {
						liveness = st.liveness
					}
					if st.readiness != "" {
						readiness = st.readiness
					}
				}
			}
			rows = append(rows, data.HealthStatusRow{
				Job:          job,
				AllocationID: allocID,
				WorkerIP:     workerIP,
				Status:       data.HealthStatusHealthy,
				Liveness:     liveness,
				Readiness:    readiness,
				CheckedAt:    now,
				RunID:        runID,
			})
			continue
		}

		status := data.HealthStatusHealthy
		liveness := data.HealthKindPass
		readiness := data.HealthKindPass
		detail := ""
		_, failed := failedIPs[workerIP]
		if failed || failSafeAll {
			status = data.HealthStatusUnhealthy
			liveness = data.HealthKindFail
			readiness = data.HealthKindSkip
			detail = allocationFailureDetail(jobErr, workerIP)
		} else if rec != nil {
			if st, ok := rec.workerState(workerIP); ok {
				if st.status == data.HealthStatusUnhealthy {
					status = data.HealthStatusUnhealthy
					liveness = st.liveness
					readiness = st.readiness
					detail = st.detail
				} else if st.liveness == data.HealthKindPass || st.readiness == data.HealthKindPass {
					status = data.HealthStatusHealthy
					if st.liveness != "" {
						liveness = st.liveness
					}
					if st.readiness != "" {
						readiness = st.readiness
					}
				} else {
					continue
				}
			} else {
				continue
			}
		} else {
			continue
		}

		if rec != nil && (failed || failSafeAll) {
			if st, ok := rec.workerState(workerIP); ok {
				if st.liveness != "" {
					liveness = st.liveness
				}
				if st.readiness != "" {
					readiness = st.readiness
				}
				if st.detail != "" {
					detail = st.detail
				}
			}
		}

		rows = append(rows, data.HealthStatusRow{
			Job:          job,
			AllocationID: allocID,
			WorkerIP:     workerIP,
			Status:       status,
			Liveness:     liveness,
			Readiness:    readiness,
			Detail:       detail,
			CheckedAt:    now,
			RunID:        runID,
		})
	}

	if len(rows) == 0 && jobErr != nil {
		rows = append(rows, data.HealthStatusRow{
			Job:          job,
			AllocationID: "",
			WorkerIP:     "",
			Status:       data.HealthStatusUnhealthy,
			Liveness:     data.HealthKindFail,
			Readiness:    data.HealthKindSkip,
			Detail:       strings.TrimSpace(jobErr.Error()),
			CheckedAt:    now,
			RunID:        runID,
		})
	}

	if len(rows) == 0 {
		return nil
	}
	if err := data.UpsertHealthStatus(tx, rows...); err != nil {
		return err
	}
	return data.AppendHealthHistory(tx, rows...)
}

func persistSkippedJob(tx *sql.Tx, job, runID, reason string) error {
	now := time.Now().UTC()
	row := data.HealthStatusRow{
		Job:          job,
		AllocationID: "",
		WorkerIP:     "",
		Status:       data.HealthStatusSkipped,
		Liveness:     data.HealthKindSkip,
		Readiness:    data.HealthKindSkip,
		Detail:       reason,
		CheckedAt:    now,
		RunID:        runID,
	}
	if err := data.UpsertHealthStatus(tx, row); err != nil {
		return err
	}
	return data.AppendHealthHistory(tx, row)
}

func failedWorkerIPSet(err error) map[string]struct{} {
	out := map[string]struct{}{}
	for _, allocErr := range collectAllocationHealthErrors(err) {
		if allocErr != nil && allocErr.WorkerIP != "" {
			out[allocErr.WorkerIP] = struct{}{}
		}
	}
	return out
}

// failedWorkerIPs is kept for execute.go recorder updates.
func failedWorkerIPs(err error) map[string]struct{} {
	return failedWorkerIPSet(err)
}

func allocationFailureDetail(err error, workerIP string) string {
	if err == nil {
		return ""
	}
	for _, allocErr := range collectAllocationHealthErrors(err) {
		if allocErr != nil && allocErr.WorkerIP == workerIP {
			if allocErr.Err != nil {
				return allocErr.Err.Error()
			}
			return allocErr.Error()
		}
	}
	var hcErr *HealthCheckError
	if errors.As(err, &hcErr) {
		return allocationFailureDetail(hcErr.Err, workerIP)
	}
	msg := strings.TrimSpace(err.Error())
	if workerIP != "" && strings.Contains(msg, workerIP) {
		if idx := strings.LastIndex(msg, ": "); idx >= 0 {
			return msg[idx+2:]
		}
	}
	return msg
}
