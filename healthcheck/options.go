// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package healthcheck

import (
	"database/sql"

	"kive/data"
)

// CheckOptions configures manifest probe targets for a health check run.
// ProbeWorkers, when non-empty, limits built-in probes (tcp/http/ssh) to that list.
// DeployMode marks a deploy-session health gate: worker scope is live promoted
// allocations (applied_hash set and not health_failed). Standalone health-check
// also probes health_failed allocations.
// Kind filters which channel to run: "" / "all", "liveness", or "readiness".
// PersistResults stores per-allocation rows in health_status when RunID is set.
type CheckOptions struct {
	ProbeWorkers   []string
	DeployMode     bool
	Kind           string
	PersistResults bool
	RunID          string
	// PersistDB, when set, receives short write transactions for status rows
	// so probes do not hold a wrapping write lock on kive.db.
	PersistDB *sql.DB
	recorder  *statusRecorder
}

// PreRollingProbeWorkers returns deployed active allocations expected to be serving
// traffic before a deploy lifecycle wave (excludes new and start-pending workers).
func PreRollingProbeWorkers(tx *sql.Tx, job string) ([]string, error) {
	return data.GetDeployedActiveAllocations(tx, job)
}

func resolveProbeWorkers(tx *sql.Tx, job string, opts CheckOptions) ([]string, error) {
	if len(opts.ProbeWorkers) > 0 {
		return opts.ProbeWorkers, nil
	}
	return resolveHealthCheckWorkers(tx, job, opts.DeployMode)
}
