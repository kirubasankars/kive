// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"

	"kive/initialize"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the bucket",
	Long: `Create a new kive bucket in the current directory, or refresh an existing one.

The first run creates kive.db, workspace layout, secrets, and tmp staging directories.
If data/kive.db exists with an incompatible schema version, remove it and run init again
(workspace and secrets may remain). Re-running init on a current schema is a no-op for the DB.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := initialize.Execute(); err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	kiveCmd.AddCommand(initCmd)
}
