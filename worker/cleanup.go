// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package worker

import (
	"context"
	"fmt"
	"log"

	"kive/bucket"
)

// JobDeployArtifactsCleanupCommand returns the remote shell command that removes
// deployed job files (including bin/) while preserving data/ and logs/ for later
// redeploy or GC. Rolling-upgrade rsync excludes and protects bin/ separately.
func JobDeployArtifactsCleanupCommand(bucketID, job string) string {
	base := bucket.WorkerJobPath(bucketID, job)
	return fmt.Sprintf(
		`if [ -d %q ]; then find %q -mindepth 1 -maxdepth 1 ! -name data ! -name logs -exec rm -rf {} +; fi`,
		base, base,
	)
}

func cleanupCmdCtx(job, action string, cmd string) bucket.CommandContext {
	return bucket.CommandContext{
		Job:    job,
		Phase:  "gc",
		Action: action,
		Cmd:    cmd,
	}
}

// RemoveJobDeployArtifacts deletes deployed job files (including bin/) on a worker
// but leaves data/ and logs/ in place so the same allocation can reuse them on redeploy.
// Permanent deletion of data/ and logs/ is handled by kive gc.
func RemoveJobDeployArtifacts(rt *bucket.Runtime, bucketID, workerIP, job string) error {
	cmd := JobDeployArtifactsCleanupCommand(bucketID, job)
	if err := ExecuteCommand(context.Background(), rt, workerIP, cleanupCmdCtx(job, "cleanup_deploy", cmd), []string{cmd}, nil); err != nil {
		return remoteError(workerIP, err)
	}
	return nil
}

// RemoveJobRuntimeDirs deletes data/, logs/, and bin/ for a job on a worker.
func RemoveJobRuntimeDirs(rt *bucket.Runtime, bucketID, workerIP, job string) error {
	base := bucket.WorkerJobPath(bucketID, job)
	cmd := fmt.Sprintf("rm -rf %s/data %s/logs %s/bin", base, base, base)
	if err := ExecuteCommand(context.Background(), rt, workerIP, cleanupCmdCtx(job, "cleanup_runtime", cmd), []string{cmd}, nil); err != nil {
		return remoteError(workerIP, err)
	}
	return nil
}

// RemoveJobTree deletes the entire jobs/<job> directory on a worker (kive gc).
func RemoveJobTree(rt *bucket.Runtime, bucketID, workerIP, job string) error {
	return RemoveJobTreeContext(context.Background(), rt, bucketID, workerIP, job)
}

// RemoveJobTreeContext deletes a worker job tree with cancellation.
func RemoveJobTreeContext(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP, job string) error {
	base := bucket.WorkerJobPath(bucketID, job)
	cmd := fmt.Sprintf("rm -rf %q", base)
	if err := ExecuteCommand(ctx, rt, workerIP, cleanupCmdCtx(job, "cleanup_job_tree", cmd), []string{cmd}, nil); err != nil {
		return remoteError(workerIP, err)
	}
	return nil
}

// RemoveJobTreeOrAssumeDead is like RemoveJobTree but ignores SSH errors when the
// worker was removed from the catalog and is assumed dead.
func RemoveJobTreeOrAssumeDead(rt *bucket.Runtime, bucketID, workerIP, job string) {
	_ = RemoveJobTreeOrAssumeDeadContext(context.Background(), rt, bucketID, workerIP, job)
}

// RemoveJobTreeOrAssumeDeadContext ignores non-cancellation SSH failures for removed workers.
func RemoveJobTreeOrAssumeDeadContext(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP, job string) error {
	if err := RemoveJobTreeContext(ctx, rt, bucketID, workerIP, job); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("gc: removed worker %s unreachable, assuming dead: %v", workerIP, err)
	}
	return nil
}
