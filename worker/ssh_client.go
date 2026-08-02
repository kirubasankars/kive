// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kive/bucket"
)

// RunRemoteScript runs script on workerIP over SSH, piping script to remote bash -s.
// Environment variables must already be present in the script body.
func RunRemoteScript(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, script io.Reader, _ bool) error {
	user, keyPath, useSudo, err := sshSettingsFromConf()
	if err != nil {
		return err
	}

	if err := EnsureSSHStateDir(); err != nil {
		return err
	}

	args := SSHClientArgs(keyPath, workerIP)
	args = append(args, sshTarget(user, workerIP), remoteShellCommand(useSudo))

	execCtx, cancel := contextWithRemoteTimeout(ctx)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "ssh", args...)
	cmd.Stdin = script
	if cmdCtx.Cmd == "" {
		cmdCtx.Action = "ssh"
	}
	return rt.RunCommand(workerIP, cmdCtx, cmd)
}

// RunRemoteScriptFile runs a local script file on workerIP over SSH.
func RunRemoteScriptFile(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, scriptPath string, _ bool) error {
	if err := ensureCommandScript(scriptPath); err != nil {
		return err
	}

	file, err := os.Open(scriptPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	if cmdCtx.Cmd == "" {
		cmdCtx.Cmd = scriptPath
	}
	return RunRemoteScript(ctx, rt, workerIP, cmdCtx, file, false)
}

// RemoteShellCommand runs a single remote shell command and returns an error on non-zero exit.
func RemoteShellCommand(workerIP, command string, timeout time.Duration) error {
	return RemoteShellCommandContext(context.Background(), workerIP, command, timeout)
}

// RemoteShellCommandContext is like RemoteShellCommand but honors parent ctx cancellation
// (timeout is applied as a child of ctx).
func RemoteShellCommandContext(ctx context.Context, workerIP, command string, timeout time.Duration) error {
	_, err := remoteShellOutput(ctx, workerIP, command, timeout)
	return err
}

// RemoteShellCommandLogged runs a remote shell command with structured logging.
func RemoteShellCommandLogged(rt *bucket.Runtime, workerIP, command string, cmdCtx bucket.CommandContext, timeout time.Duration) error {
	return RemoteShellCommandLoggedContext(context.Background(), rt, workerIP, command, cmdCtx, timeout)
}

// RemoteShellCommandLoggedContext is like RemoteShellCommandLogged but honors parent ctx.
func RemoteShellCommandLoggedContext(ctx context.Context, rt *bucket.Runtime, workerIP, command string, cmdCtx bucket.CommandContext, timeout time.Duration) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("empty remote command for worker %s", workerIP)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	user, keyPath, useSudo, err := sshSettingsFromConf()
	if err != nil {
		return err
	}
	if err := EnsureSSHStateDir(); err != nil {
		return err
	}

	remoteCommand := command
	if useSudo {
		remoteCommand = fmt.Sprintf("sudo -E bash -lc %s", shellQuote(command))
	}

	args := SSHClientArgs(keyPath, workerIP)
	args = append(args, sshTarget(user, workerIP), remoteCommand)

	if timeout <= 0 {
		timeout = remoteExecTimeout()
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "ssh", args...)
	if cmdCtx.Action == "" {
		cmdCtx.Action = "ssh"
	}
	_, err = rt.RunCommandCapture(workerIP, cmdCtx, cmd)
	return err
}

// RemoteShellOutput runs a single remote shell command and returns combined stdout/stderr.
func RemoteShellOutput(workerIP, command string) (string, error) {
	return remoteShellOutput(context.Background(), workerIP, command, remoteExecTimeout())
}

func remoteShellOutput(ctx context.Context, workerIP, command string, timeout time.Duration) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	user, keyPath, useSudo, err := sshSettingsFromConf()
	if err != nil {
		return "", err
	}

	if err := EnsureSSHStateDir(); err != nil {
		return "", err
	}

	remoteCommand := command
	if useSudo {
		remoteCommand = fmt.Sprintf("sudo -E bash -lc %s", shellQuote(command))
	}

	args := SSHClientArgs(keyPath, workerIP)
	args = append(args, sshTarget(user, workerIP), remoteCommand)

	if timeout <= 0 {
		timeout = remoteExecTimeout()
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := exec.CommandContext(execCtx, "ssh", args...).CombinedOutput()
	if err != nil {
		return "", WrapSSHError(workerIP, err, string(out))
	}
	return string(out), nil
}

// RunRemoteScriptCombined runs script on workerIP and returns combined stdout/stderr.
func RunRemoteScriptCombined(workerIP string, script io.Reader) (string, error) {
	return runRemoteScriptCombined(context.Background(), workerIP, script)
}

// RunRemoteScriptCombinedContext is like RunRemoteScriptCombined but honors parent ctx.
func RunRemoteScriptCombinedContext(ctx context.Context, workerIP string, script io.Reader) (string, error) {
	return runRemoteScriptCombined(ctx, workerIP, script)
}

// RunRemoteScriptCombinedLogged runs script on workerIP with structured logging.
func RunRemoteScriptCombinedLogged(ctx context.Context, rt *bucket.Runtime, workerIP string, cmdCtx bucket.CommandContext, script io.Reader) (string, error) {
	user, keyPath, useSudo, err := sshSettingsFromConf()
	if err != nil {
		return "", err
	}

	if err := EnsureSSHStateDir(); err != nil {
		return "", err
	}

	scriptBytes, err := io.ReadAll(script)
	if err != nil {
		return "", err
	}

	args := SSHClientArgs(keyPath, workerIP)
	args = append(args, sshTarget(user, workerIP), remoteShellCommand(useSudo))

	execCtx, cancel := contextWithRemoteTimeout(ctx)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "ssh", args...)
	cmd.Stdin = strings.NewReader(string(scriptBytes))
	if cmdCtx.Action == "" {
		cmdCtx.Action = "ssh"
	}
	return rt.RunCommandCapture(workerIP, cmdCtx, cmd)
}

func runRemoteScriptCombined(ctx context.Context, workerIP string, script io.Reader) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	user, keyPath, useSudo, err := sshSettingsFromConf()
	if err != nil {
		return "", err
	}

	if err := EnsureSSHStateDir(); err != nil {
		return "", err
	}

	args := SSHClientArgs(keyPath, workerIP)
	args = append(args, sshTarget(user, workerIP), remoteShellCommand(useSudo))

	execCtx, cancel := contextWithRemoteTimeout(ctx)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "ssh", args...)
	cmd.Stdin = script
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), WrapSSHError(workerIP, err, string(out))
	}
	return string(out), nil
}

// RSHShell returns the ssh command rsync should use via --rsh.
func RSHShell(keyPath, workerIP string) string {
	absKey, err := filepath.Abs(keyPath)
	if err != nil {
		absKey = keyPath
	}
	return "ssh " + strings.Join(SSHClientArgs(absKey, workerIP), " ")
}

func sshSettingsFromConf() (user, keyPath string, useSudo bool, err error) {
	conf, err := bucket.GetKiveConf()
	if err != nil {
		return "", "", false, err
	}
	return sshSettings(conf)
}

func contextWithRemoteTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, remoteExecTimeout())
}
