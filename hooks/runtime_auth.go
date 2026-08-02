// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
)

const runtimeAPITokenBytes = 32

type runtimeAPISession struct {
	token string
	port  int
}

var (
	runtimeSessionMu sync.Mutex
	runtimeSessions  []runtimeAPISession
)

func generateRuntimeAPIToken() (string, error) {
	buf := make([]byte, runtimeAPITokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func pushActiveRuntimeAPI(token string, port int) {
	runtimeSessionMu.Lock()
	defer runtimeSessionMu.Unlock()
	runtimeSessions = append(runtimeSessions, runtimeAPISession{token: token, port: port})
}

// popActiveRuntimeAPI removes the session for token/port. Prefer LIFO (nested
// StartRuntimeAPI); if cancel order is wrong, remove the matching entry so an
// outer session is not cleared by an inner stop.
func popActiveRuntimeAPI(token string, port int) {
	runtimeSessionMu.Lock()
	defer runtimeSessionMu.Unlock()
	for i := len(runtimeSessions) - 1; i >= 0; i-- {
		if runtimeSessions[i].token == token && runtimeSessions[i].port == port {
			runtimeSessions = append(runtimeSessions[:i], runtimeSessions[i+1:]...)
			return
		}
	}
}

// setActiveRuntimeAPIForTest replaces the session stack (tests only).
func setActiveRuntimeAPIForTest(token string, port int) {
	runtimeSessionMu.Lock()
	defer runtimeSessionMu.Unlock()
	if token == "" && port == 0 {
		runtimeSessions = nil
		return
	}
	runtimeSessions = []runtimeAPISession{{token: token, port: port}}
}

// ActiveRuntimeAPIToken returns the session token for the innermost runtime API, or "".
func ActiveRuntimeAPIToken() string {
	runtimeSessionMu.Lock()
	defer runtimeSessionMu.Unlock()
	if len(runtimeSessions) == 0 {
		return ""
	}
	return runtimeSessions[len(runtimeSessions)-1].token
}

// ActiveRuntimeAPIPort returns the ephemeral listen port for the innermost runtime API, or 0.
func ActiveRuntimeAPIPort() int {
	runtimeSessionMu.Lock()
	defer runtimeSessionMu.Unlock()
	if len(runtimeSessions) == 0 {
		return 0
	}
	return runtimeSessions[len(runtimeSessions)-1].port
}

func withRuntimeAPIAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !runtimeAPIAuthorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func runtimeAPIAuthorized(r *http.Request, want string) bool {
	got := requestRuntimeAPIToken(r)
	if got == "" || want == "" {
		return false
	}
	if len(got) != len(want) {
		subtle.ConstantTimeCompare([]byte(want), []byte(want))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func requestRuntimeAPIToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) >= len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return strings.TrimSpace(r.Header.Get(HeaderHookAPIToken))
}
