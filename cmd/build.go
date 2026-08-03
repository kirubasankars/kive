// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"fmt"
	"os"

	"kive/build"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Plan and build objects in the bucket",
	Run: func(cmd *cobra.Command, args []string) {
		deleteSecretsKV, _ := cmd.Flags().GetBool("delete-secrets-kv")
		if err := build.Execute(build.Options{
			DeleteSecretsKV: deleteSecretsKV,
		}); err != nil {
			fmt.Fprintln(os.Stderr, formatCommandError("build", err))
			os.Exit(1)
		}
	},
}

func init() {
	kiveCmd.AddCommand(buildCmd)
	buildCmd.Flags().Bool(
		"delete-secrets-kv",
		false,
		"Force delete vars/job/<job> and secrets/job/<job> for workspace jobs with no active allocations",
	)
}
