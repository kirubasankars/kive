// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"

	"kive/bucket"
	"kive/prereq"
	"kive/worker"
)

func checkDeployPrerequisites(ctx context.Context, rt *bucket.Runtime, workers []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if testHooks != nil {
		if testHooks.CheckWorkerPrerequisites != nil {
			return testHooks.CheckWorkerPrerequisites(rt, workers)
		}
		return nil
	}

	runtimes, err := prereq.WorkspaceHookRuntimesNeeded()
	if err != nil {
		return err
	}
	if err := prereq.CheckLocalDeploy(runtimes.NeedsJS, runtimes.NeedsRuby); err != nil {
		return err
	}

	conf, err := bucket.GetKiveConf()
	if err != nil {
		return err
	}
	return worker.CheckPrerequisitesWithRuntime(ctx, rt, workers, conf.UseSUDO)
}
