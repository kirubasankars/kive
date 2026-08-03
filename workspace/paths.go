// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"kive/bucket"
)

var reservedJobRuntimeDirNames = []string{"data", "logs", "bin"}

var skipJobWalkDirNames = map[string]struct{}{
	".venv":        {},
	"venv":         {},
	"node_modules": {},
	"__pycache__":  {},
}

// JobHooksDirName is the workspace subdirectory for hook scripts (hook_*.py|ts|js|rb|sh).
const JobHooksDirName = "_hooks"

// JobCertsDirName is the job-root directory for external BYO TLS PEMs (not job_files).
const JobCertsDirName = "certs"

// JobHooksGlobPattern matches hook scripts relative to the job directory root.
const JobHooksGlobPattern = "_hooks/**"

// JobHooksDir returns workspace/jobs/<job>/_hooks.
func JobHooksDir(jobName string) string {
	return path.Join(bucket.WorkspaceLocation, "jobs", jobName, JobHooksDirName)
}

// IsJobHooksPath reports whether rel is the _hooks directory or a path under it.
func IsJobHooksPath(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	return rel == JobHooksDirName || strings.HasPrefix(rel, JobHooksDirName+"/")
}

// IsJobHostLocalPath reports whether rel (relative to the job root) lies under a
// directory whose name starts with '_'. Those trees stay on the CLI host
// (staging, hooks, prometheus assembly) and are not rsynced to workers.
func IsJobHostLocalPath(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if rel == "" || rel == "." {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part != "" && strings.HasPrefix(part, "_") {
			return true
		}
	}
	return false
}

// JobFilePath returns an absolute path under workspace/jobs/.
func JobFilePath(relativePath string) string {
	return path.Join(bucket.WorkspaceLocation, "jobs", relativePath)
}

// GetJobFilePath is deprecated; use JobFilePath.
func GetJobFilePath(fpath string) string {
	return JobFilePath(fpath)
}

// ValidateReservedRuntimeDirs rejects job-root data, logs, and bin paths in the workspace.
// Those names are reserved for runtime directories on workers (created by Makefile, excluded from rsync).
func ValidateReservedRuntimeDirs(jobName string) error {
	for _, name := range reservedJobRuntimeDirNames {
		_, err := os.Stat(JobFilePath(path.Join(jobName, name)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat job %s/%s: %w", jobName, name, err)
		}
		return fmt.Errorf("%w: job %s, %s is reserved for runtime use on workers", bucket.ErrInvalidJob, jobName, name)
	}
	return nil
}

// IsReservedJobRuntimePath reports whether storePath is a reserved job-root runtime directory.
func IsReservedJobRuntimePath(jobName, storePath string) bool {
	for _, name := range reservedJobRuntimeDirNames {
		if storePath == path.Join(jobName, name) {
			return true
		}
	}
	return false
}

// JobRuntimeDirRsyncProtectFilterLines returns rsync protect (P) rules so --delete does not
// remove job runtime directories (data/, logs/, bin/) on workers.
func JobRuntimeDirRsyncProtectFilterLines() []string {
	lines := make([]string, 0, len(reservedJobRuntimeDirNames)*2)
	for _, name := range reservedJobRuntimeDirNames {
		lines = append(lines,
			fmt.Sprintf("P jobs/*/%s/\n", name),
			fmt.Sprintf("P jobs/*/%s/**\n", name),
		)
	}
	return lines
}

// WalkJobFiles walks files for jobName under workspace/jobs/.
// Skips .venv, venv, node_modules, __pycache__, and job-root certs/ trees.
// Callback paths are relative to workspace/jobs/ (e.g. api/Makefile).
func WalkJobFiles(jobName string, callback func(path string, d fs.DirEntry, err error) error) error {
	return fs.WalkDir(os.DirFS(path.Join(bucket.WorkspaceLocation, "jobs")), jobName, func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relToJob := rel
		if rel == jobName {
			relToJob = "."
		} else if strings.HasPrefix(rel, jobName+"/") {
			relToJob = strings.TrimPrefix(rel, jobName+"/")
		}
		if relToJob != "." && shouldSkipJobWalk(relToJob, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		return callback(rel, d, err)
	})
}

// ShouldSkipJobWalk reports whether rel/d should be skipped when walking job trees.
func ShouldSkipJobWalk(rel string, d fs.DirEntry) bool {
	return shouldSkipJobWalk(rel, d)
}

// ShouldSkipJobPath reports whether rel is within a dependency or cache tree
// that is excluded from job source.
func ShouldSkipJobPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if _, skip := skipJobWalkDirNames[part]; skip {
			return true
		}
	}
	return false
}

func shouldSkipJobWalk(rel string, d fs.DirEntry) bool {
	if IsJobRootCertsPath(rel) {
		return true
	}
	if d.IsDir() {
		if _, skip := skipJobWalkDirNames[d.Name()]; skip {
			return true
		}
	}
	return ShouldSkipJobPath(rel)
}

// IsJobRootCertsPath reports whether rel (relative to the job root) is the
// job-root certs/ directory or a path under it. Nested directories named certs
// (e.g. config/certs) are not matched.
func IsJobRootCertsPath(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if rel == "" || rel == "." {
		return false
	}
	return rel == JobCertsDirName || strings.HasPrefix(rel, JobCertsDirName+"/")
}

// ExternalCertPaths returns workspace paths for an external cert name.
func ExternalCertPaths(jobName, certName string) (crtPath, keyPath string) {
	base := JobFilePath(path.Join(jobName, JobCertsDirName, certName))
	return base + ".crt", base + ".key"
}

// JobHasMakefileOrTemplate reports whether the job defines Makefile in a supported form.
func JobHasMakefileOrTemplate(jobName string) bool {
	jobDir := JobFilePath(jobName)
	for _, name := range []string{
		"Makefile",
		"Makefile" + JobTemplateExt,
		"Makefile" + DeployTemplateExt,
		"Makefile" + CombinedTemplateExt,
	} {
		if _, err := os.Stat(path.Join(jobDir, name)); err == nil {
			return true
		}
	}
	return false
}
