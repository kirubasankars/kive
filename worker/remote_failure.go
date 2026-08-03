// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package worker

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var commandExitRE = regexp.MustCompile(`(?i)command execution failed \(exit (\d+)\)`)

// FormatWorkerFailure returns a single-line worker failure message.
func FormatWorkerFailure(workerIP string, err error) string {
	if err == nil {
		if strings.TrimSpace(workerIP) == "" {
			return "unknown error"
		}
		return fmt.Sprintf("worker %s: unknown error", workerIP)
	}

	var pre *PrerequisitesError
	if errors.As(err, &pre) {
		return pre.Error()
	}

	ip := strings.TrimSpace(workerIP)
	var remote *RemoteCommandError
	if errors.As(err, &remote) {
		if ip == "" {
			ip = strings.TrimSpace(remote.WorkerIP)
		}
		detail := FormatRemoteFailure(remote.Err)
		if ip == "" {
			return detail
		}
		return fmt.Sprintf("worker %s: %s", ip, detail)
	}

	msg := err.Error()
	if ip != "" && strings.HasPrefix(msg, "worker "+ip+":") {
		detail := strings.TrimSpace(strings.TrimPrefix(msg, "worker "+ip+":"))
		if detail == "" {
			detail = FormatRemoteFailure(err)
		} else {
			detail = FormatRemoteFailure(errors.New(detail))
		}
		return fmt.Sprintf("worker %s: %s", ip, detail)
	}

	detail := FormatRemoteFailure(err)
	if ip == "" {
		return detail
	}
	return fmt.Sprintf("worker %s: %s", ip, detail)
}

// FormatRemoteFailure formats a remote SSH or command error for display.
func FormatRemoteFailure(err error) string {
	if err == nil {
		return "unknown error"
	}

	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return formatCommandExit(exitCoder.ExitCode())
	}

	errMsg := err.Error()
	if m := commandExitRE.FindStringSubmatch(errMsg); len(m) == 2 {
		code, convErr := strconv.Atoi(m[1])
		if convErr == nil {
			return formatCommandExit(code)
		}
	}

	lower := strings.ToLower(errMsg)
	if strings.Contains(lower, "connection refused") {
		return errMsg
	}
	switch {
	case strings.Contains(errMsg, "exit status 255"):
		return "ssh command failed (timed out or connection error)"
	case strings.Contains(errMsg, "exit status 127"):
		return formatCommandExit(127)
	case strings.Contains(errMsg, "exit status 126"):
		return formatCommandExit(126)
	case strings.Contains(errMsg, "exit status 1"):
		return "command failed (exit 1)"
	case strings.Contains(errMsg, "command execution failed"):
		return "command execution failed"
	default:
		return errMsg
	}
}

func formatCommandExit(code int) string {
	switch code {
	case 127:
		return "command not found (exit 127)"
	case 126:
		return "command not executable (exit 126)"
	default:
		return fmt.Sprintf("command failed (exit %d)", code)
	}
}
