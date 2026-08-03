// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"kive/bucket"
	"kive/prereq"
)

// PrerequisitesError reports missing tools on a worker before deploy.
type PrerequisitesError struct {
	WorkerIP string
	Missing  []string
}

func (e *PrerequisitesError) Error() string {
	if len(e.Missing) == 0 {
		return fmt.Sprintf("worker %s: prerequisite check failed", e.WorkerIP)
	}
	return fmt.Sprintf("worker %s missing prerequisites: %s", e.WorkerIP, strings.Join(e.Missing, ", "))
}

func (e *PrerequisitesError) Is(target error) bool {
	return target == bucket.ErrWorkerPrerequisites
}

// CheckPrerequisites verifies deploy prerequisites on each worker over SSH.
func CheckPrerequisites(workerIPs []string, useSudo bool) error {
	return checkPrerequisites(context.Background(), nil, workerIPs, useSudo, prereq.DeployWorkerSpec)
}

// CheckPrerequisitesWithRuntime verifies deploy prerequisites and logs SSH probes.
// Parent ctx cancellation aborts waiting workers and in-flight SSH probes.
func CheckPrerequisitesWithRuntime(ctx context.Context, rt *bucket.Runtime, workerIPs []string, useSudo bool) error {
	return checkPrerequisites(ctx, rt, workerIPs, useSudo, prereq.DeployWorkerSpec)
}

// CheckRunCommandPrerequisites verifies SSH/bash prerequisites for run_command on workers.
func CheckRunCommandPrerequisites(workerIPs []string, useSudo bool) error {
	return CheckRunCommandPrerequisitesContext(context.Background(), workerIPs, useSudo)
}

// CheckRunCommandPrerequisitesContext verifies SSH/bash prerequisites with cancellation.
func CheckRunCommandPrerequisitesContext(ctx context.Context, workerIPs []string, useSudo bool) error {
	return checkPrerequisites(ctx, nil, workerIPs, useSudo, prereq.RunCommandWorkerSpec)
}

// CheckRunCommandPrerequisitesWithRuntime verifies run_command prerequisites with logging.
func CheckRunCommandPrerequisitesWithRuntime(ctx context.Context, rt *bucket.Runtime, workerIPs []string, useSudo bool) error {
	return checkPrerequisites(ctx, rt, workerIPs, useSudo, prereq.RunCommandWorkerSpec)
}

// CheckWorkerRunCommandPrerequisites verifies run_command prerequisites on one worker.
// Call ValidateSSHConfig and EnsureSSHStateDir once before probing many workers.
func CheckWorkerRunCommandPrerequisites(workerIP string, useSudo bool) error {
	return CheckWorkerRunCommandPrerequisitesContext(context.Background(), workerIP, useSudo)
}

// CheckWorkerRunCommandPrerequisitesContext verifies one worker with cancellation.
func CheckWorkerRunCommandPrerequisitesContext(ctx context.Context, workerIP string, useSudo bool) error {
	return checkWorkerPrerequisites(ctx, nil, workerIP, useSudo, prereq.RunCommandWorkerSpec)
}

// CheckWorkerRunCommandPrerequisitesWithRuntime verifies run_command prerequisites on one worker with logging.
func CheckWorkerRunCommandPrerequisitesWithRuntime(ctx context.Context, rt *bucket.Runtime, workerIP string, useSudo bool) error {
	return checkWorkerPrerequisites(ctx, rt, workerIP, useSudo, prereq.RunCommandWorkerSpec)
}

func checkPrerequisites(ctx context.Context, rt *bucket.Runtime, workerIPs []string, useSudo bool, spec prereq.WorkerSpec) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(workerIPs) == 0 {
		return nil
	}

	if err := ValidateSSHConfig(); err != nil {
		return fmt.Errorf("%w: %s", bucket.ErrWorkerPrerequisites, err.Error())
	}

	if err := EnsureSSHStateDir(); err != nil {
		return err
	}

	if err := CheckTrustedHostsContext(ctx, workerIPs); err != nil {
		return err
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for _, workerIP := range workerIPs {
		if err := ctx.Err(); err != nil {
			return err
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			if err := checkWorkerPrerequisites(ctx, rt, ip, useSudo, spec); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(workerIP)
	}

	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n%s", bucket.ErrWorkerPrerequisites, joinPrerequisiteErrors(errs))
}

func checkWorkerPrerequisites(ctx context.Context, rt *bucket.Runtime, workerIP string, useSudo bool, spec prereq.WorkerSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec.UseSudo = useSudo
	script := prereq.BuildWorkerCheckScript(spec)
	cmdCtx := bucket.CommandContext{
		Phase:  "validate",
		Action: "prerequisites",
	}

	var output string
	var err error
	if rt != nil {
		output, err = RunRemoteScriptCombinedLogged(ctx, rt, workerIP, cmdCtx, strings.NewReader(script))
	} else {
		output, err = RunRemoteScriptCombinedContext(ctx, workerIP, strings.NewReader(script))
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	missing := prereq.ParseMissingPrerequisites(output)
	if len(missing) > 0 {
		return &PrerequisitesError{WorkerIP: workerIP, Missing: missing}
	}
	return fmt.Errorf("worker %s: %s", workerIP, err.Error())
}

func joinPrerequisiteErrors(errs []error) string {
	seen := make(map[string]struct{}, len(errs))
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		msg := err.Error()
		if _, ok := seen[msg]; ok {
			continue
		}
		seen[msg] = struct{}{}
		messages = append(messages, msg)
	}
	return strings.Join(messages, "\n")
}
