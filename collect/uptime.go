// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package collect

import (
	"context"
	"fmt"
	"strings"

	"kive/bucket"
	"kive/worker"
	"kive/workspace"
)

const uptimeScript = `set -eu
if command -v uptime >/dev/null 2>&1; then
  uptime -p 2>/dev/null || uptime
else
  secs=$(awk '{print int($1)}' /proc/uptime)
  days=$(( secs / 86400 ))
  hours=$(( (secs % 86400) / 3600 ))
  minutes=$(( (secs % 3600) / 60 ))
  printf 'up %d days, %d hours, %d minutes\n' "${days}" "${hours}" "${minutes}"
fi
`

// ExecuteUptime probes workers and prints host uptime as each worker responds.
func ExecuteUptime(opts Options) error {
	return ExecuteUptimeContext(context.Background(), opts)
}

// ExecuteUptimeContext probes uptime with cancellation.
func ExecuteUptimeContext(ctx context.Context, opts Options) error {
	rt, err := bucket.SetupRuntime("", bucket.NewRunContext("worker", 0))
	if err != nil {
		return err
	}
	return runWorkerSubcommand(rt, "uptime", func() error {
		// Stream formatted lines as probes complete; do not also reprint a summary
		// (that duplicated each host's uptime= line).
		return executeCollect(ctx, rt, opts, "uptime", probeUptime(ctx, rt), printUptimeResult, nil, nil)
	})
}

func probeUptime(ctx context.Context, rt *bucket.Runtime) func(string) (string, error) {
	return func(workerIP string) (string, error) {
		output, err := worker.RunRemoteScriptCombinedLogged(ctx, rt, workerIP, bucket.CommandContext{
			Phase:  "worker",
			Action: "uptime",
			Quiet:  true, // raw script stdout is reformatted by printUptimeResult
		}, strings.NewReader(uptimeScript))
		if err != nil {
			return "", err
		}

		uptime := normalizeUptimeOutput(output)
		if uptime == "" {
			return "", fmt.Errorf("probe output missing uptime")
		}
		return uptime, nil
	}
}

func normalizeUptimeOutput(output string) string {
	lines := strings.Split(output, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

func printUptimeResult(host, uptime string) {
	fmt.Printf("%s uptime=%q\n", host, uptime)
}

func printDiscoveredUptime(_ []workspace.WorkerRecord, probedWorkers []workspace.WorkerRecord, values map[string]string) error {
	for _, w := range probedWorkers {
		host := strings.TrimSpace(w.Host)
		uptime, ok := values[host]
		if !ok {
			continue
		}
		printUptimeResult(host, uptime)
	}
	return nil
}
