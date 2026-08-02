// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"errors"

	"kive/bucket"
)

// UpdateHash stages a pending digest in build_hashes (CA / job certs).
func UpdateHash(tx *sql.Tx, namespace, key, hash string) error {
	var storedPendingHash string
	row := tx.QueryRow("SELECT pending_hash FROM build_hashes WHERE namespace = ? AND key = ?", namespace, key)
	err := row.Scan(&storedPendingHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.Exec("INSERT INTO build_hashes (namespace, key, pending_hash) VALUES (?, ?, ?)", namespace, key, hash)
			if err != nil {
				return bucket.DatabaseError(err)
			}
			return nil
		}
		return bucket.DatabaseError(err)
	}

	_, err = tx.Exec("UPDATE build_hashes SET pending_hash = ? WHERE namespace = ? AND key = ?", hash, namespace, key)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

func HashChanged(tx *sql.Tx, namespace, key string) (bool, error) {
	var storedPendingHash, storedAppliedHash string
	row := tx.QueryRow("SELECT ifnull(pending_hash, '') as pending_hash, ifnull(applied_hash, '') as applied_hash FROM build_hashes WHERE namespace = ? AND key = ?", namespace, key)
	err := row.Scan(&storedPendingHash, &storedAppliedHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, bucket.DatabaseError(err)
	}
	return storedPendingHash != storedAppliedHash, nil
}

func PromoteHash(tx *sql.Tx, namespace, key string) (err error) {
	_, err = tx.Exec("UPDATE build_hashes SET applied_hash = pending_hash WHERE namespace = ? AND key = ?", namespace, key)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

func RemoveHash(tx *sql.Tx, namespace, key string) (err error) {
	_, err = tx.Exec("DELETE FROM build_hashes WHERE namespace = ? AND key = ?", namespace, key)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

func GetAppliedHash(tx *sql.Tx, namespace, key string) (appliedHash string, err error) {
	row := tx.QueryRow("SELECT ifnull(applied_hash, '') FROM build_hashes WHERE namespace = ? AND key = ?", namespace, key)
	err = row.Scan(&appliedHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", bucket.DatabaseError(err)
	}
	return appliedHash, nil
}
