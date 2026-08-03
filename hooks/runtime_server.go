// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package hooks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

type runtimeAPIServer struct {
	*http.Server
	listener net.Listener
	port     int
}

type runtimeServerTimeouts struct {
	read     time.Duration
	write    time.Duration
	idle     time.Duration
	shutdown time.Duration
}

var defaultRuntimeServerTimeouts = runtimeServerTimeouts{
	read:     10 * time.Second,
	write:    10 * time.Second,
	idle:     30 * time.Second,
	shutdown: 5 * time.Second,
}

func newRuntimeAPIServer(tx *sql.Tx, token string, ln net.Listener) (*runtimeAPIServer, error) {
	port, err := listenPort(ln)
	if err != nil {
		return nil, err
	}
	apiCtx := &runtimeAPIContext{
		gate:       newTxGate(tx),
		semaphores: newSemaphoreCoordinator(),
		token:      token,
	}
	mux := newRuntimeAPIMux(apiCtx)
	return &runtimeAPIServer{
		Server: &http.Server{
			Handler:      withRuntimeAPIAuth(token, mux),
			ReadTimeout:  defaultRuntimeServerTimeouts.read,
			WriteTimeout: 0, // per-handler deadlines (semaphore acquire may block)
			IdleTimeout:  defaultRuntimeServerTimeouts.idle,
		},
		listener: ln,
		port:     port,
	}, nil
}

func listenPort(ln net.Listener) (int, error) {
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, fmt.Errorf("runtime api: unexpected listen address %v", ln.Addr())
	}
	return addr.Port, nil
}

func (s *runtimeAPIServer) runUntilCancelled(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("runtime api listen: %w", err)
			return
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultRuntimeServerTimeouts.shutdown)
		defer cancel()
		return s.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// StartRuntimeAPI serves /kv, /demands, /semaphore/*, and /http for local hook scripts.
// Call the returned stop function when the surrounding command finishes.
// A random per-session token is required on every request (see HOOK_API_TOKEN).
// The listen port is ephemeral (127.0.0.1:0) and published via ActiveRuntimeAPIPort / HOOK_API_PORT.
func StartRuntimeAPI(tx *sql.Tx) context.CancelFunc {
	token, err := generateRuntimeAPIToken()
	if err != nil {
		log.Printf("command runtime api: generate token: %v", err)
		return func() {}
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(RuntimeAPIListenHost, "0"))
	if err != nil {
		log.Printf("command runtime api: listen: %v", err)
		return func() {}
	}

	server, err := newRuntimeAPIServer(tx, token, ln)
	if err != nil {
		_ = ln.Close()
		log.Printf("command runtime api: %v", err)
		return func() {}
	}

	pushActiveRuntimeAPI(token, server.port)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := server.runUntilCancelled(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("command runtime api: %v", err)
		}
	}()

	return func() {
		cancel()
		// Pop this session only so a nested StartRuntimeAPI cannot clear an outer
		// deploy/build session (which previously left hooks falling back to :8080).
		popActiveRuntimeAPI(token, server.port)
	}
}

// SetupServer is deprecated; use StartRuntimeAPI.
func SetupServer(tx *sql.Tx) context.CancelFunc {
	return StartRuntimeAPI(tx)
}
