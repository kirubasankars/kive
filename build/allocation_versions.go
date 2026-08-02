// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"database/sql"

	"kive/bucket"
)

type allocationAppliedVersion struct {
	AllocID        string
	WorkerIP       string
	AppliedVersion string
}

func listAllocationAppliedVersions(tx *sql.Tx, jobName string) ([]allocationAppliedVersion, error) {
	rows, err := tx.Query(`
		SELECT a.alloc_id, a.worker_ip, ifnull(h.applied_version, '')
		FROM allocations a
		LEFT JOIN allocation_hashes h ON h.alloc_id = a.alloc_id
		WHERE a.job = ? AND a.removed = 0`,
		jobName,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	allocations := make([]allocationAppliedVersion, 0)
	for rows.Next() {
		var alloc allocationAppliedVersion
		if err := rows.Scan(&alloc.AllocID, &alloc.WorkerIP, &alloc.AppliedVersion); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		allocations = append(allocations, alloc)
	}
	if err := rows.Err(); err != nil {
		return nil, bucket.DatabaseError(err)
	}
	return allocations, nil
}
