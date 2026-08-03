// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package worker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kive/bucket"
)

// KnownHostsPath returns the bucket-local known_hosts file path.
func KnownHostsPath() string {
	return filepath.Join(bucket.Location, ".ssh", "known_hosts")
}

// EnsureKnownHostsDir creates bucket/.ssh and an empty known_hosts file if missing.
func EnsureKnownHostsDir() error {
	dir := filepath.Join(bucket.Location, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create SSH trust directory %s: %w", dir, err)
	}
	path := KnownHostsPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.WriteFile(path, nil, 0o600)
	}
	return nil
}

// IsHostTrusted reports whether workerIP has an entry in the bucket known_hosts file.
func IsHostTrusted(workerIP string) (bool, error) {
	return IsHostTrustedContext(context.Background(), workerIP)
}

// IsHostTrustedContext reports whether a worker is trusted and supports cancellation.
func IsHostTrustedContext(ctx context.Context, workerIP string) (bool, error) {
	if err := EnsureKnownHostsDir(); err != nil {
		return false, err
	}
	cmd := exec.CommandContext(ctx, "ssh-keygen", "-F", workerIP, "-f", KnownHostsPath())
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("lookup host key for %s: %w", workerIP, err)
	}
	return true, nil
}

// HostKeyFingerprints returns OpenSSH SHA256 fingerprint lines for workerIP from known_hosts.
// Each line is formatted as "<bits> SHA256:<fp> (TYPE)" without the known_hosts hostname field
// (which is a hashed |1|… token when keys were stored via ssh-keyscan -H).
func HostKeyFingerprints(workerIP string) ([]string, error) {
	return HostKeyFingerprintsContext(context.Background(), workerIP)
}

// HostKeyFingerprintsContext returns pinned fingerprints with cancellation.
func HostKeyFingerprintsContext(ctx context.Context, workerIP string) ([]string, error) {
	if err := EnsureKnownHostsDir(); err != nil {
		return nil, err
	}
	lookup := exec.CommandContext(ctx, "ssh-keygen", "-F", workerIP, "-f", KnownHostsPath())
	entry, err := lookup.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, fmt.Errorf("worker %s: no host key in known_hosts", workerIP)
		}
		return nil, fmt.Errorf("lookup host key for %s: %w", workerIP, err)
	}
	if len(bytes.TrimSpace(entry)) == 0 {
		return nil, fmt.Errorf("worker %s: no host key in known_hosts", workerIP)
	}

	fpCmd := exec.CommandContext(ctx, "ssh-keygen", "-lf", "-")
	fpCmd.Stdin = bytes.NewReader(entry)
	out, err := fpCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("fingerprint host key for %s: %w", workerIP, err)
	}

	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, formatFingerprintLine(line))
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("worker %s: no host key fingerprints", workerIP)
	}
	return lines, nil
}

// formatFingerprintLine keeps bits, fingerprint, and key type; drops the hostname field.
func formatFingerprintLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return line
	}
	bits, fp := fields[0], fields[1]
	if !strings.HasPrefix(fp, "SHA256:") && !strings.HasPrefix(fp, "MD5:") {
		return line
	}
	keyType := fields[len(fields)-1]
	if strings.HasPrefix(keyType, "(") && strings.HasSuffix(keyType, ")") {
		return bits + " " + fp + " " + keyType
	}
	return bits + " " + fp
}

// CheckTrustedHosts returns an error when any worker lacks a pinned host key.
func CheckTrustedHosts(workerIPs []string) error {
	return CheckTrustedHostsContext(context.Background(), workerIPs)
}

// CheckTrustedHostsContext checks pinned keys with cancellation.
func CheckTrustedHostsContext(ctx context.Context, workerIPs []string) error {
	if len(workerIPs) == 0 {
		return nil
	}
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		errors []string
	)
	for _, workerIP := range workerIPs {
		if err := ctx.Err(); err != nil {
			wg.Wait()
			return err
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			trusted, err := IsHostTrustedContext(ctx, ip)
			if err != nil {
				mu.Lock()
				errors = append(errors, err.Error())
				mu.Unlock()
				return
			}
			if !trusted {
				mu.Lock()
				errors = append(errors, fmt.Sprintf(
					"worker %s: host key not trusted. Run: kive worker trust -w %s",
					ip, ip,
				))
				mu.Unlock()
			}
		}(workerIP)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n%s", bucket.ErrWorkerPrerequisites, strings.Join(errors, "\n"))
}

// TrustHost pins workerIP using ssh-keyscan. When force is false and the host is already trusted, this is a no-op.
func TrustHost(workerIP string, force bool) error {
	return TrustHostContext(context.Background(), workerIP, force)
}

// TrustHostContext pins one worker host key with cancellation.
func TrustHostContext(ctx context.Context, workerIP string, force bool) error {
	port, err := bucket.SSHPort()
	if err != nil {
		return err
	}
	return trustHost(ctx, nil, workerIP, port, force)
}

// TrustHosts pins host keys for each worker in parallel.
func TrustHosts(workerIPs []string, force bool, concurrency int) error {
	return TrustHostsContext(context.Background(), workerIPs, force, concurrency)
}

// TrustHostsContext pins host keys with bounded concurrency and cancellation.
func TrustHostsContext(ctx context.Context, workerIPs []string, force bool, concurrency int) error {
	return trustHostsError(ctx, nil, workerIPs, force, concurrency)
}

// TrustHostsLogged pins host keys for each worker and logs ssh-keyscan commands.
func TrustHostsLogged(rt *bucket.Runtime, workerIPs []string, force bool, concurrency int) error {
	return TrustHostsLoggedContext(context.Background(), rt, workerIPs, force, concurrency)
}

// TrustHostsLoggedContext pins host keys with run logging and cancellation.
func TrustHostsLoggedContext(ctx context.Context, rt *bucket.Runtime, workerIPs []string, force bool, concurrency int) error {
	return trustHostsError(ctx, rt, workerIPs, force, concurrency)
}

// TrustHostsLoggedFailuresContext pins host keys and returns per-host failures.
// A nil/empty map means every worker succeeded. Setup or cancellation errors are
// returned via the error result instead of the map.
func TrustHostsLoggedFailuresContext(
	ctx context.Context,
	rt *bucket.Runtime,
	workerIPs []string,
	force bool,
	concurrency int,
) (map[string]error, error) {
	return trustHosts(ctx, rt, workerIPs, force, concurrency)
}

func trustHostsError(
	ctx context.Context,
	rt *bucket.Runtime,
	workerIPs []string,
	force bool,
	concurrency int,
) error {
	failures, err := trustHosts(ctx, rt, workerIPs, force, concurrency)
	if err != nil {
		return err
	}
	if len(failures) == 0 {
		return nil
	}
	lines := make([]string, 0, len(failures))
	for _, failErr := range failures {
		lines = append(lines, failErr.Error())
	}
	return fmt.Errorf("%w:\n%s", bucket.ErrWorkerPrerequisites, strings.Join(lines, "\n"))
}

func trustHosts(
	ctx context.Context,
	rt *bucket.Runtime,
	workerIPs []string,
	force bool,
	concurrency int,
) (map[string]error, error) {
	if len(workerIPs) == 0 {
		return nil, nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	port, err := bucket.SSHPort()
	if err != nil {
		return nil, err
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures = make(map[string]error)
		sem      = make(chan struct{}, concurrency)
	)
	for _, workerIP := range workerIPs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			<-sem
			wg.Wait()
			return nil, err
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := trustHost(ctx, rt, ip, port, force); err != nil {
				mu.Lock()
				failures[ip] = err
				mu.Unlock()
			}
		}(workerIP)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(failures) == 0 {
		return nil, nil
	}
	return failures, nil
}

func trustHost(ctx context.Context, rt *bucket.Runtime, workerIP string, port int, force bool) error {
	if err := EnsureKnownHostsDir(); err != nil {
		return err
	}
	path := KnownHostsPath()

	if force {
		_ = exec.CommandContext(ctx, "ssh-keygen", "-R", workerIP, "-f", path).Run()
	} else {
		trusted, err := IsHostTrustedContext(ctx, workerIP)
		if err != nil {
			return err
		}
		if trusted {
			return nil
		}
	}

	args := []string{"-H", "-T", "15"}
	if port != 22 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, workerIP)

	var lastErr error
	backoff := 200 * time.Millisecond
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < 2*time.Second {
				backoff *= 2
			}
		}

		scanCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		lines, err := runSSHKeyscan(scanCtx, rt, workerIP, args)
		cancel()
		if err != nil {
			lastErr = err
			if !isTransientSSHKeyscanErr(err) {
				return err
			}
			continue
		}
		if lines == "" {
			return fmt.Errorf("worker %s: ssh-keyscan returned no host keys (host down, wrong port, or firewalled?)", workerIP)
		}
		return appendKnownHostLines(workerIP, lines)
	}
	return lastErr
}

func runSSHKeyscan(ctx context.Context, rt *bucket.Runtime, workerIP string, args []string) (string, error) {
	if rt != nil {
		cmd := exec.CommandContext(ctx, "ssh-keyscan", args...)
		cmdCtx := bucket.CommandContext{
			Phase:  "worker",
			Action: "trust",
			Cmd:    "ssh-keyscan " + strings.Join(args, " "),
		}
		output, err := rt.RunCommandCapture(workerIP, cmdCtx, cmd)
		if err != nil {
			return "", fmt.Errorf("worker %s: ssh-keyscan failed: %w", workerIP, err)
		}
		return strings.TrimSpace(output), nil
	}

	out, err := exec.CommandContext(ctx, "ssh-keyscan", args...).Output()
	if err != nil {
		return "", fmt.Errorf("worker %s: ssh-keyscan failed: %w", workerIP, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func isTransientSSHKeyscanErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection closed"):
		return true
	case strings.Contains(msg, "connection reset"):
		return true
	case strings.Contains(msg, "temporarily unavailable"):
		return true
	case strings.Contains(msg, "exit status 1"), strings.Contains(msg, "exit 1"):
		// ssh-keyscan often exits 1 when the remote closes mid-scan (MaxStartups).
		return true
	default:
		return false
	}
}

func appendKnownHostLines(workerIP, lines string) error {
	path := KnownHostsPath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("worker %s: open known_hosts: %w", workerIP, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(lines + "\n"); err != nil {
		return fmt.Errorf("worker %s: write known_hosts: %w", workerIP, err)
	}
	return nil
}

// WrapSSHError adds actionable guidance when SSH reports a host key mismatch.
func WrapSSHError(workerIP string, err error, output string) error {
	if err == nil {
		return nil
	}
	combined := err.Error() + "\n" + output
	if strings.Contains(combined, "REMOTE HOST IDENTIFICATION HAS CHANGED") ||
		strings.Contains(combined, "Host key verification failed") {
		return fmt.Errorf(
			"worker %s: host key changed (possible MITM or reinstall). After verifying the worker out-of-band, run: kive worker trust -w %s --force",
			workerIP, workerIP,
		)
	}
	if len(strings.TrimSpace(output)) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(output))
	}
	return err
}
