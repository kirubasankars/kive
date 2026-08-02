// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"kive/data"
	"kive/kv"
)

func allocationVersionEnvForHook(tx *sql.Tx, jobName, workerIP string) ([]string, error) {
	allocID, err := data.GetAllocationID(tx, workerIP, jobName)
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

func allocationIndexForHook(tx *sql.Tx, jobName, workerIP string) (string, error) {
	if store := kv.GetKVStore(); store != nil {
		namespace := fmt.Sprintf("kive/job/%s/worker/%s", jobName, workerIP)
		key := jobName + "_allocation_index"
		entry, err := store.Get(namespace, key)
		if err == nil && entry.Value != "" {
			return entry.Value, nil
		}
		if err != nil && !errors.Is(err, kv.ErrNotFound) {
			return "", err
		}
	}

	workers, err := data.GetNonRemovedAllocationsOrdered(tx, jobName)
	if err != nil {
		return "", err
	}
	for idx, ip := range workers {
		if ip == workerIP {
			return strconv.Itoa(idx), nil
		}
	}
	return "", fmt.Errorf("allocation index not found for job %s on worker %s", jobName, workerIP)
}
