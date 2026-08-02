// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"
	"errors"
	"fmt"

	"kive/data"
	"kive/kv"
	"kive/workspace"
)

func allocationVersionEnv(tx *sql.Tx, job, workerIP string) ([]string, error) {
	allocID, err := data.GetAllocationID(tx, workerIP, job)
	if err != nil {
		return nil, err
	}
	versions, err := data.GetAllocationVersions(tx, allocID)
	if err != nil {
		return nil, err
	}
	return []string{
		fmt.Sprintf("CURRENT_VERSION=%s", versions.AppliedVersion),
		fmt.Sprintf("NEW_VERSION=%s", versions.Version),
	}, nil
}

// clearWorkerVersionKVKeys removes redundant per-allocation version keys from KV.
// Target version lives at kive/job/<job>/version (build); running vs target lives in the catalog.
func clearWorkerVersionKVKeys(job, workerIP string) error {
	store := kv.GetKVStore()
	if store == nil {
		return nil
	}
	namespace := fmt.Sprintf("kive/job/%s/worker/%s", job, workerIP)
	for _, key := range []string{"version", "current_version", "new_version"} {
		if err := store.Delete(namespace, key); err != nil {
			if errors.Is(err, kv.ErrNotFound) || errors.Is(err, kv.ErrNamespaceNotFound) {
				continue
			}
			return err
		}
	}
	return nil
}

func allocationVersionsForWorker(tx *sql.Tx, job, workerIP string) (data.AllocationVersions, error) {
	allocID, err := data.GetAllocationID(tx, workerIP, job)
	if err != nil {
		return data.AllocationVersions{}, err
	}
	return data.GetAllocationVersions(tx, allocID)
}

func versionMajor(version string) int {
	v, err := workspace.ParseVersion(version)
	if err != nil {
		return 0
	}
	return v.Major
}
