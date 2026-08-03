// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"
	"kive/cat"

	"github.com/spf13/cobra"
)

var catPortsCmd = &cobra.Command{
	Use:   "ports",
	Short: "Shows available ports",
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()
		jobsStr, _ := flags.GetString("jobs")
		workersStr, _ := flags.GetString("workers")
		listeners, _ := flags.GetBool("listeners")

		err := cat.Ports(jobsStr, workersStr, listeners)
		if err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	catCmd.AddCommand(catPortsCmd)
	catPortsCmd.Flags().String("jobs", "", "comma separated jobs")
	catPortsCmd.Flags().String("workers", "", "comma separated workers")
	catPortsCmd.Flags().Bool("listeners", false, "show allocation listener endpoints (worker_ip × port)")
}
