// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"strings"

	"kive/bucket"
)

const (
	HealthKindLiveness  = "liveness"
	HealthKindReadiness = "readiness"
)

// HealthCheckKind is one probe channel (liveness or readiness) with optional wait override.
type HealthCheckKind struct {
	Checks []HealthCheckProbe `json:"checks"`
	Wait   *HealthCheckWait   `json:"wait,omitempty"`
}

// ManifestHealthCheck is the job.conf health_check section.
type ManifestHealthCheck struct {
	Liveness       *HealthCheckKind   `json:"liveness,omitempty"`
	Readiness      *HealthCheckKind   `json:"readiness,omitempty"`
	Checks         []HealthCheckProbe `json:"checks,omitempty"` // legacy flat DB rows → readiness at Normalize
	TimeoutSeconds int                `json:"timeout_seconds"`
	Wait           *HealthCheckWait   `json:"wait,omitempty"` // default for kinds without local wait
}

// HealthCheckProbe is one built-in probe (tcp, http, or ssh).
type HealthCheckProbe struct {
	Type         string `json:"type"`
	Port         string `json:"port,omitempty"`
	Command      string `json:"command,omitempty"`
	Path         string `json:"path,omitempty"`
	ExpectStatus int    `json:"expect_status,omitempty"`
	Scheme       string `json:"scheme,omitempty"`
}

// HealthCheckWait overrides retry behavior for this job's health checks.
type HealthCheckWait struct {
	Attempts        int `json:"attempts"`
	IntervalSeconds int `json:"interval_seconds"`
}

// HasLivenessProbes reports whether built-in liveness probes are configured.
func (hc *ManifestHealthCheck) HasLivenessProbes() bool {
	return hc != nil && hc.Liveness != nil && len(hc.Liveness.Checks) > 0
}

// HasReadinessProbes reports whether built-in readiness probes are configured.
func (hc *ManifestHealthCheck) HasReadinessProbes() bool {
	return hc != nil && hc.Readiness != nil && len(hc.Readiness.Checks) > 0
}

// HasAnyProbes reports whether any built-in probes are configured.
func (hc *ManifestHealthCheck) HasAnyProbes() bool {
	return hc.HasLivenessProbes() || hc.HasReadinessProbes()
}

// LivenessChecks returns liveness probes (nil if none).
func (hc *ManifestHealthCheck) LivenessChecks() []HealthCheckProbe {
	if !hc.HasLivenessProbes() {
		return nil
	}
	return hc.Liveness.Checks
}

// ReadinessChecks returns readiness probes (nil if none).
func (hc *ManifestHealthCheck) ReadinessChecks() []HealthCheckProbe {
	if !hc.HasReadinessProbes() {
		return nil
	}
	return hc.Readiness.Checks
}

// WaitFor returns the wait override for kind (liveness/readiness): kind-local, else outer.
// Returns nil when neither is set (caller uses bucket health_wait_seconds).
func (hc *ManifestHealthCheck) WaitFor(kind string) *HealthCheckWait {
	if hc == nil {
		return nil
	}
	switch kind {
	case HealthKindLiveness:
		if hc.Liveness != nil && hc.Liveness.Wait != nil {
			return hc.Liveness.Wait
		}
	case HealthKindReadiness:
		if hc.Readiness != nil && hc.Readiness.Wait != nil {
			return hc.Readiness.Wait
		}
	}
	return hc.Wait
}

// Normalize folds legacy flat Checks (from older catalog rows) into Readiness
// and clears Checks. No-op when already kind-structured or empty. New job.conf
// must declare probes under liveness { ... } / readiness { ... }.
func (hc *ManifestHealthCheck) Normalize() {
	if hc == nil || len(hc.Checks) == 0 {
		return
	}
	if hc.Readiness == nil {
		hc.Readiness = &HealthCheckKind{}
	}
	hc.Readiness.Checks = append(hc.Readiness.Checks, hc.Checks...)
	hc.Checks = nil
}

func hasManifestHealthChecks(manifest Manifest) bool {
	return manifest.HealthCheck != nil && manifest.HealthCheck.HasAnyProbes()
}

// ValidateHealthCheck ensures manifest probes reference declared ports and known types.
// Jobs may also define liveness/readiness hooks; those run after probes at check time.
func ValidateHealthCheck(jobName string, manifest Manifest) error {
	if !hasManifestHealthChecks(manifest) {
		return nil
	}

	declaredPorts := manifest.Resources.Ports.Names()
	declared := make(map[string]struct{}, len(declaredPorts))
	for _, name := range declaredPorts {
		declared[name] = struct{}{}
	}

	hc := manifest.HealthCheck
	if err := validateProbeList(jobName, "liveness", hc.LivenessChecks(), declared); err != nil {
		return err
	}
	if err := validateProbeList(jobName, "readiness", hc.ReadinessChecks(), declared); err != nil {
		return err
	}
	return nil
}

func validateProbeList(jobName, kind string, probes []HealthCheckProbe, declared map[string]struct{}) error {
	prefix := "health_check"
	if kind != "" {
		prefix = "health_check." + kind
	}
	for idx, probe := range probes {
		probeType := strings.ToLower(strings.TrimSpace(probe.Type))
		switch probeType {
		case "tcp", "http", "ssh":
		default:
			return fmt.Errorf("%w: job %s %s.checks[%d] type %q (want tcp, http, or ssh)",
				bucket.ErrInvalidManifest, jobName, prefix, idx, probe.Type)
		}

		switch probeType {
		case "ssh":
			if strings.TrimSpace(probe.Command) == "" {
				return fmt.Errorf("%w: job %s %s.checks[%d] ssh probe requires command",
					bucket.ErrInvalidManifest, jobName, prefix, idx)
			}
		default:
			portName := strings.TrimSpace(probe.Port)
			if portName == "" {
				return fmt.Errorf("%w: job %s %s.checks[%d] missing port",
					bucket.ErrInvalidManifest, jobName, prefix, idx)
			}
			if _, ok := declared[portName]; !ok {
				return fmt.Errorf("%w: job %s %s.checks[%d] port %q not in resources.ports",
					bucket.ErrInvalidManifest, jobName, prefix, idx, portName)
			}
			if probeType == "http" {
				path := probe.Path
				if path == "" {
					path = "/"
				}
				if !strings.HasPrefix(path, "/") {
					return fmt.Errorf("%w: job %s %s.checks[%d] path must start with /",
						bucket.ErrInvalidManifest, jobName, prefix, idx)
				}
			}
		}
	}
	return nil
}
