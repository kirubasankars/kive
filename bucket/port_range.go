// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	defaultPortMin   = 30000
	defaultPortMax   = 39999
	portRangeKey     = "port_range"
	defaultPortRange = "30000,39999"
)

// PortRange is the inclusive port pool declared in kive.conf (with bucket.conf fallback).
type PortRange struct {
	Min int
	Max int
}

// ApplyPortRangeDefaults sets port_range in settings when absent or empty.
func ApplyPortRangeDefaults(settings map[string]string) {
	if settings == nil {
		return
	}
	if strings.TrimSpace(settings[portRangeKey]) == "" {
		settings[portRangeKey] = defaultPortRange
	}
}

// LoadPortRange reads port_range from kive.conf, falling back to workspace/bucket.conf
// for older buckets. Missing port_range uses default "30000,39999".
func LoadPortRange() (PortRange, error) {
	raw, err := resolvePortRangeRaw()
	if err != nil {
		return PortRange{}, err
	}
	return ParsePortRangeValue(raw)
}

func resolvePortRangeRaw() (string, error) {
	conf, err := GetKiveConf()
	if err != nil {
		return "", err
	}
	if s := strings.TrimSpace(conf.PortRange); s != "" {
		return s, nil
	}

	bucketRaw, err := loadBucketConfRaw(BucketConfPath())
	if err != nil {
		return "", err
	}
	if v, ok := bucketRaw[portRangeKey]; ok && v != nil {
		s, err := bucketConfStringValue(portRangeKey, v)
		if err != nil {
			return "", err
		}
		if s = strings.TrimSpace(s); s != "" {
			return s, nil
		}
	}
	return defaultPortRange, nil
}

// ParsePortRangeValue parses port_range values like "30000,39999".
func ParsePortRangeValue(raw string) (PortRange, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return PortRange{}, fmt.Errorf("%w: port_range %q must be \"min,max\"", ErrInvalidPortRange, raw)
	}

	min, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return PortRange{}, fmt.Errorf("%w: port_range %q: invalid min port %q", ErrInvalidPortRange, raw, parts[0])
	}
	max, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return PortRange{}, fmt.Errorf("%w: port_range %q: invalid max port %q", ErrInvalidPortRange, raw, parts[1])
	}

	r := PortRange{Min: min, Max: max}
	return r, r.Validate()
}

// Contains reports whether port is inside the inclusive kive assignment pool.
func (r PortRange) Contains(port int) bool {
	return port >= r.Min && port <= r.Max
}

// Validate checks the port pool bounds.
func (r PortRange) Validate() error {
	if r.Min < 1 || r.Max > 65535 || r.Min > r.Max {
		return fmt.Errorf("%w: port_range=%d,%d", ErrInvalidPortRange, r.Min, r.Max)
	}
	return nil
}

// DefaultBucketConf returns the initial workspace/bucket.conf content for new buckets.
func DefaultBucketConf() string {
	return fmt.Sprintf("kv_retain_days(%d)\n", DefaultKVRetainDays)
}

// DefaultBucketJobsConf returns the initial workspace/bucket.jobs.conf content for new buckets.
func DefaultBucketJobsConf() string {
	return ""
}
