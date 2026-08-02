// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"strings"
	"time"

	"kive/bucket"
	"kive/workspace"
)

const (
	HealthStatusHealthy   = "healthy"
	HealthStatusUnhealthy = "unhealthy"
	HealthStatusSkipped   = "skipped"

	HealthKindPass    = "pass"
	HealthKindFail    = "fail"
	HealthKindSkip    = "skip"
	HealthColorGray   = "gray"
	HealthColorGreen  = "green"
	HealthColorYellow = "yellow"
	HealthColorRed    = "red"
)

// HealthStatusRow is the latest health snapshot for one allocation.
type HealthStatusRow struct {
	Job          string
	AllocationID string
	WorkerIP     string
	Status       string
	Liveness     string
	Readiness    string
	Detail       string
	CheckedAt    time.Time
	RunID        string
}

// HealthHistoryRow is one append-only health check result.
type HealthHistoryRow struct {
	ID           int64
	RunID        string
	Job          string
	AllocationID string
	WorkerIP     string
	Status       string
	Liveness     string
	Readiness    string
	Detail       string
	CheckedAt    time.Time
}

// UpsertHealthStatus stores the latest per-allocation health snapshot.
func UpsertHealthStatus(tx *sql.Tx, rows ...HealthStatusRow) error {
	for _, row := range rows {
		_, err := tx.Exec(`
			INSERT INTO health_status (
				job, allocation_id, worker_ip, status, liveness, readiness, detail, checked_at, run_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(job, allocation_id) DO UPDATE SET
				worker_ip = excluded.worker_ip,
				status = excluded.status,
				liveness = excluded.liveness,
				readiness = excluded.readiness,
				detail = excluded.detail,
				checked_at = excluded.checked_at,
				run_id = excluded.run_id`,
			row.Job,
			row.AllocationID,
			row.WorkerIP,
			row.Status,
			nullIfEmpty(row.Liveness),
			nullIfEmpty(row.Readiness),
			nullIfEmpty(row.Detail),
			formatTimeUTC(row.CheckedAt),
			nullIfEmpty(row.RunID),
		)
		if err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

// AppendHealthHistory appends health check results to the history table.
func AppendHealthHistory(tx *sql.Tx, rows ...HealthStatusRow) error {
	for _, row := range rows {
		_, err := tx.Exec(`
			INSERT INTO health_status_history (
				run_id, job, allocation_id, worker_ip, status, liveness, readiness, detail, checked_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.RunID,
			row.Job,
			row.AllocationID,
			row.WorkerIP,
			row.Status,
			nullIfEmpty(row.Liveness),
			nullIfEmpty(row.Readiness),
			nullIfEmpty(row.Detail),
			formatTimeUTC(row.CheckedAt),
		)
		if err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

// ListHealthStatus returns all persisted health snapshot rows.
func ListHealthStatus(tx *sql.Tx) ([]HealthStatusRow, error) {
	rows, err := tx.Query(`
		SELECT job, allocation_id, worker_ip, status,
			COALESCE(liveness, ''), COALESCE(readiness, ''), COALESCE(detail, ''),
			checked_at, COALESCE(run_id, '')
		FROM health_status
		ORDER BY job, worker_ip`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]HealthStatusRow, 0)
	for rows.Next() {
		var row HealthStatusRow
		var checkedAt string
		if err := rows.Scan(
			&row.Job, &row.AllocationID, &row.WorkerIP, &row.Status,
			&row.Liveness, &row.Readiness, &row.Detail, &checkedAt, &row.RunID,
		); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		row.CheckedAt = parseTimeUTC(checkedAt)
		out = append(out, row)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// ListHealthStatusForJob returns snapshot rows for one job.
func ListHealthStatusForJob(tx *sql.Tx, job string) ([]HealthStatusRow, error) {
	rows, err := tx.Query(`
		SELECT job, allocation_id, worker_ip, status,
			COALESCE(liveness, ''), COALESCE(readiness, ''), COALESCE(detail, ''),
			checked_at, COALESCE(run_id, '')
		FROM health_status
		WHERE job = ?
		ORDER BY worker_ip`, job)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]HealthStatusRow, 0)
	for rows.Next() {
		var row HealthStatusRow
		var checkedAt string
		if err := rows.Scan(
			&row.Job, &row.AllocationID, &row.WorkerIP, &row.Status,
			&row.Liveness, &row.Readiness, &row.Detail, &checkedAt, &row.RunID,
		); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		row.CheckedAt = parseTimeUTC(checkedAt)
		out = append(out, row)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// ListHealthHistory returns recent history rows for a job grouped by run.
func ListHealthHistory(tx *sql.Tx, job string, limit int) ([]HealthHistoryRow, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := tx.Query(`
		SELECT id, run_id, job, allocation_id, worker_ip, status,
			COALESCE(liveness, ''), COALESCE(readiness, ''), COALESCE(detail, ''), checked_at
		FROM health_status_history
		WHERE job = ?
		ORDER BY checked_at DESC, id DESC
		LIMIT ?`, job, limit*16)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]HealthHistoryRow, 0)
	for rows.Next() {
		var row HealthHistoryRow
		var checkedAt string
		if err := rows.Scan(
			&row.ID, &row.RunID, &row.Job, &row.AllocationID, &row.WorkerIP, &row.Status,
			&row.Liveness, &row.Readiness, &row.Detail, &checkedAt,
		); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		row.CheckedAt = parseTimeUTC(checkedAt)
		out = append(out, row)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// HealthDayBucket is one job-day aggregate of check outcomes.
type HealthDayBucket struct {
	Job       string
	Day       string // YYYY-MM-DD UTC
	Healthy   int
	Unhealthy int
}

// ListHealthDailyUptime aggregates healthy/unhealthy checks per job per UTC day
// for history at or after since. Skipped checks are excluded so they neither
// help nor hurt uptime.
func ListHealthDailyUptime(tx *sql.Tx, since time.Time) ([]HealthDayBucket, error) {
	rows, err := tx.Query(`
		SELECT job, substr(checked_at, 1, 10) AS day,
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END)
		FROM health_status_history
		WHERE checked_at >= ? AND status IN (?, ?)
		GROUP BY job, day
		ORDER BY job, day`,
		HealthStatusHealthy, HealthStatusUnhealthy,
		formatTimeBoundUTC(since),
		HealthStatusHealthy, HealthStatusUnhealthy,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]HealthDayBucket, 0)
	for rows.Next() {
		var bucketRow HealthDayBucket
		if err := rows.Scan(&bucketRow.Job, &bucketRow.Day, &bucketRow.Healthy, &bucketRow.Unhealthy); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		out = append(out, bucketRow)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// HealthHourBucket is one job-hour aggregate of check outcomes.
type HealthHourBucket struct {
	Job       string
	Hour      string // YYYY-MM-DDTHH UTC
	Healthy   int
	Unhealthy int
}

// ListHealthHourlyUptime aggregates healthy/unhealthy checks per job per UTC
// hour for history at or after since. Skipped checks are excluded so they
// neither help nor hurt uptime.
func ListHealthHourlyUptime(tx *sql.Tx, since time.Time) ([]HealthHourBucket, error) {
	rows, err := tx.Query(`
		SELECT job, substr(checked_at, 1, 13) AS hour,
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END)
		FROM health_status_history
		WHERE checked_at >= ? AND status IN (?, ?)
		GROUP BY job, hour
		ORDER BY job, hour`,
		HealthStatusHealthy, HealthStatusUnhealthy,
		formatTimeBoundUTC(since),
		HealthStatusHealthy, HealthStatusUnhealthy,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]HealthHourBucket, 0)
	for rows.Next() {
		var bucketRow HealthHourBucket
		if err := rows.Scan(&bucketRow.Job, &bucketRow.Hour, &bucketRow.Healthy, &bucketRow.Unhealthy); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		out = append(out, bucketRow)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// HealthUptimeCounts totals check outcomes across all jobs in a window.
type HealthUptimeCounts struct {
	Healthy   int
	Unhealthy int
}

// Total returns the number of checks that count toward uptime.
func (c HealthUptimeCounts) Total() int {
	return c.Healthy + c.Unhealthy
}

// Ratio returns the healthy fraction (0..1) and whether any checks exist.
func (c HealthUptimeCounts) Ratio() (float64, bool) {
	total := c.Total()
	if total == 0 {
		return 0, false
	}
	return float64(c.Healthy) / float64(total), true
}

// HealthUptimeSince totals healthy/unhealthy checks across all jobs for history
// at or after since. Skipped checks are excluded.
func HealthUptimeSince(tx *sql.Tx, since time.Time) (HealthUptimeCounts, error) {
	var counts HealthUptimeCounts
	err := tx.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
		FROM health_status_history
		WHERE checked_at >= ? AND status IN (?, ?)`,
		HealthStatusHealthy, HealthStatusUnhealthy,
		formatTimeBoundUTC(since),
		HealthStatusHealthy, HealthStatusUnhealthy,
	).Scan(&counts.Healthy, &counts.Unhealthy)
	if err != nil {
		return HealthUptimeCounts{}, bucket.DatabaseError(err)
	}
	return counts, nil
}

// DeployedAllocationTarget is one deployed active allocation eligible for health checks.
type DeployedAllocationTarget struct {
	Job          string
	AllocationID string
	WorkerIP     string
}

// ListDeployedHealthTargets returns deployed active allocations for jobs with health config.
func ListDeployedHealthTargets(tx *sql.Tx) ([]DeployedAllocationTarget, error) {
	jobs, err := ListJobsWithHealthConfig(tx)
	if err != nil {
		return nil, err
	}
	out := make([]DeployedAllocationTarget, 0)
	for _, job := range jobs {
		workers, err := GetHealthCheckActiveAllocations(tx, job)
		if err != nil {
			return nil, err
		}
		for _, workerIP := range workers {
			allocID, err := GetAllocationID(tx, workerIP, job)
			if err != nil {
				return nil, err
			}
			out = append(out, DeployedAllocationTarget{
				Job:          job,
				AllocationID: allocID,
				WorkerIP:     workerIP,
			})
		}
	}
	return out, nil
}

// ListJobsWithHealthConfig returns deployed jobs that define probes or health hooks.
func ListJobsWithHealthConfig(tx *sql.Tx) ([]string, error) {
	deployed, err := GetDeployedJobs(tx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(deployed))
	for _, job := range deployed {
		ok, err := JobHasHealthConfig(tx, job)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, job)
		}
	}
	return out, nil
}

// JobHasHealthConfig reports whether a job defines manifest probes or health hooks.
func JobHasHealthConfig(tx *sql.Tx, job string) (bool, error) {
	spec, err := GetJobHealthCheck(tx, job)
	if err != nil {
		return false, err
	}
	if spec != nil && (spec.HasLivenessProbes() || spec.HasReadinessProbes()) {
		return true, nil
	}
	livenessHooks, err := GetHooks(tx, job, workspace.HealthKindLiveness)
	if err != nil {
		return false, err
	}
	if len(livenessHooks) > 0 {
		return true, nil
	}
	readinessHooks, err := GetHooks(tx, job, workspace.HealthKindReadiness)
	if err != nil {
		return false, err
	}
	return len(readinessHooks) > 0, nil
}

// JobHealthRollup summarizes allocation health for one job.
type JobHealthRollup struct {
	Color                string
	Reason               string
	AllocationsTotal     int
	AllocationsHealthy   int
	AllocationsUnhealthy int
	LastCheckedAt        time.Time
	RunID                string
}

// RollupJobHealthColor computes gray/green/yellow/red from allocation snapshots.
func RollupJobHealthColor(total, healthy, unhealthy int) string {
	if total == 0 || (healthy == 0 && unhealthy == 0) {
		return HealthColorGray
	}
	if unhealthy == 0 && healthy > 0 {
		return HealthColorGreen
	}
	if healthy == 0 && unhealthy > 0 {
		return HealthColorRed
	}
	if healthy > 0 && unhealthy > 0 {
		return HealthColorYellow
	}
	return HealthColorGray
}

// SummarizeJobHealth builds rollup counts for one job against deployed targets.
func SummarizeJobHealth(targets []DeployedAllocationTarget, snapshots []HealthStatusRow) JobHealthRollup {
	rollup := JobHealthRollup{Color: HealthColorGray, Reason: "never_checked"}
	if len(targets) == 0 {
		rollup.Reason = "no_allocations"
		return rollup
	}
	rollup.AllocationsTotal = len(targets)
	byAlloc := map[string]HealthStatusRow{}
	for _, snap := range snapshots {
		byAlloc[snap.AllocationID] = snap
	}
	var latest time.Time
	for _, target := range targets {
		snap, ok := byAlloc[target.AllocationID]
		if !ok {
			continue
		}
		switch snap.Status {
		case HealthStatusHealthy:
			rollup.AllocationsHealthy++
		case HealthStatusUnhealthy:
			rollup.AllocationsUnhealthy++
		}
		if snap.CheckedAt.After(latest) {
			latest = snap.CheckedAt
			rollup.RunID = snap.RunID
		}
	}
	rollup.Color = RollupJobHealthColor(rollup.AllocationsTotal, rollup.AllocationsHealthy, rollup.AllocationsUnhealthy)
	if !latest.IsZero() {
		rollup.LastCheckedAt = latest
		rollup.Reason = "last_check"
	}
	return rollup
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func formatTimeUTC(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339)
}

// formatTimeBoundUTC formats a query range bound. Unlike formatTimeUTC, a zero
// time stays zero so it acts as "match everything" in a checked_at >= compare.
func formatTimeBoundUTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTimeUTC(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
