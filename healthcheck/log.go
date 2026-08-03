// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package healthcheck

import (
	"errors"
	"fmt"

	"kive/hooks"
)

func healthCheckTarget(job, workerIP string) string {
	if workerIP == "" {
		return job
	}
	return fmt.Sprintf("%s %s", job, workerIP)
}

func logKindFailures(kind, job string, err error) {
	allocErrs := collectAllocationHealthErrors(err)
	if len(allocErrs) > 0 {
		for _, allocErr := range allocErrs {
			logKindFailed(kind, allocErr.Job, allocErr.WorkerIP, allocErr.Err.Error())
		}
		return
	}
	logKindFailed(kind, job, healthCheckFailureWorkerIP(err), err.Error())
}

func logKindFailed(kind, job, workerIP, detail string) {
	if detail != "" {
		fmt.Printf("%s check failed: %s: %s\n", kind, healthCheckTarget(job, workerIP), detail)
		return
	}
	fmt.Printf("%s check failed: %s\n", kind, healthCheckTarget(job, workerIP))
}

func logKindFailedRetrying(kind, job, workerIP string, attempt, attempts int) {
	fmt.Printf(
		"%s check failed: %s (attempt %d/%d), retrying...\n",
		kind,
		healthCheckTarget(job, workerIP),
		attempt,
		attempts,
	)
}

func logHealthCheckPassed(job, workerIP string) {
	fmt.Printf("health check passed: %s\n", healthCheckTarget(job, workerIP))
}

func logHealthCheckFailed(job, workerIP, detail string) {
	if detail != "" {
		fmt.Printf("health check failed: %s: %s\n", healthCheckTarget(job, workerIP), detail)
		return
	}
	fmt.Printf("health check failed: %s\n", healthCheckTarget(job, workerIP))
}

func logHealthCheckFailures(job string, err error) {
	allocErrs := collectAllocationHealthErrors(err)
	if len(allocErrs) > 0 {
		for _, allocErr := range allocErrs {
			logHealthCheckFailed(allocErr.Job, allocErr.WorkerIP, allocErr.Err.Error())
		}
		return
	}
	logHealthCheckFailed(job, healthCheckFailureWorkerIP(err), err.Error())
}

func collectAllocationHealthErrors(err error) []*AllocationHealthError {
	if err == nil {
		return nil
	}
	var out []*AllocationHealthError
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if u, ok := e.(interface{ Unwrap() []error }); ok {
			for _, nested := range u.Unwrap() {
				walk(nested)
			}
			return
		}
		var allocErr *AllocationHealthError
		if errors.As(e, &allocErr) {
			out = append(out, allocErr)
			return
		}
		if u, ok := e.(interface{ Unwrap() error }); ok {
			walk(u.Unwrap())
		}
	}
	walk(err)
	return out
}

func logHealthCheckFailedRetrying(job, workerIP string, attempt, attempts int) {
	fmt.Printf(
		"health check failed: %s (attempt %d/%d), retrying...\n",
		healthCheckTarget(job, workerIP),
		attempt,
		attempts,
	)
}

func logHealthCheckSkipped(job, reason string) {
	fmt.Printf("health check skipped: %s (%s)\n", job, reason)
}

func logHealthCheckPasses(job string, workerIPs []string) {
	for _, workerIP := range workerIPs {
		logHealthCheckPassed(job, workerIP)
	}
}

func healthCheckFailureWorkerIP(err error) string {
	var allocErr *AllocationHealthError
	if errors.As(err, &allocErr) {
		return allocErr.WorkerIP
	}
	var runErr *hooks.RunError
	if errors.As(err, &runErr) && len(runErr.Failures) > 0 {
		return runErr.Failures[0].WorkerIP
	}
	return ""
}
