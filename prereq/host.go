// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package prereq

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"kive/bucket"
)

// HostError reports missing tools on the kive CLI host.
type HostError struct {
	Missing []string
}

func (e *HostError) Error() string {
	return fmt.Sprintf("host missing prerequisites: %s", strings.Join(e.Missing, ", "))
}

func (e *HostError) Is(target error) bool {
	return target == bucket.ErrHostPrerequisites
}

var (
	localDeployTools     = []string{"bash", "ssh", "rsync", "python3"}
	localRunCommandTools = []string{"bash", "ssh"}
)

// CheckLocalDeploy verifies host tools required before deploy.
// needsJS / needsRuby add those interpreters (resolved from kive.conf when set).
func CheckLocalDeploy(needsJS, needsRuby bool) error {
	tools := append([]string{}, localDeployTools...)
	if needsJS {
		jsExe, err := resolveJSExecutable()
		if err != nil {
			return err
		}
		tools = append(tools, jsExe)
	}
	if needsRuby {
		tools = append(tools, resolveConfPath(func(c bucket.KiveConf) string { return c.RubyPath }, "ruby"))
	}
	// Prefer configured python/bash paths for baseline checks when set.
	if p := resolveConfPath(func(c bucket.KiveConf) string { return c.PythonPath }, ""); p != "" {
		replaceOrAppend(&tools, "python3", p)
	}
	if b := resolveConfPath(func(c bucket.KiveConf) string { return c.BashPath }, ""); b != "" {
		replaceOrAppend(&tools, "bash", b)
	}
	return CheckLocal(tools...)
}

func resolveJSExecutable() (string, error) {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return "", err
	}
	return bucket.ResolveJSExecutable(conf.JSPath)
}

func replaceOrAppend(tools *[]string, old, neu string) {
	for i, t := range *tools {
		if t == old {
			(*tools)[i] = neu
			return
		}
	}
	*tools = append(*tools, neu)
}

func resolveConfPath(pick func(bucket.KiveConf) string, fallback string) string {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return fallback
	}
	if path := strings.TrimSpace(pick(conf)); path != "" {
		return path
	}
	return fallback
}

// CheckLocalRunCommand verifies host tools required before run_command.
func CheckLocalRunCommand() error {
	return CheckLocal(localRunCommandTools...)
}

// CheckLocal verifies each command exists on PATH or is an executable file path.
func CheckLocal(commands ...string) error {
	missing := make([]string, 0, len(commands))
	for _, command := range commands {
		if !localCommandAvailable(command) {
			missing = append(missing, command)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", bucket.ErrHostPrerequisites, strings.Join(missing, ", "))
}

func localCommandAvailable(command string) bool {
	if strings.Contains(command, "/") {
		info, err := os.Stat(command)
		return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(command)
	return err == nil
}
