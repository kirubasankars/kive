// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"strings"
)

const (
	PublicIDLen      = 6
	publicIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	publicIDMaxTries = 64
)

// IsPublicID reports whether s is a catalog public id (exactly 6 chars [0-9a-z]).
func IsPublicID(s string) bool {
	if len(s) != PublicIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// PublicIDFromString derives a stable 6-character [0-9a-z] id from any string.
func PublicIDFromString(s string) string {
	sum := sha256.Sum256([]byte(s))
	var b strings.Builder
	b.Grow(PublicIDLen)
	for i := 0; i < PublicIDLen; i++ {
		b.WriteByte(publicIDAlphabet[int(sum[i])%len(publicIDAlphabet)])
	}
	return b.String()
}

// NewPublicID returns a random 6-character [0-9a-z] catalog id.
func NewPublicID() (string, error) {
	var raw [PublicIDLen]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate public id: %w", err)
	}
	var b strings.Builder
	b.Grow(PublicIDLen)
	for i := 0; i < PublicIDLen; i++ {
		b.WriteByte(publicIDAlphabet[int(raw[i])%len(publicIDAlphabet)])
	}
	return b.String(), nil
}

// MintUniquePublicID tries candidates until accept returns nil.
// The first candidate is base (if non-empty); then NewPublicID; then salted hashes of base.
func MintUniquePublicID(base string, accept func(id string) error) (string, error) {
	try := func(id string) (string, bool, error) {
		if !IsPublicID(id) {
			return "", false, nil
		}
		err := accept(id)
		if err == nil {
			return id, true, nil
		}
		if isUniqueConflict(err) {
			return "", false, nil
		}
		return "", false, err
	}

	if base != "" {
		if id, ok, err := try(PublicIDFromString(base)); err != nil {
			return "", err
		} else if ok {
			return id, nil
		}
	}

	for i := 0; i < publicIDMaxTries; i++ {
		var candidate string
		if base != "" {
			candidate = PublicIDFromString(fmt.Sprintf("%s:%d", base, i+1))
		} else {
			var err error
			candidate, err = NewPublicID()
			if err != nil {
				return "", err
			}
		}
		if id, ok, err := try(candidate); err != nil {
			return "", err
		} else if ok {
			return id, nil
		}
	}
	return "", fmt.Errorf("exhausted public id candidates")
}

func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}
