// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"kive/bucket"
	"kive/workspace"
)

// GetJobHealthCheck loads the built-in health_check spec for a job (nil when unset).
// Older catalog rows with flat "checks" are normalized to readiness.
func GetJobHealthCheck(tx *sql.Tx, jobName string) (*workspace.ManifestHealthCheck, error) {
	var raw sql.NullString
	err := tx.QueryRow(`SELECT health_check FROM jobs WHERE name = ?`, jobName).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var spec workspace.ManifestHealthCheck
	if err := json.Unmarshal([]byte(raw.String), &spec); err != nil {
		return nil, fmt.Errorf("%w: job %s health_check: %w", bucket.ErrInvalidManifest, jobName, err)
	}
	spec.Normalize()
	if !spec.HasAnyProbes() {
		return nil, nil
	}
	return &spec, nil
}
