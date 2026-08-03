// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package build

import (
	"fmt"
	"strings"

	"kive/bucket"
	"kive/workspace"
)

type jobCatalog struct {
	manifests map[string]workspace.Manifest
	versions  map[string]workspace.Version
	hooks     map[string]map[string]struct{}
}

func loadJobCatalog(jobWorkspace *workspace.DefaultWorkspace, jobNames []string) (*jobCatalog, error) {
	catalog := &jobCatalog{
		manifests: make(map[string]workspace.Manifest, len(jobNames)),
		versions:  make(map[string]workspace.Version, len(jobNames)),
		hooks:     make(map[string]map[string]struct{}, len(jobNames)),
	}

	for _, jobName := range jobNames {
		manifest, err := jobWorkspace.GetJobManifest(jobName)
		if err != nil {
			return nil, err
		}
		catalog.manifests[jobName] = manifest

		hookSet := make(map[string]struct{})
		for _, hook := range manifest.ListedHooks() {
			hookSet[hook.Name] = struct{}{}
		}
		catalog.hooks[jobName] = hookSet
	}

	for _, jobName := range jobNames {
		version, err := catalog.resolveJobVersion(jobName, catalog.manifests[jobName])
		if err != nil {
			return nil, err
		}
		catalog.versions[jobName] = version
	}

	return catalog, nil
}

func (c *jobCatalog) resolveJobVersion(jobName string, manifest workspace.Manifest) (workspace.Version, error) {
	raw := strings.TrimSpace(manifest.Version)
	needsExplicit := manifest.RequiresExplicitVersion() || c.isDemandTarget(jobName)

	if raw == "" {
		if needsExplicit {
			return workspace.Version{}, fmt.Errorf("%w: job %q must declare version (dependency participant)",
				bucket.ErrInvalidJobVersion, jobName)
		}
		return workspace.Version{}, nil
	}

	version, err := workspace.ParseVersion(raw)
	if err != nil {
		return workspace.Version{}, fmt.Errorf("job %q: %w", jobName, err)
	}
	return version, nil
}

func (c *jobCatalog) isDemandTarget(jobName string) bool {
	for _, manifest := range c.manifests {
		for _, hook := range manifest.ListedHooks() {
			if strings.TrimSpace(hook.Demands.Job) == jobName {
				return true
			}
		}
	}
	return false
}

// ValidateHookDemands checks demand references and version constraints across the workspace.
func ValidateHookDemands(jobWorkspace *workspace.DefaultWorkspace, jobNames []string) error {
	jobNames = sortedJobNames(jobNames)
	catalog, err := loadJobCatalog(jobWorkspace, jobNames)
	if err != nil {
		return err
	}

	for _, jobName := range jobNames {
		manifest := catalog.manifests[jobName]
		for _, hook := range manifest.ListedHooks() {
			if err := workspace.ValidateHookDemand(jobName, hook.Name, hook); err != nil {
				return err
			}

			demandJob := strings.TrimSpace(hook.Demands.Job)
			if demandJob == "" {
				continue
			}

			if _, ok := catalog.manifests[demandJob]; !ok {
				return fmt.Errorf("%w: job %s hook %s demands unknown job %q",
					bucket.ErrInvalidHookDemand, jobName, hook.Name, demandJob)
			}
			demandPriority := catalog.manifests[demandJob].DeploymentPriority
			if manifest.DeploymentPriority > demandPriority {
				return fmt.Errorf(
					"%w: job %s hook %s deployment_priority %d exceeds demanded job %s deployment_priority %d",
					bucket.ErrInvalidHookDemand,
					jobName, hook.Name, manifest.DeploymentPriority,
					demandJob, demandPriority,
				)
			}

			demandHook := strings.TrimSpace(hook.Demands.Hook)
			if demandHook != "" {
				if _, ok := catalog.hooks[demandJob][demandHook]; !ok {
					return fmt.Errorf("%w: job %s hook %s demands job %q hook %q which is not declared",
						bucket.ErrInvalidHookDemand, jobName, hook.Name, demandJob, demandHook)
				}
			}

			constraint, err := workspace.ParseVersionConstraint(hook.Demands.Config)
			if err != nil {
				return fmt.Errorf("job %s hook %s: %w", jobName, hook.Name, err)
			}
			if constraint.Min == nil && constraint.Max == nil {
				continue
			}

			upstreamVersion := catalog.versions[demandJob]
			if strings.TrimSpace(catalog.manifests[demandJob].Version) == "" {
				return fmt.Errorf("%w: job %q must declare version (required by %s hook %s)",
					bucket.ErrInvalidJobVersion, demandJob, jobName, hook.Name)
			}
			if err := constraint.Satisfies(upstreamVersion); err != nil {
				return fmt.Errorf("job %s hook %s depends on %s: %w",
					jobName, hook.Name, demandJob, err)
			}
		}
	}

	return nil
}
