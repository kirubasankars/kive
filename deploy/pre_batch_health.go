// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"
	"log"

	"kive/bucket"
	"kive/healthcheck"
)

// preBatchHealthCheck runs the deploy pre-batch health gate unless force is set.
// --force skips pre-batch health so an already-unhealthy cluster can be redeployed;
// post-batch health still applies after promote.
func preBatchHealthCheck(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, job string, force bool) error {
	if force {
		log.Printf("deploy: skipping pre-batch health for %s (--force)", job)
		return nil
	}
	return healthcheck.DeployHealthCheck(deployCancelCtx(ctx), tx, rt, true, job, true)
}
