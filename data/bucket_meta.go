// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"kive/bucket"
	"kive/utils"
)

// BucketKVNamespace holds build-synced global bucket metadata (ports, jobs, bucket_id).
const BucketKVNamespace = "kive/bucket"

// BucketInitialized reports whether the bucket table has a bucket_id row.
func BucketInitialized(tx *sql.Tx) (bool, error) {
	var bucketID string
	err := tx.QueryRow(`SELECT bucket_id FROM bucket LIMIT 1`).Scan(&bucketID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, bucket.DatabaseError(err)
	}
	return bucketID != "", nil
}

// InsertBucketRecord creates the singleton bucket row for a new installation.
func InsertBucketRecord(tx *sql.Tx, bucketID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.Exec(
		`INSERT INTO bucket (bucket_id, generation, created_at, initialized_at) VALUES (?, 0, ?, ?)`,
		bucketID, now, now,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// BundleMeta identifies the binary and source bundle stored in a bucket DB.
type BundleMeta struct {
	InitGitHash   string
	BuildGitHash  string
	BundleVersion int
}

// BucketTimestamps holds wall-clock create and last-init times from the bucket row.
type BucketTimestamps struct {
	CreatedAt     string
	InitializedAt string
}

// SetInitGitHash records the binary used by the latest kive init and refreshes initialized_at.
func SetInitGitHash(tx *sql.Tx, hash string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.Exec(
		`UPDATE bucket SET init_git_hash = ?, initialized_at = ?`,
		strings.TrimSpace(hash), now,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// GetBucketTimestamps returns created_at and initialized_at from the singleton bucket row.
func GetBucketTimestamps(tx *sql.Tx) (BucketTimestamps, error) {
	var createdAt, initializedAt sql.NullString
	err := tx.QueryRow(
		`SELECT created_at, initialized_at FROM bucket LIMIT 1`,
	).Scan(&createdAt, &initializedAt)
	if err != nil {
		return BucketTimestamps{}, bucket.DatabaseError(err)
	}
	return BucketTimestamps{
		CreatedAt:     strings.TrimSpace(createdAt.String),
		InitializedAt: strings.TrimSpace(initializedAt.String),
	}, nil
}

// SetBundleMeta marks the catalog as a pushable source bundle produced by build.
func SetBundleMeta(tx *sql.Tx, version int, hash string) error {
	_, err := tx.Exec(
		`UPDATE bucket SET bundle_version = ?, build_git_hash = ?`,
		version, strings.TrimSpace(hash),
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// GetBundleMeta returns build identity stored on the singleton bucket row.
func GetBundleMeta(tx *sql.Tx) (BundleMeta, error) {
	var meta BundleMeta
	var initHash, buildHash sql.NullString
	err := tx.QueryRow(
		`SELECT init_git_hash, build_git_hash, bundle_version FROM bucket LIMIT 1`,
	).Scan(&initHash, &buildHash, &meta.BundleVersion)
	if err != nil {
		return BundleMeta{}, bucket.DatabaseError(err)
	}
	meta.InitGitHash = strings.TrimSpace(initHash.String)
	meta.BuildGitHash = strings.TrimSpace(buildHash.String)
	return meta, nil
}

func GetBucketID(tx *sql.Tx) (string, error) {
	var bucketID string
	err := tx.QueryRow(`SELECT bucket_id FROM bucket LIMIT 1`).Scan(&bucketID)
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return bucketID, nil
}

func GetBucketGeneration(tx *sql.Tx) (int, error) {
	var generation int
	err := tx.QueryRow(`SELECT generation FROM bucket`).Scan(&generation)
	if err != nil {
		return -1, bucket.DatabaseError(err)
	}
	return generation, nil
}

func SetBucketGeneration(tx *sql.Tx, generation int) error {
	_, err := tx.Exec(`UPDATE bucket SET generation = ?`, generation)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// BucketRestoreMaterial is operator restore content stored on the singleton bucket row.
type BucketRestoreMaterial struct {
	KiveConf        string
	KVKey           string
	CAKey           string
	CACrt           string
	WorkersJSON     string
	BucketConf      string
	DisabledJSON    string
	BucketJobsConfs string // JSON object: filename -> file contents
	KnownHosts      string
	PromotionJSON   string // optional root promotion.conf
	WebhookJSON     string // optional root webhook.conf
	ClickHouseJSON  string // optional root observe.conf (legacy column name)
}

// SetBucketRestoreMaterial stores kive.conf, CA/KV secrets, workspace root files, and known_hosts on the bucket row.
func SetBucketRestoreMaterial(tx *sql.Tx, material BucketRestoreMaterial) error {
	_, err := tx.Exec(
		`UPDATE bucket SET kive_conf = ?, kv_key = ?, ca_key = ?, ca_crt = ?,
		 workers_json = ?, bucket_conf = ?, disabled_json = ?, bucket_jobs_confs = ?, known_hosts = ?,
		 promotion_json = ?, webhook_json = ?, clickhouse_json = ?`,
		material.KiveConf,
		material.KVKey,
		material.CAKey,
		material.CACrt,
		material.WorkersJSON,
		material.BucketConf,
		material.DisabledJSON,
		material.BucketJobsConfs,
		material.KnownHosts,
		material.PromotionJSON,
		material.WebhookJSON,
		material.ClickHouseJSON,
	)
	if err != nil {
		return restoreMaterialColumnError(err)
	}
	return nil
}

// GetBucketRestoreMaterial returns restore material stored on the bucket row.
func GetBucketRestoreMaterial(tx *sql.Tx) (BucketRestoreMaterial, error) {
	var material BucketRestoreMaterial
	var kiveConf, kvKey, caKey, caCrt sql.NullString
	var workersJSON, bucketConf, disabledJSON, bucketJobsConfs, knownHosts, promotionJSON, webhookJSON, clickhouseJSON sql.NullString
	err := tx.QueryRow(
		`SELECT kive_conf, kv_key, ca_key, ca_crt,
		        workers_json, bucket_conf, disabled_json, bucket_jobs_confs, known_hosts,
		        promotion_json, webhook_json, clickhouse_json
		 FROM bucket LIMIT 1`,
	).Scan(
		&kiveConf, &kvKey, &caKey, &caCrt,
		&workersJSON, &bucketConf, &disabledJSON, &bucketJobsConfs, &knownHosts,
		&promotionJSON, &webhookJSON, &clickhouseJSON,
	)
	if err != nil && (strings.Contains(err.Error(), "no such column: promotion_json") ||
		strings.Contains(err.Error(), "no such column: webhook_json") ||
		strings.Contains(err.Error(), "no such column: clickhouse_json")) {
		// Older push revision DBs predate optional restore columns.
		err = tx.QueryRow(
			`SELECT kive_conf, kv_key, ca_key, ca_crt,
			        workers_json, bucket_conf, disabled_json, bucket_jobs_confs, known_hosts
			 FROM bucket LIMIT 1`,
		).Scan(
			&kiveConf, &kvKey, &caKey, &caCrt,
			&workersJSON, &bucketConf, &disabledJSON, &bucketJobsConfs, &knownHosts,
		)
		promotionJSON = sql.NullString{}
		webhookJSON = sql.NullString{}
		clickhouseJSON = sql.NullString{}
		if err == nil {
			var promoOnly, webhookOnly, clickOnly sql.NullString
			if promoErr := tx.QueryRow(`SELECT promotion_json FROM bucket LIMIT 1`).Scan(&promoOnly); promoErr == nil {
				promotionJSON = promoOnly
			}
			if whErr := tx.QueryRow(`SELECT webhook_json FROM bucket LIMIT 1`).Scan(&webhookOnly); whErr == nil {
				webhookJSON = webhookOnly
			}
			if chErr := tx.QueryRow(`SELECT clickhouse_json FROM bucket LIMIT 1`).Scan(&clickOnly); chErr == nil {
				clickhouseJSON = clickOnly
			}
		}
	}
	if err != nil {
		return BucketRestoreMaterial{}, restoreMaterialColumnError(err)
	}
	material.KiveConf = kiveConf.String
	material.KVKey = kvKey.String
	material.CAKey = caKey.String
	material.CACrt = caCrt.String
	material.WorkersJSON = workersJSON.String
	material.BucketConf = bucketConf.String
	material.DisabledJSON = disabledJSON.String
	material.BucketJobsConfs = bucketJobsConfs.String
	material.KnownHosts = knownHosts.String
	material.PromotionJSON = promotionJSON.String
	material.WebhookJSON = webhookJSON.String
	material.ClickHouseJSON = clickhouseJSON.String
	return material, nil
}

// WorkerCATrustKVKey is the key for the CA trust bundle under namespace kive/worker.
// Kept here (not via CatalogKVSQL) because the catalog KV projection truncates values.
const WorkerCATrustKVKey = "certs/ca-trust.crt"

// GetWorkerCATrust returns the latest non-deleted kive/worker certs/ca-trust.crt value.
// Empty string means the key is missing; callers may fall back to ca.crt.
func GetWorkerCATrust(tx *sql.Tx) (string, error) {
	var value sql.NullString
	err := tx.QueryRow(`
		SELECT value FROM (
			SELECT value, deleted, max(version) AS version
			FROM key_value
			WHERE namespace = 'kive/worker' AND key = ?
			GROUP BY namespace, key
		) WHERE deleted = 0
	`, WorkerCATrustKVKey).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", bucket.DatabaseError(err)
	}
	return value.String, nil
}

func restoreMaterialColumnError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "no such column: kive_conf") ||
		strings.Contains(msg, "no such column: kv_key") ||
		strings.Contains(msg, "no such column: ca_key") ||
		strings.Contains(msg, "no such column: ca_crt") ||
		strings.Contains(msg, "no such column: workers_json") ||
		strings.Contains(msg, "no such column: bucket_conf") ||
		strings.Contains(msg, "no such column: disabled_json") ||
		strings.Contains(msg, "no such column: bucket_jobs_confs") ||
		strings.Contains(msg, "no such column: known_hosts") ||
		strings.Contains(msg, "no such column: promotion_json") ||
		strings.Contains(msg, "no such column: webhook_json") ||
		strings.Contains(msg, "no such column: clickhouse_json") {
		return fmt.Errorf(
			"%w: database is missing bucket restore columns (incompatible schema); remove data/kive.db and run kive init",
			bucket.ErrSchemaUpgradeRequired,
		)
	}
	return bucket.DatabaseError(err)
}

func GetMaxDeploymentSeq(tx *sql.Tx) (int, error) {
	var maxSeq int
	err := tx.QueryRow(`SELECT ifnull(max(deployment_seq), 0) FROM jobs`).Scan(&maxSeq)
	if err != nil {
		return -1, bucket.DatabaseError(err)
	}
	return maxSeq, nil
}

// BuildJobKVNamespaces returns job KV namespaces owned by kive build (catalog sync).
func BuildJobKVNamespaces(job string) []string {
	return []string{
		fmt.Sprintf("kive/job/%s", job),
		fmt.Sprintf("vars/bucket/job/%s", job),
	}
}

// HookKVNamespaces returns job KV namespaces written by deploy/hooks.
func HookKVNamespaces(job string) []string {
	return []string{
		fmt.Sprintf("vars/job/%s", job),
		fmt.Sprintf("secrets/job/%s", job),
	}
}

// JobKVNamespaces returns all job-scoped KV namespaces purged by GC when a job has no active allocations.
func JobKVNamespaces(job string) []string {
	namespaces := BuildJobKVNamespaces(job)
	return append(namespaces, HookKVNamespaces(job)...)
}

func AllowedKVNamespaces(job, workerIP string) []string {
	return []string{
		BucketKVNamespace,
		"vars/bucket",
		"kive/worker",
		fmt.Sprintf("kive/worker/%s", workerIP),
		fmt.Sprintf("kive/worker/%s/tags", workerIP),
		fmt.Sprintf("kive/job/%s", job),
		fmt.Sprintf("vars/bucket/job/%s", job),
		fmt.Sprintf("vars/job/%s", job),
		fmt.Sprintf("secrets/job/%s", job),
		fmt.Sprintf("kive/job/%s/worker/%s", job, workerIP),
		"kive/prometheus",
	}
}

// UpstreamDemandKVNamespaces returns KV namespaces for jobs this job depends on via command demands.
func UpstreamDemandKVNamespaces(tx *sql.Tx, job string) ([]string, error) {
	rows, err := tx.Query(
		`SELECT DISTINCT demand_job FROM hooks
		 WHERE job = ? AND ifnull(trim(demand_job), '') != ''`,
		job,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	namespaces := make([]string, 0)
	for rows.Next() {
		var upstreamJob string
		if err := rows.Scan(&upstreamJob); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		namespaces = append(namespaces,
			fmt.Sprintf("kive/job/%s", upstreamJob),
			fmt.Sprintf("vars/job/%s", upstreamJob),
			fmt.Sprintf("secrets/job/%s", upstreamJob),
		)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return namespaces, nil
}

// BuildScrapeTemplateNamespaces returns KV namespaces available when rendering scrape.yaml.tpl at build.
func BuildScrapeTemplateNamespaces(tx *sql.Tx, job string) ([]string, error) {
	namespaces := []string{
		BucketKVNamespace,
		"vars/bucket",
		fmt.Sprintf("kive/job/%s", job),
		fmt.Sprintf("vars/bucket/job/%s", job),
		fmt.Sprintf("vars/job/%s", job),
		fmt.Sprintf("secrets/job/%s", job),
	}
	if tx == nil {
		return namespaces, nil
	}
	upstream, err := UpstreamDemandKVNamespaces(tx, job)
	if err != nil {
		return nil, err
	}
	return utils.Unique(append(namespaces, upstream...)), nil
}

// AllowedKVNamespacesWithUpstream includes read namespaces for upstream jobs referenced in demands.
func AllowedKVNamespacesWithUpstream(tx *sql.Tx, job, workerIP string) ([]string, error) {
	namespaces := AllowedKVNamespaces(job, workerIP)
	if tx == nil {
		return namespaces, nil
	}
	upstream, err := UpstreamDemandKVNamespaces(tx, job)
	if err != nil {
		return nil, err
	}
	return append(namespaces, upstream...), nil
}

// AccessibleKVNamespacesForJob returns the union of KV namespaces readable by hooks
// or templates on any allocation row for the job (including upstream demand jobs).
func AccessibleKVNamespacesForJob(tx *sql.Tx, job string) ([]string, error) {
	workerIPs, err := GetNonRemovedAllocations(tx, job)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	namespaces := make([]string, 0)
	add := func(items ...string) {
		for _, ns := range items {
			if _, ok := seen[ns]; ok {
				continue
			}
			seen[ns] = struct{}{}
			namespaces = append(namespaces, ns)
		}
	}

	for _, workerIP := range workerIPs {
		allowed, err := AllowedKVNamespacesWithUpstream(tx, job, workerIP)
		if err != nil {
			return nil, err
		}
		add(allowed...)
	}
	return namespaces, nil
}
