// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os"
	"path"
	"strings"
)

// BuildCommandScript assembles a bash script body for remote execution.
// Environment variables must be included in the script; they are not applied separately over SSH.
func BuildCommandScript(commands []string, envVars []string) string {
	lines := []string{"#!/bin/bash", "set -e", "set -u"}
	for _, envVar := range envVars {
		envVar = strings.TrimSpace(envVar)
		envVar = strings.TrimPrefix(envVar, "export ")
		if envVar == "" {
			continue
		}
		key, value, hasValue := strings.Cut(envVar, "=")
		if hasValue && isValidEnvKey(key) {
			// Single-quote the value so it is treated as literal data and
			// cannot break out of the export statement into shell commands.
			lines = append(lines, fmt.Sprintf("export %s=%s", key, shellSingleQuote(value)))
			continue
		}
		lines = append(lines, fmt.Sprintf("export %s", envVar))
	}
	lines = append(lines, commands...)
	return strings.Join(lines, "\n") + "\n"
}

// isValidEnvKey reports whether name is a POSIX-style shell variable name
// ([A-Za-z_][A-Za-z0-9_]*), guarding against injection via the key.
func isValidEnvKey(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// shellSingleQuote wraps a value as a single-quoted POSIX shell literal.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// GenerateCommandScript writes a bash script under bucket/tmp and returns its filename (e.g. "<uuid>.sh").
func GenerateCommandScript(commands []string, envVars []string) (scriptFileName string, err error) {
	scriptFileName = newCommandScriptName()
	scriptPath := path.Join(TempLocation, scriptFileName)

	if err := os.MkdirAll(TempLocation, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(scriptPath, []byte(BuildCommandScript(commands, envVars)), 0o700); err != nil {
		return "", err
	}

	return scriptFileName, nil
}

func newCommandScriptName() string {
	return newUniqueName() + ".sh"
}
