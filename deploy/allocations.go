// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"kive/bucket"
	"kive/data"
	"kive/healthcheck"
	"kive/hooks"
	"kive/rollout"
	"kive/utils"
)

func handleNewAllocations(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, bucketID, job string, force bool) error {
	newAllocations, err := data.GetNewAllocations(tx, job)
	if err != nil {
		return err
	}
	if len(newAllocations) == 0 {
		return nil
	}

	startCmd := runnerCommand(bucketID, "start", job)
	startFn := func(workerIP string) error {
		env, err := allocationVersionEnv(tx, job, workerIP)
		if err != nil {
			return err
		}
		return runWorkerCommand(ctx, rt, workerIP, runnerCmdCtx(job, "rollout", "start", bucketID), []string{startCmd}, env)
	}
	rsyncFn := func(workerIP string) error {
		return syncWorkerFiles(ctx, rt, bucketID, workerIP, []string{job}, true)
	}

	if err := rolloutStartBatches(ctx, tx, rt, bucketID, job, newAllocations, force, startFn, rsyncFn); err != nil {
		return fmt.Errorf("start new allocations: %w", err)
	}
	return nil
}

func handleUpdatedAllocations(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, bucketID, job string, opts Options) error {
	updatedAllocations, err := allocationsNeedingRestart(tx, job, opts.Force)
	if err != nil {
		return err
	}
	if len(updatedAllocations) == 0 {
		return nil
	}

	policy, err := data.GetRestartPolicy(tx, job)
	if err != nil {
		return err
	}
	restartGlobs, err := data.GetRestartGlobs(tx, job)
	if err != nil {
		return err
	}
	reloadGlobs, err := data.GetReloadGlobs(tx, job)
	if err != nil {
		return err
	}

	restartWorkers := make([]string, 0)
	reloadWorkers := make([]string, 0)

	for _, workerIP := range updatedAllocations {
		allocID, err := data.GetAllocationID(tx, workerIP, job)
		if err != nil {
			return err
		}

		current, previous, ok, err := data.GetAllocationHash(tx, allocID)
		if err != nil {
			return err
		}
		hashChanged := ok && previous != "" && previous != current

		needsVersion, err := data.AllocationNeedsVersionRollout(tx, job, workerIP)
		if err != nil {
			return err
		}
		versionOnly := needsVersion && !hashChanged

		manifests, err := data.GetAllocationFileManifests(tx, allocID)
		if err != nil {
			return err
		}
		legacyNoManifest := !manifests.HasAppliedFiles

		action, _ := resolveUpdateAction(
			policy, restartGlobs, reloadGlobs,
			manifests.Applied, manifests.Pending,
			hashChanged, versionOnly, legacyNoManifest,
		)
		if action == rolloutActionSync {
			continue
		}
		if action == rolloutActionRestart {
			restartWorkers = append(restartWorkers, workerIP)
		} else {
			reloadWorkers = append(reloadWorkers, workerIP)
		}
	}

	if len(reloadWorkers) > 0 {
		reloadCmd := runnerCommand(bucketID, rolloutActionReload, job)
		reloadFn := func(workerIP string) error {
			env, err := allocationVersionEnv(tx, job, workerIP)
			if err != nil {
				return err
			}
			return runWorkerCommand(ctx, rt, workerIP, runnerCmdCtx(job, "rollout", rolloutActionReload, bucketID), []string{reloadCmd}, env)
		}
		rsyncFn := func(workerIP string) error {
			return syncWorkerFiles(ctx, rt, bucketID, workerIP, []string{job}, true)
		}
		if err := rolloutRestartBatches(ctx, tx, rt, bucketID, job, reloadWorkers, opts.Force, reloadFn, rsyncFn); err != nil {
			return err
		}
	}

	if len(restartWorkers) > 0 {
		restartCmd := runnerCommand(bucketID, rolloutActionRestart, job)
		restartFn := func(workerIP string) error {
			env, err := allocationVersionEnv(tx, job, workerIP)
			if err != nil {
				return err
			}
			return runWorkerCommand(ctx, rt, workerIP, runnerCmdCtx(job, "rollout", rolloutActionRestart, bucketID), []string{restartCmd}, env)
		}
		rsyncFn := func(workerIP string) error {
			return syncWorkerFiles(ctx, rt, bucketID, workerIP, []string{job}, true)
		}
		if err := rolloutRestartBatches(ctx, tx, rt, bucketID, job, restartWorkers, opts.Force, restartFn, rsyncFn); err != nil {
			return err
		}
	}

	return nil
}

func activeWorkers(workers []string, tx *sql.Tx, job string) ([]string, error) {
	active := make([]string, 0, len(workers))
	for _, workerIP := range workers {
		ok, err := data.IsAllocationActive(tx, workerIP, job)
		if err != nil {
			return nil, err
		}
		if ok {
			active = append(active, workerIP)
		}
	}
	return active, nil
}

func allocationsNeedingRestart(tx *sql.Tx, job string, force bool) ([]string, error) {
	newAllocations, err := data.GetNewAllocations(tx, job)
	if err != nil {
		return nil, err
	}

	if !force {
		updated, err := data.GetUpdatedAllocations(tx, job)
		if err != nil {
			return nil, err
		}
		versionPending, err := data.GetVersionPendingAllocations(tx, job)
		if err != nil {
			return nil, err
		}
		candidates := utils.Unique(append(updated, versionPending...))
		return utils.Difference(candidates, newAllocations), nil
	}
	active, err := data.GetActiveAllocations(tx, job)
	if err != nil {
		return nil, err
	}
	return utils.Difference(active, newAllocations), nil
}

func handleStoppedAllocations(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, bucketID string, jobs []string) error {
	jobFilter := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		jobFilter[job] = struct{}{}
	}

	stopped, err := data.ListStoppedAllocations(tx)
	if err != nil {
		return err
	}

	grouped := make(map[string][]stoppedReconcileItem)
	for _, alloc := range stopped {
		if _, ok := jobFilter[alloc.Job]; !ok {
			continue
		}
		grouped[alloc.Job] = append(grouped[alloc.Job], stoppedReconcileItem{
			alloc:      alloc,
			assumeDead: false,
		})
	}

	for job, items := range grouped {
		if err := reconcileStoppedAllocationsForJob(ctx, tx, rt, bucketID, job, items, Options{}); err != nil {
			return err
		}
	}
	return nil
}

type stoppedReconcileItem struct {
	alloc      data.StoppedAllocation
	assumeDead bool
}

func reconcileStoppedAllocationsForJob(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID, job string,
	items []stoppedReconcileItem,
	opts Options,
) error {
	stopItems := make([]stoppedReconcileItem, 0, len(items))
	stopIPs := make([]string, 0, len(items))

	for _, item := range items {
		// Disabled allocations are stopped on every deploy, even when never
		// promoted (empty applied_hash). Removed allocations still require a
		// prior promote so we do not stop workers that never received the job.
		if item.alloc.Disabled {
			stopItems = append(stopItems, item)
			stopIPs = append(stopIPs, item.alloc.WorkerIP)
			continue
		}
		wasDeployed, err := allocationWasDeployed(tx, item.alloc.WorkerIP, job)
		if err != nil {
			return err
		}
		if wasDeployed {
			stopItems = append(stopItems, item)
			stopIPs = append(stopIPs, item.alloc.WorkerIP)
			continue
		}
		if err := reconcileSkippedStopAllocation(tx, rt, item.alloc); err != nil {
			return fmt.Errorf("worker %s job %s: %w", item.alloc.WorkerIP, job, err)
		}
	}

	if len(stopItems) == 0 {
		return nil
	}

	resolved, err := rollout.ResolveOrder(tx, job, stopIPs)
	if err != nil {
		return err
	}

	batchSize, err := stopReconcileBatchSize(tx, job, len(stopIPs))
	if err != nil {
		return err
	}

	itemByIP := make(map[string]stoppedReconcileItem, len(stopItems))
	for _, item := range stopItems {
		itemByIP[item.alloc.WorkerIP] = item
	}

	stopCmd := runnerCommand(bucketID, "stop", job)
	batches := hooks.SplitWorkerBatches(resolved.Ordered, batchSize)
	for batchIndex, batch := range batches {
		for _, workerIP := range batch {
			if itemByIP[workerIP].alloc.Disabled {
				log.Printf("deploy: stop disabled allocation %s on %s", job, workerIP)
			}
		}

		if err := preBatchHealthCheck(ctx, tx, rt, job, opts.Force); err != nil {
			return err
		}

		stopFn := func(workerIP string) error {
			item := itemByIP[workerIP]
			stopCtx := runnerCmdCtx(job, "reconcile", "stop", bucketID)
			if item.assumeDead {
				runWorkerCommandOrAssumeDead(ctx, rt, workerIP, stopCtx, []string{stopCmd}, nil)
			} else if err := runWorkerCommand(ctx, rt, workerIP, stopCtx, []string{stopCmd}, nil); err != nil {
				return err
			}
			if item.assumeDead {
				return nil
			}
			return syncWorkerFiles(ctx, rt, bucketID, workerIP, []string{job}, true)
		}
		if err := runParallelWorkers(ctx, batch, len(batch), stopFn); err != nil {
			return fmt.Errorf("worker batch job %s: %w", job, err)
		}

		batchCtx := hooks.BatchContext{
			Job:             job,
			Phase:           deployPhaseStop,
			BatchIndex:      batchIndex,
			BatchCount:      len(batches),
			BatchAllocation: append([]string(nil), batch...),
			RolloutOrder:    resolved.FullOrder,
			OrderSource:     resolved.Source,
		}
		if err := executeAfterAllocationStopped(ctx, tx, rt, job, batch, batchCtx); err != nil {
			return fmt.Errorf("after_allocation_stopped job %s: %w", job, err)
		}

		promoteIPs := make([]string, 0, len(batch))
		for _, workerIP := range batch {
			if !itemByIP[workerIP].alloc.Disabled {
				promoteIPs = append(promoteIPs, workerIP)
			}
		}
		if err := promoteAllocationWorkers(tx, job, promoteIPs); err != nil {
			return err
		}

		if err := healthcheck.DeployHealthCheck(deployCancelCtx(ctx), tx, rt, true, job, true); err != nil {
			return err
		}
	}

	for _, item := range stopItems {
		if item.alloc.Removed {
			if err := removeJobDeployArtifactsFromWorker(
				ctx, rt, bucketID, item.alloc.WorkerIP, job, item.assumeDead,
			); err != nil {
				return err
			}
		}
		if item.alloc.Removed {
			allocID, err := data.GetAllocationID(tx, item.alloc.WorkerIP, job)
			if err != nil {
				return err
			}
			if err := data.RemoveAllocationHash(tx, allocID); err != nil {
				return err
			}
		}
	}

	return nil
}

// stopReconcileBatchSize chooses stop concurrency for reconcile.
// Off-catalog jobs stop all workers in one parallel batch (still hard-capped).
// Catalog jobs honor max_concurrent_stops (0 = all stop targets at once, hard-capped).
func stopReconcileBatchSize(tx *sql.Tx, job string, stopCount int) (int, error) {
	if stopCount < 1 {
		return 1, nil
	}
	hardCap, err := maxConcurrentSyncs()
	if err != nil {
		return 0, err
	}
	inCatalog, err := data.JobInCatalog(tx, job)
	if err != nil {
		return 0, err
	}
	if !inCatalog {
		return effectiveBatchSize(0, stopCount, hardCap), nil
	}
	parallelism, err := data.GetMaxConcurrentStops(tx, job)
	if err != nil {
		return 0, err
	}
	return effectiveBatchSize(parallelism, stopCount, hardCap), nil
}

func reconcileSkippedStopAllocation(
	tx *sql.Tx,
	rt *bucket.Runtime,
	alloc data.StoppedAllocation,
) error {
	if alloc.Removed || alloc.Disabled {
		if rt != nil {
			_ = rt.LogEvent(alloc.WorkerIP, "reconcile_skip_stop", map[string]string{
				"job":      alloc.Job,
				"reason":   "not_deployed",
				"removed":  fmt.Sprintf("%t", alloc.Removed),
				"disabled": fmt.Sprintf("%t", alloc.Disabled),
			})
		}
	}

	if alloc.Removed {
		allocID, err := data.GetAllocationID(tx, alloc.WorkerIP, alloc.Job)
		if err != nil {
			return err
		}
		if err := data.RemoveAllocationHash(tx, allocID); err != nil {
			return err
		}
	}

	return nil
}

func reconcileStoppedAllocation(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID string,
	alloc data.StoppedAllocation,
	assumeDead bool,
) error {
	return reconcileStoppedAllocationsForJob(ctx, tx, rt, bucketID, alloc.Job, []stoppedReconcileItem{{
		alloc:      alloc,
		assumeDead: assumeDead,
	}}, Options{})
}

func reconcileRemovedAndDisabledAllocations(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	bucketID string,
	jobsFilter []string,
	opts Options,
) error {
	currentWorkers, err := data.LoadWorkerCatalog(tx)
	if err != nil {
		return err
	}

	stopped, err := data.ListStoppedAllocations(tx)
	if err != nil {
		return err
	}

	offCatalogWorkers := make(map[string]struct{})
	grouped := make(map[string][]stoppedReconcileItem)
	filteredStopped := make([]data.StoppedAllocation, 0, len(stopped))

	for _, alloc := range stopped {
		if !jobInFilter(alloc.Job, jobsFilter) {
			continue
		}
		filteredStopped = append(filteredStopped, alloc)

		wasRunning, err := allocationWasDeployed(tx, alloc.WorkerIP, alloc.Job)
		if err != nil {
			return err
		}

		assumeDead := data.StoppedAllocationAssumeDead(alloc, currentWorkers)
		grouped[alloc.Job] = append(grouped[alloc.Job], stoppedReconcileItem{
			alloc:      alloc,
			assumeDead: assumeDead,
		})

		if alloc.Removed && !currentWorkers.Contains(alloc.WorkerIP) && wasRunning {
			offCatalogWorkers[alloc.WorkerIP] = struct{}{}
		}
	}

	for job, items := range grouped {
		if err := reconcileStoppedAllocationsForJob(ctx, tx, rt, bucketID, job, items, opts); err != nil {
			return err
		}
	}

	for workerIP := range offCatalogWorkers {
		if err := removeWorkerBucketFromWorker(ctx, rt, bucketID, workerIP); err != nil {
			return err
		}
	}

	return purgeHookKVForInactiveJobs(tx, filteredStopped)
}

func allocationWasDeployed(tx *sql.Tx, workerIP, job string) (bool, error) {
	allocID, err := data.GetAllocationID(tx, workerIP, job)
	if err != nil {
		return false, err
	}
	appliedHash, err := data.GetAllocationAppliedHash(tx, allocID)
	if err != nil {
		return false, err
	}
	return appliedHash != "", nil
}

func countActiveAllocations(tx *sql.Tx, job string) (int, error) {
	workers, err := data.GetActiveAllocations(tx, job)
	if err != nil {
		return 0, err
	}
	return len(workers), nil
}
