// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"

	"kive/data"
)

// jobHasPrometheusAssembleAllocations reports whether job has ≥1 non-removed
// allocation (active or disabled). Results are cached in eligible for the call.
func jobHasPrometheusAssembleAllocations(tx *sql.Tx, job string, eligible map[string]bool) (bool, error) {
	if v, ok := eligible[job]; ok {
		return v, nil
	}
	ok, err := data.JobHasNonRemovedAllocations(tx, job)
	if err != nil {
		return false, err
	}
	eligible[job] = ok
	return ok, nil
}
