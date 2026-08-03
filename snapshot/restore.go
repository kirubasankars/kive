// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package snapshot

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/internal/durablefs"
)

const (
	restoreGitignore    = "data\nlogs\nsecrets\ntmp\n.ssh\n"
	restoreManifestName = ".kive-restore.json"
)

type restoreManifest struct {
	Version       int    `json:"version"`
	SourceSHA256  string `json:"source_sha256"`
	SchemaVersion int    `json:"schema_version"`
}

// Restore atomically materializes a complete bucket tree under an absent outputDir.
// It does not contact workers.
func Restore(dbFile, outputDir string) error {
	return restoreWithHook(dbFile, outputDir, nil)
}

func restoreWithHook(dbFile, outputDir string, hook durablefs.Hook) (returnErr error) {
	dbFile = strings.TrimSpace(dbFile)
	if dbFile == "" {
		return fmt.Errorf("snapshot restore: dbfile is required")
	}
	if strings.TrimSpace(outputDir) == "" {
		return fmt.Errorf("snapshot restore: output directory is required")
	}

	absDB, err := filepath.Abs(dbFile)
	if err != nil {
		return fmt.Errorf("snapshot restore: resolve dbfile: %w", err)
	}
	sourceInfo, err := os.Lstat(absDB)
	if err != nil {
		return fmt.Errorf("snapshot restore: dbfile: %w", err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf("snapshot restore: dbfile must be a regular file")
	}

	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("snapshot restore: resolve output: %w", err)
	}
	parent := filepath.Dir(absOut)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("snapshot restore: output parent: %w", err)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("snapshot restore: output parent is not a directory")
	}

	sourceHash, err := hashFile(absDB)
	if err != nil {
		return fmt.Errorf("snapshot restore: hash dbfile: %w", err)
	}
	if _, err := os.Lstat(absOut); err == nil {
		return completeExistingRestore(absOut, sourceHash)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("snapshot restore: stat output: %w", err)
	}

	if err := runRestoreHook(hook, "read-payload", absDB); err != nil {
		return err
	}
	material, caTrust, schemaVersion, err := readRestorePayload(absDB)
	if err != nil {
		return err
	}
	if err := validateRestoreMaterial(material); err != nil {
		return err
	}
	if err := runRestoreHook(hook, "payload-validated", absDB); err != nil {
		return err
	}
	if strings.TrimSpace(caTrust) == "" {
		caTrust = material.CACrt
	}

	if err := runRestoreHook(hook, "create-stage", absOut); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(absOut)+".restore-*")
	if err != nil {
		return fmt.Errorf("snapshot restore: create stage: %w", err)
	}
	installed := false
	defer func() {
		if installed {
			return
		}
		if cleanupErr := durablefs.RemoveTree(stage, hook); cleanupErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("cleanup restore stage: %w", cleanupErr))
		}
	}()
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}
	if err := runRestoreHook(hook, "stage-created", stage); err != nil {
		return err
	}
	if err := ensureRestoreDirectories(stage, hook); err != nil {
		return err
	}
	destDB := path.Join(stage, "data", "kive.db")
	if err := copyFile(absDB, destDB, hook); err != nil {
		return fmt.Errorf("install data/kive.db: %w", err)
	}
	copiedHash, err := hashFile(destDB)
	if err != nil {
		return fmt.Errorf("hash staged data/kive.db: %w", err)
	}
	if copiedHash != sourceHash {
		return fmt.Errorf("snapshot restore: source database changed during restore")
	}
	if err := runRestoreHook(hook, "database-staged", destDB); err != nil {
		return err
	}
	if err := writeRestoreTree(stage, material, caTrust, hook); err != nil {
		return err
	}
	if err := runRestoreHook(hook, "configuration-staged", stage); err != nil {
		return err
	}

	liveDB, err := data.OpenSQLite(destDB, "mode=ro&immutable=1")
	if err != nil {
		return err
	}
	defer func() { _ = liveDB.Close() }()

	tx, err := liveDB.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := runRestoreHook(hook, "export-files", stage); err != nil {
		return err
	}
	jobsDir := path.Join(stage, "workspace", "jobs")
	if err := data.ExportAllJobFilesRaw(tx, jobsDir); err != nil {
		return fmt.Errorf("export job_files to workspace/jobs: %w", err)
	}
	if err := data.ExportTemplateFilesRaw(tx, path.Join(stage, "templates")); err != nil {
		return fmt.Errorf("export template_files to templates: %w", err)
	}
	if err := data.ExportCommandFilesRaw(tx, path.Join(stage, "workspace", "commands")); err != nil {
		return fmt.Errorf("export command_files to workspace/commands: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}
	manifestData, err := json.MarshalIndent(restoreManifest{
		Version:       1,
		SourceSHA256:  sourceHash,
		SchemaVersion: schemaVersion,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := durablefs.WriteNew(
		path.Join(stage, restoreManifestName),
		append(manifestData, '\n'),
		0o600,
		hook,
	); err != nil {
		return fmt.Errorf("write restore completion manifest: %w", err)
	}
	if err := durablefs.SyncTree(stage, hook); err != nil {
		return fmt.Errorf("sync restore tree: %w", err)
	}
	if err := durablefs.CommitDir(stage, absOut, hook); err != nil {
		if durablefs.Installed(err) {
			installed = true
		}
		return fmt.Errorf("commit restore tree: %w", err)
	}
	installed = true
	return nil
}

func readRestorePayload(dbPath string) (data.BucketRestoreMaterial, string, int, error) {
	// immutable=1 avoids creating -wal/-shm sidecars next to the source copy.
	db, err := data.OpenSQLite(dbPath, "mode=ro&immutable=1")
	if err != nil {
		return data.BucketRestoreMaterial{}, "", 0, err
	}
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		return data.BucketRestoreMaterial{}, "", 0, bucket.DatabaseError(err)
	}
	defer func() { _ = tx.Rollback() }()

	material, err := data.GetBucketRestoreMaterial(tx)
	if err != nil {
		return data.BucketRestoreMaterial{}, "", 0, err
	}
	caTrust, err := data.GetWorkerCATrust(tx)
	if err != nil {
		return data.BucketRestoreMaterial{}, "", 0, err
	}
	schemaVersion, err := data.SchemaVersion(tx)
	if err != nil {
		return data.BucketRestoreMaterial{}, "", 0, err
	}
	return material, caTrust, schemaVersion, nil
}

func validateRestoreMaterial(m data.BucketRestoreMaterial) error {
	if strings.TrimSpace(m.KiveConf) == "" {
		return fmt.Errorf("snapshot restore: bucket.kive_conf is empty in backup")
	}
	if strings.TrimSpace(m.KVKey) == "" {
		return fmt.Errorf("snapshot restore: bucket.kv_key is empty in backup")
	}
	if strings.TrimSpace(m.CAKey) == "" {
		return fmt.Errorf("snapshot restore: bucket.ca_key is empty in backup")
	}
	if strings.TrimSpace(m.CACrt) == "" {
		return fmt.Errorf("snapshot restore: bucket.ca_crt is empty in backup")
	}
	if strings.TrimSpace(m.WorkersJSON) == "" {
		return fmt.Errorf("snapshot restore: bucket.workers_json is empty in backup")
	}
	return nil
}

func hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func completeExistingRestore(outputDir, sourceHash string) error {
	manifestPath := path.Join(outputDir, restoreManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("snapshot restore: output already exists without matching completion manifest")
	}
	var manifest restoreManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Version != 1 || manifest.SourceSHA256 != sourceHash {
		return fmt.Errorf("snapshot restore: output already exists without matching completion manifest")
	}
	if err := durablefs.SyncTree(outputDir, nil); err != nil {
		return fmt.Errorf("snapshot restore: resync completed output: %w", err)
	}
	if err := durablefs.SyncDir(filepath.Dir(outputDir)); err != nil {
		return fmt.Errorf("snapshot restore: resync output parent: %w", err)
	}
	return nil
}

func runRestoreHook(hook durablefs.Hook, operation, filePath string) error {
	if hook == nil {
		return nil
	}
	if err := hook(operation, filePath); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func ensureRestoreDirectories(root string, hook durablefs.Hook) error {
	// templates/ comes from the bundle only when it has templates, and tmp/ is
	// scratch that commands create on demand — same layout as `kive init`.
	dirs := []string{
		path.Join(root, "data"),
		path.Join(root, "workspace", "jobs"),
		path.Join(root, "logs"),
		path.Join(root, "secrets"),
		path.Join(root, ".ssh"),
	}
	for _, dir := range dirs {
		mode := os.FileMode(0o755)
		if path.Base(dir) == ".ssh" || path.Base(dir) == "secrets" {
			mode = 0o700
		}
		if err := os.MkdirAll(dir, mode); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	gitignore := path.Join(root, ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		if err := durablefs.WriteNew(gitignore, []byte(restoreGitignore), 0o644, hook); err != nil {
			return fmt.Errorf("create .gitignore: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat .gitignore: %w", err)
	}
	return nil
}

func writeRestoreTree(root string, m data.BucketRestoreMaterial, caTrust string, hook durablefs.Hook) error {
	if err := durablefs.WriteNew(path.Join(root, "kive.conf"), []byte(m.KiveConf), 0o600, hook); err != nil {
		return fmt.Errorf("write kive.conf: %w", err)
	}
	if strings.TrimSpace(m.PromotionJSON) != "" {
		if err := durablefs.WriteNew(path.Join(root, "promotion.conf"), []byte(m.PromotionJSON), 0o644, hook); err != nil {
			return fmt.Errorf("write promotion.conf: %w", err)
		}
	}
	if strings.TrimSpace(m.WebhookJSON) != "" {
		if err := durablefs.WriteNew(path.Join(root, "webhook.conf"), []byte(m.WebhookJSON), 0o600, hook); err != nil {
			return fmt.Errorf("write webhook.conf: %w", err)
		}
	}
	if strings.TrimSpace(m.ClickHouseJSON) != "" {
		if err := durablefs.WriteNew(path.Join(root, "observe.conf"), []byte(m.ClickHouseJSON), 0o600, hook); err != nil {
			return fmt.Errorf("write observe.conf: %w", err)
		}
	}
	secrets := path.Join(root, "secrets")
	if err := durablefs.WriteNew(path.Join(secrets, "kv.key"), []byte(m.KVKey), 0o600, hook); err != nil {
		return fmt.Errorf("write secrets/kv.key: %w", err)
	}
	if err := durablefs.WriteNew(path.Join(secrets, "ca.key"), []byte(m.CAKey), 0o600, hook); err != nil {
		return fmt.Errorf("write secrets/ca.key: %w", err)
	}
	if err := durablefs.WriteNew(path.Join(secrets, "ca.crt"), []byte(m.CACrt), 0o644, hook); err != nil {
		return fmt.Errorf("write secrets/ca.crt: %w", err)
	}
	if err := durablefs.WriteNew(path.Join(secrets, "ca-trust.crt"), []byte(caTrust), 0o644, hook); err != nil {
		return fmt.Errorf("write secrets/ca-trust.crt: %w", err)
	}
	if err := durablefs.WriteNew(path.Join(root, ".ssh", "known_hosts"), []byte(m.KnownHosts), 0o600, hook); err != nil {
		return fmt.Errorf("write .ssh/known_hosts: %w", err)
	}

	ws := path.Join(root, "workspace")
	if err := durablefs.WriteNew(path.Join(ws, "workers.conf"), []byte(m.WorkersJSON), 0o644, hook); err != nil {
		return fmt.Errorf("write workspace/workers.conf: %w", err)
	}
	if strings.TrimSpace(m.BucketConf) != "" {
		if err := durablefs.WriteNew(path.Join(ws, "bucket.conf"), []byte(m.BucketConf), 0o644, hook); err != nil {
			return fmt.Errorf("write workspace/bucket.conf: %w", err)
		}
	}
	if strings.TrimSpace(m.DisabledJSON) != "" {
		if err := durablefs.WriteNew(path.Join(ws, "disabled.conf"), []byte(m.DisabledJSON), 0o644, hook); err != nil {
			return fmt.Errorf("write workspace/disabled.conf: %w", err)
		}
	}
	return writeBucketJobsConfsNew(ws, m.BucketJobsConfs, hook)
}

func writeBucketJobsConfsNew(ws, encoded string, hook durablefs.Hook) error {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	var files map[string]string
	if err := json.Unmarshal([]byte(trimmed), &files); err != nil {
		return fmt.Errorf("parse bucket.jobs_confs: %w", err)
	}
	for name, content := range files {
		base := path.Base(name)
		if base != name || (base != "bucket.jobs.conf" && !(strings.HasPrefix(base, "bucket.jobs.") && strings.HasSuffix(base, ".conf"))) {
			return fmt.Errorf("invalid bucket.jobs conf name %q", name)
		}
		if err := durablefs.WriteNew(path.Join(ws, base), []byte(content), 0o644, hook); err != nil {
			return fmt.Errorf("write workspace/%s: %w", base, err)
		}
	}
	return nil
}

// MaterializeSource writes only user-owned source content from a built catalog.
// Runtime data, secrets, SSH state, and logs are intentionally excluded.
func MaterializeSource(tx *sql.Tx, outputDir string) error {
	material, err := data.GetBucketRestoreMaterial(tx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(material.KiveConf) == "" {
		return fmt.Errorf("source bundle: bucket.kive_conf is empty")
	}
	// Empty workers.conf (no worker blocks) is valid; require the field to be present.
	if material.WorkersJSON == "" {
		return fmt.Errorf("source bundle: bucket.workers_json is empty")
	}
	workspaceDir := path.Join(outputDir, "workspace")
	if err := os.MkdirAll(path.Join(workspaceDir, "jobs"), 0o755); err != nil {
		return err
	}
	// templates/ is optional: ExportTemplateFilesRaw creates it only when the
	// bundle carries templates, so a source without them materializes without.
	if err := os.WriteFile(path.Join(outputDir, "kive.conf"), []byte(material.KiveConf), 0o600); err != nil {
		return err
	}
	if strings.TrimSpace(material.PromotionJSON) != "" {
		if err := os.WriteFile(path.Join(outputDir, "promotion.conf"), []byte(material.PromotionJSON), 0o644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(material.WebhookJSON) != "" {
		if err := os.WriteFile(path.Join(outputDir, "webhook.conf"), []byte(material.WebhookJSON), 0o600); err != nil {
			return err
		}
	}
	if strings.TrimSpace(material.ClickHouseJSON) != "" {
		if err := os.WriteFile(path.Join(outputDir, "observe.conf"), []byte(material.ClickHouseJSON), 0o600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path.Join(workspaceDir, "workers.conf"), []byte(material.WorkersJSON), 0o644); err != nil {
		return err
	}
	if strings.TrimSpace(material.BucketConf) != "" {
		if err := os.WriteFile(path.Join(workspaceDir, "bucket.conf"), []byte(material.BucketConf), 0o644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(material.DisabledJSON) != "" {
		if err := os.WriteFile(path.Join(workspaceDir, "disabled.conf"), []byte(material.DisabledJSON), 0o644); err != nil {
			return err
		}
	}
	if err := writeBucketJobsConfs(workspaceDir, material.BucketJobsConfs); err != nil {
		return err
	}
	if err := data.ExportAllJobFilesRaw(tx, path.Join(workspaceDir, "jobs")); err != nil {
		return err
	}
	if err := data.ExportTemplateFilesRaw(tx, path.Join(outputDir, "templates")); err != nil {
		return err
	}
	if err := os.MkdirAll(path.Join(workspaceDir, "commands"), 0o755); err != nil {
		return err
	}
	return data.ExportCommandFilesRaw(tx, path.Join(workspaceDir, "commands"))
}

func writeBucketJobsConfs(ws, encoded string) error {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	var files map[string]string
	if err := json.Unmarshal([]byte(trimmed), &files); err != nil {
		return fmt.Errorf("parse bucket.jobs_confs: %w", err)
	}
	for name, content := range files {
		base := path.Base(name)
		if base != name || (base != "bucket.jobs.conf" && !(strings.HasPrefix(base, "bucket.jobs.") && strings.HasSuffix(base, ".conf"))) {
			return fmt.Errorf("invalid bucket.jobs conf name %q", name)
		}
		if err := os.WriteFile(path.Join(ws, base), []byte(content), 0o644); err != nil {
			return fmt.Errorf("write workspace/%s: %w", base, err)
		}
	}
	return nil
}

func copyFile(src, dst string, hook durablefs.Hook) error {
	if err := os.MkdirAll(path.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return durablefs.CopyNew(dst, in, 0o644, hook)
}
