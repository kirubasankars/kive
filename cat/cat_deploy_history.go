// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cat

import (
	"fmt"
	"strings"
	"time"

	"kive/bucket"
	"kive/data"
	"kive/utils"

	"github.com/jedib0t/go-pretty/v6/table"
)

// DeployHistory lists overall deploy history with nested job outcomes.
func DeployHistory(jobsCSV string, limit int) error {
	db, err := data.OpenDatabase(true)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = db.Close()
	}()

	tx, err := db.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	jobsFilter := parseCSVFilter(jobsCSV)
	entries, err := data.ListDeployHistory(tx, limit, jobsFilter)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return bucket.NotFoundError("deploy-history")
	}

	t := utils.GetTable(table.Row{
		"ended_at", "generation", "status", "changed", "run_id",
		"source_revision", "jobs_filter", "force", "jobs",
	})
	for _, entry := range entries {
		jobSummary := formatHistoryJobs(entry.Jobs)
		src := entry.SourceRevision
		if len(src) > 12 {
			src = src[:12]
		}
		t.AppendRows([]table.Row{{
			entry.EndedAt.UTC().Format(time.RFC3339),
			entry.Generation,
			entry.Status,
			entry.Changed,
			entry.RunID,
			src,
			entry.JobsFilter,
			entry.Force,
			jobSummary,
		}})
	}
	t.Render()

	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

func formatHistoryJobs(jobs []data.DeployHistoryJob) string {
	if len(jobs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(jobs))
	for _, job := range jobs {
		part := fmt.Sprintf("%s:%s", job.Job, job.Outcome)
		if job.Reason != "" && job.Outcome != data.DeployHistoryOutcomeDeployed {
			part += "(" + job.Reason + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}
