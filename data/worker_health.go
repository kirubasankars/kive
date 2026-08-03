// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"time"

	"kive/bucket"
)

// WorkerHealthStatusRow is the latest SSH health snapshot for one worker.
type WorkerHealthStatusRow struct {
	WorkerIP  string
	Status    string
	Detail    string
	CheckedAt time.Time
	RunID     string
}

// WorkerHealthHistoryRow is one append-only worker SSH check result.
type WorkerHealthHistoryRow struct {
	ID        int64
	RunID     string
	WorkerIP  string
	Status    string
	Detail    string
	CheckedAt time.Time
}

// WorkerHealthDayBucket is one worker-day aggregate of SSH check outcomes.
type WorkerHealthDayBucket struct {
	WorkerIP  string
	Day       string
	Healthy   int
	Unhealthy int
}

// WorkerHealthHourBucket is one worker-hour aggregate of SSH check outcomes.
type WorkerHealthHourBucket struct {
	WorkerIP  string
	Hour      string
	Healthy   int
	Unhealthy int
}

// UpsertWorkerHealthStatus stores the latest per-worker SSH snapshot.
func UpsertWorkerHealthStatus(tx *sql.Tx, rows ...WorkerHealthStatusRow) error {
	for _, row := range rows {
		_, err := tx.Exec(`
			INSERT INTO worker_health_status (
				worker_ip, status, detail, checked_at, run_id
			) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(worker_ip) DO UPDATE SET
				status = excluded.status,
				detail = excluded.detail,
				checked_at = excluded.checked_at,
				run_id = excluded.run_id`,
			row.WorkerIP,
			row.Status,
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

// AppendWorkerHealthHistory appends worker SSH results to the history table.
func AppendWorkerHealthHistory(tx *sql.Tx, rows ...WorkerHealthStatusRow) error {
	for _, row := range rows {
		_, err := tx.Exec(`
			INSERT INTO worker_health_status_history (
				run_id, worker_ip, status, detail, checked_at
			) VALUES (?, ?, ?, ?, ?)`,
			row.RunID,
			row.WorkerIP,
			row.Status,
			nullIfEmpty(row.Detail),
			formatTimeUTC(row.CheckedAt),
		)
		if err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

// ListWorkerHealthStatus returns all persisted worker SSH snapshots.
func ListWorkerHealthStatus(tx *sql.Tx) ([]WorkerHealthStatusRow, error) {
	rows, err := tx.Query(`
		SELECT worker_ip, status, COALESCE(detail, ''), checked_at, COALESCE(run_id, '')
		FROM worker_health_status
		ORDER BY worker_ip`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]WorkerHealthStatusRow, 0)
	for rows.Next() {
		var row WorkerHealthStatusRow
		var checkedAt string
		if err := rows.Scan(&row.WorkerIP, &row.Status, &row.Detail, &checkedAt, &row.RunID); err != nil {
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

// ListWorkerHealthStatusForIP returns the snapshot for one worker, or ok=false.
func ListWorkerHealthStatusForIP(tx *sql.Tx, workerIP string) (WorkerHealthStatusRow, bool, error) {
	var row WorkerHealthStatusRow
	var checkedAt string
	err := tx.QueryRow(`
		SELECT worker_ip, status, COALESCE(detail, ''), checked_at, COALESCE(run_id, '')
		FROM worker_health_status
		WHERE worker_ip = ?`, workerIP).Scan(&row.WorkerIP, &row.Status, &row.Detail, &checkedAt, &row.RunID)
	if err == sql.ErrNoRows {
		return WorkerHealthStatusRow{}, false, nil
	}
	if err != nil {
		return WorkerHealthStatusRow{}, false, bucket.DatabaseError(err)
	}
	row.CheckedAt = parseTimeUTC(checkedAt)
	return row, true, nil
}

// ListWorkerHealthHistoryAll returns recent SSH history across all workers.
func ListWorkerHealthHistoryAll(tx *sql.Tx, limit int) ([]WorkerHealthHistoryRow, error) {
	return listWorkerHealthHistory(tx, "", limit)
}

// ListWorkerHealthHistory returns recent SSH history rows for one worker.
func ListWorkerHealthHistory(tx *sql.Tx, workerIP string, limit int) ([]WorkerHealthHistoryRow, error) {
	return listWorkerHealthHistory(tx, workerIP, limit)
}

func listWorkerHealthHistory(tx *sql.Tx, workerIP string, limit int) ([]WorkerHealthHistoryRow, error) {
	if limit < 1 {
		limit = 20
	}
	query := `
		SELECT id, run_id, worker_ip, status, COALESCE(detail, ''), checked_at
		FROM worker_health_status_history`
	args := []any{}
	if workerIP != "" {
		query += `
		WHERE worker_ip = ?`
		args = append(args, workerIP)
	}
	query += `
		ORDER BY checked_at DESC, id DESC
		LIMIT ?`
	args = append(args, limit)
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]WorkerHealthHistoryRow, 0)
	for rows.Next() {
		var row WorkerHealthHistoryRow
		var checkedAt string
		if err := rows.Scan(&row.ID, &row.RunID, &row.WorkerIP, &row.Status, &row.Detail, &checkedAt); err != nil {
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

// ListWorkerHealthDailyUptime aggregates healthy/unhealthy SSH checks per worker
// per UTC day for history at or after since.
func ListWorkerHealthDailyUptime(tx *sql.Tx, since time.Time) ([]WorkerHealthDayBucket, error) {
	rows, err := tx.Query(`
		SELECT worker_ip, substr(checked_at, 1, 10) AS day,
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END)
		FROM worker_health_status_history
		WHERE checked_at >= ? AND status IN (?, ?)
		GROUP BY worker_ip, day
		ORDER BY worker_ip, day`,
		HealthStatusHealthy, HealthStatusUnhealthy,
		formatTimeBoundUTC(since),
		HealthStatusHealthy, HealthStatusUnhealthy,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]WorkerHealthDayBucket, 0)
	for rows.Next() {
		var bucketRow WorkerHealthDayBucket
		if err := rows.Scan(&bucketRow.WorkerIP, &bucketRow.Day, &bucketRow.Healthy, &bucketRow.Unhealthy); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		out = append(out, bucketRow)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// ListWorkerHealthHourlyUptime aggregates healthy/unhealthy SSH checks per
// worker per UTC hour for history at or after since.
func ListWorkerHealthHourlyUptime(tx *sql.Tx, since time.Time) ([]WorkerHealthHourBucket, error) {
	rows, err := tx.Query(`
		SELECT worker_ip, substr(checked_at, 1, 13) AS hour,
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END)
		FROM worker_health_status_history
		WHERE checked_at >= ? AND status IN (?, ?)
		GROUP BY worker_ip, hour
		ORDER BY worker_ip, hour`,
		HealthStatusHealthy, HealthStatusUnhealthy,
		formatTimeBoundUTC(since),
		HealthStatusHealthy, HealthStatusUnhealthy,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]WorkerHealthHourBucket, 0)
	for rows.Next() {
		var bucketRow WorkerHealthHourBucket
		if err := rows.Scan(&bucketRow.WorkerIP, &bucketRow.Hour, &bucketRow.Healthy, &bucketRow.Unhealthy); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		out = append(out, bucketRow)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}
