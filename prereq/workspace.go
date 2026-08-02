// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package prereq

import (
	"os"
	"path/filepath"
	"strings"

	"kive/bucket"
)

// WorkspaceHookRuntimes reports which optional hook interpreters the workspace needs.
type WorkspaceHookRuntimes struct {
	NeedsJS   bool
	NeedsRuby bool
}

// WorkspaceHookRuntimesNeeded scans workspace jobs for hook script extensions.
func WorkspaceHookRuntimesNeeded() (WorkspaceHookRuntimes, error) {
	jobsRoot := filepath.Join(bucket.WorkspaceLocation, "jobs")
	return workspaceHookRuntimesIn(jobsRoot)
}

func workspaceHookRuntimesIn(jobsRoot string) (WorkspaceHookRuntimes, error) {
	var out WorkspaceHookRuntimes
	entries, err := os.ReadDir(jobsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		hooksDir := filepath.Join(jobsRoot, entry.Name(), "_hooks")
		if err := hooksDirScanRuntimes(hooksDir, &out); err != nil {
			return out, err
		}
		if out.NeedsJS && out.NeedsRuby {
			return out, nil
		}
	}
	return out, nil
}

func hooksDirScanRuntimes(hooksDir string, out *WorkspaceHookRuntimes) error {
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "hook_") {
			continue
		}
		switch filepath.Ext(name) {
		case ".ts", ".js":
			out.NeedsJS = true
		case ".rb":
			out.NeedsRuby = true
		}
	}
	return nil
}
