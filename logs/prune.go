// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package logs

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"kive/bucket"
)

// PruneOptions controls deletion of logs/runs/<run_id>/ directories.
type PruneOptions struct {
	RetentionCount int // 0 = no count limit
	RetentionDays  int // 0 = no age limit
}

type runLogDir struct {
	path string
	mod  time.Time
}

// PruneRunLogs deletes old run log directories under logs/runs/.
func PruneRunLogs(opts PruneOptions) (removed int, err error) {
	if opts.RetentionCount == 0 && opts.RetentionDays == 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(bucket.RunLogLocation)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	dirs := make([]runLogDir, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(bucket.RunLogLocation, entry.Name())
		if _, err := os.Stat(filepath.Join(path, ".server-active")); err == nil {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return removed, statErr
		}
		dirs = append(dirs, runLogDir{path: path, mod: info.ModTime()})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].mod.After(dirs[j].mod)
	})

	toDelete := make(map[string]struct{})
	if opts.RetentionCount > 0 {
		for i := opts.RetentionCount; i < len(dirs); i++ {
			toDelete[dirs[i].path] = struct{}{}
		}
	}

	if opts.RetentionDays > 0 {
		cutoff := time.Now().Add(-time.Duration(opts.RetentionDays) * 24 * time.Hour)
		for _, dir := range dirs {
			if dir.mod.Before(cutoff) {
				toDelete[dir.path] = struct{}{}
			}
		}
	}

	for path := range toDelete {
		if rmErr := os.RemoveAll(path); rmErr != nil {
			log.Printf("logs: remove run log directory %s: %v", path, rmErr)
			continue
		}
		removed++
	}
	return removed, nil
}

// PruneRunLogsFromConfig reads retention settings from kive.conf and prunes run logs.
func PruneRunLogsFromConfig() (removed int, err error) {
	count, days, disabled, err := bucket.LogRunRetentionFromConfig()
	if err != nil || disabled {
		return 0, err
	}
	return PruneRunLogs(PruneOptions{
		RetentionCount: count,
		RetentionDays:  days,
	})
}
