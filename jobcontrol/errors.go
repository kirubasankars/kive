// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package jobcontrol

import (
	"fmt"
	"strings"

	"kive/bucket"
)

// InvalidTargetError reports an unsupported runner target.
type InvalidTargetError struct {
	Target string
}

func (e *InvalidTargetError) Error() string {
	return fmt.Sprintf(
		"invalid job control target %q (built-in: %s; custom targets must match [a-zA-Z0-9][a-zA-Z0-9_.-]*)",
		e.Target,
		strings.Join(validTargets(), ", "),
	)
}

// InvalidFilterError reports job or worker filters that match nothing in the bucket.
type InvalidFilterError struct {
	Kind   string
	Values []string
}

func (e *InvalidFilterError) Error() string {
	return fmt.Sprintf("invalid %s filter: %v", e.Kind, e.Values)
}

// DisabledAllocationError reports a --allocations target disabled via disabled.conf.
type DisabledAllocationError struct {
	Job      string
	WorkerIP string
}

func (e *DisabledAllocationError) Error() string {
	return fmt.Sprintf("allocation %s for job %s is disabled", e.WorkerIP, e.Job)
}

// DisabledJobError reports a job with no active allocations (fully disabled in disabled.conf).
type DisabledJobError struct {
	Job string
}

func (e *DisabledJobError) Error() string {
	return fmt.Sprintf("job %s is disabled", e.Job)
}

// RemovedAllocationError reports a --allocations target on a removed allocation.
type RemovedAllocationError struct {
	Job      string
	WorkerIP string
}

func (e *RemovedAllocationError) Error() string {
	return fmt.Sprintf("allocation %s for job %s is removed", e.WorkerIP, e.Job)
}

// WorkerFailure ties a worker IP to a runner execution error.
type WorkerFailure struct {
	WorkerIP string
	Err      error
}

// JobRunError reports failures while controlling a single job.
type JobRunError struct {
	Job      string
	Target   Target
	Failures []WorkerFailure
	Err      error
}

func (e *JobRunError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("job %s target %s: %v", e.Job, e.Target, e.Err)
	}
	messages := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		messages = append(messages, fmt.Sprintf("%s: %v", failure.WorkerIP, failure.Err))
	}
	return fmt.Sprintf("job %s target %s failed: %s", e.Job, e.Target, strings.Join(messages, "; "))
}

func (e *JobRunError) Unwrap() error {
	if e.Err != nil {
		return e.Err
	}
	if len(e.Failures) == 1 {
		return e.Failures[0].Err
	}
	return bucket.ErrRunCommand
}

func (e *JobRunError) Is(target error) bool {
	return target == bucket.ErrRunCommand
}

// ControlError aggregates failures across multiple jobs.
type ControlError struct {
	Failures []JobRunError
}

func (e *ControlError) Error() string {
	messages := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		messages = append(messages, failure.Error())
	}
	return strings.Join(messages, "; ")
}

func (e *ControlError) Unwrap() error {
	if len(e.Failures) == 1 {
		return &e.Failures[0]
	}
	return bucket.ErrRunCommand
}

func (e *ControlError) Is(target error) bool {
	return target == bucket.ErrRunCommand
}

func newControlError(failures []JobRunError) error {
	if len(failures) == 0 {
		return nil
	}
	return &ControlError{Failures: failures}
}
