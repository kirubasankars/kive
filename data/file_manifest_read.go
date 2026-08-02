// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"errors"

	"kive/bucket"
)

// AllocationFileManifests holds pending and applied per-file digests for an allocation.
type AllocationFileManifests struct {
	Pending FileManifest
	Applied FileManifest
	// HasAppliedFiles is true when the allocation plan hash row has applied_hash set
	// (legacy / no-manifest gate). It is not "any applied per-file digest rows."
	HasAppliedFiles bool
}

// GetAllocationFileManifests returns pending and applied file manifests for an allocation.
func GetAllocationFileManifests(tx *sql.Tx, allocID string) (AllocationFileManifests, error) {
	pending := FileManifest{}
	applied := FileManifest{}

	rows, err := tx.Query(
		`SELECT path, pending_hash, applied_hash FROM allocation_file_hashes WHERE alloc_id = ?`,
		allocID,
	)
	if err != nil {
		return AllocationFileManifests{}, bucket.DatabaseError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var path string
		var pendingHash, appliedHash sql.NullString
		if err := rows.Scan(&path, &pendingHash, &appliedHash); err != nil {
			return AllocationFileManifests{}, bucket.DatabaseError(err)
		}
		if pendingHash.Valid {
			pending[path] = pendingHash.String
		}
		if appliedHash.Valid {
			applied[path] = appliedHash.String
		}
	}
	if err := rows.Err(); err != nil {
		return AllocationFileManifests{}, bucket.DatabaseError(err)
	}

	hasApplied, err := allocationHasAppliedHash(tx, allocID)
	if err != nil {
		return AllocationFileManifests{}, err
	}

	return AllocationFileManifests{
		Pending:         pending,
		Applied:         applied,
		HasAppliedFiles: hasApplied,
	}, nil
}

func allocationHasAppliedHash(tx *sql.Tx, allocID string) (bool, error) {
	var applied sql.NullString
	err := tx.QueryRow(
		`SELECT applied_hash FROM allocation_hashes WHERE alloc_id = ?`,
		allocID,
	).Scan(&applied)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, bucket.DatabaseError(err)
	}
	return applied.Valid, nil
}
