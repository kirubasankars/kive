// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package main

import (
	"log"
	"kive/cmd"
)

func main() {
	log.Default().SetFlags(log.Lmsgprefix)
	cmd.Execute()
}
