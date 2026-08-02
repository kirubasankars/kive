// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"kive/bucket"
)

// ReplaceTemplateFiles replaces the source template payload from root.
// An absent root is treated as "no templates" — templates/ is optional.
func ReplaceTemplateFiles(tx *sql.Tx, root string) error {
	if _, err := tx.Exec(`DELETE FROM template_files`); err != nil {
		return bucket.DatabaseError(err)
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return bucket.UnexpectedError(err)
	}
	return filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return bucket.UnexpectedError(walkErr)
		}
		if filePath == root {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return bucket.UnexpectedError(err)
		}
		rel = filepath.ToSlash(rel)
		if err := ValidateBundlePath(rel); err != nil {
			return fmt.Errorf("template %q: %w", rel, err)
		}
		info, err := entry.Info()
		if err != nil {
			return bucket.UnexpectedError(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return fmt.Errorf("template %q: only regular files and directories are supported", rel)
		}
		var content []byte
		if !info.IsDir() {
			content, err = os.ReadFile(filePath)
			if err != nil {
				return bucket.UnexpectedError(err)
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO template_files (path, content, isdir) VALUES (?, ?, ?)`,
			rel, content, info.IsDir(),
		); err != nil {
			return bucket.DatabaseError(err)
		}
		return nil
	})
}

// ExportTemplateFilesRaw materializes template_files below outputDir.
func ExportTemplateFilesRaw(tx *sql.Tx, outputDir string) error {
	rows, err := tx.Query(`SELECT path, content, isdir FROM template_files ORDER BY isdir DESC, path`)
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
			return fmt.Errorf("template %q: %w", rel, err)
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

// ValidateBundlePath rejects absolute, ambiguous, or escaping source paths.
func ValidateBundlePath(rel string) error {
	if rel == "" || strings.ContainsRune(rel, '\x00') || strings.Contains(rel, `\`) {
		return fmt.Errorf("invalid relative path")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean != rel || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path escapes bundle root")
	}
	return nil
}
