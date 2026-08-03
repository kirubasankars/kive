// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"database/sql"
	"fmt"
	"strings"

	"kive/bucket"
	"kive/workspace"
)

func ValidateBackwardCompatibility(tx *sql.Tx, jobWorkspace *workspace.DefaultWorkspace) error {
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
		if len(manifest.BackwardCompatibilityFrom) == 0 {
			continue
		}

		spec, err := workspace.ParseBackwardCompatibilityFrom(manifest.BackwardCompatibilityFrom)
		if err != nil {
			return fmt.Errorf("%w: job %q: %w", bucket.ErrInvalidManifest, jobName, err)
		}

		allocations, err := listAllocationAppliedVersions(tx, jobName)
		if err != nil {
			return err
		}

		allowed := strings.Join(manifest.BackwardCompatibilityFrom, " OR ")
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

			if spec.Satisfies(current) {
				continue
			}

			failures = append(failures, fmt.Sprintf(
				"job %q allocation %q on %s applied_version %s not in backward_compatibility_from (%s)",
				jobName, alloc.AllocID, alloc.WorkerIP, current, allowed,
			))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%w\n%s", bucket.ErrBackwardCompatibility, strings.Join(failures, "\n"))
	}
	return nil
}
