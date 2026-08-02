// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import "kive/schedule"

// DisabledAllocations lists jobs and workers excluded from scheduling.
type DisabledAllocations struct {
	Jobs map[string]struct {
		Allocations []string `json:"allocations"`
	} `json:"jobs"`
	Workers []string `json:"workers"`
}

// Hook is a named hook entry from a job manifest.
type Hook struct {
	Name        string             `json:"name,omitempty"`
	ExecutedOn  []string           `json:"executed_on"`
	Description string             `json:"description,omitempty"`
	Schedule    *schedule.Schedule `json:"schedule,omitempty"`
	Demands     struct {
		Job    string                 `json:"job"`
		Hook   string                 `json:"hook"`
		Config map[string]interface{} `json:"config"`
	} `json:"demands"`
}

// CertSubject describes certificate subject fields in a manifest.
type CertSubject struct {
	CommonName         string `json:"common_name"`
	Organization       string `json:"organization,omitempty"`
	OrganizationalUnit string `json:"organizational_unit,omitempty"`
	Country            string `json:"country,omitempty"`
	Province           string `json:"province,omitempty"`
	Locality           string `json:"locality,omitempty"`
}

// ManifestCert is one named certificate entry under job.conf certs {}.
// When External is true, Kive copies workspace jobs/<job>/certs/<name>.{crt,key}
// into KV (no CA signing). Otherwise Kive generates the leaf from PKCS8/One/Subject.
type ManifestCert struct {
	External bool        `json:"external,omitempty"`
	PKCS8    bool        `json:"pkcs8"`
	One      bool        `json:"one"`
	Subject  CertSubject `json:"subject"`
}

// Manifest is the jobs/<name>/job.conf schema.
type Manifest struct {
	Version            string   `json:"version"`
	Description        string   `json:"description,omitempty"`
	Selectors          []string `json:"selectors"`
	DeploymentPriority int      `json:"deployment_priority,omitempty"`
	Resources          struct {
		Memory struct {
			Min string `json:"min"`
			Max string `json:"max"`
		} `json:"memory"`
		CPU struct {
			Min string `json:"min"`
			Max string `json:"max"`
		} `json:"cpu"`
		CPUShares int           `json:"cpu_shares,omitempty"`
		Ports     ManifestPorts `json:"ports"`
	} `json:"resources"`
	Hooks map[string]Hook         `json:"hooks"`
	Certs map[string]ManifestCert `json:"certs"`
	HealthCheck               *ManifestHealthCheck `json:"health_check,omitempty"`
	MaxConcurrentRestarts     int                  `json:"max_concurrent_restarts"`
	MaxConcurrentStarts       int                  `json:"max_concurrent_starts"`
	MaxConcurrentStops        int                  `json:"max_concurrent_stops"`
	MinAllocationsCount       int                  `json:"min_allocations_count"`
	RestartPolicy             string               `json:"restart_policy,omitempty"`
	RestartGlobs              []string             `json:"restart_globs,omitempty"`
	ReloadGlobs               []string             `json:"reload_globs,omitempty"`
	BackwardCompatibilityFrom []string             `json:"backward_compatibility_from,omitempty"`
	AllowRollback             bool                 `json:"allow_rollback,omitempty"`
}
