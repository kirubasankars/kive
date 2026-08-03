// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"kive/bucket"
	"kive/buildinfo"
	"kive/workspace"
)

// NewJobTemplateVars returns .bt substitution vars for jobName from the catalog.
func NewJobTemplateVars(tx *sql.Tx, jobName string) (workspace.JobTemplateVars, error) {
	bucketID, err := GetBucketID(tx)
	if err != nil {
		return workspace.JobTemplateVars{}, err
	}
	jobID, err := GetJobID(tx, jobName)
	if err != nil {
		return workspace.JobTemplateVars{}, err
	}
	return workspace.JobTemplateVars{
		JobName:     jobName,
		BucketID:    bucketID,
		JobID:       jobID,
		KiveGitHash: buildinfo.Hash(),
	}, nil
}

func GetJobs(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT name FROM jobs`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobNames := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobNames = append(jobNames, name)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobNames, nil
}

// GetJobID returns the catalog job_id for a job name.
func GetJobID(tx *sql.Tx, jobName string) (string, error) {
	var jobID string
	err := tx.QueryRow(`SELECT job_id FROM jobs WHERE name = ?`, jobName).Scan(&jobID)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return jobID, nil
}

func GetJobMemoryLimits(tx *sql.Tx, job string) (string, string, error) {
	var minMemory, maxMemory string
	row := tx.QueryRow("SELECT min_memory_mb, max_memory_mb FROM jobs WHERE name = ?", job)
	err := row.Scan(&minMemory, &maxMemory)
	if err != nil {
		return "", "", bucket.DatabaseError(err)
	}
	return minMemory, maxMemory, nil
}

func GetJobMemory(tx *sql.Tx, job string) (string, error) {
	var memory string
	row := tx.QueryRow("SELECT current_memory_mb FROM jobs WHERE name = ?", job)
	err := row.Scan(&memory)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return memory, nil
}

func GetJobCPULimits(tx *sql.Tx, job string) (string, string, error) {
	var minCPU, maxCPU string
	row := tx.QueryRow("SELECT min_cpu_mhz, max_cpu_mhz FROM jobs WHERE name = ?", job)
	err := row.Scan(&minCPU, &maxCPU)
	if err != nil {
		return "", "", bucket.DatabaseError(err)
	}
	return minCPU, maxCPU, nil
}

func GetJobCPU(tx *sql.Tx, job string) (string, error) {
	var cpu string
	row := tx.QueryRow("SELECT current_cpu_mhz FROM jobs WHERE name = ?", job)
	err := row.Scan(&cpu)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return cpu, nil
}

func GetJobCPUShares(tx *sql.Tx, job string) (string, error) {
	var cpuShares string
	row := tx.QueryRow("SELECT cpu_shares FROM jobs WHERE name = ?", job)
	err := row.Scan(&cpuShares)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return cpuShares, nil
}

func GetJobVersion(tx *sql.Tx, job string) (string, error) {
	var version string
	row := tx.QueryRow("SELECT version FROM jobs WHERE name = ?", job)
	err := row.Scan(&version)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return version, nil
}

func GetJobSelectors(tx *sql.Tx, jobName string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT selector FROM job_selectors WHERE job_id = (SELECT job_id FROM jobs WHERE name = ?)`,
		jobName,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	selectors := make([]string, 0)
	for rows.Next() {
		var selector string
		if err := rows.Scan(&selector); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		selectors = append(selectors, selector)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return selectors, nil
}

// CopyHookModule copies one command script (and parent dirs) for health-fast staging.
// Matches a scripted module (hook_x.py, .ts, .js, .rb, .sh) as well as an
// extensionless compiled executable named exactly hook_x.
func CopyHookModule(tx *sql.Tx, jobName, commandName, outputPath string) error {
	exactPath := jobName + "/_hooks/" + commandName
	pattern := exactPath + ".%"
	copied, err := copyJobFilesMatchingOrExact(tx, jobName, exactPath, pattern, outputPath)
	if err != nil {
		return err
	}
	if copied == 0 {
		return fmt.Errorf("%w: job %s command %s module not in job_files", bucket.ErrHookFileNotFound, jobName, commandName)
	}
	return nil
}

// CopyHookLibModules copies _hooks/*_lib.* helpers for health-fast staging.
// Health hooks often import shared job helpers (e.g. prometheus_lib.py) that must
// sit beside the command module; Makefile/templates remain excluded.
func CopyHookLibModules(tx *sql.Tx, jobName, outputPath string) error {
	pattern := jobName + "/_hooks/%_lib.%"
	_, err := copyJobFilesMatching(tx, jobName, pattern, outputPath)
	return err
}

func copyJobFilesMatching(tx *sql.Tx, jobName, pattern, outputPath string) (int, error) {
	vars, err := NewJobTemplateVars(tx, jobName)
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(
		`SELECT path, content FROM job_files
		 WHERE job_id = (SELECT job_id FROM jobs WHERE name = ?)
		   AND isdir = 0 AND path LIKE ?`,
		jobName, pattern,
	)
	return writeMatchedJobFiles(rows, err, vars, outputPath)
}

func copyJobFilesMatchingOrExact(tx *sql.Tx, jobName, exactPath, pattern, outputPath string) (int, error) {
	vars, err := NewJobTemplateVars(tx, jobName)
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(
		`SELECT path, content FROM job_files
		 WHERE job_id = (SELECT job_id FROM jobs WHERE name = ?)
		   AND isdir = 0 AND (path = ? OR path LIKE ?)`,
		jobName, exactPath, pattern,
	)
	return writeMatchedJobFiles(rows, err, vars, outputPath)
}

func writeMatchedJobFiles(rows *sql.Rows, err error, vars workspace.JobTemplateVars, outputPath string) (int, error) {
	if err != nil {
		return 0, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var copied int
	for rows.Next() {
		var filePath, content string
		if err := rows.Scan(&filePath, &content); err != nil {
			return 0, bucket.DatabaseError(err)
		}
		filePath, content, _ = workspace.MaterializeJobTemplate(filePath, content, vars)
		dest := path.Join(outputPath, filePath)
		if err := os.MkdirAll(path.Dir(dest), 0o755); err != nil {
			return 0, bucket.DatabaseError(err)
		}
		if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
			return 0, bucket.DatabaseError(err)
		}
		copied++
	}
	if err := rowsErr(rows); err != nil {
		return 0, err
	}
	return copied, nil
}

func CopyJobFiles(tx *sql.Tx, jobName, outputPath string) error {
	vars, err := NewJobTemplateVars(tx, jobName)
	if err != nil {
		return err
	}
	rows, err := tx.Query(
		`SELECT path, content, isdir FROM job_files WHERE job_id = (SELECT job_id FROM jobs WHERE name = ?) ORDER BY isdir DESC`,
		jobName,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var filePath string
		var content string
		var isDir bool
		err := rows.Scan(&filePath, &content, &isDir)
		if err != nil {
			return bucket.DatabaseError(err)
		}

		if isPrometheusWorkspacePath(filePath) {
			continue
		}

		if isDir {
			err = os.MkdirAll(path.Join(outputPath, filePath), os.ModePerm)
			if err != nil {
				return bucket.DatabaseError(err)
			}
			continue
		}
		filePath, content, _ = workspace.MaterializeJobTemplate(filePath, content, vars)
		err = os.WriteFile(path.Join(outputPath, filePath), []byte(content), os.ModePerm)
		if err != nil {
			return bucket.DatabaseError(err)
		}
	}

	if err := rowsErr(rows); err != nil {
		return err
	}
	return nil
}

// ExportJobFilesRaw writes job_files for jobName under outputPath verbatim (no .bt materialize,
// includes _prometheus/). Paths in job_files already include the job name prefix.
func ExportJobFilesRaw(tx *sql.Tx, jobName, outputPath string) error {
	rows, err := tx.Query(
		`SELECT path, content, isdir FROM job_files WHERE job_id = (SELECT job_id FROM jobs WHERE name = ?) ORDER BY isdir DESC`,
		jobName,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var filePath string
		var content string
		var isDir bool
		if err := rows.Scan(&filePath, &content, &isDir); err != nil {
			return bucket.DatabaseError(err)
		}
		filePath = filepath.ToSlash(filePath)
		if err := ValidateBundlePath(filePath); err != nil {
			return fmt.Errorf("job %s file %q: %w", jobName, filePath, err)
		}
		if filePath != jobName && !strings.HasPrefix(filePath, jobName+"/") {
			return fmt.Errorf("job %s file %q is outside its job root", jobName, filePath)
		}
		dest := path.Join(outputPath, filePath)
		if isDir {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return bucket.UnexpectedError(err)
			}
			continue
		}
		if err := os.MkdirAll(path.Dir(dest), 0o755); err != nil {
			return bucket.UnexpectedError(err)
		}
		if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
			return bucket.UnexpectedError(err)
		}
	}
	return rowsErr(rows)
}

// ExportAllJobFilesRaw exports every catalog job's job_files under jobsDir (e.g. workspace/jobs).
func ExportAllJobFilesRaw(tx *sql.Tx, jobsDir string) error {
	jobs, err := GetJobs(tx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}
	for _, jobName := range jobs {
		if err := ExportJobFilesRaw(tx, jobName, jobsDir); err != nil {
			return err
		}
	}
	return nil
}

func GetAllocationID(tx *sql.Tx, workerIP, job string) (string, error) {
	var allocID string
	row := tx.QueryRow("SELECT alloc_id FROM allocations WHERE job = ? AND worker_ip = ?", job, workerIP)
	err := row.Scan(&allocID)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return allocID, nil
}

func GetNewAllocations(tx *sql.Tx, jobName string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT a.worker_ip FROM allocation_hashes h
		 JOIN allocations a ON h.alloc_id = a.alloc_id AND h.applied_hash IS NULL
		 WHERE a.job = ? AND a.removed = 0 AND a.disabled = 0`,
		jobName,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	workerIPs := make([]string, 0)
	for rows.Next() {
		var workerIP string
		if err := rows.Scan(&workerIP); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		workerIPs = append(workerIPs, workerIP)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return workerIPs, nil
}

// GetUpdatedNonRemovedAllocations returns workers (active or disabled) whose staged content
// differs from the last promoted hash.
func GetUpdatedNonRemovedAllocations(tx *sql.Tx, jobName string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT a.worker_ip FROM allocation_hashes h
		 JOIN allocations a ON h.alloc_id = a.alloc_id
		   AND h.applied_hash IS NOT NULL AND h.applied_hash != h.pending_hash
		 WHERE a.job = ? AND a.removed = 0`,
		jobName,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	workerIPs := make([]string, 0)
	for rows.Next() {
		var workerIP string
		if err := rows.Scan(&workerIP); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		workerIPs = append(workerIPs, workerIP)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return workerIPs, nil
}

// GetUpdatedAllocations returns workers with promoted content that differs from the staged plan.
func GetUpdatedAllocations(tx *sql.Tx, jobName string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT a.worker_ip FROM allocation_hashes h
		 JOIN allocations a ON h.alloc_id = a.alloc_id
		   AND h.applied_hash IS NOT NULL AND h.applied_hash != h.pending_hash
		 WHERE a.job = ? AND a.removed = 0 AND a.disabled = 0`,
		jobName,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	workerIPs := make([]string, 0)
	for rows.Next() {
		var workerIP string
		if err := rows.Scan(&workerIP); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		workerIPs = append(workerIPs, workerIP)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return workerIPs, nil
}

func GetHooks(tx *sql.Tx, job, event string) ([]string, error) {
	rows, err := tx.Query("SELECT DISTINCT name as hook_name FROM hooks WHERE job = ? AND executed_on = ?", job, event)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	commands := make([]string, 0)
	for rows.Next() {
		var command string
		err := rows.Scan(&command)
		if err != nil {
			return nil, bucket.DatabaseError(err)
		}

		commands = append(commands, command)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return commands, nil
}

// JobHasPostDeployCommands reports whether the job defines post_deploy hooks.
func JobHasPostDeployCommands(tx *sql.Tx, job string) (bool, error) {
	commands, err := GetHooks(tx, job, "post_deploy")
	if err != nil {
		return false, err
	}
	return len(commands) > 0, nil
}

// GetJobsWithHook returns job names that register commandName for event, in catalog order.
func GetJobsWithHook(tx *sql.Tx, commandName, event string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT DISTINCT job FROM hooks WHERE name = ? AND executed_on = ? ORDER BY job`,
		commandName, event,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobs := make([]string, 0)
	for rows.Next() {
		var job string
		if err := rows.Scan(&job); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobs = append(jobs, job)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobs, nil
}

// JobInCatalog reports whether the job still has a row in the catalog (jobs table).
// After a workspace job folder is removed, build deletes the row and marks allocations
// removed=1 until gc runs.
func JobInCatalog(tx *sql.Tx, job string) (bool, error) {
	var count int
	err := tx.QueryRow(`SELECT count(*) FROM jobs WHERE name = ?`, job).Scan(&count)
	if err != nil {
		return false, bucket.DatabaseError(err)
	}
	return count > 0, nil
}

func GetMaxConcurrentRestarts(tx *sql.Tx, job string) (int, error) {
	var maxConcurrentRestarts int
	row := tx.QueryRow("SELECT max_concurrent_restarts FROM jobs WHERE name = ?", job)
	err := row.Scan(&maxConcurrentRestarts)
	if err != nil {
		return 0, bucket.DatabaseError(err)
	}
	return maxConcurrentRestarts, nil
}

func GetMaxConcurrentStarts(tx *sql.Tx, job string) (int, error) {
	var maxConcurrentStarts int
	row := tx.QueryRow("SELECT max_concurrent_starts FROM jobs WHERE name = ?", job)
	err := row.Scan(&maxConcurrentStarts)
	if err != nil {
		return 0, bucket.DatabaseError(err)
	}
	if maxConcurrentStarts < 0 {
		return 0, nil
	}
	return maxConcurrentStarts, nil
}

func GetMaxConcurrentStops(tx *sql.Tx, job string) (int, error) {
	var maxConcurrentStops int
	row := tx.QueryRow("SELECT max_concurrent_stops FROM jobs WHERE name = ?", job)
	err := row.Scan(&maxConcurrentStops)
	if err != nil {
		return 0, bucket.DatabaseError(err)
	}
	if maxConcurrentStops < 0 {
		return 0, nil
	}
	return maxConcurrentStops, nil
}

func GetRestartPolicy(tx *sql.Tx, job string) (string, error) {
	var policy string
	row := tx.QueryRow("SELECT restart_policy FROM jobs WHERE name = ?", job)
	err := row.Scan(&policy)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return workspace.NormalizeRestartPolicy(policy)
}

func GetRestartGlobs(tx *sql.Tx, job string) ([]string, error) {
	var raw string
	row := tx.QueryRow("SELECT restart_globs FROM jobs WHERE name = ?", job)
	if err := row.Scan(&raw); err != nil {
		return nil, bucket.DatabaseError(err)
	}
	return workspace.ParseRestartGlobs(raw)
}

func GetReloadGlobs(tx *sql.Tx, job string) ([]string, error) {
	var raw string
	row := tx.QueryRow("SELECT reload_globs FROM jobs WHERE name = ?", job)
	if err := row.Scan(&raw); err != nil {
		return nil, bucket.DatabaseError(err)
	}
	return workspace.ParseReloadGlobs(raw)
}

func GetJobsByDeploymentSeq(tx *sql.Tx, deploymentSeq int) ([]string, error) {
	// Only jobs still in the catalog with active allocations. After a workspace job folder
	// is removed, build deletes the job row and marks allocations removed=1 until gc runs.
	rows, err := tx.Query(`
		SELECT DISTINCT a.job
		FROM allocations a
		INNER JOIN jobs j ON j.name = a.job
		WHERE j.deployment_seq = ? AND a.removed = 0`, deploymentSeq)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobNames := make([]string, 0)
	for rows.Next() {
		var jobName string
		if err := rows.Scan(&jobName); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobNames = append(jobNames, jobName)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobNames, nil
}

// DeployableJob identifies an active job and its build-computed deployment order.
type DeployableJob struct {
	Name            string
	DeploymentOrder int
	DeploymentSeq   int
}

// AssignDeploymentOrders sets contiguous zero-based deployment_order ranks on all
// catalog jobs, equivalent to sorting by deployment_priority DESC, deployment_seq
// ASC, then name ASC.
func AssignDeploymentOrders(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT name
		FROM jobs
		ORDER BY deployment_priority DESC, deployment_seq ASC, name ASC`)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return bucket.DatabaseError(err)
		}
		names = append(names, name)
	}
	if err := rowsErr(rows); err != nil {
		return err
	}

	for order, name := range names {
		if _, err := tx.Exec(`UPDATE jobs SET deployment_order = ? WHERE name = ?`, order, name); err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

// GetDeployableJobsInOrder returns active jobs sorted by persisted deployment_order.
func GetDeployableJobsInOrder(tx *sql.Tx) ([]DeployableJob, error) {
	rows, err := tx.Query(`
		SELECT j.name, j.deployment_order, j.deployment_seq
		FROM jobs j
		INNER JOIN allocations a ON a.job = j.name
		WHERE a.removed = 0
		GROUP BY j.name, j.deployment_order, j.deployment_seq
		ORDER BY j.deployment_order ASC, j.name ASC`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobs := make([]DeployableJob, 0)
	for rows.Next() {
		var job DeployableJob
		if err := rows.Scan(&job.Name, &job.DeploymentOrder, &job.DeploymentSeq); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobs = append(jobs, job)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobs, nil
}

func GetAllAllocatedJobs(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT DISTINCT job FROM allocations`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobNames := make([]string, 0)
	for rows.Next() {
		var jobName string
		if err := rows.Scan(&jobName); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobNames = append(jobNames, jobName)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobNames, nil
}

// GetDeployableJobs returns distinct non-removed allocated job names.
func GetDeployableJobs(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT DISTINCT job FROM allocations WHERE removed = 0 ORDER BY job`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobNames := make([]string, 0)
	for rows.Next() {
		var jobName string
		if err := rows.Scan(&jobName); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobNames = append(jobNames, jobName)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobNames, nil
}

// allocationDeployedSQL is the hash-table predicate for an allocation with any
// applied hash, including health_failed (used by GetDeployedJobs).
const allocationDeployedSQL = `ifnull(h.applied_hash, '') != ''`

// allocationLiveSQL is a promoted allocation that passed post-batch health
// (excludes the health_failed sentinel so deploy gates skip those rows).
const allocationLiveSQL = `ifnull(h.applied_hash, '') != '' AND h.applied_hash != '` + HealthFailedAppliedHash + `'`

// GetDeployedJobs returns jobs with at least one promoted allocation (hash.applied_hash set).
func GetDeployedJobs(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`
		SELECT DISTINCT a.job
		FROM allocations a
		JOIN allocation_hashes h ON h.alloc_id = a.alloc_id
		WHERE a.removed = 0 AND ` + allocationDeployedSQL + `
		ORDER BY a.job`)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobNames := make([]string, 0)
	for rows.Next() {
		var jobName string
		if err := rows.Scan(&jobName); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobNames = append(jobNames, jobName)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobNames, nil
}

// GetDeployedJobsByDeploymentSeq returns promoted jobs in a deployment sequence wave.
func GetDeployedJobsByDeploymentSeq(tx *sql.Tx, deploymentSeq int) ([]string, error) {
	rows, err := tx.Query(`
		SELECT DISTINCT a.job
		FROM allocations a
		INNER JOIN jobs j ON j.name = a.job
		JOIN allocation_hashes h ON h.alloc_id = a.alloc_id
		WHERE j.deployment_seq = ? AND a.removed = 0 AND `+allocationDeployedSQL+`
		ORDER BY a.job`, deploymentSeq)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	jobNames := make([]string, 0)
	for rows.Next() {
		var jobName string
		if err := rows.Scan(&jobName); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		jobNames = append(jobNames, jobName)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return jobNames, nil
}

// GetDeployedActiveAllocations returns active allocations that passed post-batch
// health (promoted hash, not health_failed). Deploy gates use this list.
func GetDeployedActiveAllocations(tx *sql.Tx, job string) ([]string, error) {
	return queryActiveAllocationWorkers(tx, job, allocationLiveSQL)
}

// GetHealthCheckActiveAllocations returns active allocations with any applied hash,
// including health_failed, for standalone kive health-check.
func GetHealthCheckActiveAllocations(tx *sql.Tx, job string) ([]string, error) {
	return queryActiveAllocationWorkers(tx, job, allocationDeployedSQL)
}

func queryActiveAllocationWorkers(tx *sql.Tx, job, hashPredicate string) ([]string, error) {
	rows, err := tx.Query(`
		SELECT a.worker_ip
		FROM allocations a
		JOIN allocation_hashes h ON h.alloc_id = a.alloc_id
		WHERE a.job = ? AND a.removed = 0 AND a.disabled = 0 AND `+hashPredicate,
		job)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	workerIPs := make([]string, 0)
	for rows.Next() {
		var workerIP string
		if err := rows.Scan(&workerIP); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		workerIPs = append(workerIPs, workerIP)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return workerIPs, nil
}

func isPrometheusWorkspacePath(filePath string) bool {
	return strings.Contains(filePath, "/_prometheus/")
}
