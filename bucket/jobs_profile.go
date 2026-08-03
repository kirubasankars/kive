// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// jobsProfilePattern allows basename-safe profile tokens for
// workspace/bucket.jobs.<profile>.conf (same family as job signer names).
var jobsProfilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// SanitizeJobsProfile returns a basename-safe jobs_profile value.
// Empty input is valid (means bucket.jobs.conf). Absolute paths, ".." segments,
// separators, and non-allowlisted characters are rejected.
func SanitizeJobsProfile(profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return "", nil
	}
	if filepath.IsAbs(profile) || strings.Contains(profile, "..") ||
		strings.ContainsAny(profile, `/\`) || filepath.Base(profile) != profile ||
		!jobsProfilePattern.MatchString(profile) {
		return "", fmt.Errorf("%w: %s must be a basename token matching [A-Za-z0-9][A-Za-z0-9._-]* (got %q)",
			ErrInvalidBucketConf, keyJobsProfile, profile)
	}
	name := JobsProfileFileName(profile)
	if filepath.Base(name) != name {
		return "", fmt.Errorf("%w: %s produces invalid jobs conf name %q",
			ErrInvalidBucketConf, keyJobsProfile, name)
	}
	return profile, nil
}

// JobsProfileFileName returns the workspace basename for a sanitized profile.
// Empty profile yields bucket.jobs.conf.
func JobsProfileFileName(profile string) string {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return "bucket.jobs.conf"
	}
	return fmt.Sprintf("bucket.jobs.%s.conf", profile)
}
