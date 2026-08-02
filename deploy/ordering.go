// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"

	"kive/rollout"
)

const (
	orderSourceKV      = rollout.OrderSourceKV
	orderSourceDefault = rollout.OrderSourceDefault
)

// ResolvedRolloutOrder is the allocation order used for a rollout phase.
type ResolvedRolloutOrder = rollout.ResolvedOrder

// ResolveRolloutOrder picks rollout order for candidates using rollout_order KV when valid.
func ResolveRolloutOrder(tx *sql.Tx, job string, candidates []string) (ResolvedRolloutOrder, error) {
	return rollout.ResolveOrder(tx, job, candidates)
}
