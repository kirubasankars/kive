// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package buildinfo exposes identity embedded in the kive binary.
package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

// GitHash is replaced by make build using -ldflags. Direct go builds use "dev".
var GitHash = "dev"

// Version is replaced by release builds using -ldflags (e.g. v0.9.6).
// Direct go builds and untagged make builds use "dev".
var Version = "dev"

// Hash returns a stable, non-empty build identifier.
func Hash() string {
	if hash := strings.TrimSpace(GitHash); hash != "" {
		return hash
	}
	return "dev"
}

// VersionLabel returns a non-empty release label, with a leading "v" when the
// value looks like a semver without one.
func VersionLabel() string {
	ver := strings.TrimSpace(Version)
	if ver == "" {
		return "dev"
	}
	if ver != "dev" && !strings.HasPrefix(ver, "v") {
		return "v" + ver
	}
	return ver
}

// String formats identity for `kive version`.
func String() string {
	return fmt.Sprintf("kive version %s (%s) %s/%s", VersionLabel(), Hash(), runtime.GOOS, runtime.GOARCH)
}
