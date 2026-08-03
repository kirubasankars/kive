// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CancelledError reports user interrupt during deploy.
type CancelledError struct{}

func (e *CancelledError) Error() string {
	return "deploy cancelled"
}

func (e *CancelledError) Unwrap() error {
	return context.Canceled
}

// JobError reports a failure while deploying a single job.
type JobError struct {
	Job string
	Err error
}

func (e *JobError) Error() string {
	return fmt.Sprintf("job %s: %v", e.Job, e.Err)
}

func (e *JobError) Unwrap() error {
	return e.Err
}

// PreDeployError reports a pre_deploy hook failure that aborts the deploy run.
type PreDeployError struct {
	Job string
	Err error
}

func (e *PreDeployError) Error() string {
	return fmt.Sprintf("job %s: pre_deploy: %v", e.Job, e.Err)
}

func (e *PreDeployError) Unwrap() error {
	return e.Err
}

// PostDeployError reports a post_deploy hook failure that aborts the deploy run.
type PostDeployError struct {
	Job string
	Err error
}

func (e *PostDeployError) Error() string {
	return fmt.Sprintf("job %s: post_deploy: %v", e.Job, e.Err)
}

func (e *PostDeployError) Unwrap() error {
	return e.Err
}

func isPreDeployError(err error) bool {
	var hookErr *PreDeployError
	return errors.As(err, &hookErr)
}

func isPostDeployError(err error) bool {
	var hookErr *PostDeployError
	return errors.As(err, &hookErr)
}

func isCancelled(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	var cancelled *CancelledError
	return errors.As(err, &cancelled)
}

func joinErrors(prefix string, errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	messages := make([]string, len(errs))
	for i, err := range errs {
		messages[i] = err.Error()
	}
	if prefix == "" {
		return fmt.Errorf("%s", strings.Join(messages, "\n"))
	}
	return fmt.Errorf("%s:\n%s", prefix, strings.Join(messages, "\n"))
}
