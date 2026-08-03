// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package build provides interfaces to build workspace
package build

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"kive/bucket"
	"kive/buildinfo"
	"kive/data"
	"kive/hooks"
	"kive/kv"
	"kive/promconfig"
	"kive/rollout"
	"kive/workspace"
)

const postBuildEvent = "post_build"
const buildPhase = "build"

// Options configures optional build behavior.
type Options struct {
	// DeleteSecretsKV tombstones vars/job/<job> and secrets/job/<job> for workspace jobs
	// with no active allocations. Removed jobs always lose both namespaces without the flag.
	DeleteSecretsKV bool
	// OnJobSignature receives each accepted job provenance record.
	OnJobSignature func(JobSignatureAudit)
}

func runPostBuildHooks(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime) error {
	if err := kv.Initialize(tx); err != nil {
		return fmt.Errorf("post_build: init kv: %w", err)
	}

	cancelHookServer := hooks.StartRuntimeAPI(tx)
	defer cancelHookServer()

	maxSequence, err := data.GetMaxDeploymentSeq(tx)
	if err != nil {
		return fmt.Errorf("post_build: get max deployment sequence: %w", err)
	}

	var hookErrors []error
	for sequence := 0; sequence <= maxSequence; sequence++ {
		jobsAtSequence, err := data.GetJobsByDeploymentSeq(tx, sequence)
		if err != nil {
			return fmt.Errorf("post_build: get jobs for deployment_seq %d: %w", sequence, err)
		}

		for _, jobName := range jobsAtSequence {
			commandNames, err := data.GetHooks(tx, jobName, postBuildEvent)
			if err != nil {
				hookErrors = append(hookErrors, fmt.Errorf("post_build: get commands for job %s: %w", jobName, err))
				continue
			}

			for _, commandName := range commandNames {
				workerIPs, err := data.GetNonRemovedAllocations(tx, jobName)
				if err != nil {
					hookErrors = append(hookErrors, fmt.Errorf("post_build: get allocations for job %s: %w", jobName, err))
					continue
				}
				resolved, err := rollout.ResolveOrder(tx, jobName, workerIPs)
				if err != nil {
					hookErrors = append(hookErrors, fmt.Errorf("post_build: rollout order for job %s: %w", jobName, err))
					continue
				}
				batchSize, err := data.GetMaxConcurrentRestarts(tx, jobName)
				if err != nil {
					hookErrors = append(hookErrors, fmt.Errorf("post_build: batch size for job %s: %w", jobName, err))
					continue
				}
				baseCtx := hooks.BatchContext{
					Phase:        postBuildEvent,
					RolloutOrder: resolved.FullOrder,
					OrderSource:  resolved.Source,
				}
				err = hooks.RunHookInBatches(
					ctx, tx, rt, jobName, commandName, postBuildEvent,
					resolved.Ordered, batchSize, baseCtx, true, nil, nil,
				)
				if err != nil {
					hookErrors = append(hookErrors, fmt.Errorf("post_build: job %s command %s: %w", jobName, commandName, err))
				}
			}
		}
	}

	if err := errors.Join(hookErrors...); err != nil {
		return err
	}

	if err := kv.PersistToSessionTransaction(tx); err != nil {
		return fmt.Errorf("post_build: persist kv: %w", err)
	}
	return nil
}

func Execute(opts ...Options) error {
	return ExecuteContext(context.Background(), opts...)
}

// ExecuteContext runs a build and returns early when ctx is cancelled between phases.
func ExecuteContext(ctx context.Context, opts ...Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var options Options
	if len(opts) > 0 {
		options = opts[0]
	}

	rt, err := bucket.SetupRuntime("", bucket.NewRunContext("build", 0))
	if err != nil {
		return err
	}
	runStarted := time.Now()
	exitCode := 0
	defer func() {
		_ = rt.LogRunEnd(exitCode, time.Since(runStarted), nil)
		_ = rt.Stop()
	}()
	if err := rt.LogRunBegin(nil); err != nil {
		return err
	}

	if err := rejectBucketSymlinks(); err != nil {
		exitCode = 1
		return err
	}

	db, err := data.OpenDatabase(true)
	if err != nil {
		exitCode = 1
		return err
	}

	defer func() {
		_ = db.Close()
		_ = os.RemoveAll(bucket.TempLocation)
	}()

	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	// Catalog TX: all SQL must use buildTx; session KV stays in memory until
	// PersistToTransaction. Never PersistSession / a second OpenDatabase write
	// here — a failed build before Commit must leave kive.db unchanged.
	buildTx, err := db.Begin()
	if err != nil {
		exitCode = 1
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = buildTx.Rollback()
	}()

	jobWorkspace := workspace.GetWorkspace()

	if err := kv.Initialize(buildTx); err != nil {
		exitCode = 1
		return err
	}

	removedWorkers, err := BuildWorkers(buildTx, jobWorkspace)
	if err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "build_workers", map[string]string{
		"removed_workers": strconv.Itoa(len(removedWorkers)),
	}); err != nil {
		exitCode = 1
		return err
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	var signatureAudits []JobSignatureAudit
	removedJobs, err := BuildJobsWithOptions(buildTx, jobWorkspace, BuildJobsOptions{
		Audits: &signatureAudits,
	})
	if err != nil {
		exitCode = 1
		return err
	}
	signatureJobs := map[string][]string{
		"signed":   {},
		"unsigned": {},
		"invalid":  {},
	}
	for _, audit := range signatureAudits {
		signatureJobs[audit.Status] = append(signatureJobs[audit.Status], audit.Job)
	}
	if err := rt.LogStepComplete(buildPhase, "build_jobs", map[string]string{
		"removed_jobs":        strconv.Itoa(len(removedJobs)),
		"signed_jobs_count":   strconv.Itoa(len(signatureJobs["signed"])),
		"signed_jobs":         strings.Join(signatureJobs["signed"], ","),
		"unsigned_jobs_count": strconv.Itoa(len(signatureJobs["unsigned"])),
		"unsigned_jobs":       strings.Join(signatureJobs["unsigned"], ","),
		"invalid_jobs_count":  strconv.Itoa(len(signatureJobs["invalid"])),
		"invalid_jobs":        strings.Join(signatureJobs["invalid"], ","),
	}); err != nil {
		exitCode = 1
		return err
	}
	for _, audit := range signatureAudits {
		extra := map[string]string{
			"job": audit.Job, "signature": audit.Status,
		}
		if audit.Signer != "" {
			extra["vendor"] = audit.Signer
		}
		if audit.Digest != "" {
			extra["sha256"] = audit.Digest
		}
		if err := rt.LogStepComplete(buildPhase, "job_signature", extra); err != nil {
			exitCode = 1
			return err
		}
		if options.OnJobSignature != nil {
			options.OnJobSignature(audit)
		}
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	if err := data.ReplaceTemplateFiles(buildTx, bucket.TemplatesLocation); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "build_templates", nil); err != nil {
		exitCode = 1
		return err
	}
	if err := data.ReplaceCommandFiles(buildTx, bucket.CommandsLocation); err != nil {
		exitCode = 1
		return err
	}
	if err := data.SetBundleMeta(buildTx, data.CurrentBundleVersion, buildinfo.Hash()); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "build_commands", nil); err != nil {
		exitCode = 1
		return err
	}

	workspaceJobNames, err := jobWorkspace.GetJobs()
	if err != nil {
		exitCode = 1
		return err
	}
	if err := ValidateHookDemands(jobWorkspace, workspaceJobNames); err != nil {
		exitCode = 1
		return err
	}

	if err := BuildAllocations(buildTx, jobWorkspace); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "build_allocations", nil); err != nil {
		exitCode = 1
		return err
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	if err := ValidateRollbackPolicy(buildTx, jobWorkspace); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "validate_rollback", nil); err != nil {
		exitCode = 1
		return err
	}

	if err := ValidateBackwardCompatibility(buildTx, jobWorkspace); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "validate_compatibility", nil); err != nil {
		exitCode = 1
		return err
	}

	if err := ValidateMinAllocationsCount(buildTx, jobWorkspace); err != nil {
		exitCode = 1
		return err
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	if err := BuildDeploymentSequence(buildTx); err != nil {
		exitCode = 1
		return err
	}
	if err := BuildDeploymentOrder(buildTx); err != nil {
		exitCode = 1
		return err
	}

	if err := BuildVariables(buildTx, jobWorkspace, removedWorkers, removedJobs, options.DeleteSecretsKV); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "build_variables", nil); err != nil {
		exitCode = 1
		return err
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	if err := checkCAExpiry(); err != nil {
		exitCode = 1
		return err
	}

	if err := BuildCerts(buildTx); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "build_certs", nil); err != nil {
		exitCode = 1
		return err
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	if err := BuildJobAllocationVariables(buildTx, removedJobs); err != nil {
		exitCode = 1
		return err
	}

	if err := promconfig.BuildCatalog(buildTx, jobWorkspace); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "build_prometheus", nil); err != nil {
		exitCode = 1
		return err
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	if err := os.MkdirAll(bucket.TempLocation, 0o755); err != nil {
		exitCode = 1
		return bucket.UnexpectedError(err)
	}

	kv.GetStore().ExpireStaleEntries(time.Now())

	kvRetainDays, err := bucket.KVRetainDaysFromConfig()
	if err != nil {
		exitCode = 1
		return err
	}
	if err := kv.GetStore().PurgeStaleVersions(buildTx, kvRetainDays); err != nil {
		exitCode = 1
		return err
	}

	if err := ValidateWorkerResources(buildTx); err != nil {
		exitCode = 1
		return err
	}

	if err := kv.PersistToTransaction(buildTx, kv.GetStore()); err != nil {
		exitCode = 1
		return err
	}

	if err := buildTx.Commit(); err != nil {
		exitCode = 1
		return bucket.DatabaseError(err)
	}
	if err := rt.LogStepComplete(buildPhase, "commit_catalog", map[string]string{
		"job_count": strconv.Itoa(len(workspaceJobNames)),
	}); err != nil {
		exitCode = 1
		return err
	}
	if err := ctx.Err(); err != nil {
		exitCode = 1
		return err
	}

	postBuildTx, err := db.Begin()
	if err != nil {
		exitCode = 1
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = postBuildTx.Rollback()
	}()

	if err := runPostBuildHooks(ctx, postBuildTx, rt); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "post_build_hooks", nil); err != nil {
		exitCode = 1
		return err
	}

	if err := postBuildTx.Commit(); err != nil {
		exitCode = 1
		return bucket.DatabaseError(err)
	}

	if err := data.Vacuum(db); err != nil {
		exitCode = 1
		return err
	}
	if err := rt.LogStepComplete(buildPhase, "vacuum", nil); err != nil {
		exitCode = 1
		return err
	}

	return nil
}
