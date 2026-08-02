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
	"sort"
	"strings"

	"kive/bucket"
)

// DeployTemplateExt is the file suffix for Go templates rendered at deploy time.
const DeployTemplateExt = ".tpl"

// CombinedTemplateExt is the only valid combined suffix (.tpl then .bt).
const CombinedTemplateExt = DeployTemplateExt + JobTemplateExt

// TemplateForm describes which template suffixes a job file uses.
type TemplateForm int

const (
	TemplateFormPlain TemplateForm = iota
	TemplateFormBT
	TemplateFormTPL
	TemplateFormTPLBT
)

const invalidCombinedTemplateExt = JobTemplateExt + DeployTemplateExt

// ParseTemplateForm parses template suffixes on a file basename.
// Rejects .bt.tpl (wrong order); accepts plain, .bt, .tpl, and .tpl.bt.
func ParseTemplateForm(name string) (baseName string, form TemplateForm, err error) {
	if strings.HasSuffix(name, invalidCombinedTemplateExt) {
		return "", TemplateFormPlain, fmt.Errorf(
			"%w: %s.bt.tpl is not supported; use .tpl.bt",
			bucket.ErrInvalidJob, strings.TrimSuffix(name, invalidCombinedTemplateExt),
		)
	}
	switch {
	case strings.HasSuffix(name, CombinedTemplateExt):
		return name[:len(name)-len(CombinedTemplateExt)], TemplateFormTPLBT, nil
	case strings.HasSuffix(name, JobTemplateExt):
		return name[:len(name)-len(JobTemplateExt)], TemplateFormBT, nil
	case strings.HasSuffix(name, DeployTemplateExt):
		return name[:len(name)-len(DeployTemplateExt)], TemplateFormTPL, nil
	default:
		return name, TemplateFormPlain, nil
	}
}

// IsDeployTemplateFile reports whether name ends with .tpl but not .tpl.bt.
func IsDeployTemplateFile(name string) bool {
	return strings.HasSuffix(name, DeployTemplateExt) && !strings.HasSuffix(name, CombinedTemplateExt)
}

// UnwrapDeployTemplatePath strips the .tpl suffix from a job-relative path.
func UnwrapDeployTemplatePath(relPath string) (outPath string, ok bool) {
	base := path.Base(relPath)
	if !IsDeployTemplateFile(base) {
		return relPath, false
	}
	return relPath[:len(relPath)-len(DeployTemplateExt)], true
}

// CanonicalPlainPath returns the final plain path after build and deploy processing.
func CanonicalPlainPath(jobRel string) (string, error) {
	dir, name := path.Split(jobRel)
	baseName, _, err := ParseTemplateForm(name)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return baseName, nil
	}
	return path.Join(dir, baseName), nil
}

// CheckTemplateFileConflicts rejects jobs with multiple template sources for the same output path.
func CheckTemplateFileConflicts(jobName string) error {
	var filePaths []string
	err := WalkJobFiles(jobName, func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		filePaths = append(filePaths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return err
	}
	return CheckTemplatePathConflicts(jobName, filePaths)
}

// CheckTemplatePathConflicts applies template conflict validation to an
// immutable list of job file paths.
func CheckTemplatePathConflicts(jobName string, filePaths []string) error {
	byCanonical := make(map[string][]string)
	for _, rel := range filePaths {
		if _, _, parseErr := ParseTemplateForm(path.Base(rel)); parseErr != nil {
			return fmt.Errorf("%w: job %s file %s: %w", bucket.ErrInvalidJob, jobName, rel, parseErr)
		}
		canonical, canonicalErr := CanonicalPlainPath(rel)
		if canonicalErr != nil {
			return canonicalErr
		}
		if isPrometheusExclusiveTemplatePath(canonical) {
			continue
		}
		byCanonical[canonical] = append(byCanonical[canonical], rel)
	}
	for canonical, sources := range byCanonical {
		if len(sources) <= 1 {
			continue
		}
		sort.Strings(sources)
		return fmt.Errorf(
			"%w: job %s has conflicting template sources for %s: %s",
			bucket.ErrInvalidJob, jobName, canonical, strings.Join(sources, ", "),
		)
	}
	return nil
}

// isPrometheusExclusiveTemplatePath reports paths whose plain/tpl mutual exclusion is
// validated by promconfig when a prometheus server job exists, not here.
func isPrometheusExclusiveTemplatePath(canonical string) bool {
	base := path.Base(canonical)
	dir := path.Dir(canonical)
	if base == "scrape.yaml" && path.Base(dir) == "_prometheus" {
		return true
	}
	return base == prometheusConfigFile
}

// JobHasManifest reports whether the job defines job.conf or job.conf.bt.
func JobHasManifest(jobName string) bool {
	jobDir := JobFilePath(jobName)
	for _, name := range []string{JobConfName, JobConfBTName} {
		if _, err := os.Stat(path.Join(jobDir, name)); err == nil {
			return true
		}
	}
	return false
}

func unsupportedJobConfForms() []string {
	return []string{
		JobConfName + DeployTemplateExt,
		JobConfName + CombinedTemplateExt,
		JobConfName + invalidCombinedTemplateExt,
		"job.conf.jt",
		LegacyManifestConfName,
		LegacyManifestConfBTName,
		LegacyManifestConfName + DeployTemplateExt,
		LegacyManifestConfName + CombinedTemplateExt,
		LegacyManifestConfName + invalidCombinedTemplateExt,
		"job.conf.jt",
		ManifestJSONName,
		ManifestJSONName + JobTemplateExt,
		ManifestJSONName + DeployTemplateExt,
		ManifestJSONName + CombinedTemplateExt,
		"manifest.json.jt",
	}
}

func invalidManifestFormExists(jobDir string) (string, bool) {
	for _, name := range unsupportedJobConfForms() {
		if _, err := os.Stat(path.Join(jobDir, name)); err == nil {
			return name, true
		}
	}
	return "", false
}

// ValidateRequiredJobFiles ensures each job has a valid manifest and Makefile source.
func ValidateRequiredJobFiles(jobName string) error {
	var filePaths []string
	err := WalkJobFiles(jobName, func(rel string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			filePaths = append(filePaths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return err
	}
	return ValidateRequiredJobFilePaths(jobName, filePaths)
}

// ValidateRequiredJobFilePaths validates required root files against an
// immutable list of captured paths.
func ValidateRequiredJobFilePaths(jobName string, filePaths []string) error {
	files := make(map[string]struct{}, len(filePaths))
	for _, filePath := range filePaths {
		files[filepath.ToSlash(filePath)] = struct{}{}
	}
	rootFile := func(name string) bool {
		_, ok := files[path.Join(jobName, name)]
		return ok
	}
	for _, name := range unsupportedJobConfForms() {
		if !rootFile(name) {
			continue
		}
		hint := "use job.conf or job.conf.bt"
		switch {
		case strings.HasPrefix(name, "manifest.json"):
			hint = "use job.conf or job.conf.bt (JSON manifests are no longer supported)"
		case strings.HasPrefix(name, "manifest.conf"):
			hint = "use job.conf or job.conf.bt (manifest.conf was renamed to job.conf)"
		case strings.HasSuffix(name, ".jt"):
			hint = ".jt was renamed to .bt; use job.conf or job.conf.bt"
		}
		return fmt.Errorf(
			"%w: job %s, %s is not supported; %s",
			bucket.ErrInvalidJob, jobName, name, hint,
		)
	}
	if !rootFile(JobConfName) && !rootFile(JobConfBTName) {
		return fmt.Errorf(
			"%w: job %s, job.conf or job.conf.bt not found",
			bucket.ErrInvalidJob, jobName,
		)
	}
	hasMakefile := false
	for _, name := range []string{"Makefile", "Makefile" + JobTemplateExt, "Makefile" + DeployTemplateExt, "Makefile" + CombinedTemplateExt} {
		hasMakefile = hasMakefile || rootFile(name)
	}
	if !hasMakefile {
		return fmt.Errorf(
			"%w: job %s, Makefile, Makefile.bt, Makefile.tpl, or Makefile.tpl.bt not found",
			bucket.ErrInvalidJob, jobName,
		)
	}
	return nil
}
