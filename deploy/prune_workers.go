// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"fmt"
	"os"
	"path"

	"kive/bucket"
	"kive/worker"
)

func removeJobDeployArtifactsFromWorker(
	ctx context.Context,
	rt *bucket.Runtime,
	bucketID, workerIP, job string,
	assumeDead bool,
) error {
	cmd := worker.JobDeployArtifactsCleanupCommand(bucketID, job)
	cmdCtx := bucket.CommandContext{
		Job:    job,
		Phase:  "reconcile",
		Action: "cleanup_deploy",
		Cmd:    cmd,
	}
	var err error
	if assumeDead {
		runWorkerCommandOrAssumeDead(ctx, rt, workerIP, cmdCtx, []string{cmd}, nil)
	} else {
		err = runWorkerCommand(ctx, rt, workerIP, cmdCtx, []string{cmd}, nil)
	}
	if err = finishRemovedWorkerCommand(workerIP, err, assumeDead); err != nil {
		return fmt.Errorf("worker %s job %s: %w", workerIP, job, err)
	}
	localDir := path.Join(bucket.GetTempWorkerPath(workerIP), "jobs", job)
	if err := os.RemoveAll(localDir); err != nil && !os.IsNotExist(err) {
		return bucket.UnexpectedError(err)
	}
	return nil
}

func removeWorkerBucketFromWorker(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP string) error {
	remoteDir := bucket.WorkerBucketPath(bucketID)
	cmd := fmt.Sprintf("rm -rf %s", remoteDir)
	cmdCtx := bucket.CommandContext{
		Phase:  "reconcile",
		Action: "cleanup_worker",
		Cmd:    cmd,
	}
	runWorkerCommandOrAssumeDead(
		ctx,
		rt,
		workerIP,
		cmdCtx,
		[]string{cmd},
		nil,
	)
	localDir := bucket.GetTempWorkerPath(workerIP)
	if err := os.RemoveAll(localDir); err != nil && !os.IsNotExist(err) {
		return bucket.UnexpectedError(err)
	}
	return nil
}
