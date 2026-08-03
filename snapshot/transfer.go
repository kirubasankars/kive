// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package snapshot

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"kive/bucket"
	"kive/worker"
)

// TestHooks overrides snapshot side effects during tests.
type TestHooks struct {
	WorkerCommand       func(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, commands []string, env []string) error
	WorkerCommandOutput func(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, commands []string) (string, error)
	RsyncFile           func(ctx context.Context, rt *bucket.Runtime, workerIP, localPath, remotePath string) error
	RsyncPull           func(ctx context.Context, rt *bucket.Runtime, workerIP, remotePath, localPath string) error
}

var testHooks *TestHooks

// SetTestHooks installs snapshot test doubles. Not for production use.
func SetTestHooks(h *TestHooks) {
	testHooks = h
}

// ClearTestHooks removes snapshot test doubles.
func ClearTestHooks() {
	testHooks = nil
}

func runWorkerCommand(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, commands []string, env []string) error {
	if testHooks != nil && testHooks.WorkerCommand != nil {
		return testHooks.WorkerCommand(ctx, rt, workerIP, cmdCtx, commands, env)
	}
	return worker.ExecuteCommand(ctx, rt, workerIP, cmdCtx, commands, env)
}

func runWorkerCommandOutput(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, commands []string) (string, error) {
	if testHooks != nil && testHooks.WorkerCommandOutput != nil {
		return testHooks.WorkerCommandOutput(ctx, rt, workerIP, cmdCtx, commands)
	}
	script := bucket.BuildCommandScript(commands, nil)
	if cmdCtx.Cmd == "" {
		cmdCtx.Cmd = bucket.SummarizeCommands(commands)
	}
	if cmdCtx.Action == "" {
		cmdCtx.Action = "ssh"
	}
	return worker.RunRemoteScriptCombinedLogged(ctx, rt, workerIP, cmdCtx, strings.NewReader(script))
}

func rsyncBackupFile(ctx context.Context, rt *bucket.Runtime, workerIP, localPath, remotePath string) error {
	if testHooks != nil && testHooks.RsyncFile != nil {
		return testHooks.RsyncFile(ctx, rt, workerIP, localPath, remotePath)
	}

	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}
	user := strings.TrimSpace(conf.SSHUser)
	if user == "" {
		user = "agent"
	}
	keyFilePath, _, err := bucket.SSHKeyHostPath(conf.SSHKeyFile)
	if err != nil {
		return err
	}
	if err := worker.EnsureSSHStateDir(); err != nil {
		return err
	}

	remoteRS := "rsync"
	if conf.UseSUDO {
		remoteRS = "sudo rsync"
	}

	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}

	args := []string{
		"--timeout=30",
		"--inplace",
		"--whole-file",
		"--checksum",
		"--compress",
		"--verbose",
		"--rsync-path=" + remoteRS,
		"--rsh=" + worker.RSHShell(keyFilePath, workerIP),
		absLocal,
		fmt.Sprintf("%s@%s:%s", user, workerIP, remotePath),
	}

	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmdCtx := bucket.CommandContext{
		Phase:  "backup",
		Action: "rsync",
		Cmd:    bucket.SummarizeExecCmd(cmd),
	}
	if err := rt.RunCommand(workerIP, cmdCtx, cmd); err != nil {
		return fmt.Errorf("backup rsync failed: worker %s: %w", workerIP, err)
	}
	return nil
}

func rsyncPullBackupFile(ctx context.Context, rt *bucket.Runtime, workerIP, remotePath, localPath string) error {
	if testHooks != nil && testHooks.RsyncPull != nil {
		return testHooks.RsyncPull(ctx, rt, workerIP, remotePath, localPath)
	}

	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}
	user := strings.TrimSpace(conf.SSHUser)
	if user == "" {
		user = "agent"
	}
	keyFilePath, _, err := bucket.SSHKeyHostPath(conf.SSHKeyFile)
	if err != nil {
		return err
	}
	if err := worker.EnsureSSHStateDir(); err != nil {
		return err
	}

	remoteRS := "rsync"
	if conf.UseSUDO {
		remoteRS = "sudo rsync"
	}

	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}

	args := []string{
		"--timeout=30",
		"--inplace",
		"--whole-file",
		"--checksum",
		"--compress",
		"--verbose",
		"--rsync-path=" + remoteRS,
		"--rsh=" + worker.RSHShell(keyFilePath, workerIP),
		fmt.Sprintf("%s@%s:%s", user, workerIP, remotePath),
		absLocal,
	}

	cmd := exec.CommandContext(ctx, "rsync", args...)
	cmdCtx := bucket.CommandContext{
		Phase:  "restore",
		Action: "rsync",
		Cmd:    bucket.SummarizeExecCmd(cmd),
	}
	if err := rt.RunCommand(workerIP, cmdCtx, cmd); err != nil {
		return fmt.Errorf("restore rsync failed: worker %s: %w", workerIP, err)
	}
	return nil
}
