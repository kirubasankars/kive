// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"errors"
	"fmt"

	"kive/bucket"
	"kive/data"

	"github.com/spf13/cobra"
)

func requireCurrentSchema(cmd *cobra.Command) error {
	if skipBucketContext(cmd) {
		return nil
	}
	if err := data.CheckSchemaVersion(); err != nil {
		if errors.Is(err, bucket.ErrNotInitialized) {
			return fmt.Errorf("%w: %s", bucket.ErrNotInitialized, initBucketHint())
		}
		if errors.Is(err, bucket.ErrSchemaUpgradeRequired) {
			return fmt.Errorf("database schema upgrade required: %s (%w)", initBucketHint(), err)
		}
		return err
	}
	return nil
}

func initBucketHint() string {
	return "run kive init in the bucket directory"
}

// skipBucketContext reports commands that must not resolve a bucket root,
// prune run logs, or require a current schema (init, version, help, completion,
// server, and the internal operation worker).
func skipBucketContext(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "init", "version", "help", "completion", "server", "server-worker":
		return true
	}
	for p := cmd.Parent(); p != nil; p = p.Parent() {
		if p.Name() == "server" {
			return true
		}
	}
	// Bare `kive` (usage only; no subcommand).
	return cmd.Parent() == nil
}

// skipExclusiveLock reports commands that resolve a bucket but must not hold
// data/kive.lock for the whole process lifetime (long-running local UIs).
func skipExclusiveLock(cmd *cobra.Command) bool {
	return cmd.Name() == "edit"
}
