// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"kive/bucket"
	"kive/workspace"
)

// rejectBucketSymlinks fails build when operator-consumed bucket inputs contain
// symlinks or other non-regular entries. Dependency/cache trees under jobs
// (.venv, venv, node_modules, __pycache__) are skipped, matching job Capture.
func rejectBucketSymlinks() error {
	for _, name := range []string{"kive.conf", "promotion.conf", "webhook.conf", "observe.conf", "clickhouse.conf"} {
		if err := rejectPathIfPresent(path.Join(bucket.Location, name), name, false); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		abs     string
		display string
	}{
		{bucket.WorkspaceLocation, "workspace"},
		{bucket.TemplatesLocation, "templates"},
		{bucket.SecretLocation, "secrets"},
	} {
		if err := rejectTreeSymlinks(item.abs, item.display); err != nil {
			return err
		}
	}
	return nil
}

func rejectPathIfPresent(abs, display string, wantDir bool) error {
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink not allowed: %s", display)
	}
	if wantDir {
		if !info.IsDir() {
			return fmt.Errorf("unsupported non-regular file: %s", display)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported non-regular file: %s", display)
	}
	return nil
}

func rejectTreeSymlinks(root, displayRoot string) error {
	if err := rejectPathIfPresent(root, displayRoot, true); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(root, func(absPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		display := displayRoot
		if rel != "." {
			display = displayRoot + "/" + rel
		}
		if absPath == root {
			return nil
		}
		if displayRoot == "workspace" {
			if jobRel, ok := jobWalkRel(rel); ok && workspace.ShouldSkipJobWalk(jobRel, d) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		entryInfo, err := d.Info()
		if err != nil {
			return err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", display)
		}
		if !entryInfo.IsDir() && !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("unsupported non-regular file: %s", display)
		}
		return nil
	})
}

// jobWalkRel returns the path relative to the job root for workspace-relative
// paths under jobs/<job>/..., and whether the path is inside a job tree.
func jobWalkRel(workspaceRel string) (string, bool) {
	workspaceRel = strings.TrimPrefix(workspaceRel, "./")
	if workspaceRel == "jobs" || !strings.HasPrefix(workspaceRel, "jobs/") {
		return "", false
	}
	rest := strings.TrimPrefix(workspaceRel, "jobs/")
	_, jobRel, ok := strings.Cut(rest, "/")
	if !ok {
		// jobs/<job> itself — not inside the job tree for skip purposes
		return "", false
	}
	return jobRel, true
}
