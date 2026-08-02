// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"fmt"
	"os"
	"path"
	"strings"

	"kive/bucket"
)

// Runtime identifies how a hook script is executed on the host.
type Runtime string

const (
	RuntimePython Runtime = "python"
	RuntimeJS     Runtime = "js"
	RuntimeRuby   Runtime = "ruby"
	RuntimeBash   Runtime = "bash"
	// RuntimeBinary is a precompiled executable (e.g. built with `go build`)
	// with no extension. It is invoked directly instead of via an interpreter.
	RuntimeBinary Runtime = "binary"
)

const (
	commandScriptPython = ".py"
	commandScriptJSTS   = ".ts"
	commandScriptJSJS   = ".js"
	commandScriptRuby   = ".rb"
	commandScriptBash   = ".sh"
	commandScriptBinary = ""
)

// ResolveHookScript picks the hook implementation file under hooksDir.
// Exactly one of hook_<name>.py, .ts, .js, .rb, .sh, or an extensionless
// compiled executable named hook_<name> must exist.
func ResolveHookScript(hooksDir, hookName string) (Runtime, string, error) {
	candidates := []struct {
		suffix  string
		runtime Runtime
	}{
		{commandScriptPython, RuntimePython},
		{commandScriptJSTS, RuntimeJS},
		{commandScriptJSJS, RuntimeJS},
		{commandScriptRuby, RuntimeRuby},
		{commandScriptBash, RuntimeBash},
		{commandScriptBinary, RuntimeBinary},
	}

	var (
		foundRuntime Runtime
		foundPath    string
		foundCount   int
	)

	for _, candidate := range candidates {
		scriptPath := path.Join(hooksDir, hookName+candidate.suffix)
		info, err := os.Stat(scriptPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", err
		}
		if candidate.runtime == RuntimeBinary && info.IsDir() {
			continue
		}
		foundRuntime = candidate.runtime
		foundPath = scriptPath
		foundCount++
	}

	switch foundCount {
	case 0:
		return "", "", fmt.Errorf(
			"%w: expected %s.py, %s.ts, %s.js, %s.rb, %s.sh, or a compiled executable named %s under %s",
			bucket.ErrHookFileNotFound,
			hookName,
			hookName,
			hookName,
			hookName,
			hookName,
			hookName,
			hooksDir,
		)
	case 1:
		return foundRuntime, foundPath, nil
	default:
		return "", "", fmt.Errorf(
			"%w: multiple implementations for %s under %s (use only one of .py, .ts, .js, .rb, .sh, or a compiled executable)",
			bucket.ErrInvalidHookConfiguration,
			hookName,
			hooksDir,
		)
	}
}

// CommandExecLines returns shell commands to run scriptPath with runtime from hooksDir.
// jobName selects a per-job virtualenv under workspace/jobs/<job>/_hooks/.venv when present
// (unless overridden by kive.conf python_path).
// For .ts/.js, uses js_path when set, else the first of bun/node/deno on PATH.
// scriptArgs are appended as shell-quoted positional arguments (CLI hooks only).
func CommandExecLines(hooksDir, scriptPath string, runtime Runtime, jobName string, scriptArgs []string) ([]string, error) {
	scriptName := path.Base(scriptPath)
	switch runtime {
	case RuntimeJS:
		cmd, err := jsExecCommand(scriptName, scriptArgs)
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("cd %s", shellQuote(hooksDir)),
			cmd,
		}, nil
	case RuntimeRuby:
		return []string{
			fmt.Sprintf("cd %s", shellQuote(hooksDir)),
			appendScriptArgs(ResolveRubyExecutable()+" "+scriptName, scriptArgs),
		}, nil
	case RuntimeBash:
		return []string{
			fmt.Sprintf("cd %s", shellQuote(hooksDir)),
			appendScriptArgs(ResolveBashExecutable()+" "+scriptName, scriptArgs),
		}, nil
	case RuntimeBinary:
		// job_files round-trips through SQLite with no mode column, so the
		// executable bit is not guaranteed to survive staging; restore it
		// before invoking the binary directly (no interpreter involved).
		return []string{
			fmt.Sprintf("cd %s", shellQuote(hooksDir)),
			fmt.Sprintf("chmod +x %s", scriptName),
			appendScriptArgs("./"+scriptName, scriptArgs),
		}, nil
	default:
		python := ResolvePythonExecutable(jobName)
		return []string{
			fmt.Sprintf("cd %s", shellQuote(hooksDir)),
			appendScriptArgs(python+" "+scriptName, scriptArgs),
		}, nil
	}
}

func jsExecCommand(scriptName string, scriptArgs []string) (string, error) {
	engine, err := ResolveJSRuntime()
	if err != nil {
		return "", err
	}
	exe, err := ResolveJSExecutable()
	if err != nil {
		return "", err
	}
	var base string
	switch engine {
	case "node":
		base = exe + " " + scriptName
	case "deno":
		base = exe + " run --allow-net --allow-env --allow-read " + scriptName
	default: // bun
		base = exe + " run " + scriptName
		if len(scriptArgs) > 0 {
			return appendScriptArgs(base+" --", scriptArgs), nil
		}
		return base, nil
	}
	return appendScriptArgs(base, scriptArgs), nil
}

// shellQuote wraps a value for a single-quoted POSIX shell argument.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func appendScriptArgs(base string, scriptArgs []string) string {
	if len(scriptArgs) == 0 {
		return base
	}
	parts := make([]string, 0, 1+len(scriptArgs))
	parts = append(parts, base)
	for _, arg := range scriptArgs {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}
