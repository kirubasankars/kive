// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"database/sql"
	"sync"
)

// txGate serializes all use of a shared *sql.Tx. database/sql transactions are
// not safe for concurrent use; hook worker setup and the runtime HTTP API must
// go through Do (or hold Lock for a multi-statement section such as Query+Scan).
type txGate struct {
	mu sync.Mutex
	tx *sql.Tx
}

func newTxGate(tx *sql.Tx) *txGate {
	return &txGate{tx: tx}
}

// Do runs fn with exclusive access to the gated transaction.
func (g *txGate) Do(fn func(*sql.Tx) error) error {
	if g == nil {
		return fn(nil)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return fn(g.tx)
}
