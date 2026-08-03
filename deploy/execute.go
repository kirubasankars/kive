// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package deploy pushes job artifacts to worker nodes via the bucket container.
//
// Pipeline: prerequisites → bump generation (in-tx) → prepare worker metadata → plan hash
// refresh (all deployable jobs) → pre_deploy → reconcile (per-batch health/stop/rsync/
// hooks) → per deployment_seq rollout (per-batch rsync/lifecycle/health) → final worker
// metadata rsync → post_deploy → commit (partial deploy on failure).
//
// Soft cancel (Ctrl+C / Activity Cancel) only aborts while waiting on a health-check
// gate. Rsync, lifecycle, hooks, final worker metadata sync, and post_deploy ignore
// soft cancel; a second interrupt force-quits. After generation / plan-hash / promote
// writes begin, deploy always commits the transaction (including on health-gate cancel
// and job failures). A deployJob error (lifecycle, health, rsync) aborts remaining jobs;
// promotions already completed are kept. Early returns before that point roll back so
// kive.db is unchanged. Never roll back promotes after workers have been mutated — that would leave
// the catalog behind the cluster. The generation bump is persisted only on a clean run
// (every job deployed or skipped); unclean/cancelled runs revert generation so remotes
// stay matched and worker.json is not stamped for a partial fleet upgrade.
package deploy

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"log"
	"os"
	"time"

	"kive/bucket"
	"kive/certs"
	"kive/data"
	"kive/hooks"
	"kive/kv"
	"kive/snapshot"
)

//go:embed runner.py
var runnerPy []byte

//go:embed worker.py
var workerPy []byte

//go:embed apply_iptables
var applyIptablesSh []byte

// BumpGeneration increments the bucket generation in its own committed transaction.
func BumpGeneration(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	generation, err := data.GetBucketGeneration(tx)
	if err != nil {
		return err
	}
	if err := data.SetBucketGeneration(tx, generation+1); err != nil {
		return err
	}
	return tx.Commit()
}

func bumpGenerationForDeploy(tx *sql.Tx, rt *bucket.Runtime, bumped *bool, previous *int) error {
	if *bumped {
		return nil
	}
	generation, err := data.GetBucketGeneration(tx)
	if err != nil {
		return err
	}
	*previous = generation
	if err := data.SetBucketGeneration(tx, generation+1); err != nil {
		return err
	}
	if rt != nil {
		rt.SetGeneration(generation + 1)
	}
	*bumped = true
	return nil
}

func revertGenerationForDeploy(tx *sql.Tx, rt *bucket.Runtime, previous int, bumped bool) error {
	if !bumped {
		return nil
	}
	if err := data.SetBucketGeneration(tx, previous); err != nil {
		return err
	}
	if rt != nil {
		rt.SetGeneration(previous)
	}
	return nil
}

// Execute deploys jobs to all workers, optionally filtered by job name.
// When opts.Force is true, jobs already promoted on all allocations are staged and
// restarted anyway, and pre-batch health gates are skipped (post-batch health still applies).
// Soft cancel (SIGINT/SIGTERM) only aborts while waiting on health-check gates;
// a second interrupt force-quits.
func Execute(jobsFilter []string, opts Options) error {
	ctx, stop := withDeployContext()
	defer stop()
	return execute(ctx, jobsFilter, opts)
}

// ExecuteContext is like Execute but soft-cancels when ctx is done (e.g. serve
// run cancel). Soft cancel only aborts while waiting on health-check gates.
func ExecuteContext(ctx context.Context, jobsFilter []string, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return execute(ctx, jobsFilter, opts)
}

func execute(ctx context.Context, jobsFilter []string, opts Options) error {
	ctx = withDeployWorkContext(ctx)
	db, err := data.OpenDatabase(true)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	if err := os.RemoveAll(bucket.TempLocation); err != nil {
		return bucket.UnexpectedError(err)
	}
	defer func() {
		_ = os.RemoveAll(bucket.TempLocation)
	}()

	tx, err := db.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := kv.Initialize(tx); err != nil {
		return err
	}

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	workers, err := data.GetWorkers(tx, nil)
	if err != nil {
		return err
	}

	generation, err := data.GetBucketGeneration(tx)
	if err != nil {
		return err
	}

	rt, err := setupDeployRuntime(bucketID, bucket.NewRunContext("deploy", generation))
	if err != nil {
		return err
	}
	cancel := hooks.StartRuntimeAPI(tx)
	runStarted := time.Now()
	exitCode := 0
	defer func() {
		cancel()
		if rt != nil {
			_ = rt.LogRunEnd(exitCode, time.Since(runStarted), nil)
			_ = rt.Stop()
		}
	}()
	if rt != nil {
		if err := rt.LogRunBegin(nil); err != nil {
			return err
		}
	}

	jobsFilter = normalizeJobFilter(jobsFilter)

	if err := checkDeployPrerequisites(ctx, rt, workers); err != nil {
		exitCode = 1
		return err
	}

	deployableJobs, err := listDeployableJobs(tx, jobsFilter)
	if err != nil {
		return err
	}

	var (
		deployFailures     []error
		generationBumped   bool
		generationPrevious int
		abortDeploy        bool
		jobOutcomes        = make(map[string]data.DeployHistoryJob, len(deployableJobs))
		prepareFailed      = make(map[string]bool, len(deployableJobs))
	)

	if err := bumpGenerationForDeploy(tx, rt, &generationBumped, &generationPrevious); err != nil {
		return err
	}
	if err := prepareWorkersFiles(tx, workers); err != nil {
		return err
	}

	for _, job := range deployableJobs {
		if err := refreshPlanHashesForJobPlan(tx, job); err != nil {
			if isCancelled(err) {
				abortDeploy = true
				break
			}
			prepareFailed[job] = true
			jobErr := &JobError{Job: job, Err: err}
			deployFailures = append(deployFailures, jobErr)
			recordJobOutcome(tx, jobOutcomes, job, data.DeployHistoryOutcomeFailed, jobErr.Error())
		}
	}

	if !abortDeploy {
		for _, job := range deployableJobs {
			if err := executePreHooksForJob(ctx, tx, rt, job); err != nil {
				deployFailures = append(deployFailures, err)
				if isPreDeployError(err) || isCancelled(err) {
					abortDeploy = true
					break
				}
			}
			if err := persistHookKV(tx, job); err != nil {
				deployFailures = append(deployFailures, err)
			}
		}
	}

	if !abortDeploy {
		if err := reconcileRemovedAndDisabledAllocations(ctx, tx, rt, bucketID, jobsFilter, opts); err != nil {
			deployFailures = append(deployFailures, err)
			// Never return here: reconcile may have already stopped workers / promoted
			// hash rows in this tx. Commit below so kive.db matches worker-side work.
			abortDeploy = true
		}
	}

	for _, job := range deployableJobs {
		if abortDeploy {
			break
		}
		if prepareFailed[job] {
			// Staging is incomplete (e.g. template error before certs/hash refresh).
			// Do not rsync or restart from a partial tree.
			continue
		}

		needsRollout, err := JobNeedsRollout(tx, job)
		if err != nil {
			deployFailures = append(deployFailures, &JobError{Job: job, Err: err})
			recordJobOutcome(tx, jobOutcomes, job, data.DeployHistoryOutcomeFailed, err.Error())
			abortDeploy = true
			break
		}

		if !opts.Force && !needsRollout {
			log.Printf("deploy: skip lifecycle for job %q (deploy complete on all allocations)", job)
			if rt != nil {
				_ = rt.LogEvent("", "deploy_skip", map[string]string{
					"job":    job,
					"reason": "deploy_complete",
				})
			}
			recordJobOutcome(tx, jobOutcomes, job, data.DeployHistoryOutcomeSkipped, "deploy_complete")
			continue
		}

		deployErr := deployJob(ctx, tx, rt, bucketID, job, opts)
		if persistErr := persistHookKV(tx, job); persistErr != nil {
			deployFailures = append(deployFailures, persistErr)
		}
		if deployErr != nil {
			deployFailures = append(deployFailures, deployErr)
			reason := deployErr.Error()
			outcome := data.DeployHistoryOutcomeFailed
			if isCancelled(deployErr) {
				outcome = data.DeployHistoryOutcomeAborted
			}
			recordJobOutcome(tx, jobOutcomes, job, outcome, reason)
			log.Printf("deploy: aborting remaining jobs after failure of %q", job)
			abortDeploy = true
			break
		}
		recordJobOutcome(tx, jobOutcomes, job, data.DeployHistoryOutcomeDeployed, "")
	}

	jobsClean := allJobsDeployedOrSkipped(deployableJobs, jobOutcomes)
	// Always refresh jobs.json/bin after a bumped run (including soft cancel).
	// worker.json is included only on a clean fleet generation.
	if generationBumped {
		if err := finalSyncWorkerMetadata(ctx, tx, rt, bucketID, workers, jobsClean); err != nil {
			deployFailures = append(deployFailures, err)
		}
	}

	if !abortDeploy {
		for _, job := range deployableJobs {
			hasPostDeploy, err := data.JobHasPostDeployCommands(tx, job)
			if err != nil {
				deployFailures = append(deployFailures, &JobError{Job: job, Err: err})
				continue
			}
			if !hasPostDeploy {
				continue
			}
			if err := executePostHooks(ctx, tx, rt, job); err != nil {
				deployFailures = append(deployFailures, err)
				if isPostDeployError(err) {
					abortDeploy = true
					break
				}
				if isCancelled(err) {
					abortDeploy = true
					break
				}
			}
			if persistErr := persistHookKV(tx, job); persistErr != nil {
				deployFailures = append(deployFailures, persistErr)
			}
		}
	}

	deployFailures = noteHealthCancel(deployFailures)

	// Persist the bump only when every job deployed/skipped. Keep it if worker.json
	// may already be on remotes after a clean metadata sync attempt.
	if generationBumped && !jobsClean {
		if err := revertGenerationForDeploy(tx, rt, generationPrevious, generationBumped); err != nil {
			deployFailures = append(deployFailures, err)
		}
	}

	if generationBumped {
		if err := writeDeployHistory(tx, rt, runStarted, jobsFilter, opts, deployableJobs, jobOutcomes, deployFailures); err != nil {
			deployFailures = append(deployFailures, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}

	if vacErr := data.Vacuum(db); vacErr != nil {
		exitCode = 1
		if len(deployFailures) == 0 {
			return vacErr
		}
		deployFailures = append(deployFailures, vacErr)
	}

	if err := snapshot.Backup(ctx, rt, db); err != nil {
		log.Printf("snapshot backup: %v", err)
	}

	err = joinErrors("deploy failed", deployFailures)
	if err != nil {
		exitCode = 1
		// Skip remote-write retries when deploy already failed so the real
		// prepare/lifecycle error is not buried under cert-metrics noise.
		return err
	}

	certs.PushMetricsContext(ctx, db)
	return nil
}

func listDeployableJobs(tx *sql.Tx, jobsFilter []string) ([]string, error) {
	orderedJobs, err := data.GetDeployableJobsInOrder(tx)
	if err != nil {
		return nil, err
	}
	allJobs := make([]string, 0, len(orderedJobs))
	for _, job := range orderedJobs {
		allJobs = append(allJobs, job.Name)
	}
	return selectJobsForDeploy(allJobs, jobsFilter), nil
}

// noteHealthCancel ensures a soft-cancel that aborted at a health gate is
// reported as CancelledError for stable CLI/history messaging.
func noteHealthCancel(failures []error) []error {
	cancelled := false
	hasCancelledError := false
	for _, err := range failures {
		if isCancelled(err) {
			cancelled = true
		}
		var c *CancelledError
		if errors.As(err, &c) {
			hasCancelledError = true
		}
	}
	if !cancelled {
		return failures
	}
	log.Println("deploy: cancelled during health check, committing partial progress...")
	if hasCancelledError {
		return failures
	}
	return append(failures, &CancelledError{})
}
