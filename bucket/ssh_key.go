// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultSSHKeyFile is the default private key basename under secrets/.
const DefaultSSHKeyFile = "worker.key"

// SanitizeSSHKeyFilename returns a basename-only SSH key filename under secrets/.
// Empty input defaults to DefaultSSHKeyFile. Absolute paths, ".." segments, and
// path separators are rejected.
func SanitizeSSHKeyFilename(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return DefaultSSHKeyFile, nil
	}
	if filepath.IsAbs(filename) || strings.Contains(filename, "..") {
		return "", fmt.Errorf("%w: ssh_key must be a filename under secrets/ (got %q)", ErrInvalidKiveConf, filename)
	}
	base := filepath.Base(filename)
	if base != filename || base == "." || base == string(filepath.Separator) {
		return "", fmt.Errorf("%w: ssh_key must be a filename under secrets/ (got %q)", ErrInvalidKiveConf, filename)
	}
	return base, nil
}

// SSHKeyHostPath resolves ssh_key from kive.conf to an absolute path under secrets/.
// Returns the absolute path and the sanitized basename.
func SSHKeyHostPath(filename string) (absPath, baseName string, err error) {
	baseName, err = SanitizeSSHKeyFilename(filename)
	if err != nil {
		return "", "", err
	}
	full := filepath.Join(SecretLocation, baseName)
	absSecrets, err := filepath.Abs(SecretLocation)
	if err != nil {
		return "", "", err
	}
	absPath, err = filepath.Abs(full)
	if err != nil {
		return "", "", err
	}
	if absPath != absSecrets && !strings.HasPrefix(absPath, absSecrets+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("%w: ssh_key path escapes secrets/", ErrInvalidKiveConf)
	}
	return absPath, baseName, nil
}
