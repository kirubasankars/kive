// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"
	"os"
	"path"

	"kive/bucket"
	"kive/data"
	"kive/promconfig"
	"kive/workspace"
)

func assemblePrometheusAlertRules(tx *sql.Tx, _ /* prometheusJob */, prometheusJobDir, _ /* workerIP */ string) error {
	entries, err := data.ListPrometheusAlertFiles(tx)
	if err != nil {
		return err
	}

	eligible := make(map[string]bool)
	for _, entry := range entries {
		ok, err := jobHasPrometheusAssembleAllocations(tx, entry.Job, eligible)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		content, err := data.GetJobFileContent(tx, entry.Path)
		if err != nil {
			return err
		}
		vars, err := data.NewJobTemplateVars(tx, entry.Job)
		if err != nil {
			return err
		}
		_, outContent, _ := workspace.MaterializeJobTemplate(entry.Path, content, vars)
		rel := entry.Rel
		if unwrapped, ok := workspace.UnwrapPath(rel); ok {
			rel = unwrapped
		}
		dest := path.Join(prometheusJobDir, "rules", entry.Job, path.Base(rel))
		if err := os.MkdirAll(path.Dir(dest), 0o755); err != nil {
			return bucket.UnexpectedError(err)
		}
		if err := os.WriteFile(dest, []byte(outContent), 0o644); err != nil {
			return bucket.UnexpectedError(err)
		}
	}
	if err := writeKiveCertAlertRules(prometheusJobDir); err != nil {
		return err
	}
	return nil
}

func writeKiveCertAlertRules(prometheusJobDir string) error {
	dest := path.Join(prometheusJobDir, "rules", promconfig.KiveAlertsJob, promconfig.KiveCertAlertsFile)
	if err := os.MkdirAll(path.Dir(dest), 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}
	if err := os.WriteFile(dest, promconfig.KiveCertAlertsYAML, 0o644); err != nil {
		return bucket.UnexpectedError(err)
	}
	return nil
}
