// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveRoot sets Location to the bucket root.
// When BUCKET_ROOT is set, that path is used (absolute). Otherwise Location is
// resolved from the current directory and its parents when it contains kive.conf
// or data/kive.db.
func ResolveRoot() error {
	if root := strings.TrimSpace(os.Getenv(EnvBucketRoot)); root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return UnexpectedError(err)
		}
		Location = abs
		UpdatePath()
		return nil
	}

	if isBucketRoot(Location) {
		abs, err := filepath.Abs(Location)
		if err != nil {
			return UnexpectedError(err)
		}
		Location = abs
		UpdatePath()
		return nil
	}

	start, err := os.Getwd()
	if err != nil {
		return UnexpectedError(err)
	}

	for dir := start; ; dir = filepath.Dir(dir) {
		if isBucketRoot(dir) {
			abs, err := filepath.Abs(dir)
			if err != nil {
				return UnexpectedError(err)
			}
			Location = abs
			UpdatePath()
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return nil
}

func isBucketRoot(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "kive.conf")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "kive.db")); err == nil {
		return true
	}
	return false
}

// LooksLikeBucket reports whether dir is a kive bucket root (kive.conf or data/kive.db).
func LooksLikeBucket(dir string) bool {
	return isBucketRoot(dir)
}

// Root returns the absolute bucket root after ResolveRoot.
func Root() (string, error) {
	if err := ResolveRoot(); err != nil {
		return "", err
	}
	return filepath.Abs(Location)
}
