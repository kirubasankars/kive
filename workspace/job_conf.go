// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"kive/bucket"
	"kive/conf"
	"kive/schedule"
)

const (
	JobConfName   = "job.conf"
	JobConfBTName = JobConfName + JobTemplateExt

	// Legacy on-disk names (rejected; no dual-read).
	LegacyManifestConfName   = "manifest.conf"
	LegacyManifestConfBTName = LegacyManifestConfName + JobTemplateExt
	ManifestJSONName         = "manifest.json"
)

var manifestKnownFields = []string{
	"version", "description", "selectors", "deployment_priority",
	"resources", "hooks", "certs", "health_check",
	"max_concurrent_restarts", "max_concurrent_starts", "max_concurrent_stops",
	"min_allocations_count", "restart_policy", "restart_globs", "reload_globs",
	"backward_compatibility_from", "allow_rollback",
	"memory", "cpu", "cpu_shares", "ports",
}

var manifestBlockFields = []string{"resources", "health_check", "certs", "hook"}

// ParseJobConf lowers a parsed conf file into a Manifest.
// Accepts bare top-level statements only (no job { ... } wrapper).
func ParseJobConf(filePath string, data []byte) (Manifest, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return Manifest{}, err
	}
	return lowerManifestFile(f)
}

func lowerManifestFile(f *conf.File) (Manifest, error) {
	if jobs := f.Blocks("job"); len(jobs) > 0 {
		return Manifest{}, conf.Err(f.Path, jobs[0].NamePos.Line, jobs[0].NamePos.Column,
			"job.conf does not use a job { ... } wrapper").
			WithHint("write bare job settings at the top level")
	}

	var m Manifest
	for _, stmt := range f.Stmts {
		switch s := stmt.(type) {
		case *conf.Call:
			if s.Name == "resources" || s.Name == "health_check" || s.Name == "certs" {
				return Manifest{}, conf.Err(f.Path, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("use %s { ... };", s.Name)).
					WithHint(fmt.Sprintf("use %s { ... };", s.Name))
			}
			if err := applyManifestCall(&m, f.Path, s); err != nil {
				return Manifest{}, err
			}
		case *conf.Block:
			switch s.Name {
			case "hook":
				if err := applyManifestHook(&m, f.Path, s); err != nil {
					return Manifest{}, err
				}
			case "resources":
				if err := applyManifestResourcesBlock(&m, f.Path, s); err != nil {
					return Manifest{}, err
				}
			case "health_check":
				if err := applyManifestHealthCheckBlock(&m, f.Path, s); err != nil {
					return Manifest{}, err
				}
			case "certs":
				if err := applyManifestCertsBlock(&m, f.Path, s); err != nil {
					return Manifest{}, err
				}
			default:
				return Manifest{}, conf.Err(f.Path, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("unexpected block %q in manifest", s.Name)).
					WithHint("known blocks: " + joinKnown(manifestBlockFields))
			}
		}
	}
	return m, nil
}

func joinKnown(fields []string) string {
	return strings.Join(fields, ", ")
}

func applyManifestCall(m *Manifest, path string, c *conf.Call) error {
	switch c.Name {
	case "version":
		s, err := conf.SingleStringArg(c, path)
		if err != nil {
			return err
		}
		m.Version = s
	case "description":
		s, err := conf.SingleStringArg(c, path)
		if err != nil {
			return err
		}
		m.Description = s
	case "selectors":
		sels, err := conf.AsStrings(c, path)
		if err != nil {
			return err
		}
		m.Selectors = sels
	case "deployment_priority":
		n, err := conf.SingleIntArg(c, path)
		if err != nil {
			return err
		}
		m.DeploymentPriority = n
	case "max_concurrent_restarts":
		n, err := conf.SingleIntArg(c, path)
		if err != nil {
			return err
		}
		m.MaxConcurrentRestarts = n
	case "max_concurrent_starts":
		n, err := conf.SingleIntArg(c, path)
		if err != nil {
			return err
		}
		m.MaxConcurrentStarts = n
	case "max_concurrent_stops":
		n, err := conf.SingleIntArg(c, path)
		if err != nil {
			return err
		}
		m.MaxConcurrentStops = n
	case "min_allocations_count":
		n, err := conf.SingleIntArg(c, path)
		if err != nil {
			return err
		}
		m.MinAllocationsCount = n
	case "restart_policy":
		s, err := conf.SingleStringArg(c, path)
		if err != nil {
			return err
		}
		m.RestartPolicy = s
	case "restart_globs":
		globs, err := conf.AsStrings(c, path)
		if err != nil {
			return err
		}
		m.RestartGlobs = globs
	case "reload_globs":
		globs, err := conf.AsStrings(c, path)
		if err != nil {
			return err
		}
		m.ReloadGlobs = globs
	case "backward_compatibility_from":
		specs, err := conf.AsStrings(c, path)
		if err != nil {
			return err
		}
		m.BackwardCompatibilityFrom = specs
	case "allow_rollback":
		v, err := conf.SingleBoolArg(c, path)
		if err != nil {
			return err
		}
		m.AllowRollback = v
	default:
		return conf.UnknownSetting(path, c.NamePos, c.Name, manifestKnownFields)
	}
	return nil
}

func applyManifestResourcesBlock(m *Manifest, path string, b *conf.Block) error {
	if b.ID != "" {
		return conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			"resources block must not have an identifier")
	}
	known := []string{"memory", "cpu", "cpu_shares", "ports"}
	for _, stmt := range b.Body {
		switch s := stmt.(type) {
		case *conf.Call:
			if s.Name != "cpu_shares" {
				return conf.UnknownSetting(path, s.NamePos, s.Name, known)
			}
			n, err := conf.SingleIntArg(s, path)
			if err != nil {
				return err
			}
			m.Resources.CPUShares = n
		case *conf.Block:
			if s.ID != "" {
				return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("unexpected identifier %q in resources block", s.ID))
			}
			switch s.Name {
			case "memory":
				min, max, err := minMaxFromBlock(s, path)
				if err != nil {
					return err
				}
				m.Resources.Memory.Min = min
				m.Resources.Memory.Max = max
			case "cpu":
				min, max, err := minMaxFromBlock(s, path)
				if err != nil {
					return err
				}
				m.Resources.CPU.Min = min
				m.Resources.CPU.Max = max
			case "ports":
				if err := applyManifestPortsBlock(m, path, s); err != nil {
					return err
				}
			default:
				return conf.UnknownSetting(path, s.NamePos, s.Name, known)
			}
		default:
			return conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"resources body only allows calls and blocks")
		}
	}
	return nil
}

func minMaxFromBlock(b *conf.Block, path string) (min, max string, err error) {
	known := []string{"min", "max"}
	for _, stmt := range b.Body {
		c, ok := stmt.(*conf.Call)
		if !ok {
			return "", "", conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				fmt.Sprintf("%s body only allows min(...) and max(...)", b.Name))
		}
		s, e := conf.SingleStringArg(c, path)
		if e != nil {
			return "", "", e
		}
		switch c.Name {
		case "min":
			min = s
		case "max":
			max = s
		default:
			return "", "", conf.UnknownSetting(path, c.NamePos, c.Name, known)
		}
	}
	return min, max, nil
}

func applyManifestPortsBlock(m *Manifest, path string, b *conf.Block) error {
	if b.ID != "" {
		return conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			"ports block must not have an identifier")
	}
	if m.Resources.Ports == nil {
		m.Resources.Ports = ManifestPorts{}
	}
	for _, stmt := range b.Body {
		pb, ok := stmt.(*conf.Block)
		if !ok {
			return conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"ports body only allows port blocks").
				WithHint(`e.g. api_http_port { protocol("http"); };`)
		}
		if pb.ID != "" {
			return conf.Err(path, pb.NamePos.Line, pb.NamePos.Column,
				"port entries must not have an identifier")
		}
		if err := applyManifestPortBlock(m, path, pb); err != nil {
			return err
		}
	}
	return nil
}

func applyManifestPortBlock(m *Manifest, path string, b *conf.Block) error {
	name := b.Name
	if err := ValidatePortKey(name); err != nil {
		return conf.Err(path, b.NamePos.Line, b.NamePos.Column, err.Error()).WithCause(err)
	}
	binding := ManifestPortBinding{
		Protocol: DefaultPortProtocol,
		Exposure: DefaultPortExposure,
	}
	for _, stmt := range b.Body {
		c, ok := stmt.(*conf.Call)
		if !ok {
			return conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				fmt.Sprintf("port %q body only allows protocol(...), exposure(...), and port(...)", name))
		}
		switch c.Name {
		case "protocol":
			s, err := conf.SingleStringArg(c, path)
			if err != nil {
				return err
			}
			proto, err := NormalizePortProtocol(s)
			if err != nil {
				return conf.Err(path, c.NamePos.Line, c.NamePos.Column, err.Error())
			}
			binding.Protocol = proto
		case "exposure":
			s, err := conf.SingleStringArg(c, path)
			if err != nil {
				return err
			}
			exp, err := NormalizePortExposure(s)
			if err != nil {
				return conf.Err(path, c.NamePos.Line, c.NamePos.Column, err.Error())
			}
			binding.Exposure = exp
		case "port":
			n, err := conf.SingleIntArg(c, path)
			if err != nil {
				return err
			}
			binding.Fixed = &n
		default:
			return conf.UnknownSetting(path, c.NamePos, c.Name, []string{"protocol", "exposure", "port"})
		}
	}
	m.Resources.Ports[name] = binding
	return nil
}

func applyManifestHealthCheckBlock(m *Manifest, path string, b *conf.Block) error {
	if b.ID != "" {
		return conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			"health_check block must not have an identifier")
	}
	hc := &ManifestHealthCheck{}
	known := []string{"timeout_seconds", "wait", "liveness", "readiness"}
	for _, stmt := range b.Body {
		switch s := stmt.(type) {
		case *conf.Call:
			if s.Name != "timeout_seconds" {
				return conf.UnknownSetting(path, s.NamePos, s.Name, known)
			}
			n, err := conf.SingleIntArg(s, path)
			if err != nil {
				return err
			}
			hc.TimeoutSeconds = n
		case *conf.Block:
			if s.ID != "" {
				return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("unexpected identifier %q in health_check block", s.ID))
			}
			switch s.Name {
			case "wait":
				w, err := parseHealthCheckWaitBlock(s, path)
				if err != nil {
					return err
				}
				hc.Wait = w
			case "liveness", "readiness":
				kind, err := parseHealthCheckKindBlock(s, path)
				if err != nil {
					return err
				}
				if s.Name == "liveness" {
					if hc.Liveness != nil {
						return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
							"health_check.liveness specified more than once")
					}
					hc.Liveness = kind
				} else {
					if hc.Readiness != nil {
						return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
							"health_check.readiness specified more than once")
					}
					hc.Readiness = kind
				}
			case "tcp", "http", "ssh":
				return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
					"health_check requires probes under liveness { ... } or readiness { ... }").
					WithHint("wrap tcp/http/ssh in readiness { ... } or liveness { ... }")
			default:
				return conf.UnknownSetting(path, s.NamePos, s.Name, known)
			}
		default:
			return conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"health_check body only allows calls and blocks")
		}
	}
	m.HealthCheck = hc
	return nil
}

func parseHealthCheckWaitBlock(b *conf.Block, path string) (*HealthCheckWait, error) {
	if b.ID != "" {
		return nil, conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			"wait block must not have an identifier")
	}
	w := &HealthCheckWait{}
	known := []string{"attempts", "interval_seconds"}
	for _, stmt := range b.Body {
		c, ok := stmt.(*conf.Call)
		if !ok {
			return nil, conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"wait body only allows attempts(...) and interval_seconds(...)")
		}
		switch c.Name {
		case "attempts":
			n, err := conf.SingleIntArg(c, path)
			if err != nil {
				return nil, err
			}
			w.Attempts = n
		case "interval_seconds":
			n, err := conf.SingleIntArg(c, path)
			if err != nil {
				return nil, err
			}
			w.IntervalSeconds = n
		default:
			return nil, conf.UnknownSetting(path, c.NamePos, c.Name, known)
		}
	}
	return w, nil
}

func parseHealthCheckKindBlock(b *conf.Block, path string) (*HealthCheckKind, error) {
	if b.ID != "" {
		return nil, conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			fmt.Sprintf("%s block must not have an identifier", b.Name))
	}
	kind := &HealthCheckKind{}
	known := []string{"wait", "tcp", "http", "ssh"}
	for _, stmt := range b.Body {
		switch s := stmt.(type) {
		case *conf.Block:
			if s.ID != "" {
				return nil, conf.Err(path, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("unexpected identifier %q in %s block", s.ID, b.Name))
			}
			switch s.Name {
			case "wait":
				w, err := parseHealthCheckWaitBlock(s, path)
				if err != nil {
					return nil, err
				}
				kind.Wait = w
			case "tcp", "http", "ssh":
				probe, err := parseHealthCheckProbeBlock(s, path)
				if err != nil {
					return nil, err
				}
				kind.Checks = append(kind.Checks, probe)
			default:
				return nil, conf.UnknownSetting(path, s.NamePos, s.Name, known)
			}
		default:
			return nil, conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				fmt.Sprintf("%s body only allows wait { ... } and probe blocks", b.Name))
		}
	}
	return kind, nil
}

func parseHealthCheckProbeBlock(b *conf.Block, path string) (HealthCheckProbe, error) {
	if b.ID != "" {
		return HealthCheckProbe{}, conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			"probe blocks must not have an identifier")
	}
	probe := HealthCheckProbe{Type: b.Name}
	known := []string{"port", "path", "command", "expect_status", "scheme"}
	for _, stmt := range b.Body {
		c, ok := stmt.(*conf.Call)
		if !ok {
			return HealthCheckProbe{}, conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				fmt.Sprintf("%s probe body only allows calls", b.Name))
		}
		switch c.Name {
		case "port":
			s, err := conf.SingleStringArg(c, path)
			if err != nil {
				return HealthCheckProbe{}, err
			}
			probe.Port = s
		case "path":
			s, err := conf.SingleStringArg(c, path)
			if err != nil {
				return HealthCheckProbe{}, err
			}
			probe.Path = s
		case "command":
			s, err := conf.SingleStringArg(c, path)
			if err != nil {
				return HealthCheckProbe{}, err
			}
			probe.Command = s
		case "expect_status":
			n, err := conf.SingleIntArg(c, path)
			if err != nil {
				return HealthCheckProbe{}, err
			}
			probe.ExpectStatus = n
		case "scheme":
			s, err := conf.SingleStringArg(c, path)
			if err != nil {
				return HealthCheckProbe{}, err
			}
			probe.Scheme = s
		default:
			return HealthCheckProbe{}, conf.UnknownSetting(path, c.NamePos, c.Name, known)
		}
	}
	return probe, nil
}

func applyManifestCertsBlock(m *Manifest, path string, b *conf.Block) error {
	if b.ID != "" {
		return conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			"certs block must not have an identifier")
	}
	if m.Certs == nil {
		m.Certs = map[string]ManifestCert{}
	}
	for _, stmt := range b.Body {
		cb, ok := stmt.(*conf.Block)
		if !ok {
			return conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"certs body only allows cert blocks").
				WithHint(`e.g. tls { pkcs8(false); one(false); };`)
		}
		if cb.ID != "" {
			return conf.Err(path, cb.NamePos.Line, cb.NamePos.Column,
				"cert entries must not have an identifier")
		}
		entry := m.Certs[cb.Name]
		var sawPKCS8, sawOne, sawSubject bool
		for _, inner := range cb.Body {
			switch s := inner.(type) {
			case *conf.Call:
				switch s.Name {
				case "external":
					v, err := conf.SingleBoolArg(s, path)
					if err != nil {
						return err
					}
					entry.External = v
				case "pkcs8":
					v, err := conf.SingleBoolArg(s, path)
					if err != nil {
						return err
					}
					entry.PKCS8 = v
					sawPKCS8 = true
				case "one":
					v, err := conf.SingleBoolArg(s, path)
					if err != nil {
						return err
					}
					entry.One = v
					sawOne = true
				default:
					return conf.UnknownSetting(path, s.NamePos, s.Name, []string{"external", "pkcs8", "one", "subject"})
				}
			case *conf.Block:
				if s.Name != "subject" {
					return conf.UnknownSetting(path, s.NamePos, s.Name, []string{"external", "pkcs8", "one", "subject"})
				}
				subject, err := parseCertSubjectBlock(s, path)
				if err != nil {
					return err
				}
				entry.Subject = subject
				sawSubject = true
			default:
				return conf.Err(path, inner.Pos().Line, inner.Pos().Column,
					"cert body only allows calls and subject { ... }")
			}
		}
		if entry.External && (sawPKCS8 || sawOne || sawSubject) {
			return conf.Err(path, cb.NamePos.Line, cb.NamePos.Column,
				fmt.Sprintf("cert %q: external(true) cannot be combined with pkcs8, one, or subject", cb.Name)).
				WithHint(`use external(true); alone, or omit external for generated certs`)
		}
		m.Certs[cb.Name] = entry
	}
	return nil
}

func parseCertSubjectBlock(b *conf.Block, path string) (CertSubject, error) {
	if b.ID != "" {
		return CertSubject{}, conf.Err(path, b.NamePos.Line, b.NamePos.Column,
			"subject block must not have an identifier")
	}
	var subject CertSubject
	known := []string{
		"common_name", "organization", "organizational_unit", "country", "province", "locality",
	}
	for _, stmt := range b.Body {
		c, ok := stmt.(*conf.Call)
		if !ok {
			return CertSubject{}, conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"subject body only allows field calls")
		}
		var target *string
		switch c.Name {
		case "common_name":
			target = &subject.CommonName
		case "organization":
			target = &subject.Organization
		case "organizational_unit":
			target = &subject.OrganizationalUnit
		case "country":
			target = &subject.Country
		case "province":
			target = &subject.Province
		case "locality":
			target = &subject.Locality
		default:
			return CertSubject{}, conf.UnknownSetting(path, c.NamePos, c.Name, known)
		}
		s, err := conf.SingleStringArg(c, path)
		if err != nil {
			return CertSubject{}, err
		}
		*target = s
	}
	return subject, nil
}

func applyManifestHook(m *Manifest, path string, b *conf.Block) error {
	if b.ID == "" {
		return conf.Err(path, b.NamePos.Line, b.NamePos.Column, "hook block requires a name").
			WithHint("write hook hook_ready { ... };")
	}
	h := Hook{Name: b.ID}
	for _, stmt := range b.Body {
		switch s := stmt.(type) {
		case *conf.Call:
			switch s.Name {
			case "executed_on":
				ons, err := conf.AsStrings(s, path)
				if err != nil {
					return err
				}
				h.ExecutedOn = ons
			case "description":
				str, err := conf.SingleStringArg(s, path)
				if err != nil {
					return err
				}
				h.Description = str
			default:
				return conf.UnknownSetting(path, s.NamePos, s.Name, []string{"executed_on", "description", "demands", "schedule"})
			}
		case *conf.Block:
			switch s.Name {
			case "schedule":
				sched, err := schedule.ParseBlock(path, s)
				if err != nil {
					return err
				}
				h.Schedule = &sched
			case "demands":
				if s.ID != "" {
					return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
						"demands block must not have an identifier")
				}
				if err := applyManifestDemandsBlock(&h, path, s); err != nil {
					return err
				}
			default:
				return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("unexpected block %q in hook", s.Name)).
					WithHint(`use schedule { ... }; or demands { ... };`)
			}
		default:
			return conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"hook body only allows calls, schedule { ... }, and demands { ... }")
		}
	}
	if m.Hooks == nil {
		m.Hooks = map[string]Hook{}
	}
	m.Hooks[b.ID] = h
	return nil
}

func applyManifestDemandsBlock(h *Hook, path string, b *conf.Block) error {
	d := &h.Demands
	known := []string{"job", "hook", "config"}
	for _, stmt := range b.Body {
		switch s := stmt.(type) {
		case *conf.Call:
			switch s.Name {
			case "job":
				str, err := conf.SingleStringArg(s, path)
				if err != nil {
					return err
				}
				d.Job = str
			case "hook":
				str, err := conf.SingleStringArg(s, path)
				if err != nil {
					return err
				}
				d.Hook = str
			default:
				return conf.UnknownSetting(path, s.NamePos, s.Name, known)
			}
		case *conf.Block:
			if s.Name != "config" {
				return conf.UnknownSetting(path, s.NamePos, s.Name, known)
			}
			if s.ID != "" {
				return conf.Err(path, s.NamePos.Line, s.NamePos.Column,
					"config block must not have an identifier")
			}
			if d.Config == nil {
				d.Config = map[string]interface{}{}
			}
			for _, inner := range s.Body {
				c, ok := inner.(*conf.Call)
				if !ok {
					return conf.Err(path, inner.Pos().Line, inner.Pos().Column,
						"config body only allows key(value) calls")
				}
				if len(c.Args) != 1 {
					return conf.Err(path, c.NamePos.Line, c.NamePos.Column,
						fmt.Sprintf("config.%s expects one value", c.Name))
				}
				switch v := c.Args[0].Value.(type) {
				case *conf.StringLit:
					d.Config[c.Name] = v.Value
				case *conf.NumberLit:
					if v.IsFloat {
						d.Config[c.Name] = v.Float
					} else {
						d.Config[c.Name] = v.Int
					}
				case *conf.BoolLit:
					d.Config[c.Name] = v.Value
				default:
					str, err := conf.AsString(c.Args[0].Value, path)
					if err != nil {
						return err
					}
					d.Config[c.Name] = str
				}
			}
		default:
			return conf.Err(path, stmt.Pos().Line, stmt.Pos().Column,
				"demands body only allows job(...), hook(...), and config { ... }")
		}
	}
	return nil
}

// EmitJobConf serializes a Manifest to block conf text.
func EmitJobConf(m Manifest) string {
	f := &conf.File{}
	if m.Version != "" {
		f.Stmts = append(f.Stmts, conf.CallStmt("version", m.Version))
	}
	if m.Description != "" {
		f.Stmts = append(f.Stmts, conf.CallStmt("description", m.Description))
	}
	if len(m.Selectors) > 0 {
		f.Stmts = append(f.Stmts, conf.CallStmt("selectors", m.Selectors...))
	}
	if m.DeploymentPriority != 0 {
		f.Stmts = append(f.Stmts, conf.CallStmtInt("deployment_priority", m.DeploymentPriority))
	}
	if m.MaxConcurrentRestarts != 0 {
		f.Stmts = append(f.Stmts, conf.CallStmtInt("max_concurrent_restarts", m.MaxConcurrentRestarts))
	}
	if m.MaxConcurrentStarts != 0 {
		f.Stmts = append(f.Stmts, conf.CallStmtInt("max_concurrent_starts", m.MaxConcurrentStarts))
	}
	if m.MaxConcurrentStops != 0 {
		f.Stmts = append(f.Stmts, conf.CallStmtInt("max_concurrent_stops", m.MaxConcurrentStops))
	}
	if m.MinAllocationsCount != 0 {
		f.Stmts = append(f.Stmts, conf.CallStmtInt("min_allocations_count", m.MinAllocationsCount))
	}
	if m.RestartPolicy != "" {
		f.Stmts = append(f.Stmts, conf.CallStmt("restart_policy", m.RestartPolicy))
	}
	if len(m.RestartGlobs) > 0 {
		f.Stmts = append(f.Stmts, conf.CallStmt("restart_globs", m.RestartGlobs...))
	}
	if len(m.ReloadGlobs) > 0 {
		f.Stmts = append(f.Stmts, conf.CallStmt("reload_globs", m.ReloadGlobs...))
	}
	if len(m.BackwardCompatibilityFrom) > 0 {
		f.Stmts = append(f.Stmts, conf.CallStmt("backward_compatibility_from", m.BackwardCompatibilityFrom...))
	}
	if m.AllowRollback {
		f.Stmts = append(f.Stmts, conf.CallStmtBool("allow_rollback", true))
	}
	if res := emitResourcesBlock(m); res != nil {
		f.Stmts = append(f.Stmts, res)
	}
	if m.HealthCheck != nil {
		f.Stmts = append(f.Stmts, emitHealthCheckBlock(m.HealthCheck))
	}
	if certs := emitCertsBlock(m); certs != nil {
		f.Stmts = append(f.Stmts, certs)
	}
	hookNames := make([]string, 0, len(m.Hooks))
	for name := range m.Hooks {
		hookNames = append(hookNames, name)
	}
	sort.Strings(hookNames)
	for _, name := range hookNames {
		h := m.Hooks[name]
		hb := &conf.Block{Name: "hook", ID: name}
		if len(h.ExecutedOn) > 0 {
			hb.Body = append(hb.Body, conf.CallStmt("executed_on", h.ExecutedOn...))
		}
		if h.Description != "" {
			hb.Body = append(hb.Body, conf.CallStmt("description", h.Description))
		}
		if h.Schedule != nil {
			hb.Body = append(hb.Body, schedule.EmitBlock(h.Schedule))
		}
		if h.Demands.Job != "" || h.Demands.Hook != "" || len(h.Demands.Config) > 0 {
			hb.Body = append(hb.Body, emitDemandsBlock(h))
		}
		f.Stmts = append(f.Stmts, hb)
	}
	return conf.Emit(f, conf.EmitOptions{})
}

func emitMinMaxBlock(name, min, max string) *conf.Block {
	b := &conf.Block{Name: name}
	if min != "" {
		b.Body = append(b.Body, conf.CallStmt("min", min))
	}
	if max != "" {
		b.Body = append(b.Body, conf.CallStmt("max", max))
	}
	return b
}

func emitResourcesBlock(m Manifest) *conf.Block {
	rb := &conf.Block{Name: "resources"}
	if m.Resources.Memory.Min != "" || m.Resources.Memory.Max != "" {
		rb.Body = append(rb.Body, emitMinMaxBlock("memory", m.Resources.Memory.Min, m.Resources.Memory.Max))
	}
	if m.Resources.CPU.Min != "" || m.Resources.CPU.Max != "" {
		rb.Body = append(rb.Body, emitMinMaxBlock("cpu", m.Resources.CPU.Min, m.Resources.CPU.Max))
	}
	if m.Resources.CPUShares != 0 {
		rb.Body = append(rb.Body, conf.CallStmtInt("cpu_shares", m.Resources.CPUShares))
	}
	if len(m.Resources.Ports) > 0 {
		pb := &conf.Block{Name: "ports"}
		for _, name := range m.Resources.Ports.Names() {
			pb.Body = append(pb.Body, emitPortBlock(name, m.Resources.Ports[name]))
		}
		rb.Body = append(rb.Body, pb)
	}
	if len(rb.Body) == 0 {
		return nil
	}
	return rb
}

func emitPortBlock(name string, binding ManifestPortBinding) *conf.Block {
	pb := &conf.Block{Name: name}
	if binding.Fixed != nil && binding.Protocol == DefaultPortProtocol && binding.Exposure == DefaultPortExposure {
		pb.Body = append(pb.Body, conf.CallStmtInt("port", *binding.Fixed))
	} else {
		if binding.Protocol != "" && binding.Protocol != DefaultPortProtocol {
			pb.Body = append(pb.Body, conf.CallStmt("protocol", binding.Protocol))
		}
		if binding.Exposure != "" && binding.Exposure != DefaultPortExposure {
			pb.Body = append(pb.Body, conf.CallStmt("exposure", binding.Exposure))
		}
		if binding.Fixed != nil {
			pb.Body = append(pb.Body, conf.CallStmtInt("port", *binding.Fixed))
		}
	}
	return pb
}

func emitHealthCheckBlock(hc *ManifestHealthCheck) *conf.Block {
	b := &conf.Block{Name: "health_check"}
	if hc.TimeoutSeconds != 0 {
		b.Body = append(b.Body, conf.CallStmtInt("timeout_seconds", hc.TimeoutSeconds))
	}
	if hc.Wait != nil {
		b.Body = append(b.Body, emitHealthCheckWaitBlock(hc.Wait))
	}
	if hc.Liveness != nil {
		b.Body = append(b.Body, emitHealthCheckKindBlock("liveness", hc.Liveness))
	}
	if hc.Readiness != nil {
		b.Body = append(b.Body, emitHealthCheckKindBlock("readiness", hc.Readiness))
	}
	return b
}

func emitHealthCheckWaitBlock(w *HealthCheckWait) *conf.Block {
	out := &conf.Block{Name: "wait"}
	if w.Attempts != 0 {
		out.Body = append(out.Body, conf.CallStmtInt("attempts", w.Attempts))
	}
	if w.IntervalSeconds != 0 {
		out.Body = append(out.Body, conf.CallStmtInt("interval_seconds", w.IntervalSeconds))
	}
	return out
}

func emitHealthCheckKindBlock(name string, kind *HealthCheckKind) *conf.Block {
	out := &conf.Block{Name: name}
	if kind.Wait != nil {
		out.Body = append(out.Body, emitHealthCheckWaitBlock(kind.Wait))
	}
	for _, probe := range kind.Checks {
		out.Body = append(out.Body, emitHealthCheckProbeBlock(probe))
	}
	return out
}

func emitHealthCheckProbeBlock(probe HealthCheckProbe) *conf.Block {
	pb := &conf.Block{Name: probe.Type}
	if probe.Port != "" {
		pb.Body = append(pb.Body, conf.CallStmt("port", probe.Port))
	}
	if probe.Path != "" {
		pb.Body = append(pb.Body, conf.CallStmt("path", probe.Path))
	}
	if probe.Command != "" {
		pb.Body = append(pb.Body, conf.CallStmt("command", probe.Command))
	}
	if probe.ExpectStatus != 0 {
		pb.Body = append(pb.Body, conf.CallStmtInt("expect_status", probe.ExpectStatus))
	}
	if probe.Scheme != "" {
		pb.Body = append(pb.Body, conf.CallStmt("scheme", probe.Scheme))
	}
	return pb
}

func emitCertSubjectBlock(subject CertSubject) *conf.Block {
	type field struct {
		name  string
		value string
	}
	fields := []field{
		{"common_name", subject.CommonName},
		{"organization", subject.Organization},
		{"organizational_unit", subject.OrganizationalUnit},
		{"country", subject.Country},
		{"province", subject.Province},
		{"locality", subject.Locality},
	}
	sb := &conf.Block{Name: "subject"}
	for _, f := range fields {
		if f.value != "" {
			sb.Body = append(sb.Body, conf.CallStmt(f.name, f.value))
		}
	}
	if len(sb.Body) == 0 {
		return nil
	}
	return sb
}

func emitCertsBlock(m Manifest) *conf.Block {
	if len(m.Certs) == 0 {
		return nil
	}
	names := make([]string, 0, len(m.Certs))
	for name := range m.Certs {
		names = append(names, name)
	}
	sort.Strings(names)
	cb := &conf.Block{Name: "certs"}
	for _, name := range names {
		entry := m.Certs[name]
		eb := &conf.Block{Name: name}
		if entry.External {
			eb.Body = append(eb.Body, conf.CallStmtBool("external", true))
		} else {
			eb.Body = append(eb.Body, conf.CallStmtBool("pkcs8", entry.PKCS8))
			eb.Body = append(eb.Body, conf.CallStmtBool("one", entry.One))
			if subject := emitCertSubjectBlock(entry.Subject); subject != nil {
				eb.Body = append(eb.Body, subject)
			}
		}
		cb.Body = append(cb.Body, eb)
	}
	return cb
}

func emitDemandsBlock(h Hook) *conf.Block {
	d := h.Demands
	db := &conf.Block{Name: "demands"}
	if d.Job != "" {
		db.Body = append(db.Body, conf.CallStmt("job", d.Job))
	}
	if d.Hook != "" {
		db.Body = append(db.Body, conf.CallStmt("hook", d.Hook))
	}
	if len(d.Config) > 0 {
		cfg := &conf.Block{Name: "config"}
		cfgKeys := make([]string, 0, len(d.Config))
		for k := range d.Config {
			cfgKeys = append(cfgKeys, k)
		}
		sort.Strings(cfgKeys)
		for _, k := range cfgKeys {
			cfg.Body = append(cfg.Body, conf.CallStmt(k, fmt.Sprint(d.Config[k])))
		}
		db.Body = append(db.Body, cfg)
	}
	return db
}

// LoadJobConfBytes reads job.conf or transpiles job.conf.bt.
func LoadJobConfBytes(jobName string) ([]byte, error) {
	jobDir := JobFilePath(jobName)
	confPath := path.Join(jobDir, JobConfName)
	confBTPath := path.Join(jobDir, JobConfBTName)
	legacyConfPath := path.Join(jobDir, LegacyManifestConfName)
	legacyBTPath := path.Join(jobDir, LegacyManifestConfBTName)
	jsonPath := path.Join(jobDir, ManifestJSONName)
	jsonBTPath := path.Join(jobDir, ManifestJSONName+JobTemplateExt)

	hasConf := manifestFileExists(confPath)
	hasBT := manifestFileExists(confBTPath)
	hasLegacy := manifestFileExists(legacyConfPath) || manifestFileExists(legacyBTPath)
	hasJSON := manifestFileExists(jsonPath) || manifestFileExists(jsonBTPath)

	if hasLegacy && (hasConf || hasBT) {
		return nil, conf.RejectBothFormats(LegacyManifestConfName, JobConfName)
	}
	if hasLegacy {
		old := LegacyManifestConfName
		if manifestFileExists(legacyBTPath) {
			old = LegacyManifestConfBTName
		}
		return nil, conf.Err(old, 0, 0, fmt.Sprintf("found %s; use %s or %s", old, JobConfName, JobConfBTName)).
			WithHint("manifest.conf was renamed to job.conf")
	}
	if hasJSON && (hasConf || hasBT) {
		return nil, conf.RejectBothFormats(ManifestJSONName, JobConfName)
	}
	if hasJSON {
		old := ManifestJSONName
		if manifestFileExists(jsonBTPath) {
			old = ManifestJSONName + JobTemplateExt
		}
		return nil, conf.RejectOldJSONFormat(old, JobConfName)
	}
	if hasConf && hasBT {
		return nil, fmt.Errorf(
			"%w: job %s has both job.conf and job.conf.bt",
			bucket.ErrInvalidJob, jobName,
		)
	}
	if hasConf {
		data, err := conf.ReadBytes(confPath)
		if err != nil {
			return nil, fmt.Errorf("%w:%w", bucket.ErrUnexpectedError, err)
		}
		return data, nil
	}
	if hasBT {
		data, err := conf.ReadBytes(confBTPath)
		if err != nil {
			return nil, fmt.Errorf("%w:%w", bucket.ErrUnexpectedError, err)
		}
		return []byte(Transpile(string(data), JobTemplateVarsForName(jobName))), nil
	}
	return nil, fmt.Errorf("%w: job %s job.conf not found", bucket.ErrInvalidJob, jobName)
}
