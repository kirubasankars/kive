// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

import (
	"fmt"
	"io"
	"os"
)

// ReadFile reads path with hard size limits and parses it as block-dialect conf.
// Non-regular files (dirs, devices) and oversize files are rejected before a
// full read. Symlinks to regular files are followed for size/content (same as
// os.ReadFile); callers that need NOFOLLOW should open the path themselves.
func ReadFile(path string) (*File, error) {
	data, err := ReadBytes(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}

// ReadBytes reads path with hard size limits and returns the raw bytes.
// Use when callers need to Parse after transform (e.g. template transpile).
func ReadBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return nil, err
		}
	}
	if !info.Mode().IsRegular() {
		return nil, Err(path, 0, 0, "conf path is not a regular file").
			WithCause(ErrLimitExceeded)
	}
	if info.Size() > MaxSourceBytes {
		return nil, Err(path, 0, 0, fmt.Sprintf("source exceeds %d byte limit", MaxSourceBytes)).
			WithCause(ErrLimitExceeded).
			WithHint("split or reduce the conf file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSourceBytes {
		return nil, Err(path, 0, 0, fmt.Sprintf("source exceeds %d byte limit", MaxSourceBytes)).
			WithCause(ErrLimitExceeded).
			WithHint("split or reduce the conf file")
	}
	return data, nil
}
