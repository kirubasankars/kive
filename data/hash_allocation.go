// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"errors"

	"kive/bucket"
)

// RemoveAllocationHash deletes plan and per-file digest rows for an allocation.
func RemoveAllocationHash(tx *sql.Tx, allocID string) error {
	_, err := tx.Exec(`DELETE FROM allocation_hashes WHERE alloc_id = ?`, allocID)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	_, err = tx.Exec(`DELETE FROM allocation_file_hashes WHERE alloc_id = ?`, allocID)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// UpdateAllocationPlan records pending content hash and file digests for an allocation.
// Target version lives on the allocations row (updated by build).
func UpdateAllocationPlan(tx *sql.Tx, allocID, hash string, pendingFiles FileManifest) error {
	var storedPendingHash string
	row := tx.QueryRow(
		`SELECT ifnull(pending_hash, '') FROM allocation_hashes WHERE alloc_id = ?`,
		allocID,
	)
	err := row.Scan(&storedPendingHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.Exec(
				`INSERT INTO allocation_hashes (alloc_id, pending_hash, applied_version)
				 VALUES (?, ?, ?)`,
				allocID, hash, DefaultAllocationVersion,
			)
			if err != nil {
				return bucket.DatabaseError(err)
			}
			return upsertAllocationPendingFiles(tx, allocID, pendingFiles)
		}
		return bucket.DatabaseError(err)
	}

	if storedPendingHash == hash {
		return nil
	}

	_, err = tx.Exec(
		`UPDATE allocation_hashes SET pending_hash = ? WHERE alloc_id = ?`,
		hash, allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return upsertAllocationPendingFiles(tx, allocID, pendingFiles)
}

func upsertAllocationPendingFiles(tx *sql.Tx, allocID string, pendingFiles FileManifest) error {
	if pendingFiles == nil {
		pendingFiles = FileManifest{}
	}

	rows, err := tx.Query(
		`SELECT path, applied_hash FROM allocation_file_hashes WHERE alloc_id = ?`,
		allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	existingApplied := make(map[string]sql.NullString)
	for rows.Next() {
		var path string
		var applied sql.NullString
		if err := rows.Scan(&path, &applied); err != nil {
			_ = rows.Close()
			return bucket.DatabaseError(err)
		}
		existingApplied[path] = applied
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return bucket.DatabaseError(err)
	}
	if err := rows.Close(); err != nil {
		return bucket.DatabaseError(err)
	}

	seen := make(map[string]struct{}, len(pendingFiles))
	for path, digest := range pendingFiles {
		seen[path] = struct{}{}
		applied := existingApplied[path]
		_, err := tx.Exec(
			`INSERT INTO allocation_file_hashes (alloc_id, path, pending_hash, applied_hash)
			 VALUES (?, ?, ?, ?)
			 ON CONFLICT(alloc_id, path) DO UPDATE SET pending_hash = excluded.pending_hash`,
			allocID, path, digest, nullStringValue(applied),
		)
		if err != nil {
			return bucket.DatabaseError(err)
		}
	}

	for path, applied := range existingApplied {
		if _, ok := seen[path]; ok {
			continue
		}
		if !applied.Valid {
			_, err := tx.Exec(
				`DELETE FROM allocation_file_hashes WHERE alloc_id = ? AND path = ?`,
				allocID, path,
			)
			if err != nil {
				return bucket.DatabaseError(err)
			}
			continue
		}
		_, err := tx.Exec(
			`UPDATE allocation_file_hashes SET pending_hash = NULL WHERE alloc_id = ? AND path = ?`,
			allocID, path,
		)
		if err != nil {
			return bucket.DatabaseError(err)
		}
	}
	return nil
}

func nullStringValue(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func deleteOrphanFileHashRows(tx *sql.Tx, allocID string) error {
	_, err := tx.Exec(
		`DELETE FROM allocation_file_hashes WHERE alloc_id = ? AND pending_hash IS NULL AND applied_hash IS NULL`,
		allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// PromoteAllocationState marks pending content and running version as applied on the allocation.
func PromoteAllocationState(tx *sql.Tx, allocID string) error {
	_, err := tx.Exec(
		`UPDATE allocation_hashes
		 SET applied_hash = pending_hash,
		     applied_version = COALESCE(
		       (SELECT version FROM allocations WHERE alloc_id = ?),
		       ?
		     )
		 WHERE alloc_id = ?`,
		allocID, DefaultAllocationVersion, allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	_, err = tx.Exec(
		`UPDATE allocation_file_hashes SET applied_hash = pending_hash WHERE alloc_id = ?`,
		allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return deleteOrphanFileHashRows(tx, allocID)
}

// MarkAllocationHealthFailed records that post-batch health failed after promote
// so the next deploy retries this allocation instead of treating it as complete.
func MarkAllocationHealthFailed(tx *sql.Tx, allocID string) error {
	_, err := tx.Exec(
		`UPDATE allocation_hashes SET applied_hash = ? WHERE alloc_id = ?`,
		HealthFailedAppliedHash, allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// MarkAllocationStartPending clears applied_hash so deploy treats a re-enabled allocation as needing start.
func MarkAllocationStartPending(tx *sql.Tx, allocID string) error {
	_, err := tx.Exec(
		`UPDATE allocation_hashes SET applied_hash = NULL WHERE alloc_id = ?`,
		allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return clearAllocationAppliedFiles(tx, allocID)
}

// ClearAllocationLiveState clears promoted hash and running version for disabled allocations.
func ClearAllocationLiveState(tx *sql.Tx, allocID string) error {
	_, err := tx.Exec(
		`UPDATE allocation_hashes SET applied_hash = NULL, applied_version = NULL WHERE alloc_id = ?`,
		allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return clearAllocationAppliedFiles(tx, allocID)
}

func clearAllocationAppliedFiles(tx *sql.Tx, allocID string) error {
	_, err := tx.Exec(
		`UPDATE allocation_file_hashes SET applied_hash = NULL WHERE alloc_id = ?`,
		allocID,
	)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return deleteOrphanFileHashRows(tx, allocID)
}

// GetAllocationVersions returns applied version from allocation_hashes and target from allocations.
func GetAllocationVersions(tx *sql.Tx, allocID string) (AllocationVersions, error) {
	var appliedVersion sql.NullString
	row := tx.QueryRow(
		`SELECT ifnull(h.applied_version, '') FROM allocation_hashes h WHERE h.alloc_id = ?`,
		allocID,
	)
	err := row.Scan(&appliedVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AllocationVersions{}, bucket.DatabaseError(err)
	}

	version, err := GetAllocationVersion(tx, allocID)
	if err != nil {
		return AllocationVersions{}, err
	}

	return AllocationVersions{
		AppliedVersion: normalizeStoredVersion(appliedVersion),
		Version:        version,
	}, nil
}
