// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"kive/bucket"
	"kive/certs"
	"kive/conf"
	"kive/data"
	"kive/kv"
	"kive/utils"
	"kive/workspace"
)

func BuildVariables(tx *sql.Tx, jobWorkspace workspace.Workspace, removedWorkers, removedJobs []string, deleteSecretsKV bool) error {
	workerMeta, err := workerMetaByIP(jobWorkspace)
	if err != nil {
		return err
	}
	if err := buildWorkerVariables(tx, workerMeta, removedWorkers); err != nil {
		return err
	}
	if err := buildJobVariables(tx, removedJobs, deleteSecretsKV); err != nil {
		return err
	}
	if err := buildSharedWorkerVariables(tx); err != nil {
		return err
	}
	return buildBucketVariables(tx)
}

func workerMetaByIP(jobWorkspace workspace.Workspace) (map[string]workspace.Worker, error) {
	workers, err := jobWorkspace.ListWorkers()
	if err != nil {
		return nil, err
	}
	meta := make(map[string]workspace.Worker, len(workers))
	for _, worker := range workers {
		meta[worker.Host] = worker
	}
	return meta, nil
}

func buildWorkerVariables(tx *sql.Tx, workerMeta map[string]workspace.Worker, removedWorkers []string) error {
	workers, err := data.GetWorkers(tx, nil)
	if err != nil {
		return err
	}

	for _, workerIP := range workers {
		variables := make(map[string]string)

		workerID, err := data.GetWorkerID(tx, workerIP)
		if err != nil {
			return err
		}

		variables["worker_ip"] = workerIP
		variables["worker_id"] = workerID

		workerLabels, err := data.GetWorkerLabels(tx, workerID)
		if err != nil {
			return err
		}
		variables["labels"] = strings.Join(workerLabels, ",")

		for _, label := range workerLabels {
			labelWorkers, err := data.GetWorkers(tx, []string{label})
			if err != nil {
				return err
			}
			peerWorkerIPs := utils.Difference(labelWorkers, []string{workerIP})
			if len(peerWorkerIPs) > 0 {
				variables[fmt.Sprintf("%s_peers", label)] = strings.Join(peerWorkerIPs, ",")
			}
		}

		labels, err := data.GetLabels(tx)
		if err != nil {
			return err
		}

		for _, label := range labels {
			labelWorkers, err := data.GetWorkers(tx, []string{label})
			if err != nil {
				return err
			}

			allocationIndex := -1
			for i, worker := range labelWorkers {
				if worker == workerIP {
					allocationIndex = i
					break
				}
			}
			if allocationIndex >= 0 {
				variables[fmt.Sprintf("%s_allocation_index", label)] = strconv.Itoa(allocationIndex)
			}
		}

		availableCPUMHz, err := data.GetWorkerCPU(tx, workerIP)
		if err != nil {
			return err
		}
		variables["worker_cpu_mhz"] = availableCPUMHz

		availableMemoryMB, err := data.GetWorkerMemory(tx, workerIP)
		if err != nil {
			return err
		}
		variables["worker_memory_mb"] = availableMemoryMB

		allocatedJobNames, err := data.GetActiveAllocatedJobs(tx, workerIP)
		if err != nil {
			return err
		}
		variables["jobs"] = strings.Join(allocatedJobNames, ",")

		position, err := data.GetWorkerPosition(tx, workerIP)
		if err != nil {
			return err
		}
		variables["position"] = strconv.Itoa(position)
		if worker, ok := workerMeta[workerIP]; ok && worker.Hostname != "" {
			variables["hostname"] = worker.Hostname
		}

		workerNamespace := fmt.Sprintf("kive/worker/%s", workerIP)
		err = syncKeyValues(tx, workerNamespace, variables)
		if err != nil {
			return err
		}

		tags, err := data.GetWorkerTags(tx, workerID)
		if err != nil {
			return err
		}
		tagsNamespace := fmt.Sprintf("kive/worker/%s/tags", workerIP)
		err = syncKeyValues(tx, tagsNamespace, tags)
		if err != nil {
			return err
		}
	}

	for _, workerIP := range removedWorkers {
		namespaces := []string{fmt.Sprintf("kive/worker/%s", workerIP)}
		allocatedJobNames, err := data.GetAllocatedJobs(tx, workerIP)
		if err != nil {
			return err
		}

		for _, jobName := range allocatedJobNames {
			namespaces = append(namespaces, fmt.Sprintf("kive/job/%s/worker/%s", jobName, workerIP))
		}

		for _, namespace := range namespaces {
			keys, err := kv.GetKVStore().GetKeys(namespace)
			if err != nil {
				return err
			}
			for _, key := range keys {
				err := kv.GetKVStore().Delete(namespace, key)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func buildSharedWorkerVariables(tx *sql.Tx) error {
	labels, err := data.GetLabels(tx)
	if err != nil {
		return err
	}

	sharedVariables := make(map[string]string)
	for _, label := range labels {
		labelWorkers, err := data.GetWorkers(tx, []string{label})
		if err != nil {
			return err
		}

		if len(labelWorkers) > 0 {
			sharedVariables[fmt.Sprintf("%s_label_id", label)] = workspace.GetHashUUID(label)
			sharedVariables[fmt.Sprintf("%s_workers", label)] = strings.Join(labelWorkers, ",")
			sharedVariables[fmt.Sprintf("%s_workers_length", label)] = strconv.Itoa(len(labelWorkers))
		}
		for idx, workerIP := range labelWorkers {
			sharedVariables[fmt.Sprintf("%s_%d", label, idx)] = workerIP
		}
	}

	caCertificate, err := os.ReadFile(path.Join(bucket.SecretLocation, "ca.crt"))
	if err != nil {
		return fmt.Errorf("%w: unable to read ca.crt", bucket.ErrUnexpectedError)
	}
	sharedVariables["certs/ca.crt"] = string(caCertificate)

	trustBundle, err := certs.ReadDedupedCATrustBundle()
	if err != nil {
		return fmt.Errorf("%w: unable to read ca-trust.crt", bucket.ErrUnexpectedError)
	}
	sharedVariables[certs.WorkerCATrustKVKey] = string(trustBundle)

	return syncKeyValues(tx, "kive/worker", sharedVariables)
}

func buildBucketVariables(tx *sql.Tx) error {
	bucketConfig, err := bucket.LoadBucketConfVars()
	if err != nil {
		return err
	}
	portRange, err := bucket.LoadPortRange()
	if err != nil {
		return err
	}
	bucketConfig["port_range"] = fmt.Sprintf("%d,%d", portRange.Min, portRange.Max)

	jobNames, err := data.GetJobs(tx)
	if err != nil {
		return err
	}

	kiveVariables := make(map[string]string)
	activeJobNames := make([]string, 0, len(jobNames))
	for _, jobName := range jobNames {
		hasActive, err := data.JobHasActiveAllocations(tx, jobName)
		if err != nil {
			return err
		}
		if hasActive {
			activeJobNames = append(activeJobNames, jobName)
		}
		hasNonRemoved, err := data.JobHasNonRemovedAllocations(tx, jobName)
		if err != nil {
			return err
		}
		if !hasNonRemoved {
			continue
		}
		portMap, err := data.GetPortMap(tx, jobName)
		if err != nil {
			return err
		}
		for name, port := range portMap {
			kiveVariables[name] = port
		}
	}
	kiveVariables["jobs"] = strings.Join(jobNames, ",")
	kiveVariables["active_jobs"] = strings.Join(activeJobNames, ",")
	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}
	kiveVariables["bucket_id"] = bucketID

	err = syncKeyValues(tx, "vars/bucket", bucketConfig)
	if err != nil {
		return err
	}
	err = syncKeyValues(tx, data.BucketKVNamespace, kiveVariables)
	if err != nil {
		return err
	}

	if err := storeBucketRestoreMaterial(tx); err != nil {
		return err
	}

	return nil
}

func storeBucketRestoreMaterial(tx *sql.Tx) error {
	kiveConf, err := os.ReadFile(bucket.KiveConfPath())
	if err != nil {
		return fmt.Errorf("read kive.conf for restore material: %w", err)
	}
	kvKey, err := os.ReadFile(path.Join(bucket.SecretLocation, "kv.key"))
	if err != nil {
		return fmt.Errorf("read secrets/kv.key for restore material: %w", err)
	}
	caKey, err := os.ReadFile(path.Join(bucket.SecretLocation, "ca.key"))
	if err != nil {
		return fmt.Errorf("read secrets/ca.key for restore material: %w", err)
	}
	caCrt, err := os.ReadFile(path.Join(bucket.SecretLocation, "ca.crt"))
	if err != nil {
		return fmt.Errorf("read secrets/ca.crt for restore material: %w", err)
	}

	workersJSON, err := readOptionalWorkspaceFile("workers.conf")
	if err != nil {
		return err
	}
	bucketConf, err := readOptionalWorkspaceFile("bucket.conf")
	if err != nil {
		return err
	}
	disabledJSON, err := readOptionalWorkspaceFile("disabled.conf")
	if err != nil {
		return err
	}
	bucketJobsConfs, err := packBucketJobsConfs()
	if err != nil {
		return err
	}
	knownHosts, err := readOptionalKnownHosts()
	if err != nil {
		return err
	}
	promotionJSON, err := readOptionalPromotionJSON()
	if err != nil {
		return err
	}
	webhookJSON, err := readOptionalWebhookJSON()
	if err != nil {
		return err
	}
	clickhouseJSON, err := readOptionalClickHouseJSON()
	if err != nil {
		return err
	}

	return data.SetBucketRestoreMaterial(tx, data.BucketRestoreMaterial{
		KiveConf:        string(kiveConf),
		KVKey:           string(kvKey),
		CAKey:           string(caKey),
		CACrt:           string(caCrt),
		WorkersJSON:     workersJSON,
		BucketConf:      bucketConf,
		DisabledJSON:    disabledJSON,
		BucketJobsConfs: bucketJobsConfs,
		KnownHosts:      knownHosts,
		PromotionJSON:   promotionJSON,
		WebhookJSON:     webhookJSON,
		ClickHouseJSON:  clickhouseJSON,
	})
}

func readOptionalWorkspaceFile(name string) (string, error) {
	p := path.Join(bucket.WorkspaceLocation, name)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read workspace/%s for restore material: %w", name, err)
	}
	return string(raw), nil
}

func readOptionalKnownHosts() (string, error) {
	p := path.Join(bucket.Location, ".ssh", "known_hosts")
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read .ssh/known_hosts for restore material: %w", err)
	}
	return string(raw), nil
}

func readOptionalPromotionJSON() (string, error) {
	raw, err := os.ReadFile(bucket.PromotionJSONPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read promotion.conf for restore material: %w", err)
	}
	return string(raw), nil
}

func readOptionalWebhookJSON() (string, error) {
	raw, err := os.ReadFile(bucket.WebhookConfPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read webhook.conf for restore material: %w", err)
	}
	return string(raw), nil
}

func readOptionalClickHouseJSON() (string, error) {
	raw, err := os.ReadFile(bucket.ObserveConfPath())
	if err != nil {
		if os.IsNotExist(err) {
			// Legacy filename still accepted at build time.
			raw, err = os.ReadFile(path.Join(bucket.Location, "clickhouse.conf"))
			if err != nil {
				if os.IsNotExist(err) {
					return "", nil
				}
				return "", fmt.Errorf("read clickhouse.conf for restore material: %w", err)
			}
			return string(raw), nil
		}
		return "", fmt.Errorf("read observe.conf for restore material: %w", err)
	}
	return string(raw), nil
}

func packBucketJobsConfs() (string, error) {
	entries, err := os.ReadDir(bucket.WorkspaceLocation)
	if err != nil {
		if os.IsNotExist(err) {
			return "{}", nil
		}
		return "", fmt.Errorf("list workspace for bucket.jobs*.conf: %w", err)
	}
	out := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name != "bucket.jobs.conf" && !(strings.HasPrefix(name, "bucket.jobs.") && strings.HasSuffix(name, ".conf")) {
			continue
		}
		raw, err := os.ReadFile(path.Join(bucket.WorkspaceLocation, name))
		if err != nil {
			return "", fmt.Errorf("read workspace/%s for restore material: %w", name, err)
		}
		out[name] = string(raw)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func loadJobBucketConfig(jobName string) (configFileName string, settings map[string]string, err error) {
	allJobSettings := make(map[string]map[string]string)

	bucketSettings, err := bucket.LoadBucketSettings()
	if err != nil {
		return "", nil, err
	}

	configFileName = "bucket.jobs.conf"
	profile := strings.TrimSpace(bucketSettings.JobsProfile)
	if profile != "" {
		profile, err = bucket.SanitizeJobsProfile(profile)
		if err != nil {
			return "", nil, err
		}
		configFileName = bucket.JobsProfileFileName(profile)
	}
	configFilePath := path.Join(bucket.WorkspaceLocation, configFileName)
	// Defense in depth: never follow a path that escapes workspace/.
	wsAbs, err := filepath.Abs(bucket.WorkspaceLocation)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", bucket.ErrUnexpectedError, err)
	}
	cfgAbs, err := filepath.Abs(configFilePath)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", bucket.ErrUnexpectedError, err)
	}
	rel, err := filepath.Rel(wsAbs, cfgAbs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", nil, fmt.Errorf("%w: job config path escapes workspace/", bucket.ErrInvalidBucketConf)
	}

	if _, err := os.Stat(configFilePath); err == nil {
		configFileData, err := conf.ReadBytes(configFilePath)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %w", bucket.ErrUnexpectedError, err)
		}

		parsed, err := bucket.ParseBucketJobsConf(configFilePath, configFileData)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", bucket.ErrInvalidBucketConf, err)
		}
		allJobSettings = parsed
	}

	if _, ok := allJobSettings[jobName]; !ok {
		allJobSettings[jobName] = make(map[string]string)
	}

	return configFileName, allJobSettings[jobName], nil
}

// jobShouldPurgeBuildKV reports whether build-owned job KV should be cleared.
// Jobs with no allocation rows, or with disabled-only rows, keep KV; jobs whose
// allocations are all removed are purged.
func jobShouldPurgeBuildKV(tx *sql.Tx, jobName string) (bool, error) {
	var total, nonRemoved int
	err := tx.QueryRow(`SELECT count(*) FROM allocations WHERE job = ?`, jobName).Scan(&total)
	if err != nil {
		return false, bucket.DatabaseError(err)
	}
	if total == 0 {
		return false, nil
	}
	err = tx.QueryRow(`SELECT count(*) FROM allocations WHERE job = ? AND removed = 0`, jobName).Scan(&nonRemoved)
	if err != nil {
		return false, bucket.DatabaseError(err)
	}
	return nonRemoved == 0, nil
}

func buildJobVariables(tx *sql.Tx, removedJobs []string, deleteSecretsKV bool) error {
	jobNames, err := data.GetJobs(tx)
	if err != nil {
		return err
	}

	for _, jobName := range jobNames {
		if deleteSecretsKV {
			hasActive, err := data.JobHasActiveAllocations(tx, jobName)
			if err != nil {
				return err
			}
			if !hasActive {
				if err := purgeHookKVNamespaces(tx, jobName); err != nil {
					return err
				}
			}
		}

		shouldPurge, err := jobShouldPurgeBuildKV(tx, jobName)
		if err != nil {
			return err
		}
		if shouldPurge {
			if err := purgeBuildJobKVNamespaces(tx, jobName); err != nil {
				return err
			}
			continue
		}

		workersForVars, err := data.GetActiveAllocationsOrdered(tx, jobName)
		if err != nil {
			return err
		}

		variables := make(map[string]string)

		jobID, err := data.GetJobID(tx, jobName)
		if err != nil {
			return err
		}
		variables["job_id"] = jobID
		variables["job_name"] = jobName

		version, err := data.GetJobVersion(tx, jobName)
		if err != nil {
			return err
		}
		variables["version"] = data.NormalizeDeployVersion(version)

		jobSelectors, err := data.GetJobSelectors(tx, jobName)
		if err != nil {
			return err
		}
		variables["selectors"] = strings.Join(jobSelectors, ",")

		minMemory, maxMemory, err := data.GetJobMemoryLimits(tx, jobName)
		if err != nil {
			return err
		}
		variables["min_memory_mb"] = minMemory
		variables["max_memory_mb"] = maxMemory

		minCPU, maxCPU, err := data.GetJobCPULimits(tx, jobName)
		if err != nil {
			return err
		}
		variables["min_cpu_mhz"] = minCPU
		variables["max_cpu_mhz"] = maxCPU

		_, bucketJobSettings, err := loadJobBucketConfig(jobName)
		if err != nil {
			return err
		}

		variables["memory"], err = data.GetJobMemory(tx, jobName)
		if err != nil {
			return err
		}
		if minMemory == "0" && maxMemory == "0" {
			variables["min_memory_mb"] = variables["memory"]
			variables["max_memory_mb"] = variables["memory"]
		}

		variables["cpu"], err = data.GetJobCPU(tx, jobName)
		if err != nil {
			return err
		}
		if minCPU == "0" && maxCPU == "0" {
			variables["min_cpu_mhz"] = variables["cpu"]
			variables["max_cpu_mhz"] = variables["cpu"]
		}

		variables["cpu_shares"], err = data.GetJobCPUShares(tx, jobName)
		if err != nil {
			return err
		}

		variables["workers"] = strings.Join(workersForVars, ",")
		variables["workers_length"] = strconv.Itoa(len(workersForVars))
		for idx, workerIP := range workersForVars {
			variables[fmt.Sprintf("worker_%d", idx)] = workerIP
		}
		variables["rollout_order"] = strings.Join(workersForVars, ",")

		jobNamespace := fmt.Sprintf("kive/job/%s", jobName)
		err = syncKeyValues(tx, jobNamespace, variables)
		if err != nil {
			return err
		}

		bucketJobNamespace := fmt.Sprintf("vars/bucket/job/%s", jobName)
		jobVarsSettings, err := loadWorkspaceJobVars(jobName)
		if err != nil {
			return err
		}
		err = syncKeyValues(tx, bucketJobNamespace, mergeJobBucketSettings(jobVarsSettings, bucketJobSettings))
		if err != nil {
			return err
		}
	}

	for _, jobName := range removedJobs {
		if err := purgeBuildJobKVNamespaces(tx, jobName); err != nil {
			return err
		}
		if err := purgeHookKVNamespaces(tx, jobName); err != nil {
			return err
		}
	}

	return nil
}

func purgeHookKVNamespaces(tx *sql.Tx, jobName string) error {
	for _, namespace := range data.HookKVNamespaces(jobName) {
		if err := syncKeyValues(tx, namespace, map[string]string{}); err != nil {
			return err
		}
	}
	return nil
}

func purgeBuildJobKVNamespaces(tx *sql.Tx, jobName string) error {
	namespaces := data.BuildJobKVNamespaces(jobName)
	allocatedWorkerIPs, err := data.GetAllocatedWorkers(tx, jobName)
	if err != nil {
		return err
	}
	for _, workerIP := range allocatedWorkerIPs {
		namespaces = append(namespaces, fmt.Sprintf("kive/job/%s/worker/%s", jobName, workerIP))
	}
	for _, namespace := range namespaces {
		if err := syncKeyValues(tx, namespace, map[string]string{}); err != nil {
			return err
		}
	}
	return nil
}

// loadWorkspaceJobVars reads jobs/<job>/vars.conf when present.
func loadWorkspaceJobVars(jobName string) (map[string]string, error) {
	varsPath := path.Join(bucket.WorkspaceLocation, "jobs", jobName, workspace.JobVarsFileName)
	data, err := conf.ReadBytes(varsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: read %s: %w", bucket.ErrUnexpectedError, varsPath, err)
	}

	vars, err := bucket.ParseVarsConf(varsPath, data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", bucket.ErrInvalidJobVars, err)
	}
	return vars, nil
}

// mergeJobBucketSettings combines vars.conf with bucket.jobs.conf; bucket.jobs.conf wins on conflict.
func mergeJobBucketSettings(varsConf, bucketJobs map[string]string) map[string]string {
	merged := make(map[string]string, len(varsConf)+len(bucketJobs))
	for key, value := range varsConf {
		merged[key] = value
	}
	for key, value := range bucketJobs {
		merged[key] = value
	}
	return merged
}

// syncKeyValues updates the in-memory KV session store only. Rows land in
// kive.db later via PersistToTransaction on the catalog buildTx — do not call
// PersistSession here (that would commit outside the open catalog TX).
func syncKeyValues(_ *sql.Tx, namespace string, keyValues map[string]string) error {
	var presentKeys []string
	for key, value := range keyValues {
		kv.GetKVStore().Put(namespace, key, value, 0)
		presentKeys = append(presentKeys, key)
	}

	existingKeys, err := kv.GetKVStore().GetKeys(namespace)
	if err != nil {
		return err
	}

	staleKeys := utils.Difference(existingKeys, presentKeys)
	for _, staleKey := range staleKeys {
		err := kv.GetKVStore().Delete(namespace, staleKey)
		if err != nil {
			return err
		}
	}

	return nil
}

// bucket.conf => vars/bucket
// workers meta => kive/worker, kive/worker/10.0.0.1
// worker tags => kive/worker/10.0.0.1/tags
// custom job variables (hooks) = vars/job/a
// vars.conf + bucket.jobs.conf => vars/bucket/job/a
// bucket.jobs.conf (memory, cpu) => kive/job/a
// job resources (memory and cpu) => kive/job/a
// ports (non-removed allocations, includes disabled) => kive/bucket
// active_jobs => kive/bucket
// job meta (workers, version, resources) => kive/job/a
// job certs => kive/job/a/worker/10.0.0.1
