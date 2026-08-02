// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"strings"

	"kive/bucket"
)

type specifierOp string

const (
	specEq  specifierOp = "=="
	specGte specifierOp = ">="
	specLte specifierOp = "<="
	specGt  specifierOp = ">"
	specLt  specifierOp = "<"
	specCompat specifierOp = "~="
)

// VersionSpecifier is a single pip-style version constraint.
type VersionSpecifier struct {
	Op             specifierOp
	Version        Version
	CompatSegments int // for ~= only: significant release segments in the specifier
}

// VersionSpecifierSet is a comma-separated AND group of specifiers.
type VersionSpecifierSet []VersionSpecifier

// BackwardCompatibilitySpec is an OR of VersionSpecifierSet entries from manifest.
type BackwardCompatibilitySpec []VersionSpecifierSet

// ParseVersionSpecifier parses one specifier (e.g. ">=2.0.0", "~=2.1.0", or "2.0.0").
func ParseVersionSpecifier(raw string) (VersionSpecifier, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return VersionSpecifier{}, fmt.Errorf("%w: empty specifier", bucket.ErrInvalidVersionSpecifier)
	}

	ops := []specifierOp{specCompat, specGte, specLte, specEq, specGt, specLt}
	for _, op := range ops {
		if strings.HasPrefix(raw, string(op)) {
			versionRaw := strings.TrimSpace(raw[len(op):])
			if versionRaw == "" {
				return VersionSpecifier{}, fmt.Errorf("%w: missing version after %s", bucket.ErrInvalidVersionSpecifier, op)
			}
			compatSegments := 0
			if op == specCompat {
				compatSegments = countReleaseSegments(versionRaw)
			}
			version, err := ParseVersion(versionRaw)
			if err != nil {
				return VersionSpecifier{}, fmt.Errorf("%w: %w", bucket.ErrInvalidVersionSpecifier, err)
			}
			return VersionSpecifier{Op: op, Version: version, CompatSegments: compatSegments}, nil
		}
	}

	version, err := ParseVersion(raw)
	if err != nil {
		return VersionSpecifier{}, fmt.Errorf("%w: %w", bucket.ErrInvalidVersionSpecifier, err)
	}
	return VersionSpecifier{Op: specEq, Version: version}, nil
}

// ParseVersionSpecifierSet parses a comma-separated AND group (e.g. ">=2.0.0,<3.0.0").
func ParseVersionSpecifierSet(raw string) (VersionSpecifierSet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: empty specifier set", bucket.ErrInvalidVersionSpecifier)
	}

	parts := strings.Split(raw, ",")
	set := make(VersionSpecifierSet, 0, len(parts))
	for _, part := range parts {
		spec, err := ParseVersionSpecifier(part)
		if err != nil {
			return nil, err
		}
		set = append(set, spec)
	}
	return set, nil
}

// ParseBackwardCompatibilityFrom parses manifest backward_compatibility_from (OR across entries).
func ParseBackwardCompatibilityFrom(entries []string) (BackwardCompatibilitySpec, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	spec := make(BackwardCompatibilitySpec, 0, len(entries))
	for _, entry := range entries {
		set, err := ParseVersionSpecifierSet(entry)
		if err != nil {
			return nil, err
		}
		spec = append(spec, set)
	}
	return spec, nil
}

// Satisfies reports whether version meets this specifier.
func (s VersionSpecifier) Satisfies(version Version) bool {
	switch s.Op {
	case specEq:
		return version.Compare(s.Version) == 0
	case specGte:
		return version.Compare(s.Version) >= 0
	case specLte:
		return version.Compare(s.Version) <= 0
	case specGt:
		return version.Compare(s.Version) > 0
	case specLt:
		return version.Compare(s.Version) < 0
	case specCompat:
		return compatibleReleaseSatisfied(version, s.Version, s.CompatSegments)
	default:
		return false
	}
}

func compatibleReleaseSatisfied(version, anchor Version, segments int) bool {
	if version.Compare(anchor) < 0 {
		return false
	}

	switch segments {
	case 1:
		upper := Version{Major: anchor.Major + 1, Minor: 0, Patch: 0}
		return version.Compare(upper) < 0
	case 2:
		upper := Version{Major: anchor.Major, Minor: anchor.Minor + 1, Patch: 0}
		return version.Compare(upper) < 0
	default:
		upper := Version{Major: anchor.Major, Minor: anchor.Minor + 1, Patch: 0}
		return version.Compare(upper) < 0
	}
}

func countReleaseSegments(raw string) int {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "v"), "V")
	if idx := strings.Index(raw, "-"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		count++
	}
	if count == 0 {
		return 3
	}
	return count
}

// Satisfies reports whether version meets all specifiers in the set (AND).
func (set VersionSpecifierSet) Satisfies(version Version) bool {
	for _, spec := range set {
		if !spec.Satisfies(version) {
			return false
		}
	}
	return true
}

// Satisfies reports whether version matches any specifier set (OR).
func (spec BackwardCompatibilitySpec) Satisfies(version Version) bool {
	for _, set := range spec {
		if set.Satisfies(version) {
			return true
		}
	}
	return false
}
