// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"

	"kive/bucket"
)

// SetAllocationVersion records the deploy target version on an allocation row (build-time).
func SetAllocationVersion(tx *sql.Tx, allocID, version string) error {
	version = NormalizeDeployVersion(version)
	_, err := tx.Exec(`UPDATE allocations SET version = ? WHERE alloc_id = ?`, version, allocID)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// GetAllocationVersion returns the target version stored on the allocation row.
func GetAllocationVersion(tx *sql.Tx, allocID string) (string, error) {
	var version sql.NullString
	err := tx.QueryRow(`SELECT version FROM allocations WHERE alloc_id = ?`, allocID).Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return DefaultAllocationVersion, nil
		}
		return "", bucket.DatabaseError(err)
	}
	return normalizeStoredVersion(version), nil
}

// GetVersionPendingNonRemovedAllocations returns workers (active or disabled) whose running
// version differs from the build target.
func GetVersionPendingNonRemovedAllocations(tx *sql.Tx, job string) ([]string, error) {
	rows, err := tx.Query(`
		SELECT a.worker_ip
		FROM allocations a
		LEFT JOIN allocation_hashes h ON h.alloc_id = a.alloc_id
		WHERE a.job = ? AND a.removed = 0
		  AND ifnull(h.applied_version, ?) != ifnull(a.version, ?)`,
		job, DefaultAllocationVersion, DefaultAllocationVersion,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	workers := make([]string, 0)
	for rows.Next() {
		var workerIP string
		if err := rows.Scan(&workerIP); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		workers = append(workers, workerIP)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return workers, nil
}

// GetVersionPendingAllocations returns active workers whose running version differs from the build target.
func GetVersionPendingAllocations(tx *sql.Tx, job string) ([]string, error) {
	rows, err := tx.Query(`
		SELECT a.worker_ip
		FROM allocations a
		LEFT JOIN allocation_hashes h ON h.alloc_id = a.alloc_id
		WHERE a.job = ? AND a.removed = 0 AND a.disabled = 0
		  AND ifnull(h.applied_version, ?) != ifnull(a.version, ?)`,
		job, DefaultAllocationVersion, DefaultAllocationVersion,
	)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	workers := make([]string, 0)
	for rows.Next() {
		var workerIP string
		if err := rows.Scan(&workerIP); err != nil {
			return nil, bucket.DatabaseError(err)
		}
		workers = append(workers, workerIP)
	}
	if err := rowsErr(rows); err != nil {
		return nil, err
	}
	return workers, nil
}

// AllocationNeedsVersionRollout reports whether running and target versions differ for one allocation.
func AllocationNeedsVersionRollout(tx *sql.Tx, job, workerIP string) (bool, error) {
	allocID, err := GetAllocationID(tx, workerIP, job)
	if err != nil {
		return false, err
	}
	versions, err := GetAllocationVersions(tx, allocID)
	if err != nil {
		return false, err
	}
	return versions.AppliedVersion != versions.Version, nil
}
