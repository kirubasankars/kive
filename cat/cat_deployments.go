// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cat

import (
	"database/sql"
	"fmt"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/utils"

	"github.com/jedib0t/go-pretty/v6/table"
)

func Deployments(jobsCSV, workersCSV string, activeOnly bool) error {
	db, err := data.OpenDatabase(true)
	if err != nil {
		return bucket.DatabaseError(err)
	}

	tx, err := db.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	jobsFilter := parseCSVFilter(jobsCSV)
	workersFilter := parseCSVFilter(workersCSV)

	if err := validateDeploymentFilters(tx, jobsFilter, workersFilter); err != nil {
		return err
	}

	query := `SELECT alloc_id, worker_ip, job, disabled, removed,
		pending_hash, applied_hash, applied_version, version
		FROM (` + data.CatalogDeploymentsSQL + `) AS cat_deployments`

	var conditions []string
	if len(jobsFilter) > 0 {
		conditions = append(conditions, fmt.Sprintf("job IN ('%s')", strings.Join(jobsFilter, "','")))
	}
	if len(workersFilter) > 0 {
		conditions = append(conditions, fmt.Sprintf("worker_ip IN ('%s')", strings.Join(workersFilter, "','")))
	}
	if activeOnly {
		conditions = append(conditions, "removed = 0", "disabled = 0")
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY job, worker_ip"

	rows, err := tx.Query(query)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	t := utils.GetTable(table.Row{
		"job", "worker_ip", "allocation_id", "rollout",
		"pending_hash", "applied_hash",
		"applied_version", "version",
		"disabled", "removed",
	})

	rowCount := 0
	for rows.Next() {
		var (
			allocID, workerIP, job     string
			disabled, removed          int
			pendingHash, appliedHash   string
			currentVersion, newVersion string
		)
		if err := rows.Scan(
			&allocID, &workerIP, &job, &disabled, &removed,
			&pendingHash, &appliedHash, &currentVersion, &newVersion,
		); err != nil {
			return bucket.DatabaseError(err)
		}

		t.AppendRows([]table.Row{{
			job, workerIP, allocID,
			RolloutStatus(disabled, removed, pendingHash, appliedHash, currentVersion, newVersion),
			pendingHash, appliedHash,
			DisplayVersion(currentVersion), DisplayVersion(newVersion),
			disabled, removed,
		}})
		rowCount++
	}
	if err := data.RowsErr(rows); err != nil {
		return err
	}
	if rowCount == 0 {
		return bucket.NotFoundError("deployments")
	}

	t.Render()

	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

func parseCSVFilter(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return utils.Unique(out)
}

func validateDeploymentFilters(tx *sql.Tx, jobsFilter, workersFilter []string) error {
	if len(jobsFilter) > 0 {
		allJobs, err := data.GetAllAllocatedJobs(tx)
		if err != nil {
			return err
		}
		if len(utils.Intersection(allJobs, jobsFilter)) == 0 {
			return fmt.Errorf("invalid input, jobs %v", jobsFilter)
		}
	}
	if len(workersFilter) > 0 {
		allocatedWorkers, err := data.GetAllocatedWorkerIPs(tx)
		if err != nil {
			return err
		}
		if len(utils.Intersection(allocatedWorkers, workersFilter)) == 0 {
			return fmt.Errorf("invalid input, workers %v", workersFilter)
		}
	}
	return nil
}

// RolloutStatus is the rollout column for kive cat deployments.
func RolloutStatus(disabled, removed int, pendingHash, appliedHash, currentVersion, newVersion string) string {
	if removed == 1 {
		return "removed"
	}
	if disabled == 1 {
		if appliedHash != "" && appliedHash != pendingHash {
			return "disabled_restart"
		}
		if DisplayVersion(currentVersion) != DisplayVersion(newVersion) {
			return "disabled_restart"
		}
		return "disabled"
	}
	if pendingHash == "" && appliedHash == "" {
		return "new"
	}
	if appliedHash == "" {
		return "new"
	}
	if appliedHash == data.HealthFailedAppliedHash {
		return "health_failed"
	}
	if appliedHash != pendingHash {
		return "restart"
	}
	if DisplayVersion(currentVersion) != DisplayVersion(newVersion) {
		return "restart"
	}
	return "promoted"
}

// DisplayVersion returns the allocation/job version shown in cat output.
// Empty and legacy "unknown" values map to DefaultAllocationVersion (0.0.0).
func DisplayVersion(version string) string {
	return data.NormalizeDeployVersion(version)
}
