// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Runtime runs bucket-local and worker-prep commands on the kive CLI host.
type Runtime struct {
	logMu        sync.Mutex
	run          RunContext
	runStartedAt time.Time
	runEnded     bool
}

// SetupRuntime prepares host execution for a bucket session.
func SetupRuntime(_ string, run RunContext) (*Runtime, error) {
	return &Runtime{run: run}, nil
}

// Run returns the run context attached to this runtime.
func (r *Runtime) Run() RunContext {
	return r.run
}

// SetGeneration updates the bucket generation stamped on subsequent log lines.
func (r *Runtime) SetGeneration(generation int) {
	r.run.Generation = generation
}

// Stop releases runtime resources and logs run_end when LogRunBegin was called without LogRunEnd.
func (r *Runtime) Stop() error {
	if !r.runStartedAt.IsZero() && !r.runEnded {
		_ = r.LogRunEnd(0, time.Since(r.runStartedAt), nil)
	}
	return nil
}

// RunCommandCapture runs cmd like RunCommand and returns combined stdout/stderr.
func (r *Runtime) RunCommandCapture(workerIP string, cmdCtx CommandContext, cmd *exec.Cmd) (string, error) {
	cmdCtx = cmdCtx.withDefaults(workerIP, cmd, nil)

	if cmd.Dir == "" {
		bucketRoot, err := filepath.Abs(Location)
		if err != nil {
			return "", UnexpectedError(err)
		}
		cmd.Dir = bucketRoot
	}

	if err := r.appendLog(workerIP, formatCommandBegin(r.run, workerIP, cmdCtx)); err != nil {
		return "", err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", UnexpectedError(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", UnexpectedError(err)
	}
	ensureCommandProcessGroup(cmd)
	armCancelClosesPipes(cmd, stdout, stderr)

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return "", UnexpectedError(err)
	}

	var (
		wg        sync.WaitGroup
		streamErr error
		streamMu  sync.Mutex
		outputBuf strings.Builder
		outputMu  sync.Mutex
	)

	stream := func(reader io.Reader, streamName string) {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			outputMu.Lock()
			if outputBuf.Len() > 0 {
				outputBuf.WriteByte('\n')
			}
			outputBuf.WriteString(line)
			outputMu.Unlock()

			formatted := formatStreamLine(r.run, workerIP, cmdCtx, streamName, line)
			if !cmdCtx.Quiet {
				log.Printf("%s", formatCLIStreamLine(workerIP, cmdCtx, streamName, line))
			}
			if err := r.appendLog(workerIP, formatted); err != nil {
				streamMu.Lock()
				if streamErr == nil {
					streamErr = err
				}
				streamMu.Unlock()
			}
		}
		if err := scanner.Err(); err != nil && !isPipeClosedErr(err) {
			streamMu.Lock()
			if streamErr == nil {
				streamErr = UnexpectedError(err)
			}
			streamMu.Unlock()
		}
	}

	wg.Add(2)
	go stream(stdout, "stdout")
	go stream(stderr, "stderr")

	wg.Wait()
	waitErr := cmd.Wait()

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	if err := r.appendLog(workerIP, formatCommandEnd(r.run, workerIP, cmdCtx, exitCode, time.Since(started))); err != nil {
		return "", err
	}

	output := outputBuf.String()
	if streamErr != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return output, streamErr
	}
	if waitErr != nil {
		return output, commandFailedError(waitErr)
	}
	return output, nil
}

// LogRunBegin writes event=run_begin for the current CLI invocation.
func (r *Runtime) LogRunBegin(extra map[string]string) error {
	r.runStartedAt = time.Now()
	return r.LogEvent("", "run_begin", extra)
}

// LogRunEnd writes event=run_end with exit code and duration.
func (r *Runtime) LogRunEnd(exitCode int, duration time.Duration, extra map[string]string) error {
	if extra == nil {
		extra = make(map[string]string)
	}
	extra["exit"] = strconv.Itoa(exitCode)
	extra["duration_ms"] = strconv.FormatInt(duration.Milliseconds(), 10)
	r.runEnded = true
	return r.LogEvent("", "run_end", extra)
}

// LogStepComplete writes event=step_complete with phase and action metadata.
func (r *Runtime) LogStepComplete(phase, action string, extra map[string]string) error {
	if extra == nil {
		extra = make(map[string]string)
	}
	if phase != "" {
		extra["phase"] = phase
	}
	if action != "" {
		extra["action"] = action
	}
	return r.LogEvent("", "step_complete", extra)
}

// LogEvent writes a structured event line to bucket logs.
func (r *Runtime) LogEvent(workerIP, event string, extra map[string]string) error {
	line := formatEventLine(r.run, workerIP, event, extra)
	return r.appendLog(workerIP, line)
}

// Exec runs bash commands locally on the CLI host and logs output per workerIP.
// Pass an empty workerIP for bucket-local commands (logged to kive.log).
func (r *Runtime) Exec(ctx context.Context, workerIP string, cmdCtx CommandContext, commandLines []string, env []string) error {
	script := strings.Join([]string{
		"#!/bin/bash",
		"set -e",
		"set -u",
		strings.Join(commandLines, "\n"),
	}, "\n") + "\n"

	cmd := exec.CommandContext(ctx, "bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	if len(env) > 0 {
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}

	return r.RunCommand(workerIP, cmdCtx, cmd)
}

// RunCommand starts cmd, streams stdout/stderr to logs, and returns on completion.
func (r *Runtime) RunCommand(workerIP string, cmdCtx CommandContext, cmd *exec.Cmd) error {
	cmdCtx = cmdCtx.withDefaults(workerIP, cmd, nil)

	if cmd.Dir == "" {
		bucketRoot, err := filepath.Abs(Location)
		if err != nil {
			return UnexpectedError(err)
		}
		cmd.Dir = bucketRoot
	}

	if err := r.appendLog(workerIP, formatCommandBegin(r.run, workerIP, cmdCtx)); err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return UnexpectedError(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return UnexpectedError(err)
	}
	ensureCommandProcessGroup(cmd)
	armCancelClosesPipes(cmd, stdout, stderr)

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return UnexpectedError(err)
	}

	var (
		wg        sync.WaitGroup
		streamErr error
		streamMu  sync.Mutex
	)

	stream := func(reader io.Reader, streamName string) {
		defer wg.Done()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			formatted := formatStreamLine(r.run, workerIP, cmdCtx, streamName, line)
			if !cmdCtx.Quiet {
				log.Printf("%s", formatCLIStreamLine(workerIP, cmdCtx, streamName, line))
			}
			if err := r.appendLog(workerIP, formatted); err != nil {
				streamMu.Lock()
				if streamErr == nil {
					streamErr = err
				}
				streamMu.Unlock()
			}
		}
		if err := scanner.Err(); err != nil && !isPipeClosedErr(err) {
			streamMu.Lock()
			if streamErr == nil {
				streamErr = UnexpectedError(err)
			}
			streamMu.Unlock()
		}
	}

	wg.Add(2)
	go stream(stdout, "stdout")
	go stream(stderr, "stderr")

	wg.Wait()
	waitErr := cmd.Wait()

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	if err := r.appendLog(workerIP, formatCommandEnd(r.run, workerIP, cmdCtx, exitCode, time.Since(started))); err != nil {
		return err
	}

	if streamErr != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return streamErr
	}

	if waitErr != nil {
		return commandFailedError(waitErr)
	}
	return nil
}

// ensureCommandProcessGroup puts cmd in its own process group so cancel can
// SIGKILL hook interpreters and their children together.
func ensureCommandProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// armCancelClosesPipes wraps cmd.Cancel so context cancel kills the process
// group and closes stdout/stderr pipes. Otherwise descendants that outlive the
// direct child (or children that never EOF, common with ssh/rsync) can block
// stream drain before cmd.Wait.
func armCancelClosesPipes(cmd *exec.Cmd, stdout, stderr io.Closer) {
	if cmd.Cancel == nil {
		return
	}
	cmd.Cancel = func() error {
		var err error
		if cmd.Process != nil {
			if killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); killErr != nil {
				err = cmd.Process.Kill()
			}
		}
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
}

func isPipeClosedErr(err error) bool {
	return errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) ||
		strings.Contains(err.Error(), "file already closed")
}

// commandExitError is a failed remote/local command that preserves *exec.ExitError
// for callers that map exit codes (e.g. worker sync checks).
type commandExitError struct {
	code int
	err  error
}

func (e *commandExitError) Error() string {
	return fmt.Sprintf("command execution failed (exit %d)", e.code)
}

func (e *commandExitError) Unwrap() error {
	return e.err
}

func (e *commandExitError) ExitCode() int {
	return e.code
}

func commandFailedError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &commandExitError{code: exitErr.ExitCode(), err: err}
	}
	return UnexpectedError(err)
}

func (r *Runtime) appendLog(workerIP, line string) error {
	r.logMu.Lock()
	defer r.logMu.Unlock()

	if err := os.MkdirAll(LogLocation, 0o755); err != nil {
		return UnexpectedError(err)
	}

	if err := appendLine(filepath.Join(LogLocation, workerLogFileName(workerIP)), line); err != nil {
		return err
	}

	if r.run.RunID == "" {
		return nil
	}

	runDir := runLogDir(r.run.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return UnexpectedError(err)
	}
	return appendLine(filepath.Join(runDir, workerLogFileName(workerIP)), line)
}

func appendLine(path, line string) error {
	logFile, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return UnexpectedError(err)
	}
	defer func() {
		_ = logFile.Close()
	}()

	if _, err := logFile.WriteString(line + "\n"); err != nil {
		return UnexpectedError(err)
	}
	return nil
}
