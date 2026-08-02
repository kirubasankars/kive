// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"fmt"
	"strings"

	"kive/bucket"
)

// LatestSchemaVersion is the schema stamp this binary expects.
//
// Policy: catalog DDL changes bump this constant. Fresh DBs (version 0) get
// the full current schema. Older stamps are upgraded with additive in-place
// steps (ALTER TABLE ADD COLUMN, new tables/indexes). Stamps newer than this
// binary are rejected. Destructive renames/drops are not supported — avoid
// them or introduce an explicit future strategy.
const LatestSchemaVersion = 8

// CurrentBundleVersion is the source-content format understood by push.
// It changes only when the extraction contract changes incompatibly.
const CurrentBundleVersion = 1

// oldestMigratableSchemaVersion is the oldest stamped version this binary can
// step up from. Stamps in (0, oldest) exclusive of steps are rejected.
const oldestMigratableSchemaVersion = 2

func incompatibleSchemaError(currentVersion int) error {
	if currentVersion > LatestSchemaVersion {
		return fmt.Errorf(
			"%w: database schema version %d is newer than this binary (supports %d); use a newer kive",
			bucket.ErrSchemaUpgradeRequired,
			currentVersion,
			LatestSchemaVersion,
		)
	}
	return fmt.Errorf(
		"%w: database schema version %d cannot be migrated by this binary (supports %d–%d, or empty); remove data/kive.db and run kive init",
		bucket.ErrSchemaUpgradeRequired,
		currentVersion,
		oldestMigratableSchemaVersion,
		LatestSchemaVersion,
	)
}

// CheckSchemaVersion verifies kive.conf and kive.db exist and the schema stamp
// equals LatestSchemaVersion. Older stamps need kive init / server reinit to
// run MigrateSchema; newer stamps need a newer binary.
func CheckSchemaVersion() error {
	if !BucketFilesPresent() {
		return bucket.ErrNotInitialized
	}

	db, err := OpenDatabase(false)
	if err != nil {
		return err
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

	currentVersion, err := readSchemaVersion(tx)
	if err != nil {
		return err
	}

	if currentVersion != LatestSchemaVersion {
		return incompatibleSchemaError(currentVersion)
	}
	return nil
}

// MigrateSchema brings the database to LatestSchemaVersion.
// Version 0 applies the full current DDL. Versions oldestMigratableSchemaVersion
// through Latest-1 run additive step functions. Already-current is a no-op.
func MigrateSchema(tx *sql.Tx) error {
	if err := ensureSchemaVersionTable(tx); err != nil {
		return err
	}

	currentVersion, err := readSchemaVersion(tx)
	if err != nil {
		return err
	}
	if currentVersion == LatestSchemaVersion {
		return nil
	}
	if currentVersion == 0 {
		if err := applyCurrentSchema(tx); err != nil {
			return err
		}
		return writeSchemaVersion(tx, LatestSchemaVersion)
	}
	if currentVersion > LatestSchemaVersion {
		return incompatibleSchemaError(currentVersion)
	}
	if currentVersion < oldestMigratableSchemaVersion {
		return incompatibleSchemaError(currentVersion)
	}

	for v := currentVersion; v < LatestSchemaVersion; v++ {
		step, ok := schemaMigrations[v]
		if !ok {
			return incompatibleSchemaError(v)
		}
		if err := step(tx); err != nil {
			return err
		}
		if err := writeSchemaVersion(tx, v+1); err != nil {
			return err
		}
	}
	return nil
}

// schemaMigrations maps fromVersion → migrate to fromVersion+1.
var schemaMigrations = map[int]func(*sql.Tx) error{
	2: migrateSchema2To3,
	3: migrateSchema3To4,
	4: migrateSchema4To5,
	5: migrateSchema5To6,
	6: migrateSchema6To7,
	7: migrateSchema7To8,
}

// migrateSchema2To3 adds optional hook description and schedule columns.
func migrateSchema2To3(tx *sql.Tx) error {
	if err := addColumnIfMissing(tx, "hooks", "description", "TEXT"); err != nil {
		return err
	}
	return addColumnIfMissing(tx, "hooks", "schedule", "TEXT")
}

// migrateSchema3To4 adds max_concurrent_restarts. Does not copy from the
// legacy max_concurrent_upgrades column (clean break; default 1 until rebuild).
func migrateSchema3To4(tx *sql.Tx) error {
	return addColumnIfMissing(tx, "jobs", "max_concurrent_restarts", "INT NOT NULL DEFAULT 1")
}

// migrateSchema4To5 adds optional observe.conf restore material (clickhouse_json column).
func migrateSchema4To5(tx *sql.Tx) error {
	err := addColumnIfMissing(tx, "bucket", "clickhouse_json", "TEXT")
	if err != nil && strings.Contains(err.Error(), "no such table") {
		// Minimal fixture DBs in migration tests may omit bucket entirely.
		return nil
	}
	return err
}

// migrateSchema5To6 adds external flag for BYO job leaf certificates.
func migrateSchema5To6(tx *sql.Tx) error {
	err := addColumnIfMissing(tx, "job_certs", "external", "INT NOT NULL DEFAULT 0")
	if err != nil && strings.Contains(err.Error(), "no such table") {
		// Minimal fixture DBs in migration tests may omit job_certs.
		return nil
	}
	return err
}

// migrateSchema6To7 adds persisted health status snapshot and history tables.
func migrateSchema6To7(tx *sql.Tx) error {
	return execStatements(tx, healthStatusDDL())
}

// migrateSchema7To8 adds persisted worker SSH health snapshot and history tables.
func migrateSchema7To8(tx *sql.Tx) error {
	return execStatements(tx, workerHealthStatusDDL())
}

func addColumnIfMissing(tx *sql.Tx, table, column, decl string) error {
	has, err := tableHasColumn(tx, table, column)
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl))
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

func tableHasColumn(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, bucket.DatabaseError(err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, bucket.DatabaseError(err)
	}
	return false, nil
}

// SchemaVersion returns the database schema version, or 0 if unset.
func SchemaVersion(tx *sql.Tx) (int, error) {
	return readSchemaVersion(tx)
}

func readSchemaVersion(tx *sql.Tx) (int, error) {
	var tableCount int
	err := tx.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'`,
	).Scan(&tableCount)
	if err != nil {
		return 0, bucket.DatabaseError(err)
	}
	if tableCount == 0 {
		return 0, nil
	}

	var version int
	err = tx.QueryRow(`SELECT version FROM schema_version WHERE id = 1`).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, bucket.DatabaseError(err)
	}
	return version, nil
}

func writeSchemaVersion(tx *sql.Tx, version int) error {
	_, err := tx.Exec(
		`INSERT INTO schema_version (id, version) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET version = excluded.version`,
		version,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

func ensureSchemaVersionTable(tx *sql.Tx) error {
	return execStatements(tx, []string{
		`CREATE TABLE IF NOT EXISTS schema_version (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			version INTEGER NOT NULL
		)`,
	})
}

func applyCurrentSchema(tx *sql.Tx) error {
	if err := execStatements(tx, baseTableDDL()); err != nil {
		return err
	}
	return execStatements(tx, uniqueIndexes())
}

func uniqueIndexes() []string {
	return []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_worker_worker_id ON workers(worker_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_job_job_id ON jobs(job_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_allocations_alloc_id ON allocations(alloc_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_bucket_bucket_id ON bucket(bucket_id) WHERE bucket_id IS NOT NULL AND bucket_id != ''`,
	}
}

func baseTableDDL() []string {
	// Ownership children reference workers(worker_id) / jobs(job_id). allocations are
	// intentionally not FK'd (soft-orphan build/GC). allocation_hashes,
	// allocation_file_hashes, build_hashes, and key_value stay unconstrained.
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS bucket (
			bucket_id TEXT,
			generation INT,
			kive_conf TEXT,
			kv_key TEXT,
			ca_key TEXT,
			ca_crt TEXT,
			workers_json TEXT,
			bucket_conf TEXT,
			disabled_json TEXT,
			bucket_jobs_confs TEXT,
			known_hosts TEXT,
			promotion_json TEXT,
			webhook_json TEXT,
			clickhouse_json TEXT,
			init_git_hash TEXT,
			build_git_hash TEXT,
			bundle_version INT NOT NULL DEFAULT 0,
			created_at TEXT,
			initialized_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS workers (
			worker_id TEXT NOT NULL UNIQUE,
			worker_ip TEXT,
			available_memory_mb TEXT,
			available_cpu_mhz TEXT,
			position INT,
			PRIMARY KEY(worker_ip)
		)`,
		`CREATE TABLE IF NOT EXISTS worker_labels (
			worker_id TEXT NOT NULL,
			label TEXT NOT NULL,
			UNIQUE(worker_id, label),
			FOREIGN KEY (worker_id) REFERENCES workers(worker_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS worker_tags (
			worker_id TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT,
			UNIQUE(worker_id, key),
			FOREIGN KEY (worker_id) REFERENCES workers(worker_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS allocations (
			alloc_id TEXT NOT NULL UNIQUE,
			worker_ip TEXT,
			job TEXT,
			disabled INT,
			removed INT,
			version TEXT,
			PRIMARY KEY(worker_ip, job)
		)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			job_id TEXT NOT NULL UNIQUE,
			name TEXT,
			version TEXT,
			deployment_priority INT NOT NULL DEFAULT 0,
			deployment_order INT NOT NULL DEFAULT 0,
			deployment_seq INT NOT NULL DEFAULT 0,
			min_memory_mb TEXT,
			max_memory_mb TEXT,
			current_memory_mb TEXT,
			current_memory_source TEXT NOT NULL DEFAULT 'manifest',
			min_cpu_mhz TEXT,
			max_cpu_mhz TEXT,
			current_cpu_mhz TEXT,
			current_cpu_source TEXT NOT NULL DEFAULT 'manifest',
			cpu_shares INT NOT NULL DEFAULT 0,
			cpu_shares_source TEXT NOT NULL DEFAULT 'manifest',
			max_concurrent_restarts INT NOT NULL DEFAULT 1,
			max_concurrent_starts INT NOT NULL DEFAULT 0,
			max_concurrent_stops INT NOT NULL DEFAULT 0,
			restart_policy TEXT NOT NULL DEFAULT 'always',
			restart_globs TEXT NOT NULL DEFAULT '[]',
			reload_globs TEXT NOT NULL DEFAULT '[]',
			health_check TEXT,
			signature_status TEXT NOT NULL DEFAULT 'unsigned',
			signature_signer TEXT NOT NULL DEFAULT '',
			signature_digest TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(name)
		)`,
		`CREATE TABLE IF NOT EXISTS job_selectors (
			job_id TEXT NOT NULL,
			selector TEXT NOT NULL,
			UNIQUE(job_id, selector),
			FOREIGN KEY (job_id) REFERENCES jobs(job_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS job_ports (
			job_id TEXT NOT NULL,
			name TEXT NOT NULL,
			port INT,
			protocol TEXT NOT NULL DEFAULT 'tcp',
			exposure TEXT NOT NULL DEFAULT 'cluster',
			UNIQUE(job_id, name),
			FOREIGN KEY (job_id) REFERENCES jobs(job_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS job_certs (
			job_id TEXT NOT NULL,
			name TEXT NOT NULL,
			pkcs8 INT,
			one INT,
			subject TEXT,
			external INT NOT NULL DEFAULT 0,
			UNIQUE(job_id, name),
			FOREIGN KEY (job_id) REFERENCES jobs(job_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS job_files (
			job_id TEXT NOT NULL,
			path TEXT NOT NULL,
			content BLOB,
			isdir BOOL,
			UNIQUE(job_id, path),
			FOREIGN KEY (job_id) REFERENCES jobs(job_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS template_files (
			path TEXT PRIMARY KEY,
			content BLOB,
			isdir BOOL NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS command_files (
			path TEXT PRIMARY KEY,
			content BLOB,
			isdir BOOL NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS hooks (
			job_id TEXT NOT NULL,
			job TEXT,
			name TEXT NOT NULL,
			executed_on TEXT NOT NULL,
			demand_job TEXT,
			demand_hook TEXT,
			demand_config TEXT,
			description TEXT,
			schedule TEXT,
			UNIQUE(job_id, name, executed_on),
			FOREIGN KEY (job_id) REFERENCES jobs(job_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS key_value (
			key TEXT,
			value TEXT,
			namespace TEXT,
			version INT,
			ttl TEXT,
			created_date TEXT,
			deleted INT
		)`,
		`CREATE TABLE IF NOT EXISTS allocation_hashes (
			alloc_id TEXT PRIMARY KEY,
			pending_hash TEXT,
			applied_hash TEXT,
			applied_version TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS allocation_file_hashes (
			alloc_id TEXT NOT NULL,
			path TEXT NOT NULL,
			pending_hash TEXT,
			applied_hash TEXT,
			PRIMARY KEY(alloc_id, path)
		)`,
		`CREATE TABLE IF NOT EXISTS build_hashes (
			namespace TEXT,
			key TEXT,
			pending_hash TEXT,
			applied_hash TEXT,
			PRIMARY KEY(namespace, key)
		)`,
	}
	return append(ddl, append(deployHistoryDDL(), append(healthStatusDDL(), workerHealthStatusDDL()...)...)...)
}

func healthStatusDDL() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS health_status (
			job TEXT NOT NULL,
			allocation_id TEXT NOT NULL,
			worker_ip TEXT NOT NULL,
			status TEXT NOT NULL,
			liveness TEXT,
			readiness TEXT,
			detail TEXT,
			checked_at TEXT NOT NULL,
			run_id TEXT,
			PRIMARY KEY (job, allocation_id)
		)`,
		`CREATE TABLE IF NOT EXISTS health_status_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			job TEXT NOT NULL,
			allocation_id TEXT NOT NULL,
			worker_ip TEXT NOT NULL,
			status TEXT NOT NULL,
			liveness TEXT,
			readiness TEXT,
			detail TEXT,
			checked_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_health_history_job_time ON health_status_history (job, checked_at DESC)`,
	}
}

func workerHealthStatusDDL() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS worker_health_status (
			worker_ip TEXT NOT NULL PRIMARY KEY,
			status TEXT NOT NULL,
			detail TEXT,
			checked_at TEXT NOT NULL,
			run_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS worker_health_status_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			worker_ip TEXT NOT NULL,
			status TEXT NOT NULL,
			detail TEXT,
			checked_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_worker_health_history_ip_time ON worker_health_status_history (worker_ip, checked_at DESC)`,
	}
}

func deployHistoryDDL() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS deploy_history (
			run_id TEXT PRIMARY KEY,
			generation INT NOT NULL,
			started_at TEXT NOT NULL,
			ended_at TEXT NOT NULL,
			status TEXT NOT NULL,
			source_revision TEXT NOT NULL DEFAULT '',
			build_git_hash TEXT NOT NULL DEFAULT '',
			jobs_filter TEXT NOT NULL DEFAULT '',
			force INT NOT NULL DEFAULT 0,
			changed INT NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS deploy_history_jobs (
			run_id TEXT NOT NULL,
			job TEXT NOT NULL,
			outcome TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			deployment_order INT NOT NULL DEFAULT 0,
			PRIMARY KEY(run_id, job),
			FOREIGN KEY (run_id) REFERENCES deploy_history(run_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_deploy_history_ended_at ON deploy_history(ended_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_deploy_history_jobs_job ON deploy_history_jobs(job, run_id)`,
	}
}

func execStatements(tx *sql.Tx, statements []string) error {
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}
