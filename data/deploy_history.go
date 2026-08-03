// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kive/bucket"
)

const (
	// DeployHistoryRetention is the maximum number of overall deploy history rows retained.
	DeployHistoryRetention = 50

	DeployHistoryStatusSucceeded = "succeeded"
	DeployHistoryStatusFailed    = "failed"
	DeployHistoryStatusCancelled = "cancelled"

	DeployHistoryOutcomeSkipped  = "skipped"
	DeployHistoryOutcomeDeployed = "deployed"
	DeployHistoryOutcomeFailed   = "failed"
	DeployHistoryOutcomeAborted  = "aborted"
)

// DeployHistoryJob is one job outcome within an overall deploy.
type DeployHistoryJob struct {
	Job             string `json:"job"`
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason,omitempty"`
	Version         string `json:"version,omitempty"`
	DeploymentOrder int    `json:"deployment_order"`
}

// DeployHistoryEntry is one overall non-dry-run deploy with nested job outcomes.
type DeployHistoryEntry struct {
	RunID               string             `json:"run_id"`
	Generation          int                `json:"generation"`
	StartedAt           time.Time          `json:"started_at"`
	EndedAt             time.Time          `json:"ended_at"`
	Status              string             `json:"status"`
	SourceRevision      string             `json:"source_revision,omitempty"`
	SourceRevisionLabel string             `json:"source_revision_label,omitempty"`
	BuildGitHash        string             `json:"build_git_hash,omitempty"`
	JobsFilter          string             `json:"jobs_filter,omitempty"`
	Force               bool               `json:"force"`
	Changed             bool               `json:"changed"`
	Error               string             `json:"error,omitempty"`
	Jobs                []DeployHistoryJob `json:"jobs,omitempty"`
}

// DeployHistoryRecord is the payload written at the end of a deploy.
type DeployHistoryRecord struct {
	RunID          string
	Generation     int
	StartedAt      time.Time
	EndedAt        time.Time
	Status         string
	SourceRevision string
	BuildGitHash   string
	JobsFilter     string
	Force          bool
	Error          string
	Jobs           []DeployHistoryJob
}

// InsertDeployHistory stores one overall deploy and its job outcomes.
// History prune for gone revisions runs during GC, not on insert.
func InsertDeployHistory(tx *sql.Tx, record DeployHistoryRecord) error {
	if strings.TrimSpace(record.RunID) == "" {
		return fmt.Errorf("deploy history run_id is required")
	}
	changed := false
	for _, job := range record.Jobs {
		if job.Outcome != DeployHistoryOutcomeSkipped {
			changed = true
			break
		}
	}
	force := 0
	if record.Force {
		force = 1
	}
	changedInt := 0
	if changed {
		changedInt = 1
	}
	_, err := tx.Exec(`
		INSERT INTO deploy_history (
			run_id, generation, started_at, ended_at, status,
			source_revision, build_git_hash, jobs_filter, force, changed, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RunID,
		record.Generation,
		record.StartedAt.UTC().Format(time.RFC3339Nano),
		record.EndedAt.UTC().Format(time.RFC3339Nano),
		record.Status,
		strings.TrimSpace(record.SourceRevision),
		strings.TrimSpace(record.BuildGitHash),
		strings.TrimSpace(record.JobsFilter),
		force,
		changedInt,
		strings.TrimSpace(record.Error),
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	for _, job := range record.Jobs {
		if strings.TrimSpace(job.Job) == "" {
			continue
		}
		_, err := tx.Exec(`
			INSERT INTO deploy_history_jobs (
				run_id, job, outcome, reason, version, deployment_order
			) VALUES (?, ?, ?, ?, ?, ?)`,
			record.RunID,
			job.Job,
			job.Outcome,
			job.Reason,
			job.Version,
			job.DeploymentOrder,
		)
		if err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

// CountPromotableSourceRevisions returns the number of distinct source_revision
// values among succeeded+changed deploy history rows.
func CountPromotableSourceRevisions(tx *sql.Tx) (int, error) {
	hashes, err := PromotableSourceRevisions(tx)
	if err != nil {
		return 0, err
	}
	return len(hashes), nil
}

// PromotableSourceRevisions returns distinct source_revision hashes from
// succeeded+changed deploy history rows (promotion candidates).
func PromotableSourceRevisions(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`
		SELECT DISTINCT source_revision FROM deploy_history
		WHERE status = ? AND changed = 1 AND trim(source_revision) != ''`,
		DeployHistoryStatusSucceeded)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()
	out := make([]string, 0)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		hash = strings.TrimSpace(hash)
		if hash != "" {
			out = append(out, hash)
		}
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// PruneDeployHistory deletes deploy history rows beyond keep newest entries.
func PruneDeployHistory(tx *sql.Tx, keep int) error {
	if keep < 1 {
		keep = DeployHistoryRetention
	}
	_, err := tx.Exec(`
		DELETE FROM deploy_history_jobs
		WHERE run_id NOT IN (
			SELECT run_id FROM deploy_history
			ORDER BY ended_at DESC, generation DESC
			LIMIT ?
		)`, keep)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	_, err = tx.Exec(`
		DELETE FROM deploy_history
		WHERE run_id NOT IN (
			SELECT run_id FROM deploy_history
			ORDER BY ended_at DESC, generation DESC
			LIMIT ?
		)`, keep)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// PruneDeployHistoryKeepingRevisions drops history rows whose source_revision is
// non-empty and not in keep. Rows with empty source_revision are capped to keepNewestOrphan.
func PruneDeployHistoryKeepingRevisions(tx *sql.Tx, keep []string, keepNewestOrphan int) error {
	keepSet := make(map[string]struct{}, len(keep))
	for _, hash := range keep {
		hash = strings.TrimSpace(hash)
		if hash != "" {
			keepSet[hash] = struct{}{}
		}
	}
	rows, err := tx.Query(`SELECT run_id, source_revision FROM deploy_history`)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()
	drop := make([]string, 0)
	orphanIDs := make([]string, 0)
	for rows.Next() {
		var runID, sourceRevision string
		if err := rows.Scan(&runID, &sourceRevision); err != nil {
			return bucket.DatabaseError(err)
		}
		sourceRevision = strings.TrimSpace(sourceRevision)
		if sourceRevision == "" {
			orphanIDs = append(orphanIDs, runID)
			continue
		}
		if _, ok := keepSet[sourceRevision]; !ok {
			drop = append(drop, runID)
		}
	}
	if err := rowsErr(rows); err != nil {
		return err
	}
	if keepNewestOrphan < 1 {
		keepNewestOrphan = DeployHistoryRetention
	}
	if len(orphanIDs) > keepNewestOrphan {
		// Prefer newest orphans: load ended_at order
		orphanRows, err := tx.Query(`
			SELECT run_id FROM deploy_history
			WHERE trim(source_revision) = '' OR source_revision IS NULL
			ORDER BY ended_at DESC, generation DESC`)
		if err != nil {
			return bucket.DatabaseError(err)
		}
		ordered := make([]string, 0)
		for orphanRows.Next() {
			var id string
			if err := orphanRows.Scan(&id); err != nil {
				_ = orphanRows.Close()
				return bucket.DatabaseError(err)
			}
			ordered = append(ordered, id)
		}
		if err := rowsErr(orphanRows); err != nil {
			_ = orphanRows.Close()
			return err
		}
		_ = orphanRows.Close()
		if len(ordered) > keepNewestOrphan {
			drop = append(drop, ordered[keepNewestOrphan:]...)
		}
	}
	return deleteDeployHistoryRuns(tx, drop)
}

func deleteDeployHistoryRuns(tx *sql.Tx, runIDs []string) error {
	for _, runID := range runIDs {
		if strings.TrimSpace(runID) == "" {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM deploy_history_jobs WHERE run_id = ?`, runID); err != nil {
			return bucket.DatabaseError(err)
		}
		if _, err := tx.Exec(`DELETE FROM deploy_history WHERE run_id = ?`, runID); err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

// DeployHistorySourceRevisions returns non-empty source_revision hashes from deploy_history.
func DeployHistorySourceRevisions(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT DISTINCT source_revision FROM deploy_history WHERE trim(source_revision) != ''`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()
	out := make([]string, 0)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		hash = strings.TrimSpace(hash)
		if hash != "" {
			out = append(out, hash)
		}
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDeployHistory returns newest-first overall deploys with nested jobs.
// When jobsFilter is non-empty, only deploys that include those jobs are returned,
// and nested jobs are limited to the filter.
func ListDeployHistory(tx *sql.Tx, limit int, jobsFilter []string) ([]DeployHistoryEntry, error) {
	if limit <= 0 {
		limit = DeployHistoryRetention
	}
	jobsFilter = normalizeHistoryJobFilter(jobsFilter)

	rows, err := tx.Query(`
		SELECT run_id, generation, started_at, ended_at, status,
			source_revision, build_git_hash, jobs_filter, force, changed, error
		FROM deploy_history
		ORDER BY ended_at DESC, generation DESC
		LIMIT ?`, limit*4) // over-fetch when filtering by job
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()
	return collectDeployHistoryEntries(tx, rows, limit, jobsFilter)
}

// ListPromotableDeployHistory returns newest-first succeeded+changed deploys with
// a non-empty source_revision. This is the promotion selection window: failed,
// cancelled, and unchanged rows do not occupy slots.
func ListPromotableDeployHistory(tx *sql.Tx, limit int) ([]DeployHistoryEntry, error) {
	if limit <= 0 {
		limit = DeployHistoryRetention
	}
	rows, err := tx.Query(`
		SELECT run_id, generation, started_at, ended_at, status,
			source_revision, build_git_hash, jobs_filter, force, changed, error
		FROM deploy_history
		WHERE status = ? AND changed = 1 AND trim(source_revision) != ''
		ORDER BY ended_at DESC, generation DESC
		LIMIT ?`, DeployHistoryStatusSucceeded, limit)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()
	return collectDeployHistoryEntries(tx, rows, limit, nil)
}

// NewestPromotableDeployMark returns the newest (ended_at, generation) among
// succeeded+changed rows for sourceRevision. Used when the applied FIFO cursor
// has aged out of the promotable selection window but still exists in history.
func NewestPromotableDeployMark(tx *sql.Tx, sourceRevision string) (endedAt time.Time, generation int, found bool, err error) {
	sourceRevision = strings.TrimSpace(sourceRevision)
	if sourceRevision == "" {
		return time.Time{}, 0, false, nil
	}
	var endedAtStr string
	err = tx.QueryRow(`
		SELECT ended_at, generation FROM deploy_history
		WHERE status = ? AND changed = 1 AND trim(source_revision) = ?
		ORDER BY ended_at DESC, generation DESC
		LIMIT 1`, DeployHistoryStatusSucceeded, sourceRevision).Scan(&endedAtStr, &generation)
	if err == sql.ErrNoRows {
		return time.Time{}, 0, false, nil
	}
	if err != nil {
		return time.Time{}, 0, false, bucket.DatabaseError(err)
	}
	endedAt, _ = time.Parse(time.RFC3339Nano, endedAtStr)
	if endedAt.IsZero() {
		endedAt, _ = time.Parse(time.RFC3339, endedAtStr)
	}
	return endedAt, generation, true, nil
}

func collectDeployHistoryEntries(tx *sql.Tx, rows *sql.Rows, limit int, jobsFilter []string) ([]DeployHistoryEntry, error) {
	entries := make([]DeployHistoryEntry, 0)
	runIDs := make([]string, 0)
	for rows.Next() {
		var (
			entry              DeployHistoryEntry
			startedAt, endedAt string
			force, changed     int
		)
		if err := rows.Scan(
			&entry.RunID, &entry.Generation, &startedAt, &endedAt, &entry.Status,
			&entry.SourceRevision, &entry.BuildGitHash, &entry.JobsFilter,
			&force, &changed, &entry.Error,
		); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		entry.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
		if entry.StartedAt.IsZero() {
			entry.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
		}
		entry.EndedAt, _ = time.Parse(time.RFC3339Nano, endedAt)
		if entry.EndedAt.IsZero() {
			entry.EndedAt, _ = time.Parse(time.RFC3339, endedAt)
		}
		entry.Force = force != 0
		entry.Changed = changed != 0
		entries = append(entries, entry)
		runIDs = append(runIDs, entry.RunID)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return entries, nil
	}

	jobsByRun, err := loadDeployHistoryJobs(tx, runIDs, jobsFilter)
	if err != nil {
		return nil, err
	}

	out := make([]DeployHistoryEntry, 0, len(entries))
	for _, entry := range entries {
		jobs := jobsByRun[entry.RunID]
		if len(jobsFilter) > 0 && len(jobs) == 0 {
			continue
		}
		entry.Jobs = jobs
		if entry.Jobs == nil {
			entry.Jobs = []DeployHistoryJob{}
		}
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func loadDeployHistoryJobs(tx *sql.Tx, runIDs, jobsFilter []string) (map[string][]DeployHistoryJob, error) {
	out := make(map[string][]DeployHistoryJob, len(runIDs))
	if len(runIDs) == 0 {
		return out, nil
	}
	filterSet := make(map[string]struct{}, len(jobsFilter))
	for _, job := range jobsFilter {
		filterSet[job] = struct{}{}
	}

	placeholders := make([]string, len(runIDs))
	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `
		SELECT run_id, job, outcome, reason, version, deployment_order
		FROM deploy_history_jobs
		WHERE run_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY deployment_order ASC, job ASC`
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var job DeployHistoryJob
		var runID string
		if err := rows.Scan(&runID, &job.Job, &job.Outcome, &job.Reason, &job.Version, &job.DeploymentOrder); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		if len(filterSet) > 0 {
			if _, ok := filterSet[job.Job]; !ok {
				continue
			}
		}
		out[runID] = append(out[runID], job)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeHistoryJobFilter(jobs []string) []string {
	if len(jobs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(jobs))
	out := make([]string, 0, len(jobs))
	for _, job := range jobs {
		job = strings.TrimSpace(job)
		if job == "" {
			continue
		}
		if _, ok := seen[job]; ok {
			continue
		}
		seen[job] = struct{}{}
		out = append(out, job)
	}
	return out
}

// JobDeployMeta returns catalog version and deployment_order for a job.
func JobDeployMeta(tx *sql.Tx, job string) (version string, order int, err error) {
	err = tx.QueryRow(
		`SELECT ifnull(version, ''), ifnull(deployment_order, 0) FROM jobs WHERE name = ?`,
		job,
	).Scan(&version, &order)
	if err != nil {
		return "", 0, bucket.DatabaseError(err)
	}
	return version, order, nil
}

type sourceRevisionStateFile struct {
	Current string `json:"current"`
}

// CurrentSourceRevision returns the server bucket's current source revision hash, or "".
func CurrentSourceRevision() string {
	path := filepath.Join(bucket.Location, ".kive", "source-revisions", "state.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var state sourceRevisionStateFile
	if err := json.Unmarshal(encoded, &state); err != nil {
		return ""
	}
	return strings.TrimSpace(state.Current)
}
