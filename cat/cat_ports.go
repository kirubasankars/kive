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
	"kive/workspace"

	"github.com/jedib0t/go-pretty/v6/table"
)

func Ports(jobsCSV, workersCSV string, listeners bool) error {
	if listeners {
		return portsListeners(jobsCSV, workersCSV)
	}
	return portsByJob(jobsCSV)
}

func sqlInPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func portsByJob(jobsCSV string) error {
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

	if len(jobsFilter) > 0 {
		allJobs, err := data.GetJobs(tx)
		if err != nil {
			return err
		}
		if len(utils.Intersection(allJobs, jobsFilter)) == 0 {
			return fmt.Errorf("invalid input, jobs %v", jobsFilter)
		}
	}

	count := 0
	row := tx.QueryRow("SELECT count(*) FROM job_ports")
	err = row.Scan(&count)
	if errors.Is(err, sql.ErrNoRows) || count == 0 {
		return bucket.NotFoundError("ports")
	}

	sqlQuery := `SELECT j.name as job, jp.name, jp.port, jp.protocol, jp.exposure FROM job_ports jp JOIN jobs j ON jp.job_id = j.job_id`
	var args []any
	if len(jobsFilter) > 0 {
		sqlQuery += " WHERE j.name IN (" + sqlInPlaceholders(len(jobsFilter)) + ")"
		args = stringsToAny(jobsFilter)
	}

	rows, err := tx.Query(sqlQuery, args...)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	t := utils.GetTable(table.Row{"job", "name", "port", "protocol", "exposure"})

	for rows.Next() {
		var job string
		var name string
		var port int
		var protocol string
		var exposure string

		err = rows.Scan(&job, &name, &port, &protocol, &exposure)
		if err != nil {
			return bucket.DatabaseError(err)
		}
		if protocol == "" {
			protocol = workspace.DefaultPortProtocol
		}
		if exposure == "" {
			exposure = workspace.DefaultPortExposure
		}

		t.AppendRows([]table.Row{{job, name, port, protocol, exposure}})
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

func portsListeners(jobsCSV, workersCSV string) error {
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

	sqlQuery := `
		SELECT a.alloc_id, a.worker_ip, a.job, jp.name, jp.port, jp.protocol, jp.exposure
		FROM allocations a
		JOIN jobs j ON j.name = a.job
		JOIN job_ports jp ON jp.job_id = j.job_id`

	var conditions []string
	var args []any
	if len(jobsFilter) > 0 {
		conditions = append(conditions, "a.job IN ("+sqlInPlaceholders(len(jobsFilter))+")")
		args = append(args, stringsToAny(jobsFilter)...)
	}
	if len(workersFilter) > 0 {
		conditions = append(conditions, "a.worker_ip IN ("+sqlInPlaceholders(len(workersFilter))+")")
		args = append(args, stringsToAny(workersFilter)...)
	}
	if len(conditions) > 0 {
		sqlQuery += " WHERE " + strings.Join(conditions, " AND ")
	}
	sqlQuery += " ORDER BY a.job, a.worker_ip, jp.name"

	rows, err := tx.Query(sqlQuery, args...)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	t := utils.GetTable(table.Row{"allocation_id", "worker_ip", "job", "name", "port", "protocol", "exposure", "ip:port"})
	rowCount := 0

	for rows.Next() {
		var allocID string
		var workerIP string
		var job string
		var name string
		var port int
		var protocol string
		var exposure string

		err = rows.Scan(&allocID, &workerIP, &job, &name, &port, &protocol, &exposure)
		if err != nil {
			return bucket.DatabaseError(err)
		}
		if protocol == "" {
			protocol = workspace.DefaultPortProtocol
		}
		if exposure == "" {
			exposure = workspace.DefaultPortExposure
		}

		rowCount++
		t.AppendRows([]table.Row{{
			allocID,
			workerIP,
			job,
			name,
			port,
			protocol,
			exposure,
			fmt.Sprintf("%s:%d", workerIP, port),
		}})
	}
	if err := data.RowsErr(rows); err != nil {
		return err
	}
	if rowCount == 0 {
		return bucket.NotFoundError("listeners")
	}

	t.Render()

	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}

	return nil
}
