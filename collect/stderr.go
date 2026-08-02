// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package collect

import (
	"io"
	"os"
)

var stderrWriter io.Writer = os.Stderr

func stderr() io.Writer {
	return stderrWriter
}
