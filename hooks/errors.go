// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"fmt"
	"net/http"
	"strings"

	"kive/bucket"
)

// NotFoundError reports a hook that is not registered for the given event.
type NotFoundError struct {
	Job   string
	Hook  string
	Event string
}

func (e *NotFoundError) Error() string {
	if e.Job == "" {
		return fmt.Sprintf("hook %q is not registered for event %q", e.Hook, e.Event)
	}
	return fmt.Sprintf("job %q hook %q is not registered for event %q", e.Job, e.Hook, e.Event)
}

// WorkerFailure ties a worker IP to a command execution error.
type WorkerFailure struct {
	WorkerIP string
	Err      error
}

// RunError reports failures across one or more worker allocations.
type RunError struct {
	Job      string
	Hook     string
	Failures []WorkerFailure
}

func (e *RunError) Error() string {
	messages := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		messages = append(messages, fmt.Sprintf("%s: %v", failure.WorkerIP, failure.Err))
	}
	return fmt.Sprintf("job %s hook %s failed: %s", e.Job, e.Hook, strings.Join(messages, "; "))
}

func (e *RunError) Is(target error) bool {
	return target == bucket.ErrHookFailed
}

func (e *RunError) Unwrap() error {
	if len(e.Failures) == 1 {
		return e.Failures[0].Err
	}
	return bucket.ErrHookFailed
}

func newRunError(job, hook string, failures []WorkerFailure) error {
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("%w", &RunError{Job: job, Hook: hook, Failures: failures})
}

type httpError struct {
	message    string
	statusCode int
}

func (e *httpError) Write(w http.ResponseWriter) {
	http.Error(w, e.message, e.statusCode)
}

var httpErrors = struct {
	invalidContentType *httpError
	missingAllocID     *httpError
	invalidAllocID     *httpError
	badRequestBody     *httpError
	invalidFormat      *httpError
	missingFields      *httpError
	invalidNamespace   *httpError
	kvNotFound         *httpError
}{
	invalidContentType: &httpError{"Content-Type must be application/json", http.StatusUnsupportedMediaType},
	missingAllocID:     &httpError{"X-ALLOCATION-ID header is missing", http.StatusBadRequest},
	invalidAllocID:     &httpError{"Invalid allocation ID", http.StatusNotFound},
	badRequestBody:     &httpError{"Failed to read request body", http.StatusBadRequest},
	invalidFormat:      &httpError{"Invalid JSON format", http.StatusBadRequest},
	missingFields:      &httpError{"Both namespace and key are required", http.StatusBadRequest},
	invalidNamespace:   &httpError{"Invalid or unauthorized namespace", http.StatusBadRequest},
	kvNotFound:         &httpError{"KV get operation failed", http.StatusNotFound},
}
