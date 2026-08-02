// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"
	"strings"
	"time"

	"kive/bucket"
	"kive/data"
)

func recordJobOutcome(tx *sql.Tx, outcomes map[string]data.DeployHistoryJob, job, outcome, reason string) {
	version, order, err := data.JobDeployMeta(tx, job)
	if err != nil {
		version, order = "", 0
	}
	outcomes[job] = data.DeployHistoryJob{
		Job:             job,
		Outcome:         outcome,
		Reason:          reason,
		Version:         version,
		DeploymentOrder: order,
	}
}

// allJobsDeployedOrSkipped reports whether every deployable job either skipped
// (already complete) or deployed successfully. Missing outcomes or failed/aborted
// jobs return false so worker.json is not refreshed and generation is not persisted
// on a partial run.
func allJobsDeployedOrSkipped(deployableJobs []string, outcomes map[string]data.DeployHistoryJob) bool {
	for _, job := range deployableJobs {
		entry, ok := outcomes[job]
		if !ok {
			return false
		}
		switch entry.Outcome {
		case data.DeployHistoryOutcomeDeployed, data.DeployHistoryOutcomeSkipped:
			continue
		default:
			return false
		}
	}
	return true
}

func writeDeployHistory(
	tx *sql.Tx,
	rt *bucket.Runtime,
	startedAt time.Time,
	jobsFilter []string,
	opts Options,
	deployableJobs []string,
	outcomes map[string]data.DeployHistoryJob,
	deployFailures []error,
) error {
	runID := ""
	generation := 0
	if rt != nil {
		run := rt.Run()
		runID = run.RunID
		generation = run.Generation
	}
	if runID == "" {
		return nil
	}

	jobs := make([]data.DeployHistoryJob, 0, len(deployableJobs))
	for _, job := range deployableJobs {
		if entry, ok := outcomes[job]; ok {
			jobs = append(jobs, entry)
			continue
		}
		version, order, err := data.JobDeployMeta(tx, job)
		if err != nil {
			version, order = "", 0
		}
		jobs = append(jobs, data.DeployHistoryJob{
			Job:             job,
			Outcome:         data.DeployHistoryOutcomeAborted,
			Reason:          "deploy_aborted",
			Version:         version,
			DeploymentOrder: order,
		})
	}

	status := data.DeployHistoryStatusSucceeded
	errSummary := ""
	cancelled := false
	for _, err := range deployFailures {
		if isCancelled(err) {
			cancelled = true
			break
		}
	}
	if cancelled {
		status = data.DeployHistoryStatusCancelled
	} else if len(deployFailures) > 0 {
		status = data.DeployHistoryStatusFailed
	}
	if joined := joinErrors("", deployFailures); joined != nil {
		errSummary = joined.Error()
		if len(errSummary) > 2000 {
			errSummary = errSummary[:2000] + "..."
		}
	}

	meta, err := data.GetBundleMeta(tx)
	if err != nil {
		meta = data.BundleMeta{}
	}

	return data.InsertDeployHistory(tx, data.DeployHistoryRecord{
		RunID:          runID,
		Generation:      generation,
		StartedAt:      startedAt,
		EndedAt:        time.Now(),
		Status:         status,
		SourceRevision: data.CurrentSourceRevision(),
		BuildGitHash:   meta.BuildGitHash,
		JobsFilter:     strings.Join(jobsFilter, ","),
		Force:          opts.Force,
		Error:          errSummary,
		Jobs:           jobs,
	})
}
