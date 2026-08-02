// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"kive/bucket"
	"kive/runcommand"

	"github.com/spf13/cobra"
)

var runCommandCmd = &cobra.Command{
	Use:   "run_command [command]",
	Short: "Run a shell command across workers",
	Long: `Run a shell command on workers in the bucket.

Workers run with at most -c concurrent SSH sessions. Without --health_check, the next
worker starts as soon as a slot frees (streaming). With --health_check, workers run in
fixed batches and every job is health-checked after each batch (waiting until checks pass).
--ignore-failure may be combined with --health_check.

Provide the command as an argument, or use --script <name> to run
workspace/commands/<name>.sh (shipped via kive build / kive push). Use --list to print
available script names.

Examples:
  kive run_command "uptime"
  kive run_command -s prebuild
  kive run_command -s staged -w 10.0.0.1 -c 2
  kive run_command -c 3 "hostname"
  kive run_command -w 10.0.0.1,10.0.0.2 -c 2 "uptime"
  kive run_command -l worker --health_check -c 2 "df -h"
  kive run_command --list`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()

		listScripts, _ := flags.GetBool("list")
		if listScripts {
			names, err := runcommand.ListScripts()
			if err != nil {
				log.Fatal(err)
			}
			for _, name := range names {
				fmt.Println(name)
			}
			return
		}

		workerCSV, _ := flags.GetString("workers")
		labelCSV, _ := flags.GetString("labels")
		batchSize, _ := flags.GetInt("concurrency")
		runHealthChecks, _ := flags.GetBool("health_check")
		ignoreFailure, _ := flags.GetBool("ignore-failure")
		scriptName, _ := flags.GetString("script")

		shellCommand := ""
		if len(args) > 0 {
			shellCommand = strings.TrimSpace(args[0])
		}

		err := runcommand.Execute(workerCSV, labelCSV, batchSize, shellCommand, scriptName, runHealthChecks, ignoreFailure)
		if errors.Is(err, bucket.ErrRunCommand) {
			log.Println(err)
			os.Exit(1)
		}
		if err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	kiveCmd.AddCommand(runCommandCmd)
	runCommandCmd.Flags().StringP("workers", "w", "", "Comma-separated worker IPs")
	runCommandCmd.Flags().StringP("labels", "l", "", "Comma-separated worker labels")
	runCommandCmd.Flags().IntP("concurrency", "c", 1, "Workers per batch (parallel executions at a time)")
	runCommandCmd.Flags().Bool("health_check", false, "Run health checks for all jobs after every batch")
	runCommandCmd.Flags().Bool("ignore-failure", false, "Continue when some workers fail; log partial results")
	runCommandCmd.Flags().StringP("script", "s", "", "Named script under workspace/commands/<name>.sh")
	runCommandCmd.Flags().Bool("list", false, "List named scripts and exit")
}
