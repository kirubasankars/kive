// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package workspace reads job manifests and worker definitions from disk.
package workspace

import (
	"fmt"
	"os"
	"path"
	"strings"

	"kive/bucket"
	"kive/conf"
)

// JobVarsFileName is the optional per-job vars file under workspace/jobs/<job>/.
const JobVarsFileName = "vars.conf"

// Workspace loads on-disk workspace configuration.
type Workspace interface {
	ListWorkers() ([]Worker, error)
	ListJobNames() ([]string, error)
	LoadManifest(jobName string) (Manifest, error)
	LoadDisabled() (DisabledAllocations, error)
}

// DefaultWorkspace reads from bucket.WorkspaceLocation.
type DefaultWorkspace struct{}

// Default returns the standard workspace reader.
func Default() *DefaultWorkspace {
	return &DefaultWorkspace{}
}

// GetWorkspace is deprecated; use Default.
func GetWorkspace() *DefaultWorkspace {
	return Default()
}

func (ws *DefaultWorkspace) ListWorkers() ([]Worker, error) {
	return ws.GetWorkers()
}

func (ws *DefaultWorkspace) GetWorkers() ([]Worker, error) {
	rawWorkers, err := ReadWorkersFile()
	if err != nil {
		return nil, err
	}

	workers := make([]Worker, 0, len(rawWorkers))
	seenHosts := make(map[string]struct{}, len(rawWorkers))
	for idx, raw := range rawWorkers {
		host := strings.TrimSpace(raw.Host)
		if host == "" {
			return nil, fmt.Errorf("%w: host attribute can't be empty", bucket.ErrInvalidWorkerJSON)
		}
		if _, dup := seenHosts[host]; dup {
			return nil, fmt.Errorf("%w: duplicate host %q", bucket.ErrInvalidWorkerJSON, host)
		}
		seenHosts[host] = struct{}{}
		position := idx
		if raw.Position != nil {
			position = *raw.Position
		}
		w := NewWorker(host, raw.Labels, raw.Memory, raw.CPU, raw.Tags, position)
		if hn := strings.TrimSpace(raw.Hostname); hn != "" {
			w.Hostname = hn
		}
		workers = append(workers, w)
	}
	return workers, nil
}

func (ws *DefaultWorkspace) ListJobNames() ([]string, error) {
	return ws.GetJobs()
}

func (ws *DefaultWorkspace) GetJobs() ([]string, error) {
	jobsRoot := path.Join(bucket.WorkspaceLocation, "jobs")
	entries, err := os.ReadDir(jobsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("%w: %w", bucket.ErrUnexpectedError, err)
	}

	jobs := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", bucket.ErrUnexpectedError, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink not allowed: workspace/jobs/%s", entry.Name())
		}
		if !info.IsDir() {
			continue
		}
		jobName := entry.Name()
		if err := ValidateJobName(jobName); err != nil {
			return nil, err
		}
		jobs = append(jobs, jobName)
	}
	return jobs, nil
}

func (ws *DefaultWorkspace) LoadManifest(jobName string) (Manifest, error) {
	return ws.GetJobManifest(jobName)
}

func (ws *DefaultWorkspace) GetJobManifest(jobName string) (Manifest, error) {
	data, err := LoadJobConfBytes(jobName)
	if err != nil {
		return Manifest{}, err
	}
	manifest, err := ParseJobConf(path.Join(JobFilePath(jobName), JobConfName), data)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: job %s\n%w", bucket.ErrInvalidManifest, jobName, err)
	}
	return manifest, nil
}

func (ws *DefaultWorkspace) LoadDisabled() (DisabledAllocations, error) {
	return ws.GetDisabled()
}

func (ws *DefaultWorkspace) GetDisabled() (DisabledAllocations, error) {
	if err := checkDisabledLegacyJSON(); err != nil {
		return DisabledAllocations{}, err
	}
	data, err := conf.ReadBytes(disabledConfPath())
	if err != nil {
		if os.IsNotExist(err) {
			return DisabledAllocations{}, nil
		}
		return DisabledAllocations{}, err
	}
	return ParseDisabledConf(disabledConfPath(), data)
}
