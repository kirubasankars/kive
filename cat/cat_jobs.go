// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cat

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/utils"

	"github.com/jedib0t/go-pretty/v6/table"
)

func Jobs() error {
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

	var count int
	query := "SELECT count(*) FROM jobs"
	row := tx.QueryRow(query)
	err = row.Scan(&count)
	if errors.Is(err, sql.ErrNoRows) || count == 0 {
		return bucket.NotFoundError("jobs")
	}
	if err != nil {
		return bucket.DatabaseError(err)
	}

	rows, err := tx.Query(`SELECT job_id, name, version, deployment_priority, deployment_order, disabled, deployment_seq, selectors, current_memory_mb, current_memory_source, current_cpu_mhz, current_cpu_source, cpu_shares, cpu_shares_source, signature_status FROM (` + data.CatalogJobsSQL + `)`)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	t := utils.GetTable(table.Row{"job", "job_id", "version", "signature", "disabled", "deployment_order", "cpu", "cpu_shares", "memory", "selectors"})

	for rows.Next() {
		var jobID string
		var name string
		var version string
		var deploymentPriority int
		var deploymentOrder int
		var disabled int
		var deploymentSeq int
		var selectors string
		var memoryMB string
		var memorySource string
		var cpuMHz string
		var cpuSource string
		var cpuShares string
		var cpuSharesSource string
		var signatureStatus string

		err = rows.Scan(
			&jobID, &name, &version, &deploymentPriority, &deploymentOrder, &disabled, &deploymentSeq, &selectors,
			&memoryMB, &memorySource, &cpuMHz, &cpuSource,
			&cpuShares, &cpuSharesSource, &signatureStatus,
		)
		if err != nil {
			return bucket.DatabaseError(err)
		}

		t.AppendRows([]table.Row{
			{
				name,
				jobID,
				DisplayVersion(version),
				signatureStatus,
				disabled,
				FormatDeploymentOrder(deploymentOrder, deploymentPriority, deploymentSeq),
				FormatJobCPU(cpuMHz, cpuSource),
				FormatJobCPUShares(cpuShares, cpuSharesSource),
				FormatJobMemory(memoryMB, memorySource),
				selectors,
			},
		})
	}
	if err := data.RowsErr(rows); err != nil {
		return err
	}

	t.Render()

	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}

	return nil
}

func formatJobResource(value, unit, source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "manifest"
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = "0"
	}
	return fmt.Sprintf("%s %s (%s)", value, unit, source)
}

// FormatDeploymentOrder formats the cat jobs deployment_order column as
// "order (priority, seq)".
func FormatDeploymentOrder(order, priority, seq int) string {
	return fmt.Sprintf("%d (%d, %d)", order, priority, seq)
}

// FormatJobCPU formats the cat jobs CPU column.
func FormatJobCPU(mhz, source string) string {
	return formatJobResource(mhz, "mhz", source)
}

// FormatJobCPUShares formats the cat jobs CPU shares column.
func FormatJobCPUShares(shares, source string) string {
	return formatJobResource(shares, "shares", source)
}

// FormatJobMemory formats the cat jobs memory column.
func FormatJobMemory(mb, source string) string {
	return formatJobResource(mb, "mb", source)
}
