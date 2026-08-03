// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"
	"kive/bucket"
	"kive/gc"

	"github.com/spf13/cobra"
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Cleanup unused objects in the bucket",
	Long:  "Removes soft-deleted allocations from kive.db, purges KV references for removed allocations, deletes worker data/logs/bin for removed allocations, purges old key_value history, and prunes old logs/runs/ session directories.",
	Run: func(cmd *cobra.Command, args []string) {
		retainDays := bucket.DefaultKVRetainDays
		if cmd.Flags().Changed("retain-days") {
			retainDays, _ = cmd.Flags().GetInt("retain-days")
		} else {
			var err error
			retainDays, err = bucket.KVRetainDaysFromConfig()
			if err != nil {
				log.Fatalln(err)
			}
		}
		if err := gc.Execute(retainDays); err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	kiveCmd.AddCommand(gcCmd)
	gcCmd.Flags().Int("retain-days", bucket.DefaultKVRetainDays, "Days to retain deleted KV rows before purge (overrides bucket.conf kv_retain_days; 0 = aggressive)")
}
