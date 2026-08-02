// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cat

import (
	"database/sql"
	"errors"

	"kive/bucket"
	"kive/data"
	"kive/utils"

	"github.com/jedib0t/go-pretty/v6/table"
)

func Hooks() error {
	// TODO: demand job filter

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

	var hooksCount int
	query := "SELECT count(*) FROM hooks"
	row := tx.QueryRow(query)
	err = row.Scan(&hooksCount)
	if errors.Is(err, sql.ErrNoRows) || hooksCount == 0 {
		return bucket.NotFoundError("hooks")
	}
	if err != nil {
		return bucket.DatabaseError(err)
	}

	rows, err := tx.Query(`SELECT job, hook_name, executed_on, demand_job, demand_hook, demand_config, description, schedule FROM (` + data.CatalogHooksSQL + `)`)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	t := utils.GetTable(table.Row{"job", "hook_name", "executed_on", "description", "schedule", "demand_job", "demand_hook", "demand_config"})

	for rows.Next() {
		var jobName string
		var hookName string
		var executedOn string
		var demandJob string
		var demandHook string
		var demandConfig string
		var description string
		var scheduleJSON string

		err = rows.Scan(&jobName, &hookName, &executedOn, &demandJob, &demandHook, &demandConfig, &description, &scheduleJSON)
		if err != nil {
			return bucket.DatabaseError(err)
		}

		t.AppendRows([]table.Row{{jobName, hookName, executedOn, description, scheduleJSON, demandJob, demandHook, demandConfig}})
	}
	if err := data.RowsErr(rows); err != nil {
		return err
	}

	t.Render()

	err = tx.Commit()
	if err != nil {
		return bucket.DatabaseError(err)
	}

	return nil
}
