// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"database/sql"

	"kive/data"
	"kive/utils/pathmatch"
	"kive/workspace"
)

func effectiveUpdateAction(policy string) string {
	action, _ := resolveUpdateAction(policy, nil, nil, nil, nil, true, false, false)
	return action
}

func resolveUpdateAction(
	policy string,
	restartGlobs, reloadGlobs []string,
	previousFiles, currentFiles data.FileManifest,
	hashChanged, versionOnly, legacyNoManifest bool,
) (string, []string) {
	if !legacyNoManifest {
		changed := data.DiffFileManifests(previousFiles, currentFiles)
		if onlyJobHooksChanged(changed) {
			return rolloutActionSync, nil
		}
	}

	switch policy {
	case workspace.RestartPolicyNever:
		return rolloutActionSync, nil
	case workspace.RestartPolicyAlways:
		if len(reloadGlobs) == 0 {
			return rolloutActionRestart, nil
		}
		if versionOnly && !hashChanged {
			return rolloutActionReload, nil
		}
		if legacyNoManifest && hashChanged {
			return rolloutActionRestart, nil
		}
		changed := data.DiffFileManifests(previousFiles, currentFiles)
		if len(changed) == 0 {
			return rolloutActionReload, nil
		}
		outside := pathmatch.MatchNotAny(reloadGlobs, changed)
		if len(outside) > 0 {
			return rolloutActionRestart, outside
		}
		return rolloutActionReload, pathmatch.MatchAny(reloadGlobs, changed)
	case workspace.RestartPolicyReload:
		if versionOnly && !hashChanged {
			return rolloutActionReload, nil
		}
		if legacyNoManifest && hashChanged {
			return rolloutActionReload, nil
		}
		if len(restartGlobs) == 0 {
			return rolloutActionReload, nil
		}
		changed := data.DiffFileManifests(previousFiles, currentFiles)
		matched := pathmatch.MatchAny(restartGlobs, changed)
		if len(matched) > 0 {
			return rolloutActionRestart, matched
		}
		return rolloutActionReload, nil
	default:
		return rolloutActionRestart, nil
	}
}

func resolveAllocationLifecycle(
	tx *sql.Tx,
	job, workerIP string,
	opts Options,
	policy string,
	restartGlobs, reloadGlobs []string,
	hashChanged, versionOnly bool,
) (string, []string, bool, error) {
	allocID, err := data.GetAllocationID(tx, workerIP, job)
	if err != nil {
		return "", nil, false, err
	}
	manifests, err := data.GetAllocationFileManifests(tx, allocID)
	if err != nil {
		return "", nil, false, err
	}
	changed := data.DiffFileManifests(manifests.Applied, manifests.Pending)
	hooksOnly := onlyJobHooksChanged(changed)
	action, matched := resolveUpdateAction(
		policy, restartGlobs, reloadGlobs,
		manifests.Applied, manifests.Pending,
		hashChanged, versionOnly, !manifests.HasAppliedFiles,
	)
	return action, matched, hooksOnly, nil
}

func onlyJobHooksChanged(changed []string) bool {
	if len(changed) == 0 {
		return false
	}
	matched := pathmatch.MatchAny([]string{workspace.JobHooksGlobPattern}, changed)
	return len(matched) == len(changed)
}
