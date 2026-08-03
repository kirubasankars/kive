// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kive/bucket"
)

var commandScriptNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateCommandScriptName accepts a script basename without .sh.
func ValidateCommandScriptName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("command script name is required")
	}
	if !commandScriptNameRE.MatchString(name) {
		return fmt.Errorf("invalid command script name %q (use [A-Za-z0-9_-]+)", name)
	}
	return nil
}

// ReplaceCommandFiles replaces the workspace/commands payload from root.
// Missing root is valid (empty catalog). Only top-level *.sh files are stored.
func ReplaceCommandFiles(tx *sql.Tx, root string) error {
	if _, err := tx.Exec(`DELETE FROM command_files`); err != nil {
		return bucket.DatabaseError(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return bucket.UnexpectedError(err)
	}
	if !info.IsDir() {
		return fmt.Errorf("commands: %s is not a directory", root)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return bucket.UnexpectedError(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		display := "commands/" + name
		if entry.IsDir() {
			return fmt.Errorf("command %q: nested directories are not supported", display)
		}
		info, err := entry.Info()
		if err != nil {
			return bucket.UnexpectedError(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("command %q: only regular *.sh files are supported", display)
		}
		if !strings.HasSuffix(name, ".sh") {
			return fmt.Errorf("command %q: only *.sh files are supported", display)
		}
		scriptName := strings.TrimSuffix(name, ".sh")
		if err := ValidateCommandScriptName(scriptName); err != nil {
			return fmt.Errorf("command %q: %w", display, err)
		}
		rel := name
		if err := ValidateBundlePath(rel); err != nil {
			return fmt.Errorf("command %q: %w", display, err)
		}
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return bucket.UnexpectedError(err)
		}
		if _, err := tx.Exec(
			`INSERT INTO command_files (path, content, isdir) VALUES (?, ?, 0)`,
			rel, content,
		); err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

// ExportCommandFilesRaw materializes command_files below outputDir.
// A missing command_files table (older push bundles) is a no-op.
func ExportCommandFilesRaw(tx *sql.Tx, outputDir string) error {
	exists, err := sqliteTableExists(tx, "command_files")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	rows, err := tx.Query(`SELECT path, content, isdir FROM command_files ORDER BY isdir DESC, path`)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var rel string
		var content []byte
		var isDir bool
		if err := rows.Scan(&rel, &content, &isDir); err != nil {
			return bucket.DatabaseError(err)
		}
		if err := ValidateBundlePath(rel); err != nil {
			return fmt.Errorf("command %q: %w", rel, err)
		}
		dest := filepath.Join(outputDir, filepath.FromSlash(rel))
		if isDir {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return bucket.UnexpectedError(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return bucket.UnexpectedError(err)
		}
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			return bucket.UnexpectedError(err)
		}
	}
	return rowsErr(rows)
}

// CommandFilesStats returns entry count and expanded byte size for push limits.
// Missing table (older bundles) counts as zero.
func CommandFilesStats(tx *sql.Tx) (entries, expanded int64, err error) {
	exists, err := sqliteTableExists(tx, "command_files")
	if err != nil {
		return 0, 0, err
	}
	if !exists {
		return 0, 0, nil
	}
	if err := tx.QueryRow(`
		SELECT count(*), ifnull(sum(length(content)), 0) FROM command_files
	`).Scan(&entries, &expanded); err != nil {
		return 0, 0, bucket.DatabaseError(err)
	}
	return entries, expanded, nil
}

func sqliteTableExists(tx *sql.Tx, name string) (bool, error) {
	var n int
	if err := tx.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name,
	).Scan(&n); err != nil {
		return false, bucket.DatabaseError(err)
	}
	return n > 0, nil
}
