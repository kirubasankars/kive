// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"
	"os"

	"kive/bucket"
)

// refreshPlanHashesForJobPlan stages one job and updates plan hashes from the
// build-time catalog and KV. pre_deploy is not run here; it runs separately after
// all hashes are refreshed.
func refreshPlanHashesForJobPlan(tx *sql.Tx, job string) error {
	return RefreshPlanHashesForJobs(tx, []string{job})
}

// RefreshPlanHashesForJobs stages jobs under tmp/workers/ and updates allocation
// pending_hash from the rendered tree. Deploy runs this before JobNeedsRollout;
// PreviewPendingRollout runs it after build so content and KV baked into the
// staged tree are visible without waiting for deploy. pre_deploy is not run.
func RefreshPlanHashesForJobs(tx *sql.Tx, jobs []string) error {
	if len(jobs) == 0 {
		return nil
	}

	if err := os.MkdirAll(bucket.TempLocation, 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}

	if err := prepareJobsFiles(tx, jobs); err != nil {
		return err
	}
	return updateAllocationHash(tx, jobs)
}
