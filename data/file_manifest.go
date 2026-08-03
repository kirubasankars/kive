// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import "sort"

// FileManifest maps job-relative paths to content MD5 hex digests.
type FileManifest map[string]string

// DiffFileManifests returns added, modified, and deleted paths between manifests.
func DiffFileManifests(previous, current FileManifest) []string {
	changed := make([]string, 0)
	seen := make(map[string]struct{})

	for path, hash := range current {
		prevHash, ok := previous[path]
		if !ok || prevHash != hash {
			changed = append(changed, path)
			seen[path] = struct{}{}
		}
	}
	for path := range previous {
		if _, ok := current[path]; !ok {
			if _, listed := seen[path]; !listed {
				changed = append(changed, path)
			}
		}
	}
	sort.Strings(changed)
	return changed
}
