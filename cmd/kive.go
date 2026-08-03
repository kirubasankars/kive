// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package cmd provides interfaces to work with kive
package cmd

import (
	"log"

	"kive/bucket"
	"kive/logs"

	"github.com/spf13/cobra"
)

var cliBucketLock *bucket.ExclusiveLock

var kiveCmd = &cobra.Command{
	Use:   "kive",
	Short: "Agentless orchestrator for a local bucket",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if skipBucketContext(cmd) {
			return nil
		}
		if err := bucket.ResolveRoot(); err != nil {
			return err
		}
		if !skipExclusiveLock(cmd) {
			lock, err := bucket.TryExclusive(bucket.Location)
			if err != nil {
				return err
			}
			cliBucketLock = lock
		}
		if _, err := logs.PruneRunLogsFromConfig(); err != nil {
			log.Printf("logs: prune run logs: %v", err)
		}
		if err := requireCurrentSchema(cmd); err != nil {
			releaseCLIBucketLock()
			return err
		}
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		releaseCLIBucketLock()
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
	},
}

func releaseCLIBucketLock() {
	if cliBucketLock == nil {
		return
	}
	_ = cliBucketLock.Unlock()
	cliBucketLock = nil
}

func Execute() {
	if err := kiveCmd.Execute(); err != nil {
		log.Fatalln(err)
	}
}
