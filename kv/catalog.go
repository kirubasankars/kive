// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package kv

import "fmt"

// RolloutOrderKey is the KV key for rollout worker order under JobCatalogNamespace.
const RolloutOrderKey = "rollout_order"

// JobCatalogNamespace returns kive/job/<job> (build-synced catalog metadata).
func JobCatalogNamespace(job string) string {
	return fmt.Sprintf("kive/job/%s", job)
}

// JobWorkerNamespace returns kive/job/<job>/worker/<ip> (per-allocation metadata).
func JobWorkerNamespace(job, workerIP string) string {
	return fmt.Sprintf("kive/job/%s/worker/%s", job, workerIP)
}

// WorkerCatalogNamespace returns kive/worker/<ip> (build-synced worker metadata).
func WorkerCatalogNamespace(workerIP string) string {
	return fmt.Sprintf("kive/worker/%s", workerIP)
}
