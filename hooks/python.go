// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"kive/workspace"
)

// WorkspaceJobHooksDir is workspace/jobs/<job>/_hooks (where you create .venv).
func WorkspaceJobHooksDir(jobName string) string {
	return workspace.JobHooksDir(jobName)
}
