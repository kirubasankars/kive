// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
	"kive/kv"
)

const (
	rolloutActionStart       = "start"
	rolloutActionRestart     = "restart"
	rolloutActionReload      = "reload"
	rolloutActionSync        = "sync"
	rolloutActionSkip        = "skip"
	rolloutActionStop        = "stop"
	rolloutActionPromote     = "promote"
	rolloutActionStopPromote = "stop+promote"
)

// AllocationPlan describes one worker allocation after plan hashes are refreshed.
type AllocationPlan struct {
	WorkerIP     string
	Action       string
	AppliedHash  string
	PendingHash  string
	MatchedPaths []string
}

// JobPlan summarizes whether a job would be deployed in the current wave.
type JobPlan struct {
	Job                 string
	DeploymentOrder     int
	DeploymentSeq       int
	NeedsRollout        bool
	RunsPostDeployHooks bool
	SkipReason          string
	Allocations         []AllocationPlan
}

// DryRunResult is the outcome of a deploy dry-run.
type DryRunResult struct {
	Jobs     []JobPlan
	Required bool
}

// DryRun stages job files locally, refreshes allocation content hashes in a rolled-back
// transaction, and reports which jobs and allocations would be deployed. Plan hashes
// reflect build-time catalog content; pre_deploy hooks run after hash refresh (same order
// as deploy) but the transaction is rolled back so no state is persisted. pre_deploy may
// SSH to workers; rsync, lifecycle, and hash promotion are not performed.
func DryRun(jobsFilter []string, opts Options) (DryRunResult, error) {
	return runPendingRollout(jobsFilter, opts, pendingRolloutMode{
		persistHashes: false,
		runPreDeploy:  true,
	})
}

// PreviewPendingRollout stages job trees, persists allocation plan hashes, and
// returns the pending rollout plan without running pre_deploy or touching workers.
// Unlike DryRun, hash state is committed so cat/UI reflect content and KV changes
// after build. Call after a successful build (including post_build).
func PreviewPendingRollout(jobsFilter []string) (DryRunResult, error) {
	return runPendingRollout(jobsFilter, Options{}, pendingRolloutMode{
		persistHashes: true,
		runPreDeploy:  false,
	})
}

// BestEffortPreviewAfterBuild refreshes and persists plan hashes after a successful
// build. On failure it returns the error for the caller to log; the build itself
// should still be treated as successful.
func BestEffortPreviewAfterBuild() (DryRunResult, error) {
	return PreviewPendingRollout(nil)
}

type pendingRolloutMode struct {
	persistHashes bool
	runPreDeploy  bool
}

func runPendingRollout(jobsFilter []string, opts Options, mode pendingRolloutMode) (DryRunResult, error) {
	var result DryRunResult

	db, err := data.OpenDatabase(true)
	if err != nil {
		return result, err
	}
	defer func() {
		_ = db.Close()
	}()

	if err := os.RemoveAll(bucket.TempLocation); err != nil {
		return result, bucket.UnexpectedError(err)
	}
	defer func() {
		_ = os.RemoveAll(bucket.TempLocation)
	}()

	tx, err := db.Begin()
	if err != nil {
		return result, bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := kv.Initialize(tx); err != nil {
		return result, err
	}

	var rt *bucket.Runtime
	if mode.runPreDeploy {
		cancelRuntimeAPI := hooks.StartRuntimeAPI(tx)
		defer cancelRuntimeAPI()

		bucketID, err := data.GetBucketID(tx)
		if err != nil {
			return result, err
		}
		generation, err := data.GetBucketGeneration(tx)
		if err != nil {
			return result, err
		}
		rt, err = setupDeployRuntime(bucketID, bucket.NewRunContext("deploy", generation))
		if err != nil {
			return result, err
		}
		defer func() {
			_ = rt.Stop()
		}()
	}

	workers, err := data.GetWorkers(tx, nil)
	if err != nil {
		return result, err
	}

	orderedJobs, err := data.GetDeployableJobsInOrder(tx)
	if err != nil {
		return result, err
	}

	jobsFilter = normalizeJobFilter(jobsFilter)
	jobs := make([]data.DeployableJob, 0, len(orderedJobs))
	for _, job := range orderedJobs {
		if jobInFilter(job.Name, jobsFilter) {
			jobs = append(jobs, job)
		}
	}

	if len(jobs) > 0 {
		if err := prepareWorkersFiles(tx, workers); err != nil {
			return result, err
		}
	}

	for _, job := range jobs {
		if err := refreshPlanHashesForJobPlan(tx, job.Name); err != nil {
			return result, err
		}
	}

	if mode.runPreDeploy {
		for _, job := range jobs {
			if err := executePreHooksForJob(context.Background(), tx, rt, job.Name); err != nil {
				return result, err
			}
			if err := persistHookKV(tx, job.Name); err != nil {
				return result, err
			}
		}
	}

	for _, job := range jobs {
		plan, err := planJobRollout(tx, job.Name, job.DeploymentOrder, job.DeploymentSeq, opts)
		if err != nil {
			return result, err
		}
		result.Jobs = append(result.Jobs, plan)
		if plan.NeedsRollout || plan.RunsPostDeployHooks {
			result.Required = true
		}
	}

	if mode.persistHashes {
		if err := tx.Commit(); err != nil {
			return result, bucket.DatabaseError(err)
		}
	}

	return result, nil
}

func planJobRollout(tx *sql.Tx, job string, deploymentOrder, deploymentSeq int, opts Options) (JobPlan, error) {
	plan := JobPlan{
		Job:             job,
		DeploymentOrder: deploymentOrder,
		DeploymentSeq:   deploymentSeq,
	}

	workers, err := data.GetNonRemovedAllocationsOrdered(tx, job)
	if err != nil {
		return plan, err
	}
	if len(workers) == 0 {
		plan.SkipReason = "no allocations"
		return plan, nil
	}

	policy, err := data.GetRestartPolicy(tx, job)
	if err != nil {
		return plan, err
	}
	restartGlobs, err := data.GetRestartGlobs(tx, job)
	if err != nil {
		return plan, err
	}
	reloadGlobs, err := data.GetReloadGlobs(tx, job)
	if err != nil {
		return plan, err
	}

	for _, workerIP := range workers {
		disabled, err := data.IsAllocationDisabled(tx, workerIP, job)
		if err != nil {
			return plan, err
		}
		if disabled == 1 {
			ap, needs, err := planDisabledAllocation(tx, job, workerIP, opts)
			if err != nil {
				return plan, err
			}
			if needs {
				plan.NeedsRollout = true
			}
			plan.Allocations = append(plan.Allocations, ap)
			continue
		}

		ap, needs, err := planActiveAllocation(tx, job, workerIP, opts, policy, restartGlobs, reloadGlobs)
		if err != nil {
			return plan, err
		}
		if needs {
			plan.NeedsRollout = true
		}
		plan.Allocations = append(plan.Allocations, ap)
	}

	if !plan.NeedsRollout && !opts.Force {
		hasPostDeploy, err := data.JobHasPostDeployCommands(tx, job)
		if err != nil {
			return plan, err
		}
		if hasPostDeploy {
			plan.RunsPostDeployHooks = true
			plan.SkipReason = "post_deploy hooks only"
		} else if plan.SkipReason == "" {
			plan.SkipReason = "already promoted on all allocations"
		}
	} else if !plan.NeedsRollout {
		if plan.SkipReason == "" {
			plan.SkipReason = "already promoted on all allocations"
		}
	}
	return plan, nil
}

func planActiveAllocation(
	tx *sql.Tx,
	job, workerIP string,
	opts Options,
	policy string,
	restartGlobs, reloadGlobs []string,
) (AllocationPlan, bool, error) {
	allocID, err := data.GetAllocationID(tx, workerIP, job)
	if err != nil {
		return AllocationPlan{}, false, err
	}

	current, previous, ok, err := data.GetAllocationHash(tx, allocID)
	if err != nil {
		return AllocationPlan{}, false, err
	}

	ap := AllocationPlan{WorkerIP: workerIP}
	switch {
	case !ok:
		ap.Action = rolloutActionStart
		return ap, true, nil
	case previous == "":
		ap.Action = rolloutActionStart
		ap.PendingHash = current
		return ap, true, nil
	case previous != current:
		action, matched, hooksOnly, err := resolveAllocationLifecycle(tx, job, workerIP, opts, policy, restartGlobs, reloadGlobs, true, false)
		if err != nil {
			return AllocationPlan{}, false, err
		}
		ap.Action = action
		ap.MatchedPaths = matched
		ap.AppliedHash = previous
		ap.PendingHash = current
		return ap, !(hooksOnly && action == rolloutActionSync), nil
	case opts.Force:
		action, matched, _, err := resolveAllocationLifecycle(tx, job, workerIP, opts, policy, restartGlobs, reloadGlobs, false, false)
		if err != nil {
			return AllocationPlan{}, false, err
		}
		ap.Action = action
		ap.MatchedPaths = matched
		ap.AppliedHash = previous
		ap.PendingHash = current
		return ap, true, nil
	default:
		needsVersion, err := data.AllocationNeedsVersionRollout(tx, job, workerIP)
		if err != nil {
			return AllocationPlan{}, false, err
		}
		if needsVersion {
			action, matched, _, err := resolveAllocationLifecycle(tx, job, workerIP, opts, policy, restartGlobs, reloadGlobs, false, true)
			if err != nil {
				return AllocationPlan{}, false, err
			}
			ap.Action = action
			ap.MatchedPaths = matched
			ap.AppliedHash = previous
			ap.PendingHash = current
			return ap, true, nil
		}
		ap.Action = rolloutActionSkip
		ap.AppliedHash = previous
		ap.PendingHash = current
		return ap, false, nil
	}
}

func planDisabledAllocation(
	tx *sql.Tx,
	job, workerIP string,
	opts Options,
) (AllocationPlan, bool, error) {
	allocID, err := data.GetAllocationID(tx, workerIP, job)
	if err != nil {
		return AllocationPlan{}, false, err
	}

	current, previous, ok, err := data.GetAllocationHash(tx, allocID)
	if err != nil {
		return AllocationPlan{}, false, err
	}

	ap := AllocationPlan{
		WorkerIP:    workerIP,
		AppliedHash: previous,
		PendingHash: current,
	}

	wasDeployed := ok && previous != ""
	needsVersion, err := data.AllocationNeedsVersionRollout(tx, job, workerIP)
	if err != nil {
		return AllocationPlan{}, false, err
	}
	hashMismatch := wasDeployed && previous != current
	needsPromote := hashMismatch || needsVersion || (opts.Force && wasDeployed)

	// Disabled allocations are always stopped on deploy (reconcile), then may
	// still promote content/version without starting.
	ap.Action = rolloutActionStop
	needs := true
	if needsPromote {
		ap.Action = rolloutActionStopPromote
	}
	return ap, needs, nil
}

// PrintDryRun writes a human-readable dry-run report to stdout.
func PrintDryRun(result DryRunResult) {
	printRolloutPlan(result, "deploy dry-run")
}

// PrintPendingRollout writes a human-readable pending-rollout report after build.
func PrintPendingRollout(result DryRunResult) {
	printRolloutPlan(result, "build")
}

func printRolloutPlan(result DryRunResult, label string) {
	if len(result.Jobs) == 0 {
		// Build with an empty catalog is a normal no-op; stay quiet.
		if label == "build" {
			return
		}
		fmt.Printf("%s: no jobs matched\n", label)
		return
	}

	if result.Required {
		if label == "build" {
			fmt.Println("build: deployment pending")
		} else {
			fmt.Printf("%s: deployment required\n", label)
		}
	} else {
		if label == "build" {
			fmt.Println("build: no pending rollout")
		} else {
			fmt.Printf("%s: no deployment required\n", label)
		}
	}
	fmt.Println()

	currentSeq := -1
	for _, job := range result.Jobs {
		if job.DeploymentSeq != currentSeq {
			currentSeq = job.DeploymentSeq
			fmt.Printf("deployment sequence %d:\n", currentSeq)
		}

		if job.NeedsRollout {
			fmt.Printf("  job %q: deploy required\n", job.Job)
			for _, alloc := range job.Allocations {
				printAllocationPlan(alloc)
			}
			continue
		}

		if job.RunsPostDeployHooks {
			fmt.Printf("  job %q: post_deploy hooks only (no rollout)\n", job.Job)
			continue
		}

		reason := job.SkipReason
		if reason == "" {
			reason = "no rollout needed"
		}
		fmt.Printf("  job %q: skip (%s)\n", job.Job, reason)
	}
}

func printAllocationPlan(alloc AllocationPlan) {
	switch alloc.Action {
	case rolloutActionStart:
		if alloc.PendingHash != "" {
			fmt.Printf("    %s  start   pending_hash=%s\n", alloc.WorkerIP, alloc.PendingHash)
		} else {
			fmt.Printf("    %s  start   (no hash row yet)\n", alloc.WorkerIP)
		}
	case rolloutActionRestart:
		if len(alloc.MatchedPaths) > 0 {
			fmt.Printf(
				"    %s  restart applied_hash=%s pending_hash=%s matched=%s\n",
				alloc.WorkerIP, alloc.AppliedHash, alloc.PendingHash, strings.Join(alloc.MatchedPaths, ","),
			)
			break
		}
		fmt.Printf(
			"    %s  restart applied_hash=%s pending_hash=%s\n",
			alloc.WorkerIP, alloc.AppliedHash, alloc.PendingHash,
		)
	case rolloutActionReload:
		if len(alloc.MatchedPaths) > 0 {
			fmt.Printf(
				"    %s  reload  applied_hash=%s pending_hash=%s matched=%s\n",
				alloc.WorkerIP, alloc.AppliedHash, alloc.PendingHash, strings.Join(alloc.MatchedPaths, ","),
			)
			break
		}
		fmt.Printf(
			"    %s  reload  applied_hash=%s pending_hash=%s\n",
			alloc.WorkerIP, alloc.AppliedHash, alloc.PendingHash,
		)
	case rolloutActionSync:
		fmt.Printf(
			"    %s  sync    applied_hash=%s pending_hash=%s\n",
			alloc.WorkerIP, alloc.AppliedHash, alloc.PendingHash,
		)
	case rolloutActionSkip:
		fmt.Printf("    %s  skip    hash=%s\n", alloc.WorkerIP, alloc.PendingHash)
	case rolloutActionStop:
		fmt.Printf("    %s  stop    applied_hash=%s\n", alloc.WorkerIP, alloc.AppliedHash)
	case rolloutActionPromote:
		fmt.Printf(
			"    %s  promote applied_hash=%s pending_hash=%s\n",
			alloc.WorkerIP, alloc.AppliedHash, alloc.PendingHash,
		)
	case rolloutActionStopPromote:
		fmt.Printf(
			"    %s  stop+promote applied_hash=%s pending_hash=%s\n",
			alloc.WorkerIP, alloc.AppliedHash, alloc.PendingHash,
		)
	default:
		fmt.Printf("    %s  %s\n", alloc.WorkerIP, alloc.Action)
	}
}
