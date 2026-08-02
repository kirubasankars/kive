// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package collect

import (
	"fmt"
	"strings"

	"kive/worker"
)

func errConcurrencyTooLow() error {
	return fmt.Errorf("concurrency must be at least 1")
}

func errWorkersAndLabelsTogether() error {
	return fmt.Errorf("use either --workers or --labels, not both")
}

func errNoWorkersConfigured() error {
	return fmt.Errorf("no workers configured; add workers to workspace/workers.conf")
}

func errNoTargetWorkers() error {
	return fmt.Errorf("no workers matched the requested filters")
}

func errUnknownWorkers(hosts []string) error {
	return fmt.Errorf("unknown workers: %s", strings.Join(hosts, ", "))
}

func printWorkerFailures(probeName string, failures map[string]error) {
	if len(failures) == 0 {
		return
	}

	fmt.Fprintf(stderr(), "worker %s failures (%d):\n", probeName, len(failures))
	for host, err := range failures {
		fmt.Fprintf(stderr(), "%s\n", worker.FormatWorkerFailure(host, err))
	}
}

func probeFailuresError(probeName string, failures map[string]error) error {
	if len(failures) == 0 {
		return nil
	}

	lines := make([]string, 0, len(failures))
	for host, err := range failures {
		lines = append(lines, worker.FormatWorkerFailure(host, err))
	}
	return fmt.Errorf("worker %s probe failed:\n%s", probeName, strings.Join(lines, "\n"))
}
