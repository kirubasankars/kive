// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"os"
	"path/filepath"
	"strings"

	"kive/bucket"
)

// ResolvePythonExecutable returns the Python interpreter for hooks.
// Precedence: kive.conf python_path, then workspace/jobs/<job>/_hooks/.venv (or venv), else python3.
func ResolvePythonExecutable(jobName string) string {
	if path := confPythonPath(); path != "" {
		return path
	}
	hooksDir := WorkspaceJobHooksDir(jobName)
	for _, venvDir := range []string{".venv", "venv"} {
		for _, name := range []string{"python3", "python"} {
			candidate := filepath.Join(hooksDir, venvDir, "bin", name)
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				abs, err := filepath.Abs(candidate)
				if err == nil {
					return abs
				}
				return candidate
			}
		}
	}
	return "python3"
}

// ResolveJSExecutable returns the JS interpreter for .ts/.js hooks.
// Precedence: kive.conf js_path, else first of bun/node/deno on PATH.
func ResolveJSExecutable() (string, error) {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return "", err
	}
	return bucket.ResolveJSExecutable(conf.JSPath)
}

// ResolveJSRuntime returns the engine command shape (bun|node|deno) for the resolved executable.
func ResolveJSRuntime() (string, error) {
	exe, err := ResolveJSExecutable()
	if err != nil {
		return "", err
	}
	return bucket.JSEngineFromExecutable(exe), nil
}

// ResolveRubyExecutable returns the Ruby interpreter for hooks (.rb).
// Precedence: kive.conf ruby_path, else ruby on PATH.
func ResolveRubyExecutable() string {
	if path := confRubyPath(); path != "" {
		return path
	}
	return "ruby"
}

// ResolveBashExecutable returns the Bash interpreter for hooks (.sh).
// Precedence: kive.conf bash_path, else bash on PATH.
func ResolveBashExecutable() string {
	if path := confBashPath(); path != "" {
		return path
	}
	return "bash"
}

func confPythonPath() string {
	return confInterpreterPath(func(c bucket.KiveConf) string { return c.PythonPath })
}

func confRubyPath() string {
	return confInterpreterPath(func(c bucket.KiveConf) string { return c.RubyPath })
}

func confBashPath() string {
	return confInterpreterPath(func(c bucket.KiveConf) string { return c.BashPath })
}

func confInterpreterPath(pick func(bucket.KiveConf) string) string {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(pick(conf))
}
