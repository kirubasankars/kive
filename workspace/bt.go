// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"os"
	"path"
	"strings"

	"kive/buildinfo"
)

// JobTemplateExt is the file suffix for job-name templates transpiled when materializing.
const JobTemplateExt = ".bt"

// JobNamePlaceholder is the canonical token replaced with the job folder name
// during .bt transpile. JobNamePlaceholderAlias ([[jobname]]) is also accepted.
const JobNamePlaceholder = "[[job_name]]"

// JobNamePlaceholderAlias is a spelling variant of JobNamePlaceholder.
const JobNamePlaceholderAlias = "[[jobname]]"

// BucketIDPlaceholder is replaced with the catalog bucket public id.
const BucketIDPlaceholder = "[[bucket_id]]"

// BucketIDPlaceholderAlias is a spelling variant of BucketIDPlaceholder.
const BucketIDPlaceholderAlias = "[[bucketid]]"

// JobIDPlaceholder is replaced with the catalog job public id.
const JobIDPlaceholder = "[[job_id]]"

// JobIDPlaceholderAlias is a spelling variant of JobIDPlaceholder.
const JobIDPlaceholderAlias = "[[jobid]]"

// KiveGitHashPlaceholder is replaced with the running kive binary git hash.
const KiveGitHashPlaceholder = "[[kive_git_hash]]"

// KiveGitHashPlaceholderAlias is a spelling variant of KiveGitHashPlaceholder.
const KiveGitHashPlaceholderAlias = "[[kivegithash]]"

// JobTemplateVars holds values substituted into .bt job template content.
type JobTemplateVars struct {
	JobName     string
	BucketID    string
	JobID       string
	KiveGitHash string
}

// JobTemplateVarsForName returns vars with job name and the running kive git hash.
// BucketID and JobID are left empty (for workspace-only readers without catalog IDs).
func JobTemplateVarsForName(jobName string) JobTemplateVars {
	return JobTemplateVars{
		JobName:     jobName,
		KiveGitHash: buildinfo.Hash(),
	}
}

// Transpile replaces .bt placeholders in job template content.
func Transpile(content string, vars JobTemplateVars) string {
	content = strings.ReplaceAll(content, JobNamePlaceholder, vars.JobName)
	content = strings.ReplaceAll(content, JobNamePlaceholderAlias, vars.JobName)
	content = strings.ReplaceAll(content, BucketIDPlaceholder, vars.BucketID)
	content = strings.ReplaceAll(content, BucketIDPlaceholderAlias, vars.BucketID)
	content = strings.ReplaceAll(content, JobIDPlaceholder, vars.JobID)
	content = strings.ReplaceAll(content, JobIDPlaceholderAlias, vars.JobID)
	content = strings.ReplaceAll(content, KiveGitHashPlaceholder, vars.KiveGitHash)
	content = strings.ReplaceAll(content, KiveGitHashPlaceholderAlias, vars.KiveGitHash)
	return content
}

// UnwrapPath strips the .bt suffix from a job-relative path.
func UnwrapPath(relPath string) (outPath string, ok bool) {
	if !IsJobTemplateFile(path.Base(relPath)) {
		return relPath, false
	}
	return relPath[:len(relPath)-len(JobTemplateExt)], true
}

// IsJobTemplateFile reports whether name ends with .bt.
func IsJobTemplateFile(name string) bool {
	return len(name) > len(JobTemplateExt) && name[len(name)-len(JobTemplateExt):] == JobTemplateExt
}

// MaterializeJobTemplate transpiles and unwraps a .bt job file for staging.
// Non-.bt paths are returned unchanged with changed=false.
func MaterializeJobTemplate(relPath, content string, vars JobTemplateVars) (outPath, outContent string, changed bool) {
	if !IsJobTemplateFile(path.Base(relPath)) {
		return relPath, content, false
	}
	outPath, _ = UnwrapPath(relPath)
	return outPath, Transpile(content, vars), true
}

// CheckJobTemplateConflicts rejects jobs that define both a plain file and its .bt counterpart.
func CheckJobTemplateConflicts(jobName string) error {
	return CheckTemplateFileConflicts(jobName)
}

func manifestFileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
