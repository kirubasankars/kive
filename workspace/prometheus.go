// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"os"
	"path"
	"strings"

	"kive/bucket"
)

const (
	prometheusConfigFile    = "prometheus.yml"
	prometheusConfigTplFile = "prometheus.yml.tpl"
)

// PrometheusServerConfigFileNames are accepted workspace/job_files names for the prometheus server config.
func PrometheusServerConfigFileNames() []string {
	return []string{
		prometheusConfigFile,
		prometheusConfigFile + JobTemplateExt,
		prometheusConfigTplFile,
		prometheusConfigTplFile + JobTemplateExt,
	}
}

// JobHasPrometheusServerConfigFile reports whether the job defines prometheus.yml in any template form.
func JobHasPrometheusServerConfigFile(jobName string) bool {
	jobDir := JobFilePath(jobName)
	for _, name := range PrometheusServerConfigFileNames() {
		if fileExists(path.Join(jobDir, name)) {
			return true
		}
	}
	return false
}

// ValidatePrometheusServerFiles ensures a job does not define conflicting prometheus config sources.
func ValidatePrometheusServerFiles(jobName string) error {
	var present []string
	for _, name := range PrometheusServerConfigFileNames() {
		rel := path.Join(jobName, name)
		if fileExists(JobFilePath(rel)) {
			present = append(present, rel)
		}
	}
	if len(present) > 1 {
		return fmt.Errorf(
			"%w: job %s has conflicting template sources for %s: %s",
			bucket.ErrInvalidJob, jobName, prometheusConfigFile, strings.Join(present, ", "),
		)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
