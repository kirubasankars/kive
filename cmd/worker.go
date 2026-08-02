// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"
	"os"

	"kive/collect"

	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Worker operations over SSH",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Usage()
	},
}

var workerFactsCmd = &cobra.Command{
	Use:   "facts",
	Short: "Probe worker memory and CPU over SSH",
	Long: `SSH to workers listed in workspace/workers.conf, read host memory and CPU
capacity, and print the discovered values.

Memory comes from /proc/meminfo (MemTotal). CPU is computed as logical cores
times per-core MHz from /proc/cpuinfo or lscpu.

By default this command does not modify workers.conf. Use --generate-workers to
write the updated workers.conf in place, then run kive build to sync the catalog.

Examples:
  kive worker facts
  kive worker facts --generate-workers
  kive worker facts -w 10.0.0.1,10.0.0.2 -c 2
  kive worker facts -l prod`,
	Run: runWorkerFacts,
}

var workerUptimeCmd = &cobra.Command{
	Use:   "uptime",
	Short: "Print worker uptime over SSH",
	Long: `SSH to workers listed in workspace/workers.conf and print host uptime.

Examples:
  kive worker uptime
  kive worker uptime -w 10.0.0.1,10.0.0.2 -c 2
  kive worker uptime -l prod`,
	Run: runWorkerUptime,
}

var workerSysstatCmd = &cobra.Command{
	Use:   "sysstat",
	Short: "Print worker sysstat snapshot over SSH",
	Long: `SSH to workers listed in workspace/workers.conf and print a short sysstat
snapshot (sar, mpstat, iostat, pidstat). Hosts without sysstat tools report
status="missing" and still succeed.

Examples:
  kive worker sysstat
  kive worker sysstat -w 10.0.0.1,10.0.0.2 -c 2
  kive worker sysstat -l prod`,
	Run: runWorkerSysstat,
}

var workerTrustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Pin SSH host keys for workers",
	Long: `Capture each worker's SSH host key with ssh-keyscan and store it in
bucket/.ssh/known_hosts. Subsequent kive SSH connections verify keys against this file.

Run from a trusted network when adding workers. Re-run with --force after a
legitimate worker reinstall or host key rotation.

Examples:
  kive worker trust
  kive worker trust -w 10.0.0.1
  kive worker trust --force`,
	Run: runWorkerTrust,
}

func runWorkerFacts(cmd *cobra.Command, _ []string) {
	opts := workerOptions(cmd)
	if err := collect.Execute(opts); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func runWorkerUptime(cmd *cobra.Command, _ []string) {
	opts := workerOptions(cmd)
	if err := collect.ExecuteUptime(opts); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func runWorkerSysstat(cmd *cobra.Command, _ []string) {
	opts := workerOptions(cmd)
	if err := collect.ExecuteSysstat(opts); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func runWorkerTrust(cmd *cobra.Command, _ []string) {
	opts := workerOptions(cmd)
	force, _ := cmd.Flags().GetBool("force")
	if err := collect.ExecuteTrust(opts, force); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func workerOptions(cmd *cobra.Command) collect.Options {
	flags := cmd.Flags()
	workerCSV, _ := flags.GetString("workers")
	labelCSV, _ := flags.GetString("labels")
	concurrency, _ := flags.GetInt("concurrency")
	ignoreFailure, _ := flags.GetBool("ignore-failure")
	generateWorkers, _ := flags.GetBool("generate-workers")
	return collect.Options{
		WorkerCSV:       workerCSV,
		LabelCSV:        labelCSV,
		Concurrency:     concurrency,
		IgnoreFailure:   ignoreFailure,
		GenerateWorkers: generateWorkers,
	}
}

func bindWorkerTargetFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("workers", "w", "", "Comma-separated worker IPs")
	cmd.Flags().StringP("labels", "l", "", "Comma-separated worker labels")
	cmd.Flags().IntP("concurrency", "c", 1, "Workers to probe in parallel")
	cmd.Flags().Bool("ignore-failure", false, "Continue when some workers fail; print partial results")
}

func init() {
	kiveCmd.AddCommand(workerCmd)
	workerCmd.AddCommand(workerFactsCmd)
	workerCmd.AddCommand(workerUptimeCmd)
	workerCmd.AddCommand(workerSysstatCmd)
	workerCmd.AddCommand(workerTrustCmd)
	bindWorkerTargetFlags(workerFactsCmd)
	bindWorkerTargetFlags(workerUptimeCmd)
	bindWorkerTargetFlags(workerSysstatCmd)
	bindWorkerTargetFlags(workerTrustCmd)
	workerFactsCmd.Flags().Bool("generate-workers", false, "Write updated facts to workspace/workers.conf in place")
	workerTrustCmd.Flags().Bool("force", false, "Replace existing host key entries")
}
