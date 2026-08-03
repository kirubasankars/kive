// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"

	"kive/healthcheck"

	"github.com/spf13/cobra"
)

var healthCheckCmd = &cobra.Command{
	Use:   "health_check",
	Short: "Run health_check hooks",
	Long: `Run liveness and readiness health checks defined in each job manifest.

Liveness runs first when configured; readiness runs only if liveness passes
(or is not configured). Use --kind to run only one channel. With --wait, each
kind is retried with its own wait budget until it passes or the retry limit is reached.`,
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()
		wait, _ := flags.GetBool("wait")
		jobsComma, _ := flags.GetString("jobs")
		verbose, _ := flags.GetBool("verbose")
		kind, _ := flags.GetString("kind")

		if err := healthcheck.ExecuteContext(cmd.Context(), wait, verbose, jobsComma, kind, ""); err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	kiveCmd.AddCommand(healthCheckCmd)
	healthCheckCmd.Flags().Bool("verbose", false, "Per-worker pass lines and stream health hook output (default: one pass line per job)")
	healthCheckCmd.Flags().Bool("wait", false, "Retry until health checks pass (per-kind wait budget)")
	healthCheckCmd.Flags().String("jobs", "", "Comma-separated job names (default: all jobs)")
	healthCheckCmd.Flags().String("kind", "all", "Health channel to run: all, liveness, or readiness")
}
