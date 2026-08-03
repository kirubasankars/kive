// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package worker

import (
	"context"

	"kive/bucket"
)

// TestHooks overrides worker side effects during tests. Clear with ClearTestHooks when done.
type TestHooks struct {
	ExecuteCommand     func(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, commands []string, env []string) error
	ExecuteFileCommand func(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, hostScriptPath string, env []string) error
}

var testHooks *TestHooks

// SetTestHooks installs worker test doubles. Not for production use.
func SetTestHooks(h *TestHooks) {
	testHooks = h
}

// ClearTestHooks removes worker test doubles.
func ClearTestHooks() {
	testHooks = nil
}
