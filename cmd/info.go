// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import (
	"log"
	"kive/cat"

	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Information about the bucket",
	Run: func(cmd *cobra.Command, args []string) {
		err := cat.Info()
		if err != nil {
			log.Fatalln(err)
		}
	},
}

func init() {
	kiveCmd.AddCommand(infoCmd)
}
