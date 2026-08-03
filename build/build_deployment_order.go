// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"database/sql"

	"kive/data"
)

// BuildDeploymentOrder assigns contiguous zero-based deployment_order ranks from
// deployment_priority, dependency-derived deployment_seq, and job name.
func BuildDeploymentOrder(tx *sql.Tx) error {
	return data.AssignDeploymentOrders(tx)
}
