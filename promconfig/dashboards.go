// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package promconfig

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// DashboardFileEntry indexes one dashboard file under _prometheus/dashboards/.
type DashboardFileEntry struct {
	Job  string `json:"job"`
	Rel  string `json:"rel"`
	File string `json:"file"`
}

// ListDashboardFiles returns relative paths under _prometheus/ for dashboard files.
func ListDashboardFiles(jobName string) ([]DashboardFileEntry, error) {
	root := path.Join(JobPrometheusDir(jobName), DashboardsDir)
	entries, err := walkDashboardFiles(jobName, root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

func walkDashboardFiles(jobName, root string) ([]DashboardFileEntry, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	entries := make([]DashboardFileEntry, 0)
	err = filepath.WalkDir(root, func(absPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		filePath := path.Join(jobName, "_prometheus", DashboardsDir, rel)
		entries = append(entries, DashboardFileEntry{
			Job:  jobName,
			Rel:  path.Join(DashboardsDir, rel),
			File: filePath,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}
