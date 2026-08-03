// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"path/filepath"
	"sync"
)

var (
	locationMu   sync.Mutex
	locationCond = sync.NewCond(&locationMu)
	activeLoc    string
	holders      int
	savedPrev    string
)

// WithLocation runs fn with Location set to root (absolute).
//
// Callers that need the same absolute root may run concurrently (refcount).
// Callers that need a different root wait until the active root has no holders.
// That lets serve answer catalog/API reads for an env while a long run_command
// (or similar) is in progress on that same bucket, without racing Location.
func WithLocation(root string, fn func() error) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return UnexpectedError(err)
	}

	locationMu.Lock()
	for holders > 0 && activeLoc != abs {
		locationCond.Wait()
	}
	if holders == 0 {
		savedPrev = Location
		activeLoc = abs
		Location = abs
		UpdatePath()
	}
	holders++
	locationMu.Unlock()

	defer func() {
		locationMu.Lock()
		holders--
		if holders == 0 {
			Location = savedPrev
			UpdatePath()
			activeLoc = ""
			savedPrev = ""
		}
		locationCond.Broadcast()
		locationMu.Unlock()
	}()

	return fn()
}
