// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"regexp"
	"strings"

	"kive/bucket"
)

const MaxJobNameLen = 32

var jobNamePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9_]{0,30}[a-z0-9])?$`)

// ValidateJobName checks workspace job folder name syntax.
func ValidateJobName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: job name cannot be empty", bucket.ErrInvalidJobName)
	}
	if len(name) > MaxJobNameLen {
		return fmt.Errorf("%w: job name %q exceeds %d characters", bucket.ErrInvalidJobName, name, MaxJobNameLen)
	}
	if !jobNamePattern.MatchString(name) {
		return fmt.Errorf("%w: job name %q must be lowercase alphanumeric with optional internal underscores, start with a letter, and not start or end with underscore", bucket.ErrInvalidJobName, name)
	}
	return nil
}
