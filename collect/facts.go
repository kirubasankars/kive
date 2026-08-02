// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package collect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kive/bucket"
	"kive/worker"
	"kive/workspace"
)

// Execute probes workers and prints discovered memory and CPU values.
// With GenerateWorkers, writes updated facts back to workspace/workers.conf.
func Execute(opts Options) error {
	return ExecuteContext(context.Background(), opts)
}

// ExecuteContext probes workers and supports cancellation of active SSH commands.
func ExecuteContext(ctx context.Context, opts Options) error {
	rt, err := bucket.SetupRuntime("", bucket.NewRunContext("worker", 0))
	if err != nil {
		return err
	}
	return runWorkerSubcommand(rt, "facts", func() error {
		if opts.GenerateWorkers {
			// Full merge required; do not stream per-host lines.
			return executeCollect(ctx, rt, opts, "facts", probeWorker(ctx, rt), nil, writeGeneratedWorkersFile, nil)
		}
		// Stream formatted lines as probes complete (same as worker uptime).
		return executeCollect(ctx, rt, opts, "facts", probeWorker(ctx, rt), printFactsResult, nil, nil)
	})
}

func probeWorker(ctx context.Context, rt *bucket.Runtime) func(string) (factsProbeResult, error) {
	return func(workerIP string) (factsProbeResult, error) {
		output, err := worker.RunRemoteScriptCombinedLogged(ctx, rt, workerIP, bucket.CommandContext{
			Phase:  "worker",
			Action: "facts",
			Quiet:  true, // raw probe lines are reformatted by printFactsResult
		}, strings.NewReader(probeScript))
		if err != nil {
			return factsProbeResult{}, err
		}

		parsed, err := parseProbeOutput(output)
		if err != nil {
			return factsProbeResult{}, fmt.Errorf("parse probe output: %w", err)
		}
		return toFactsProbeResult(parsed), nil
	}
}

func printFactsResult(host string, result factsProbeResult) {
	memory := workspace.FormatMemoryMB(result.MemoryCPU.MemoryMB)
	cpu := workspace.FormatCPUMHz(result.MemoryCPU.CPUMHz)

	parts := []string{
		fmt.Sprintf("%s memory=%q cpu=%q", host, memory, cpu),
	}
	for _, volume := range result.Volumes {
		parts = append(parts, fmt.Sprintf("volume=%q", formatVolumeField(volume)))
	}
	fmt.Println(strings.Join(parts, " "))
}

func writeGeneratedWorkersFile(allWorkers, _ []workspace.WorkerRecord, updates map[string]factsProbeResult) error {
	memoryCPU := make(map[string]workspace.WorkerFacts, len(updates))
	for host, result := range updates {
		memoryCPU[host] = result.MemoryCPU
	}
	merged := workspace.MergeWorkerFacts(allWorkers, memoryCPU)
	return workspace.WriteWorkersFile(merged)
}

func runWorkerSubcommand(rt *bucket.Runtime, action string, fn func() error) error {
	runStarted := time.Now()
	exitCode := 0
	defer func() {
		_ = rt.LogRunEnd(exitCode, time.Since(runStarted), map[string]string{"action": action})
		_ = rt.Stop()
	}()
	if err := rt.LogRunBegin(map[string]string{"action": action}); err != nil {
		return err
	}
	err := fn()
	if err != nil {
		exitCode = 1
	}
	return err
}
