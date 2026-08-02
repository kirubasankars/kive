// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
	"kive/jobsign"
	"kive/schedule"
	"kive/utils"
	"kive/workspace"
)

// JobSignatureAudit describes the provenance accepted for one built job.
type JobSignatureAudit struct {
	Job    string
	Status string
	Signer string
	Digest string
}

// BuildJobsOptions controls optional job build reporting.
type BuildJobsOptions struct {
	Audits *[]JobSignatureAudit
}

func BuildJobs(tx *sql.Tx, jobWorkspace *workspace.DefaultWorkspace) ([]string, error) {
	return BuildJobsWithOptions(tx, jobWorkspace, BuildJobsOptions{})
}

// BuildJobsWithOptions builds jobs from one immutable snapshot per job.
func BuildJobsWithOptions(tx *sql.Tx, jobWorkspace *workspace.DefaultWorkspace, options BuildJobsOptions) ([]string, error) {
	workspaceJobNames, err := jobWorkspace.GetJobs()
	if err != nil {
		return nil, err
	}
	workspaceJobNames = sortedJobNames(workspaceJobNames)

	snapshots := make(map[string]jobsign.Snapshot, len(workspaceJobNames))
	signatures := make(map[string]jobsign.Verification, len(workspaceJobNames))
	manifests := make(map[string]workspace.Manifest, len(workspaceJobNames))
	var trustBundle []byte
	var trustErr error
	trustLoaded := false
	for _, jobName := range workspaceJobNames {
		snapshot, err := jobsign.Capture(jobName)
		if err != nil {
			return nil, err
		}
		snapshots[jobName] = snapshot
		filePaths := make([]string, 0, len(snapshot.Files))
		for _, file := range snapshot.Files {
			if !file.IsDir {
				filePaths = append(filePaths, file.Path)
			}
			if workspace.IsReservedJobRuntimePath(jobName, file.Path) {
				return nil, fmt.Errorf("%w: job %s, %s is reserved for runtime use on workers", bucket.ErrInvalidJob, jobName, path.Base(file.Path))
			}
		}
		_, signaturePresent := snapshot.Signature()
		if !signaturePresent {
			signatures[jobName] = jobsign.Verification{Status: "unsigned"}
		} else {
			if !trustLoaded {
				trustLoaded = true
				conf, err := bucket.GetKiveConf()
				if err != nil {
					return nil, err
				}
				trustBundle, trustErr = bucket.LoadJobSignerTrustBundle(conf)
			}
			if trustErr != nil {
				signatures[jobName] = jobsign.Verification{Status: "invalid"}
			} else {
				verification, err := jobsign.Verify(snapshot, trustBundle)
				if err != nil {
					signatures[jobName] = jobsign.Verification{Status: "invalid"}
				} else {
					signatures[jobName] = verification
				}
			}
		}
		if err := workspace.CheckTemplatePathConflicts(jobName, filePaths); err != nil {
			return nil, err
		}
		if err := workspace.ValidateRequiredJobFilePaths(jobName, filePaths); err != nil {
			return nil, err
		}
		manifestBytes, err := snapshot.ManifestBytes()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", bucket.ErrInvalidManifest, err)
		}
		manifest, err := workspace.ParseJobConf(jobName+"/"+workspace.JobConfName, manifestBytes)
		if err != nil {
			return nil, fmt.Errorf("%w: job %s\n%w", bucket.ErrInvalidManifest, jobName, err)
		}
		manifests[jobName] = manifest
	}

	portAllocator, err := buildPortAllocator(tx, workspaceJobNames, manifests)
	if err != nil {
		return nil, err
	}

	defaultTimezone, err := bucket.TimezoneFromConfig()
	if err != nil {
		return nil, err
	}

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return nil, err
	}

	for _, jobName := range workspaceJobNames {
		var jobID string
		err := tx.QueryRow(`SELECT job_id FROM jobs WHERE name = ?`, jobName).Scan(&jobID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, bucket.DatabaseError(err)
		}
		if jobID == "" {
			jobID, err = data.MintUniquePublicID(bucketID+"|"+jobName, func(id string) error {
				var other string
				qerr := tx.QueryRow(`SELECT name FROM jobs WHERE job_id = ?`, id).Scan(&other)
				if errors.Is(qerr, sql.ErrNoRows) {
					return nil
				}
				if qerr != nil {
					return bucket.DatabaseError(qerr)
				}
				if other != jobName {
					return fmt.Errorf("unique constraint")
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}

		purgeChildTableQueries := []string{
			"DELETE FROM job_selectors WHERE job_id = ?",
			"DELETE FROM hooks WHERE job_id = ?",
			"DELETE FROM job_files WHERE job_id = ?",
			"DELETE FROM job_certs WHERE job_id = ?",
		}

		for _, stmt := range purgeChildTableQueries {
			_, err := tx.Exec(stmt, jobID)
			if err != nil {
				return nil, bucket.DatabaseError(err)
			}
		}

		manifest := manifests[jobName]

		minCPUMHZ, err := utils.ExtractCPUFrequencyInMHz(workspace.GetMinCPU(manifest))
		if err != nil {
			return nil, fmt.Errorf("%w: job %s %w", bucket.ErrInvalidManifest, jobName, err)
		}
		maxCPUMHZ, err := utils.ExtractCPUFrequencyInMHz(workspace.GetMaxCPU(manifest))
		if err != nil {
			return nil, fmt.Errorf("%w: job %s %w", bucket.ErrInvalidManifest, jobName, err)
		}

		if minCPUMHZ != 0 && maxCPUMHZ == 0 {
			maxCPUMHZ = minCPUMHZ
		}
		if minCPUMHZ > maxCPUMHZ {
			return nil, fmt.Errorf("%w: job %s minCPUMhz > maxCPUMhz", bucket.ErrInvalidManifest, jobName)
		}
		cpuShares := manifest.Resources.CPUShares
		if err := validateCPUShares(cpuShares); err != nil {
			return nil, fmt.Errorf("%w: job %s resources.cpu_shares: %w", bucket.ErrInvalidManifest, jobName, err)
		}

		minMemoryMB, err := utils.ExtractSizeInMB(workspace.GetMinMemory(manifest))
		if err != nil {
			return nil, fmt.Errorf("%w: job %s %w", bucket.ErrInvalidManifest, jobName, err)
		}
		maxMemoryMB, err := utils.ExtractSizeInMB(workspace.GetMaxMemory(manifest))
		if err != nil {
			return nil, fmt.Errorf("%w: job %s %w", bucket.ErrInvalidManifest, jobName, err)
		}
		if minMemoryMB != 0 && maxMemoryMB == 0 {
			maxMemoryMB = minMemoryMB
		}
		if minMemoryMB > maxMemoryMB {
			return nil, fmt.Errorf("%w: job %s minMemoryMb > maxMemoryMb", bucket.ErrInvalidManifest, jobName)
		}

		bucketJobsConfigFile, jobConfig, err := loadJobBucketConfig(jobName)
		if err != nil {
			return nil, err
		}

		requestedMemoryMB := maxMemoryMB
		if _, ok := jobConfig["memory"]; ok {
			requestedMemoryMB, err = utils.ExtractSizeInMB(jobConfig["memory"])
			if err != nil {
				return nil, err
			}

			if minMemoryMB == 0 && maxMemoryMB == 0 {
				minMemoryMB = requestedMemoryMB
				maxMemoryMB = requestedMemoryMB
			}

			if requestedMemoryMB > maxMemoryMB {
				return nil, fmt.Errorf("%w: %s, job %s max_memory_mb %.2f mb, requested %.2f mb", bucket.ErrUnsupportedResourceConfiguration, bucketJobsConfigFile, jobName, maxMemoryMB, requestedMemoryMB)
			}
			if requestedMemoryMB < minMemoryMB {
				return nil, fmt.Errorf("%w: %s, job %s min_memory_mb %.2f mb, requested %.2f mb", bucket.ErrUnsupportedResourceConfiguration, bucketJobsConfigFile, jobName, minMemoryMB, requestedMemoryMB)
			}
		}

		requestedCPUMHz := maxCPUMHZ
		if _, ok := jobConfig["cpu"]; ok {
			requestedCPUMHz, err = utils.ExtractCPUFrequencyInMHz(jobConfig["cpu"])
			if err != nil {
				return nil, err
			}

			if minCPUMHZ == 0 && maxCPUMHZ == 0 {
				minCPUMHZ = requestedCPUMHz
				maxCPUMHZ = requestedCPUMHz
			}

			if requestedCPUMHz > maxCPUMHZ {
				return nil, fmt.Errorf("%w: %s, job %s max_cpu_mhz %.2f mhz, requested %.2f mhz", bucket.ErrUnsupportedResourceConfiguration, bucketJobsConfigFile, jobName, maxCPUMHZ, requestedCPUMHz)
			}
			if requestedCPUMHz < minCPUMHZ {
				return nil, fmt.Errorf("%w: %s, job %s min_cpu_mhz %.2f mhz, requested %.2f mhz", bucket.ErrUnsupportedResourceConfiguration, bucketJobsConfigFile, jobName, minCPUMHZ, requestedCPUMHz)
			}
		}

		memorySource := "manifest"
		if _, ok := jobConfig["memory"]; ok {
			memorySource = bucketJobsConfigFile
		}

		cpuSource := "manifest"
		if _, ok := jobConfig["cpu"]; ok {
			cpuSource = bucketJobsConfigFile
		}
		cpuSharesSource := "manifest"
		if rawCPUShares, ok := jobConfig["cpu_shares"]; ok {
			cpuShares, err = strconv.Atoi(strings.TrimSpace(rawCPUShares))
			if err != nil {
				return nil, fmt.Errorf(
					"%w: %s, job %s cpu_shares must be an integer: %q",
					bucket.ErrUnsupportedResourceConfiguration, bucketJobsConfigFile, jobName, rawCPUShares,
				)
			}
			if err := validateCPUShares(cpuShares); err != nil {
				return nil, fmt.Errorf(
					"%w: %s, job %s cpu_shares: %w",
					bucket.ErrUnsupportedResourceConfiguration, bucketJobsConfigFile, jobName, err,
				)
			}
			cpuSharesSource = bucketJobsConfigFile
		}

		if err := workspace.ValidateHealthCheck(jobName, manifest); err != nil {
			return nil, err
		}
		if err := workspace.ValidateRestartPolicy(jobName, manifest); err != nil {
			return nil, err
		}
		if err := workspace.ValidateRestartGlobs(jobName, manifest); err != nil {
			return nil, err
		}
		if manifest.MinAllocationsCount < 0 {
			return nil, fmt.Errorf("%w: job %s min_allocations_count must be >= 0", bucket.ErrInvalidManifest, jobName)
		}
		healthCheckJSON := ""
		if manifest.HealthCheck != nil {
			manifest.HealthCheck.Normalize()
			if manifest.HealthCheck.HasAnyProbes() {
				encoded, err := json.Marshal(manifest.HealthCheck)
				if err != nil {
					return nil, fmt.Errorf("%w: job %s health_check: %w", bucket.ErrInvalidManifest, jobName, err)
				}
				healthCheckJSON = string(encoded)
			}
		}

		version := workspace.GetVersion(manifest)
		restartPolicy, err := workspace.NormalizeRestartPolicy(manifest.RestartPolicy)
		if err != nil {
			return nil, fmt.Errorf("%w: job %s %w", bucket.ErrInvalidManifest, jobName, err)
		}
		restartGlobsJSON, err := workspace.EncodeRestartGlobs(manifest.RestartGlobs)
		if err != nil {
			return nil, fmt.Errorf("%w: job %s %w", bucket.ErrInvalidManifest, jobName, err)
		}
		reloadGlobsJSON, err := workspace.EncodeReloadGlobs(manifest.ReloadGlobs)
		if err != nil {
			return nil, fmt.Errorf("%w: job %s %w", bucket.ErrInvalidManifest, jobName, err)
		}
		signature := signatures[jobName]
		upsertJobQuery := `
			INSERT OR REPLACE INTO jobs (job_id, name, version, deployment_priority, min_memory_mb, max_memory_mb, current_memory_mb, current_memory_source, min_cpu_mhz, max_cpu_mhz, current_cpu_mhz, current_cpu_source, cpu_shares, cpu_shares_source, max_concurrent_restarts, max_concurrent_starts, max_concurrent_stops, restart_policy, restart_globs, reload_globs, health_check, signature_status, signature_signer, signature_digest)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err = tx.Exec(
			upsertJobQuery, jobID, jobName, version, manifest.DeploymentPriority,
			formatResourceQuantity(minMemoryMB),
			formatResourceQuantity(maxMemoryMB),
			formatResourceQuantity(requestedMemoryMB),
			memorySource,
			formatResourceQuantity(minCPUMHZ),
			formatResourceQuantity(maxCPUMHZ),
			formatResourceQuantity(requestedCPUMHz),
			cpuSource,
			cpuShares,
			cpuSharesSource,
			workspace.GetMaxConcurrentRestarts(manifest),
			workspace.GetMaxConcurrentStarts(manifest),
			workspace.GetMaxConcurrentStops(manifest),
			restartPolicy,
			restartGlobsJSON,
			reloadGlobsJSON,
			healthCheckJSON,
			signature.Status,
			signature.Signer,
			signature.Digest,
		)
		if err != nil {
			return nil, bucket.DatabaseError(err)
		}

		for _, selector := range workspace.PlacementSelectors(jobName, manifest) {
			_, err := tx.Exec("INSERT INTO job_selectors (job_id, selector) VALUES (?, ?)", jobID, selector)
			if err != nil {
				return nil, bucket.DatabaseError(err)
			}
		}

		if err := syncPorts(tx, jobID, jobName, manifest.Resources.Ports, portAllocator); err != nil {
			return nil, err
		}

		for _, hook := range manifest.ListedHooks() {
			if !strings.HasPrefix(hook.Name, "hook_") {
				return nil, fmt.Errorf("%w: invalid hook name, name: %s, excepted prefix : %s", bucket.ErrInvalidHookConfiguration, hook.Name, "hook_")
			}
			if len(hook.ExecutedOn) == 0 {
				return nil, fmt.Errorf("%w: job %s, hook %s missing executed_on", bucket.ErrInvalidHookConfiguration, jobName, hook.Name)
			}
			invalidExecutedOnEvents := utils.Difference(hook.ExecutedOn, []string{
				"post_build", "liveness", "readiness", "cli", "pre_deploy", "post_deploy", "job_control",
				"after_allocation_started", "after_allocation_stopped", "after_allocation_restarted",
			})
			if len(invalidExecutedOnEvents) > 0 {
				return nil, fmt.Errorf("%w: job %s, hook %s invalid executed_on %v", bucket.ErrInvalidHookConfiguration, jobName, hook.Name, invalidExecutedOnEvents)
			}
			if hook.Schedule != nil {
				schedule.ApplyDefaultTimezone(hook.Schedule, defaultTimezone)
				if err := schedule.Normalize(hook.Schedule, "schedule"); err != nil {
					return nil, fmt.Errorf("%w: job %s, hook %s: %v", bucket.ErrInvalidHookConfiguration, jobName, hook.Name, err)
				}
				hasCLI := false
				for _, ev := range hook.ExecutedOn {
					if ev == "cli" {
						hasCLI = true
						break
					}
				}
				if !hasCLI {
					return nil, fmt.Errorf("%w: job %s, hook %s schedule requires executed_on(\"cli\")", bucket.ErrInvalidHookConfiguration, jobName, hook.Name)
				}
			}
			if hook.Demands.Job == jobName {
				return nil, fmt.Errorf("%w: job %s, hook %s invalid configuration, self referencing", bucket.ErrInvalidHookConfiguration, jobName, hook.Name)
			}
			if err := workspace.ValidateHookDemand(jobName, hook.Name, hook); err != nil {
				return nil, err
			}
			hooksDir := path.Join(bucket.WorkspaceLocation, "jobs", jobName, "_hooks")
			if _, _, err := hooks.ResolveHookScript(hooksDir, hook.Name); err != nil {
				return nil, fmt.Errorf("job %s hook %s: %w", jobName, hook.Name, err)
			}

			scheduleJSON := ""
			if hook.Schedule != nil {
				b, err := json.Marshal(hook.Schedule)
				if err != nil {
					return nil, err
				}
				scheduleJSON = string(b)
			}

			insertHookQuery := `
				INSERT INTO hooks (job_id, job, name, executed_on, demand_job, demand_hook, demand_config, description, schedule)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`
			for _, executedOn := range hook.ExecutedOn {
				demandConfigJSON, err := json.Marshal(hook.Demands.Config)
				if err != nil {
					return nil, err
				}

				_, err = tx.Exec(insertHookQuery, jobID, jobName, hook.Name, executedOn, hook.Demands.Job, hook.Demands.Hook, string(demandConfigJSON), hook.Description, scheduleJSON)
				if err != nil {
					return nil, bucket.DatabaseError(err)
				}
			}
		}

		insertJobFileQuery := `INSERT INTO job_files (job_id, path, content, isdir) VALUES (?, ?, ?, ?)`
		for _, file := range snapshots[jobName].Files {
			if workspace.IsReservedJobRuntimePath(jobName, file.Path) {
				return nil, fmt.Errorf("%w: job %s, %s is reserved for runtime use on workers", bucket.ErrInvalidJob, jobName, path.Base(file.Path))
			}
			_, err = tx.Exec(insertJobFileQuery, jobID, file.Path, file.Content, file.IsDir)
			if err != nil {
				return nil, bucket.DatabaseError(err)
			}
		}

		// hash update manifest's cert config (+ external PEM content)
		pendingHash, err := certsBuildHash(jobName, manifest.Certs)
		if err != nil {
			return nil, err
		}

		err = data.UpdateHash(tx, "build_certs", jobName, pendingHash)
		if err != nil {
			return nil, err
		}

		insertJobCertQuery := `INSERT INTO job_certs (job_id, name, pkcs8, one, subject, external) VALUES (?, ?, ?, ?, ?, ?)`
		for name, config := range manifest.Certs {
			subject, err := json.Marshal(config.Subject)
			if err != nil {
				return nil, err
			}
			external := 0
			if config.External {
				external = 1
			}
			_, err = tx.Exec(insertJobCertQuery, jobID, name, config.PKCS8, config.One, subject, external)
			if err != nil {
				return nil, bucket.DatabaseError(err)
			}
		}
	}

	databaseJobNames, err := data.GetJobs(tx)
	if err != nil {
		return nil, err
	}

	jobsToRemove := utils.Difference(databaseJobNames, workspaceJobNames)
	removedJobs := make([]string, 0, len(jobsToRemove))
	for _, jobName := range jobsToRemove {
		removedJobs = append(removedJobs, jobName)
		jobID := workspace.GetHashUUID(jobName)
		for _, stmt := range []string{
			"DELETE FROM job_ports WHERE job_id = ?",
			"DELETE FROM job_selectors WHERE job_id = ?",
			"DELETE FROM hooks WHERE job_id = ?",
			"DELETE FROM job_files WHERE job_id = ?",
			"DELETE FROM job_certs WHERE job_id = ?",
		} {
			if _, err := tx.Exec(stmt, jobID); err != nil {
				return nil, bucket.DatabaseError(err)
			}
		}
		_, err := tx.Exec("DELETE FROM jobs WHERE name = ?", jobName)
		if err != nil {
			return nil, bucket.DatabaseError(err)
		}
		_, err = tx.Exec("DELETE FROM build_hashes WHERE namespace = 'build_certs' AND key = ?", jobName)
		if err != nil {
			return nil, bucket.DatabaseError(err)
		}
	}
	if options.Audits != nil {
		for _, jobName := range workspaceJobNames {
			signature := signatures[jobName]
			*options.Audits = append(*options.Audits, JobSignatureAudit{
				Job: jobName, Status: signature.Status, Signer: signature.Signer, Digest: signature.Digest,
			})
		}
	}
	return removedJobs, nil
}

func validateCPUShares(cpuShares int) error {
	if cpuShares == 0 {
		return nil
	}
	if cpuShares < 2 || cpuShares > 262144 {
		return fmt.Errorf("must be 0 (unset) or between 2 and 262144, got %d", cpuShares)
	}
	return nil
}

// formatResourceQuantity stores memory/CPU as whole MB/MHz for KV and templates
// that call int(get ...). Fractional inputs (e.g. "1.4 gb" → 1433.6) are rounded.
func formatResourceQuantity(v float64) string {
	return strconv.FormatInt(int64(math.Round(v)), 10)
}

// certsBuildHash hashes the certs manifest plus content of external PEM files
// so edits to jobs/<job>/certs/<name>.{crt,key} invalidate build_certs.
func certsBuildHash(jobName string, certs map[string]workspace.ManifestCert) (string, error) {
	certData, err := json.Marshal(certs)
	if err != nil {
		return "", err
	}
	material := append([]byte(nil), certData...)

	names := make([]string, 0, len(certs))
	for name, cfg := range certs {
		if cfg.External {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		crtPath, keyPath := workspace.ExternalCertPaths(jobName, name)
		crtHash, err := utils.CalculateFileMD5(crtPath)
		if err != nil {
			return "", fmt.Errorf("%w: job %s external cert %s: %w", bucket.ErrInvalidJob, jobName, name+".crt", err)
		}
		keyHash, err := utils.CalculateFileMD5(keyPath)
		if err != nil {
			return "", fmt.Errorf("%w: job %s external cert %s: %w", bucket.ErrInvalidJob, jobName, name+".key", err)
		}
		material = append(material, []byte(name+"\x00"+crtHash+"\x00"+keyHash+"\x00")...)
	}
	return utils.MD5Content(material)
}
