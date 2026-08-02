// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"fmt"

	"kive/bucket"
	"kive/workspace"
)

// GetJobManifestPorts loads resources.ports from a job's manifest in job_files.
func GetJobManifestPorts(tx *sql.Tx, jobName string) (workspace.ManifestPorts, error) {
	content, err := GetJobFileContent(tx, jobName+"/"+workspace.JobConfName)
	if err != nil {
		btContent, btErr := GetJobFileContent(tx, jobName+"/"+workspace.JobConfBTName)
		if btErr != nil {
			return nil, err
		}
		vars, varsErr := NewJobTemplateVars(tx, jobName)
		if varsErr != nil {
			return nil, varsErr
		}
		content = workspace.Transpile(btContent, vars)
	}
	manifest, err := workspace.ParseJobConf(jobName+"/"+workspace.JobConfName, []byte(content))
	if err != nil {
		return nil, fmt.Errorf("%w: job %s job.conf: %w", bucket.ErrInvalidJob, jobName, err)
	}
	if manifest.Resources.Ports == nil {
		return workspace.ManifestPorts{}, nil
	}
	return manifest.Resources.Ports, nil
}
