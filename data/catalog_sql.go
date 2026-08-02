// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

// Catalog SELECT projections formerly exposed as cat_* SQLite views.
// Callers may query these directly or wrap them as subqueries.

const CatalogAllocationsSQL = `
SELECT a.alloc_id, a.worker_ip, a.job, a.disabled, a.removed, a.version,
	(SELECT wt.value FROM worker_tags wt
	 JOIN workers w ON w.worker_id = wt.worker_id
	 WHERE w.worker_ip = a.worker_ip AND wt.key = 'zone') AS zone
FROM allocations a ORDER BY job`

const CatalogJobsSQL = `
SELECT DISTINCT job_id, name, version,
	j.deployment_priority,
	j.deployment_order,
	(CASE WHEN (SELECT COUNT(1) FROM allocations wj WHERE j.name = wj.job AND wj.disabled = 0) > 0 THEN 0 ELSE 1 END) AS disabled,
	j.deployment_seq,
	ifnull((SELECT GROUP_CONCAT(selector) FROM job_selectors jl WHERE jl.job_id = j.job_id), '') as selectors,
	j.current_memory_mb, j.current_memory_source,
	j.current_cpu_mhz, j.current_cpu_source,
	j.cpu_shares, j.cpu_shares_source,
	j.signature_status, j.signature_signer, j.signature_digest
FROM jobs j ORDER BY deployment_order ASC, name ASC`

const CatalogHooksSQL = `
SELECT job, name as hook_name, executed_on, demand_job, demand_hook, demand_config,
	ifnull(description, '') as description, ifnull(schedule, '') as schedule
FROM hooks ORDER BY job, name`

const CatalogKVSQL = `
SELECT * FROM (
	SELECT namespace, key,
		(CASE
			WHEN value LIKE 'enc:v1:%' THEN '[encrypted]'
			WHEN LENGTH(value) > 50 THEN substr(value, 1, 50) || '...'
			ELSE value
		END) as value,
		max(version) as version, ttl, created_date, deleted
	FROM key_value GROUP BY namespace, key
) t ORDER BY namespace, key`

const CatalogWorkersSQL = `
SELECT w.worker_id, w.worker_ip, w.available_memory_mb, w.available_cpu_mhz, w.position,
	(SELECT group_concat(label) AS labels FROM worker_labels WHERE worker_id = w.worker_id) AS labels,
	(SELECT wt.value FROM worker_tags wt WHERE wt.worker_id = w.worker_id AND wt.key = 'zone') AS zone
FROM workers w ORDER BY position`

// CatalogDeploymentsSQL projects allocation plan hashes from allocation_hashes.
const CatalogDeploymentsSQL = `
SELECT a.alloc_id, a.worker_ip, a.job, a.disabled, a.removed,
       ifnull(h.pending_hash, '') AS pending_hash,
       ifnull(h.applied_hash, '') AS applied_hash,
       ifnull(h.applied_version, '') AS applied_version,
       ifnull(a.version, '') AS version
FROM allocations a
LEFT JOIN allocation_hashes h ON h.alloc_id = a.alloc_id`
