// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package healthcheck

import (
	"context"
	"database/sql"

	"kive/bucket"
)

// TestHooks overrides healthcheck side effects during tests. Clear with ClearTestHooks when done.
type TestHooks struct {
	// DeployHealthCheckInterceptor replaces DeployHealthCheck when set.
	DeployHealthCheckInterceptor func(
		ctx context.Context,
		tx *sql.Tx,
		rt *bucket.Runtime,
		wait bool,
		job string,
		verbose bool,
	) error
	// HoldWorkerDials, when set, runs before catalog SSH dials start.
	HoldWorkerDials func()
}

var testHooks *TestHooks

// SetTestHooks installs healthcheck test doubles. Not for production use.
func SetTestHooks(h *TestHooks) {
	testHooks = h
}

// ClearTestHooks removes healthcheck test doubles.
func ClearTestHooks() {
	testHooks = nil
}
