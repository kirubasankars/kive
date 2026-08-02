// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"

	"kive/bucket"
)

func closeRows(rows *sql.Rows) error {
	if rows == nil {
		return nil
	}
	if err := rows.Close(); err != nil {
		return bucket.DatabaseError(err)
	}
	if err := rows.Err(); err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}

// RowsErr returns a database error if row iteration failed.
func RowsErr(rows *sql.Rows) error {
	return rowsErr(rows)
}

func rowsErr(rows *sql.Rows) error {
	if rows == nil {
		return nil
	}
	if err := rows.Err(); err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}
