// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"kive/bucket"
)

func runHookOnWorker(
	ctx context.Context,
	rt *bucket.Runtime,
	allocationID, jobName, workerIP, allocationIndex string,
	disabled int,
	hookName, event string,
	verbose bool,
	extraEnv []string,
	scriptArgs []string,
) error {
	workerDir := bucket.GetTempWorkerPath(workerIP)
	hooksDir := path.Join(workerDir, "jobs", jobName, "_hooks")

	runtime, scriptPath, err := ResolveHookScript(hooksDir, hookName)
	if err != nil {
		return err
	}

	env := buildHookEnv(allocationID, jobName, workerIP, allocationIndex, disabled, hookName, event, extraEnv)
	cmdCtx := bucket.CommandContext{
		Job:    jobName,
		Phase:  "hook",
		Action: hookName,
		Cmd:    path.Join(hooksDir, scriptPath),
		Quiet:  isHealthProbeEvent(event) && !verbose,
	}
	lines, err := CommandExecLines(hooksDir, scriptPath, runtime, jobName, scriptArgs)
	if err != nil {
		return err
	}
	logHookRunning(jobName, workerIP, hookName, verbose)
	start := time.Now()
	err = rt.Exec(ctx, workerIP, cmdCtx, lines, env)
	logHookDone(jobName, workerIP, hookName, err, time.Since(start), verbose)
	return err
}

func buildHookEnv(
	allocationID, jobName, workerIP, allocationIndex string,
	disabled int,
	hookName, event string,
	extraEnv []string,
) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, extraEnv...)
	env = append(env,
		fmt.Sprintf("ALLOCATION_ID=%s", allocationID),
		fmt.Sprintf("ALLOCATION_IP=%s", workerIP),
		fmt.Sprintf("ALLOCATION_INDEX=%s", allocationIndex),
		fmt.Sprintf("DISABLED=%d", disabled),
		fmt.Sprintf("JOB=%s", jobName),
		fmt.Sprintf("EVENT=%s", event),
		fmt.Sprintf("HOOK=%s", hookName),
		fmt.Sprintf("%s=%s", EnvHookAPIHost, runtimeAPIHost()),
	)
	if token := ActiveRuntimeAPIToken(); token != "" {
		env = append(env, fmt.Sprintf("%s=%s", EnvHookAPIToken, token))
	}
	if port := ActiveRuntimeAPIPort(); port > 0 {
		env = append(env, fmt.Sprintf("%s=%d", EnvHookAPIPort, port))
	}
	if root, err := bucket.Root(); err == nil {
		env = append(env, fmt.Sprintf("%s=%s", bucket.EnvBucketRoot, root))
	}
	return env
}

// runtimeAPIHost is the hostname hook scripts use to reach StartRuntimeAPI on the host.
func runtimeAPIHost() string {
	return "127.0.0.1"
}

func hookAllowed(allowed []string, hookName string) bool {
	for _, name := range allowed {
		if name == hookName {
			return true
		}
	}
	return false
}
