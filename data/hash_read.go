// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"errors"

	"kive/bucket"
)

// GetAllocationHash returns staged and promoted plan hashes for an allocation.
// ok is false when no hash row exists yet.
func GetAllocationHash(tx *sql.Tx, allocID string) (current, previous string, ok bool, err error) {
	row := tx.QueryRow(
		"SELECT ifnull(pending_hash, ''), ifnull(applied_hash, '') FROM allocation_hashes WHERE alloc_id = ?",
		allocID,
	)
	err = row.Scan(&current, &previous)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, bucket.DatabaseError(err)
	}
	return current, previous, true, nil
}

// GetAllocationAppliedHash returns the promoted plan digest for an allocation.
func GetAllocationAppliedHash(tx *sql.Tx, allocID string) (string, error) {
	var appliedHash string
	err := tx.QueryRow(
		`SELECT ifnull(applied_hash, '') FROM allocation_hashes WHERE alloc_id = ?`,
		allocID,
	).Scan(&appliedHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", bucket.DatabaseError(err)
	}
	return appliedHash, nil
}
