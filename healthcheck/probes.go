// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package healthcheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"kive/bucket"
	"kive/data"
	"kive/hooks"
	"kive/rollout"
	"kive/worker"
	"kive/workspace"
)

const defaultProbeTimeout = 5 * time.Second

func runManifestProbes(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	checks []workspace.HealthCheckProbe,
	timeout time.Duration,
	opts CheckOptions,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(checks) == 0 {
		return nil
	}
	workers, err := resolveProbeWorkers(tx, job, opts)
	if err != nil {
		return err
	}
	if len(workers) == 0 {
		return nil
	}

	resolved, err := rollout.ResolveOrder(tx, job, workers)
	if err != nil {
		return err
	}
	batchSize, err := data.GetMaxConcurrentRestarts(tx, job)
	if err != nil {
		return err
	}

	var errs []error
	for _, batchIPs := range hooks.SplitWorkerBatches(resolved.Ordered, batchSize) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runManifestProbesOnWorkers(ctx, tx, rt, job, checks, timeout, batchIPs); err != nil {
			errs = append(errs, err)
		}
	}
	return joinProbeErrors(errs)
}

func runManifestProbesOnWorkers(
	ctx context.Context,
	tx *sql.Tx,
	rt *bucket.Runtime,
	job string,
	checks []workspace.HealthCheckProbe,
	timeout time.Duration,
	workerIPs []string,
) error {
	if len(workerIPs) == 0 || len(checks) == 0 {
		return nil
	}

	activeIPs := make([]string, 0, len(workerIPs))
	for _, workerIP := range workerIPs {
		active, err := data.IsAllocationActive(tx, workerIP, job)
		if err != nil {
			return err
		}
		if active {
			activeIPs = append(activeIPs, workerIP)
		}
	}
	if len(activeIPs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(activeIPs)*len(checks))

	for _, workerIP := range activeIPs {
		if err := ctx.Err(); err != nil {
			return err
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			for idx, probe := range checks {
				if err := ctx.Err(); err != nil {
					errCh <- err
					return
				}
				if err := runProbe(ctx, tx, rt, job, ip, idx, probe, timeout); err != nil {
					errCh <- &AllocationHealthError{Job: job, WorkerIP: ip, Err: err}
					return
				}
			}
		}(workerIP)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return joinProbeErrors(errs)
}

func joinProbeErrors(errs []error) error {
	filtered := make([]error, 0, len(errs))
	for _, err := range errs {
		if err == nil {
			continue
		}
		if u, ok := err.(interface{ Unwrap() []error }); ok {
			filtered = append(filtered, u.Unwrap()...)
			continue
		}
		filtered = append(filtered, err)
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return errors.Join(filtered...)
}

func probeTimeout(spec *workspace.ManifestHealthCheck) time.Duration {
	if spec == nil || spec.TimeoutSeconds <= 0 {
		return defaultProbeTimeout
	}
	return time.Duration(spec.TimeoutSeconds) * time.Second
}

func waitConfig(spec *workspace.ManifestHealthCheck, kind string) (attempts int, interval time.Duration, err error) {
	seconds, err := bucket.HealthWaitSecondsFromConfig()
	if err != nil {
		return 0, 0, err
	}
	attempts = seconds
	if attempts < 1 {
		attempts = 1
	}
	interval = waitRetryInterval
	wait := (*workspace.HealthCheckWait)(nil)
	if spec != nil {
		wait = spec.WaitFor(kind)
	}
	if wait != nil {
		if wait.Attempts > 0 {
			attempts = wait.Attempts
		}
		if wait.IntervalSeconds > 0 {
			interval = time.Duration(wait.IntervalSeconds) * time.Second
		}
	}
	return attempts, interval, nil
}

func runProbe(ctx context.Context, tx *sql.Tx, rt *bucket.Runtime, job, workerIP string, idx int, probe workspace.HealthCheckProbe, timeout time.Duration) error {
	switch strings.ToLower(strings.TrimSpace(probe.Type)) {
	case "ssh":
		return probeSSH(ctx, rt, job, workerIP, probe.Command, timeout)
	case "tcp", "http":
		portName := strings.TrimSpace(probe.Port)
		port, err := data.GetPortNumber(tx, job, portName)
		if err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(probe.Type), "tcp") {
			return probeTCP(ctx, workerIP, port, timeout)
		}
		return probeHTTP(ctx, workerIP, port, probe, timeout)
	default:
		return fmt.Errorf("health_check.checks[%d]: unsupported type %q", idx, probe.Type)
	}
}

func probeSSH(ctx context.Context, rt *bucket.Runtime, job, workerIP, command string, timeout time.Duration) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("ssh %s: empty command", workerIP)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cmdCtx := bucket.CommandContext{
		Job:    job,
		Phase:  "health_check",
		Action: "ssh",
		Cmd:    command,
	}
	if rt != nil {
		if err := worker.RemoteShellCommandLoggedContext(ctx, rt, workerIP, command, cmdCtx, timeout); err != nil {
			return fmt.Errorf("ssh %s: %w", workerIP, err)
		}
		return nil
	}
	if err := worker.RemoteShellCommandContext(ctx, workerIP, command, timeout); err != nil {
		return fmt.Errorf("ssh %s: %w", workerIP, err)
	}
	return nil
}

func probeTCP(ctx context.Context, host string, port int, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp %s: %w", addr, err)
	}
	_ = conn.Close()
	return nil
}

func probeHTTP(ctx context.Context, host string, port int, probe workspace.HealthCheckProbe, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	scheme := strings.ToLower(strings.TrimSpace(probe.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	path := probe.Path
	if path == "" {
		path = "/"
	}
	expectStatus := probe.ExpectStatus
	if expectStatus == 0 {
		expectStatus = http.StatusOK
	}

	url := fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("http %s: %w", url, err)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http %s: %w", url, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != expectStatus {
		return fmt.Errorf("http %s: status %d want %d", url, resp.StatusCode, expectStatus)
	}
	return nil
}
