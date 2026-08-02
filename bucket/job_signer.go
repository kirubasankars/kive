// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var jobSignerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateJobSignerConfig validates all secret basenames used for job signing.
func ValidateJobSignerConfig(conf KiveConf) error {
	if strings.TrimSpace(conf.JobSignerCA) != "" {
		if _, err := sanitizeJobSignerName(conf.JobSignerCA, "job_signer_ca"); err != nil {
			return err
		}
	}
	seen := map[string]struct{}{}
	for _, name := range conf.JobSignerCATrust {
		clean, err := sanitizeJobSignerName(name, "job_signer_ca_trust")
		if err != nil {
			return err
		}
		if _, ok := seen[clean]; ok {
			return fmt.Errorf("%w: duplicate job_signer_ca_trust entry %q", ErrInvalidKiveConf, clean)
		}
		seen[clean] = struct{}{}
	}
	return nil
}

// JobSignerPaths returns the configured signer certificate and private-key paths.
func JobSignerPaths(conf KiveConf) (certPath, keyPath, signerName string, err error) {
	if strings.TrimSpace(conf.JobSignerCA) == "" {
		return "", "", "", fmt.Errorf("%w: job_signer_ca is not configured", ErrInvalidKiveConf)
	}
	signerName, err = sanitizeJobSignerName(conf.JobSignerCA, "job_signer_ca")
	if err != nil {
		return "", "", "", err
	}
	return filepath.Join(SecretLocation, signerName+".crt"),
		filepath.Join(SecretLocation, signerName+".key"), signerName, nil
}

// LoadJobSignerTrustBundle loads configured public CA certificates.
func LoadJobSignerTrustBundle(conf KiveConf) ([]byte, error) {
	if len(conf.JobSignerCATrust) == 0 {
		return nil, fmt.Errorf("%w: job_signer_ca_trust is not configured", ErrInvalidKiveConf)
	}
	var bundle bytes.Buffer
	for _, configuredName := range conf.JobSignerCATrust {
		name, err := sanitizeJobSignerName(configuredName, "job_signer_ca_trust")
		if err != nil {
			return nil, err
		}
		certPath := filepath.Join(SecretLocation, name+".crt")
		content, err := os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("read trusted job signer %s: %w", certPath, err)
		}
		bundle.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			bundle.WriteByte('\n')
		}
	}
	return bundle.Bytes(), nil
}

// CheckJobSignerPrivateKeyPermissions requires a regular, operator-only key.
func CheckJobSignerPrivateKeyPermissions(keyPath string) error {
	info, err := os.Lstat(keyPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("job signer key must be a regular file: %s", keyPath)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("job signer key %s must have permissions 0600 (got %04o)", keyPath, info.Mode().Perm())
	}
	return nil
}

func sanitizeJobSignerName(name, field string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || filepath.IsAbs(name) || filepath.Base(name) != name ||
		strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) ||
		strings.HasSuffix(name, ".crt") || strings.HasSuffix(name, ".key") ||
		!jobSignerNamePattern.MatchString(name) {
		return "", fmt.Errorf("%w: %s must be a certificate basename under secrets/ without an extension (got %q)", ErrInvalidKiveConf, field, name)
	}
	return name, nil
}
