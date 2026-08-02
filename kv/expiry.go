// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package kv

import "time"

// EntryExpired reports whether an active entry is past its TTL.
// TTL is in seconds; 0 means no expiry.
func EntryExpired(entry Entry, now time.Time) bool {
	if entry.Deleted == 1 || entry.TTL <= 0 {
		return false
	}
	return entry.LastModifiedTime+int64(entry.TTL) <= now.Unix()
}

// ExpireStaleEntries tombstones keys whose created_date + ttl has passed.
// Entries modified in the current session (Changed) are skipped; they receive a
// fresh created_date when persisted.
func (s *Store) ExpireStaleEntries(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowUnix := now.Unix()
	for _, keys := range s.namespaces {
		for _, entry := range keys {
			if entry.Deleted == 1 || entry.Changed || entry.TTL <= 0 {
				continue
			}
			if entry.LastModifiedTime+int64(entry.TTL) > nowUnix {
				continue
			}
			entry.Deleted = 1
			entry.Changed = true
			entry.Version++
		}
	}
}
