// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"

	"kive/cat"

	"github.com/spf13/cobra"
)

var catDeployHistoryCmd = &cobra.Command{
	Use:   "deploy-history",
	Short: "Show overall deploy history with per-job outcomes",
	Long: `Show newest-first deploy history recorded by non-dry-run kive deploy.

Each row is one overall deploy (generation / run_id). The jobs column summarizes
per-job outcomes: skipped, deployed, failed, or aborted.

Use --jobs to show only deploys that touched those jobs (comma-separated).
Use --limit to bound how many overall deploys are listed (default 50).`,
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()
		jobsStr, _ := flags.GetString("jobs")
		limit, _ := flags.GetInt("limit")
		if err := cat.DeployHistory(jobsStr, limit); err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	catCmd.AddCommand(catDeployHistoryCmd)
	catDeployHistoryCmd.Flags().String("jobs", "", "Comma-separated job names")
	catDeployHistoryCmd.Flags().Int("limit", 50, "Maximum overall deploys to list")
}
