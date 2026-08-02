// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"
	"fmt"

	"kive/kv"
)

// persistHookKV flushes in-memory KV changes from hooks into the deploy transaction.
// Called after each job's pre_deploy and post_deploy hooks; changes commit with the deploy tx.
func persistHookKV(tx *sql.Tx, job string) error {
	if err := kv.PersistToSessionTransaction(tx); err != nil {
		return &JobError{Job: job, Err: fmt.Errorf("persist hook kv: %w", err)}
	}
	return nil
}
