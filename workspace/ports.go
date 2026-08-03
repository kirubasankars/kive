// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"kive/bucket"
)

var manifestPortKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

const (
	DefaultPortProtocol = "tcp"
	DefaultPortExposure = "cluster"
)

// ManifestPortBinding is one port entry in a job manifest.
// Fixed is nil when kive should assign from the bucket port pool (manifest value {}).
// Fixed is non-nil when the manifest sets an explicit port number.
type ManifestPortBinding struct {
	Fixed    *int
	Protocol string // http | https | tcp | udp
	Exposure string // cluster | public
}

// Provisioned reports whether kive assigns the port number at build time.
func (b ManifestPortBinding) Provisioned() bool {
	return b.Fixed == nil
}

// ManifestPorts lists named ports declared in a job manifest.
type ManifestPorts map[string]ManifestPortBinding

type manifestPortObject struct {
	Protocol string `json:"protocol"`
	Exposure string `json:"exposure"`
	Port     *int   `json:"port"`
}

// UnmarshalJSON accepts {} / object with protocol/exposure/port, or a JSON integer (fixed port).
func (p *ManifestPorts) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*p = nil
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	ports := make(ManifestPorts, len(raw))
	for name, value := range raw {
		if err := ValidatePortKey(name); err != nil {
			return err
		}

		trimmed := strings.TrimSpace(string(value))
		if trimmed == "{}" {
			ports[name] = ManifestPortBinding{
				Protocol: DefaultPortProtocol,
				Exposure: DefaultPortExposure,
			}
			continue
		}

		var port int
		if err := json.Unmarshal(value, &port); err == nil {
			ports[name] = ManifestPortBinding{
				Fixed:    &port,
				Protocol: DefaultPortProtocol,
				Exposure: DefaultPortExposure,
			}
			continue
		}

		var obj manifestPortObject
		if err := json.Unmarshal(value, &obj); err != nil {
			return fmt.Errorf("%w: port %q must be {}, an integer, or an object with protocol/exposure/port, not %s",
				bucket.ErrInvalidManifestPort, name, trimmed)
		}

		protocol, err := NormalizePortProtocol(obj.Protocol)
		if err != nil {
			return fmt.Errorf("%w: port %q: %w", bucket.ErrInvalidManifestPort, name, err)
		}
		exposure, err := NormalizePortExposure(obj.Exposure)
		if err != nil {
			return fmt.Errorf("%w: port %q: %w", bucket.ErrInvalidManifestPort, name, err)
		}

		binding := ManifestPortBinding{
			Protocol: protocol,
			Exposure: exposure,
		}
		if obj.Port != nil {
			binding.Fixed = obj.Port
		}
		ports[name] = binding
	}
	*p = ports
	return nil
}

// NormalizePortProtocol returns a canonical protocol or an error for unknown values.
// Empty defaults to tcp.
func NormalizePortProtocol(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return DefaultPortProtocol, nil
	}
	switch v {
	case "http", "https", "tcp", "udp":
		return v, nil
	default:
		return "", fmt.Errorf("protocol %q must be http, https, tcp, or udp", raw)
	}
}

// NormalizePortExposure returns a canonical exposure or an error for unknown values.
// Empty defaults to cluster.
func NormalizePortExposure(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return DefaultPortExposure, nil
	}
	switch v {
	case "cluster", "public":
		return v, nil
	default:
		return "", fmt.Errorf("exposure %q must be cluster or public", raw)
	}
}

// Names returns sorted port keys from the manifest.
func (p ManifestPorts) Names() []string {
	if len(p) == 0 {
		return nil
	}
	names := make([]string, 0, len(p))
	for name := range p {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidatePortKey checks a manifest port name syntax (lowercase snake_case).
func ValidatePortKey(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: port name cannot be empty", bucket.ErrPortKeyFormat)
	}
	if !manifestPortKeyPattern.MatchString(name) {
		return fmt.Errorf("%w: port name %q", bucket.ErrPortKeyFormat, name)
	}
	return nil
}

// ValidateJobPortKey checks that a manifest port name is prefixed with "<job>_"
// so keys stay distinct in the flat kive/bucket KV namespace.
func ValidateJobPortKey(jobName, portName string) error {
	if err := ValidatePortKey(portName); err != nil {
		return err
	}
	prefix := jobName + "_"
	if !strings.HasPrefix(portName, prefix) {
		return fmt.Errorf("%w: job %s port %q must be prefixed with %q", bucket.ErrPortKeyFormat, jobName, portName, prefix)
	}
	return nil
}
