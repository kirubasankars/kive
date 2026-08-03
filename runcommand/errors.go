// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package runcommand

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"kive/bucket"
	"kive/worker"
)

// RunCommandError reports one or more worker command failures.
type RunCommandError struct {
	Failures []WorkerCommandFailure
}

// WorkerCommandFailure ties a worker IP to the error returned for that worker.
type WorkerCommandFailure struct {
	WorkerIP string
	Err      error
}

func (e *RunCommandError) Error() string {
	if e == nil || len(e.Failures) == 0 {
		return "run_command failed"
	}

	failures := append([]WorkerCommandFailure(nil), e.Failures...)
	sort.Slice(failures, func(i, j int) bool {
		return failures[i].WorkerIP < failures[j].WorkerIP
	})

	lines := make([]string, 0, len(failures))
	for _, failure := range failures {
		lines = append(lines, formatWorkerFailure(failure))
	}
	if len(lines) == 1 {
		return lines[0]
	}
	return fmt.Sprintf("run_command failed on %d workers:\n%s", len(lines), strings.Join(lines, "\n"))
}

func (e *RunCommandError) Is(target error) bool {
	return target == bucket.ErrRunCommand
}

func formatWorkerFailure(failure WorkerCommandFailure) string {
	return worker.FormatWorkerFailure(failure.WorkerIP, failure.Err)
}

func newRunCommandError(failures map[string]error) error {
	if len(failures) == 0 {
		return nil
	}

	failureList := make([]WorkerCommandFailure, 0, len(failures))
	for workerIP, err := range failures {
		failureList = append(failureList, WorkerCommandFailure{
			WorkerIP: workerIP,
			Err:      err,
		})
	}

	return fmt.Errorf("%w", &RunCommandError{Failures: failureList})
}

func errConcurrencyTooLow() error {
	return errors.New("batch size (-c) must be at least 1")
}

func errCommandRequired() error {
	return errors.New("command text or --script name is required")
}

func errCommandAndScriptTogether() error {
	return errors.New("specify either command text or --script, not both")
}

func errUnknownScript(name string) error {
	return fmt.Errorf("unknown command script %q (expected workspace/commands/%s.sh)", name, name)
}

func errEmptyCommand() error {
	return errors.New("command is empty")
}

func errWorkersAndLabelsTogether() error {
	return errors.New("specify either --workers or --labels, not both")
}

func errUnknownWorkers(workerIPs []string) error {
	return fmt.Errorf("workers not in this bucket: %v", workerIPs)
}

func errNoTargetWorkers() error {
	return errors.New("no workers matched the selection")
}

func printWorkerFailure(workerIP string, err error) {
	fmt.Fprintf(os.Stderr, "%s\n", worker.FormatWorkerFailure(workerIP, err))
}
