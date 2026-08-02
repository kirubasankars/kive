// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"strings"
)

// DefaultAllocationVersion is the running version before the first successful promote.
const DefaultAllocationVersion = "0.0.0"

// NormalizeDeployVersion maps empty or unknown catalog versions to DefaultAllocationVersion.
func NormalizeDeployVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "unknown") {
		return DefaultAllocationVersion
	}
	return raw
}

// TargetJobVersion returns the deploy target version string for a job.
func TargetJobVersion(tx *sql.Tx, job string) (string, error) {
	version, err := GetJobVersion(tx, job)
	if err != nil {
		return "", err
	}
	return NormalizeDeployVersion(version), nil
}

// AllocationVersions holds per-allocation applied (running) and target versions.
type AllocationVersions struct {
	AppliedVersion string
	Version        string
}

func normalizeStoredVersion(value sql.NullString) string {
	if !value.Valid {
		return DefaultAllocationVersion
	}
	return NormalizeDeployVersion(value.String)
}
