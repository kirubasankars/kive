//go:build !darwin && !linux

// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package durablefs

import (
	"fmt"
	"os"
)

func renameNoReplace(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(oldPath, newPath)
}
