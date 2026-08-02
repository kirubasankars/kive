// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"

	"kive/hooks"

	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks <hook> [job] [-- <args...>]",
	Short: "Run a manifest hook across allocations",
	Long: `Run a hook registered for the cli event.

When job is omitted, the hook runs only on jobs that register it for the cli event.
Jobs that lack the hook or register it for other events only are skipped.
If no jobs match, the command succeeds with no output.

Pass script arguments after --, for example:
  kive hooks hook_migrate api -- dry-run --force
  kive hooks hook_status -- --format json`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		flags := cmd.Flags()
		quiet, _ := flags.GetBool("quiet")
		verbose, _ := flags.GetBool("verbose")
		if quiet {
			verbose = false
		}

		inv, err := hooks.ParseCLIHookArgs(args, cmd.ArgsLenAtDash())
		if err != nil {
			log.Fatalln(err)
		}

		err = hooks.Execute(inv.Hook, inv.Job, "cli", verbose, []string{}, inv.ScriptArgs)
		if err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	kiveCmd.AddCommand(hooksCmd)
	hooksCmd.Flags().Bool("quiet", false, "Less verbose: hide preparing/running/ok and batch progress")
	hooksCmd.Flags().Bool("verbose", true, "Show preparing/running/ok and batch progress (default; use --quiet for less)")
}
