// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"kive/prereq"
)

func validateHostRuntime(jobName, hookName string) error {
	hooksDir := WorkspaceJobHooksDir(jobName)
	runtime, _, err := ResolveHookScript(hooksDir, hookName)
	if err != nil {
		return err
	}

	switch runtime {
	case RuntimeJS:
		exe, err := ResolveJSExecutable()
		if err != nil {
			return err
		}
		return prereq.CheckLocal(exe)
	case RuntimeRuby:
		return prereq.CheckLocal(ResolveRubyExecutable())
	case RuntimeBash:
		return prereq.CheckLocal(ResolveBashExecutable())
	case RuntimeBinary:
		// The hook is a self-contained executable; no interpreter prerequisite
		// to check on the host.
		return nil
	default:
		return prereq.CheckLocal(ResolvePythonExecutable(jobName))
	}
}
