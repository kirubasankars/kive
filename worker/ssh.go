// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kive/bucket"
)

const (
	defaultSSHUser        = "agent"
	defaultSSHTimeoutSecs = 300
	defaultSSHConnectSecs = 15
)

// ValidateSSHConfig checks kive.conf SSH settings and that the private key file exists.
func ValidateSSHConfig() error {
	_, _, _, err := sshSettingsFromConf()
	return err
}

// sshSettings applies defaults and validates SSH configuration for remote execution.
func sshSettings(conf bucket.KiveConf) (user string, keyHostPath string, useSudo bool, err error) {
	user = strings.TrimSpace(conf.SSHUser)
	if user == "" {
		user = defaultSSHUser
	}

	keyHostPath, keyName, err := bucket.SSHKeyHostPath(conf.SSHKeyFile)
	if err != nil {
		return "", "", false, err
	}

	info, statErr := os.Stat(keyHostPath)
	if statErr != nil {
		return "", "", false, sshKeyError(keyName, keyHostPath, statErr)
	}
	if info.IsDir() {
		return "", "", false, fmt.Errorf("SSH private key path %s is a directory; ssh_key in kive.conf must point to a file", secretKeyDisplayPath(keyHostPath))
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return "", "", false, fmt.Errorf(
			"SSH private key %s has mode %o; chmod 600 %s",
			secretKeyDisplayPath(keyHostPath),
			perm,
			secretKeyDisplayPath(keyHostPath),
		)
	}

	return user, keyHostPath, conf.UseSUDO, nil
}

func sshKeyError(keyName, keyHostPath string, statErr error) error {
	displayPath := secretKeyDisplayPath(keyHostPath)
	if os.IsNotExist(statErr) {
		return fmt.Errorf(
			"SSH private key not found at %s (ssh { key(%q); } in kive.conf); copy your worker private key to %s",
			displayPath,
			keyName,
			displayPath,
		)
	}
	return fmt.Errorf("SSH private key %s (ssh.key in kive.conf): %w", displayPath, statErr)
}

func secretKeyDisplayPath(keyHostPath string) string {
	rel, err := filepath.Rel(bucket.Location, keyHostPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return keyHostPath
	}
	return rel
}

// EnsureSSHStateDir creates the short ControlPath directory for per-worker mux sockets.
// Sockets live under /tmp/kive-ssh (not bucket/tmp/ssh): sockaddr_un.sun_path is ~104
// bytes on macOS, and OpenSSH appends a ~17-char random suffix when binding the listener.
func EnsureSSHStateDir() error {
	return os.MkdirAll(sshControlDir(), 0o700)
}

// SSHClientArgs returns ssh(1) arguments shared by worker commands and rsync --rsh.
// workerIP selects a dedicated control socket path so parallel rsync does not race on ControlMaster.
func SSHClientArgs(keyPath, workerIP string) []string {
	controlPath := controlSocketPath(workerIP)
	strict := "yes"
	port := 22
	if conf, err := bucket.GetKiveConf(); err == nil && bucket.KiveConfExists() {
		if s, err := conf.SSHStrictHostKeyChecking(); err == nil {
			strict = s
		}
		if conf.SSHPort > 0 {
			port = conf.SSHPort
		}
	}

	args := []string{
		"-o", "StrictHostKeyChecking=" + strict,
		"-o", "UserKnownHostsFile=" + KnownHostsPath(),
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "BatchMode=yes",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "PreferredAuthentications=publickey",
		"-o", "KexAlgorithms=curve25519-sha256,curve25519-sha256@libssh.org,diffie-hellman-group-exchange-sha256",
		"-o", "Ciphers=chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com",
		"-o", "MACs=hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com,umac-128-etm@openssh.com",
		"-o", "HostKeyAlgorithms=ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256",
		"-o", fmt.Sprintf("ConnectTimeout=%d", defaultSSHConnectSecs),
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "IdentitiesOnly=yes",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlPath,
		"-o", "ControlPersist=60",
		"-i", keyPath,
	}
	if port != 22 {
		args = append([]string{"-p", strconv.Itoa(port)}, args...)
	}
	return args
}

func sshControlDir() string {
	// Fixed short root — os.TempDir() on macOS is often /var/folders/.../T and eats the budget.
	return filepath.Join("/tmp", "kive-ssh")
}

// controlSocketPath returns a short absolute ControlPath unique to this bucket + worker.
func controlSocketPath(workerIP string) string {
	sum := sha256.Sum256([]byte(bucket.Location + "\x00" + workerIP))
	return filepath.Join(sshControlDir(), hex.EncodeToString(sum[:8]))
}

// shellQuote wraps a value for a single-quoted POSIX shell argument.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func sshTarget(user, workerIP string) string {
	host := workerIP
	if parsed := net.ParseIP(workerIP); parsed != nil && parsed.To4() == nil {
		host = "[" + workerIP + "]"
	}
	return user + "@" + host
}

// remoteShellCommand is the remote command that reads a bash script from stdin.
func remoteShellCommand(useSudo bool) string {
	parts := []string{fmt.Sprintf("timeout %d", defaultSSHTimeoutSecs)}
	if useSudo {
		parts = append(parts, "sudo", "-E", "bash", "-s")
	} else {
		parts = append(parts, "bash", "-s")
	}
	return strings.Join(parts, " ")
}

func ensureCommandScript(hostScriptPath string) error {
	info, err := os.Stat(hostScriptPath)
	if err != nil {
		return fmt.Errorf("command script: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("command script %s is a directory", hostScriptPath)
	}
	if info.Size() == 0 {
		return fmt.Errorf("command script %s is empty", hostScriptPath)
	}
	return nil
}

func remoteExecTimeout() time.Duration {
	return defaultSSHTimeoutSecs * time.Second
}
