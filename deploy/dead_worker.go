// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"log"

	"kive/bucket"
)

func runWorkerCommandOrAssumeDead(
	ctx context.Context,
	rt *bucket.Runtime,
	workerIP string,
	cmdCtx bucket.CommandContext,
	commands []string,
	env []string,
) {
	if err := runWorkerCommand(ctx, rt, workerIP, cmdCtx, commands, env); err != nil {
		log.Printf("deploy: removed worker %s unreachable, assuming dead: %v", workerIP, err)
	}
}

func finishRemovedWorkerCommand(workerIP string, err error, assumeDead bool) error {
	if err == nil {
		return nil
	}
	if assumeDead {
		log.Printf("deploy: removed worker %s unreachable, assuming dead: %v", workerIP, err)
		return nil
	}
	return err
}
