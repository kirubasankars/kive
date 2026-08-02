// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"fmt"

	"kive/buildinfo"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the kive version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(buildinfo.String())
	},
}

func init() {
	kiveCmd.AddCommand(versionCmd)
}
