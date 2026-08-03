// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

// MinMemory returns the manifest minimum memory requirement with a default.
func (m Manifest) MinMemory() string {
	if m.Resources.Memory.Min == "" {
		return "0 mb"
	}
	return m.Resources.Memory.Min
}

// MaxMemory returns the manifest maximum memory requirement with a default.
func (m Manifest) MaxMemory() string {
	if m.Resources.Memory.Max == "" {
		return "0 mb"
	}
	return m.Resources.Memory.Max
}

// MinCPU returns the manifest minimum CPU requirement with a default.
func (m Manifest) MinCPU() string {
	if m.Resources.CPU.Min == "" {
		return "0 mhz"
	}
	return m.Resources.CPU.Min
}

// MaxCPU returns the manifest maximum CPU requirement with a default.
func (m Manifest) MaxCPU() string {
	if m.Resources.CPU.Max == "" {
		return "0 mhz"
	}
	return m.Resources.CPU.Max
}

// JobVersion returns the manifest version with a default.
func (m Manifest) JobVersion() string {
	if m.Version == "" {
		return "0.0.0"
	}
	return m.Version
}

// AllowsRollback reports whether version downgrades are permitted at build time.
func (m Manifest) AllowsRollback() bool {
	return m.AllowRollback
}

// GetMaxConcurrentStarts returns max_concurrent_starts (0 = all-at-once on first deploy).
func GetMaxConcurrentStarts(manifest Manifest) int {
	if manifest.MaxConcurrentStarts < 0 {
		return 0
	}
	return manifest.MaxConcurrentStarts
}

// GetMaxConcurrentStops returns max_concurrent_stops (0 = all-at-once on stop reconcile).
func GetMaxConcurrentStops(manifest Manifest) int {
	if manifest.MaxConcurrentStops < 0 {
		return 0
	}
	return manifest.MaxConcurrentStops
}

// GetMaxConcurrentRestarts returns max_concurrent_restarts (minimum 1).
func GetMaxConcurrentRestarts(manifest Manifest) int {
	if manifest.MaxConcurrentRestarts <= 0 {
		return 1
	}
	return manifest.MaxConcurrentRestarts
}
func (m Manifest) MinRequiredAllocations() int {
	if m.MinAllocationsCount < 0 {
		return 0
	}
	return m.MinAllocationsCount
}

// PlacementSelectors returns worker labels required to place this job.
// When manifest selectors are set, only those are used; otherwise the job name is the selector.
func PlacementSelectors(jobName string, manifest Manifest) []string {
	if len(manifest.Selectors) > 0 {
		selectors := make([]string, 0, len(manifest.Selectors))
		seen := make(map[string]struct{}, len(manifest.Selectors))
		for _, selector := range manifest.Selectors {
			if selector == "" {
				continue
			}
			if _, ok := seen[selector]; ok {
				continue
			}
			seen[selector] = struct{}{}
			selectors = append(selectors, selector)
		}
		return selectors
	}
	return []string{jobName}
}

// ListedHooks returns manifest hooks with names filled from map keys.
func (m Manifest) ListedHooks() []Hook {
	hooks := make([]Hook, 0, len(m.Hooks))
	for name, hook := range m.Hooks {
		hook.Name = name
		if hook.Demands.Config == nil {
			hook.Demands.Config = make(map[string]interface{})
		}
		hooks = append(hooks, hook)
	}
	return hooks
}

// GetMinMemory is deprecated; use Manifest.MinMemory.
func GetMinMemory(manifest Manifest) string {
	return manifest.MinMemory()
}

// GetMaxMemory is deprecated; use Manifest.MaxMemory.
func GetMaxMemory(manifest Manifest) string {
	return manifest.MaxMemory()
}

// GetMinCPU is deprecated; use Manifest.MinCPU.
func GetMinCPU(manifest Manifest) string {
	return manifest.MinCPU()
}

// GetMaxCPU is deprecated; use Manifest.MaxCPU.
func GetMaxCPU(manifest Manifest) string {
	return manifest.MaxCPU()
}

// GetVersion is deprecated; use Manifest.JobVersion.
func GetVersion(manifest Manifest) string {
	return manifest.JobVersion()
}

// MinRequiredAllocations returns min_allocations_count (0 when unset).
func GetMinAllocationsCount(manifest Manifest) int {
	return manifest.MinRequiredAllocations()
}

// GetPlacementSelectors returns effective placement selectors for a job.
func GetPlacementSelectors(jobName string, manifest Manifest) []string {
	return PlacementSelectors(jobName, manifest)
}
