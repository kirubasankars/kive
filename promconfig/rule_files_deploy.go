// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package promconfig

import (
	"database/sql"
	"fmt"
	"path"
	"strings"

	"kive/data"
	"kive/workspace"
)

// RenderRuleFilesYAML returns a rule_files YAML fragment listing assembled alert paths.
// Paths are relative to the prometheus config file: rules/<job>/<file>.yaml
// (*.yaml.bt catalog entries are unwrapped to match assemble output).
// Jobs without at least one non-removed allocation are omitted (same gate as assemble).
func RenderRuleFilesYAML(tx *sql.Tx) (string, error) {
	entries, err := data.ListPrometheusAlertFiles(tx)
	if err != nil {
		return "", err
	}
	hasPrometheusJob, err := data.JobHasPrometheusServerConfig(tx, "prometheus")
	if err != nil {
		return "", err
	}

	eligible := make(map[string]bool)
	filtered := make([]data.PrometheusAlertFileEntry, 0, len(entries))
	for _, entry := range entries {
		ok, cached := eligible[entry.Job]
		if !cached {
			ok, err = data.JobHasNonRemovedAllocations(tx, entry.Job)
			if err != nil {
				return "", err
			}
			eligible[entry.Job] = ok
		}
		if ok {
			filtered = append(filtered, entry)
		}
	}

	if len(filtered) == 0 && !hasPrometheusJob {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("rule_files:\n")
	if hasPrometheusJob {
		_, err := fmt.Fprintf(&b, "  - rules/%s/%s\n", KiveAlertsJob, KiveCertAlertsFile)
		if err != nil {
			return "", err
		}
	}
	for _, entry := range filtered {
		base := path.Base(entry.Rel)
		if unwrapped, ok := workspace.UnwrapPath(base); ok {
			base = unwrapped
		}
		_, err := fmt.Fprintf(&b, "  - rules/%s/%s\n", entry.Job, base)
		if err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// RuleFilesGlob returns a filepath.Glob pattern for kive-assembled rules (one job directory level).
func RuleFilesGlob(absolute bool) string {
	if absolute {
		return "/etc/prometheus/rules/*/*.yaml"
	}
	return "rules/*/*.yaml"
}
