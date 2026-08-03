// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package deploy

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

type deployCancelKey struct{}

// withDeployWorkContext returns a work context that ignores user cancel for
// mutations (rsync, lifecycle, hooks, metadata sync). Soft cancel is attached
// via context.Value and is only honored by deployCancelCtx at health gates.
// Idempotent: already-wrapped contexts are returned unchanged.
func withDeployWorkContext(cancelCtx context.Context) context.Context {
	if cancelCtx == nil {
		cancelCtx = context.Background()
	}
	if existing, ok := cancelCtx.Value(deployCancelKey{}).(context.Context); ok && existing != nil {
		return cancelCtx
	}
	return context.WithValue(context.WithoutCancel(cancelCtx), deployCancelKey{}, cancelCtx)
}

// deployCancelCtx extracts the user-cancellable context for health-check waits.
// Falls back to ctx when no cancel parent was attached (plain test contexts).
func deployCancelCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if cancelCtx, ok := ctx.Value(deployCancelKey{}).(context.Context); ok && cancelCtx != nil {
		return cancelCtx
	}
	return ctx
}

// withDeployContext returns a context cancelled on SIGINT or SIGTERM.
// A second interrupt force-exits with code 130.
func withDeployContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("deploy: cancel requested (takes effect at next health check; interrupt again to force quit)...")
		cancel()
		<-sigCh
		os.Exit(130)
	}()

	stop := func() {
		signal.Stop(sigCh)
		cancel()
	}
	return ctx, stop
}
