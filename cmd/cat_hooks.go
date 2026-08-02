// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"
	"kive/cat"

	"github.com/spf13/cobra"
)

var catHooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Shows manifest hooks from the catalog",
	Run: func(cmd *cobra.Command, args []string) {
		err := cat.Hooks()
		if err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	catCmd.AddCommand(catHooksCmd)
}
