// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// PreferredJSEngines is the PATH lookup order when js_path is unset.
var PreferredJSEngines = []string{"bun", "node", "deno"}

// ResolveJSExecutable returns js_path when set, otherwise the first of
// bun, node, deno found on PATH.
func ResolveJSExecutable(jsPath string) (string, error) {
	if p := strings.TrimSpace(jsPath); p != "" {
		return p, nil
	}
	for _, name := range PreferredJSEngines {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("%w: no JS engine on PATH (tried bun, node, deno); set js_path in kive.conf", ErrHostPrerequisites)
}

// JSEngineFromExecutable infers bun/node/deno command shape from an executable path or name.
// Basename bun → bun; deno → deno; anything else (node, tsx, …) → node.
func JSEngineFromExecutable(exe string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(exe)))
	// Strip common Windows .exe suffix for basename checks.
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "bun":
		return "bun"
	case "deno":
		return "deno"
	default:
		return "node"
	}
}
