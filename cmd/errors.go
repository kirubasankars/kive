// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cmd

import "fmt"

func formatCommandError(command string, err error) string {
	return fmt.Sprintf("kive %s failed: %v", command, err)
}
