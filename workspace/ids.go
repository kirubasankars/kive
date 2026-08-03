// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"crypto/sha256"
	"strings"
)

const (
	publicIDLen      = 6
	publicIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
)

// StableJobID derives a deterministic 6-character [0-9a-z] id from arbitrary input.
func StableJobID(value string) string {
	sum := sha256.Sum256([]byte(value))
	var b strings.Builder
	b.Grow(publicIDLen)
	for i := 0; i < publicIDLen; i++ {
		b.WriteByte(publicIDAlphabet[int(sum[i])%len(publicIDAlphabet)])
	}
	return b.String()
}

// GetHashUUID is deprecated; use StableJobID.
func GetHashUUID(value string) string {
	return StableJobID(value)
}
