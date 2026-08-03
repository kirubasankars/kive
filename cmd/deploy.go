// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"
	"kive/build"
	"kive/deploy"
	"strings"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy bucket to workers",
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()

		jobsStr, _ := flags.GetString("jobs")
		var jobsFilter []string
		if len(jobsStr) > 0 {
			jobsFilter = strings.Split(strings.Trim(jobsStr, ""), ",")
		}

		buildFlag, _ := flags.GetBool("build")
		force, _ := flags.GetBool("force")
		dryRun, _ := flags.GetBool("dry-run")
		if buildFlag {
			err := build.Execute()
			if err != nil {
				log.Fatalln(err)
			}
			if _, err := deploy.BestEffortPreviewAfterBuild(); err != nil {
				log.Printf("build: pending rollout preview failed: %v", err)
			}
		}
		opts := deploy.Options{Force: force}
		if dryRun {
			result, err := deploy.DryRun(jobsFilter, opts)
			if err != nil {
				log.Fatalln(err)
			}
			deploy.PrintDryRun(result)
			return
		}

		err := deploy.Execute(jobsFilter, opts)
		if err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	kiveCmd.AddCommand(deployCmd)
	deployCmd.Flags().StringP("jobs", "", "", "comma seperated jobs")
	deployCmd.Flags().BoolP("build", "b", false, "build before deploy")
	deployCmd.Flags().BoolP("dry-run", "n", false, "show whether deploy is required using allocation hashes (no changes)")
	deployCmd.Flags().Bool("force", false, "Redeploy jobs even when all allocations are already promoted; skips pre-batch health (post-batch health still required)")
}
