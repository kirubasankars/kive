// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"fmt"
	"log"
	"time"

	"kive/bucket"
)

func logHookPreparing(job string, workerCount int, verbose bool) {
	if !verbose {
		return
	}
	log.Printf("hooks: %s preparing (%d workers)...", job, workerCount)
}

func logHookBatch(job, hookName string, batchIndex, batchCount int, verbose bool) {
	if !verbose || batchCount <= 1 {
		return
	}
	log.Printf("hooks: %s batch %d/%d %s...", job, batchIndex+1, batchCount, hookName)
}

func logHookRunning(job, workerIP, hookName string, verbose bool) {
	if !verbose {
		return
	}
	target := bucket.CLIStreamTarget(workerIP, job)
	log.Printf("%s %s running...", target, hookName)
}

func logHookDone(job, workerIP, hookName string, err error, duration time.Duration, verbose bool) {
	target := bucket.CLIStreamTarget(workerIP, job)
	if err != nil {
		log.Printf("%s %s failed", target, hookName)
		return
	}
	if !verbose {
		return
	}
	log.Printf("%s %s ok (%s)", target, hookName, formatHookDuration(duration))
}

func formatHookDuration(duration time.Duration) string {
	seconds := duration.Seconds()
	if seconds < 1 {
		return fmt.Sprintf("%dms", duration.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", seconds)
}
