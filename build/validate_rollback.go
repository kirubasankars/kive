// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"database/sql"
	"fmt"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/workspace"
)

func ValidateRollbackPolicy(tx *sql.Tx, jobWorkspace *workspace.DefaultWorkspace) error {
	jobNames, err := jobWorkspace.GetJobs()
	if err != nil {
		return err
	}

	var failures []string
	for _, jobName := range jobNames {
		manifest, err := jobWorkspace.GetJobManifest(jobName)
		if err != nil {
			return err
		}
		if manifest.AllowsRollback() {
			continue
		}

		targetRaw := data.NormalizeDeployVersion(manifest.JobVersion())
		target, err := workspace.ParseVersion(targetRaw)
		if err != nil {
			return fmt.Errorf("%w: job %q: %w", bucket.ErrInvalidJobVersion, jobName, err)
		}

		allocations, err := listAllocationAppliedVersions(tx, jobName)
		if err != nil {
			return err
		}

		for _, alloc := range allocations {
			currentRaw := strings.TrimSpace(alloc.AppliedVersion)
			if currentRaw == "" {
				continue
			}

			current, err := workspace.ParseVersion(currentRaw)
			if err != nil {
				failures = append(failures, fmt.Sprintf(
					"job %q allocation %q on %s has invalid applied_version %q",
					jobName, alloc.AllocID, alloc.WorkerIP, currentRaw,
				))
				continue
			}

			if current.Compare(target) <= 0 {
				continue
			}

			failures = append(failures, fmt.Sprintf(
				"job %q allocation %q on %s applied_version %s exceeds target version %s (set allow_rollback: true to permit downgrade)",
				jobName, alloc.AllocID, alloc.WorkerIP, current, target,
			))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%w\n%s", bucket.ErrVersionRollbackNotAllowed, strings.Join(failures, "\n"))
	}
	return nil
}
