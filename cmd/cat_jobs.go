// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"
	"kive/cat"

	"github.com/spf13/cobra"
)

var catJobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Shows available jobs",
	Run: func(cmd *cobra.Command, args []string) {
		err := cat.Jobs()
		if err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	catCmd.AddCommand(catJobsCmd)
}
