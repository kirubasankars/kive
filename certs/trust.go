// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package certs

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path"

	"kive/bucket"
)

const (
	// WorkerCATrustKVKey is the KV key for the CA trust bundle on workers.
	WorkerCATrustKVKey = "certs/ca-trust.crt"
)

// CATrustSecretsPath returns the path to secrets/ca-trust.crt.
func CATrustSecretsPath() string {
	return path.Join(bucket.SecretLocation, "ca-trust.crt")
}

// EnsureCATrustFile creates secrets/ca-trust.crt from secrets/ca.crt when absent.
func EnsureCATrustFile() error {
	trustPath := CATrustSecretsPath()
	if _, err := os.Stat(trustPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat ca-trust.crt: %w", err)
	}

	caPEM, err := os.ReadFile(path.Join(bucket.SecretLocation, "ca.crt"))
	if err != nil {
		return fmt.Errorf("read ca.crt for ca-trust.crt bootstrap: %w", err)
	}
	return os.WriteFile(trustPath, caPEM, 0o644)
}

// ReadDedupedCATrustBundle reads secrets/ca-trust.crt and returns a PEM bundle with duplicate certificates removed.
func ReadDedupedCATrustBundle() ([]byte, error) {
	data, err := os.ReadFile(CATrustSecretsPath())
	if err != nil {
		return nil, err
	}
	return DedupPEMCertificates(data)
}

// DedupPEMCertificates returns PEM data with duplicate CERTIFICATE blocks removed (first occurrence kept).
// Non-PEM content is returned unchanged.
func DedupPEMCertificates(pemData []byte) ([]byte, error) {
	if len(bytes.TrimSpace(pemData)) == 0 {
		return pemData, nil
	}

	seen := make(map[[sha256.Size]byte]struct{})
	var out bytes.Buffer
	foundBlock := false

	rest := pemData
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			break
		}
		foundBlock = true
		rest = remaining

		if block.Type != "CERTIFICATE" {
			if err := pem.Encode(&out, block); err != nil {
				return nil, err
			}
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate in trust bundle: %w", err)
		}
		hash := sha256.Sum256(cert.Raw)
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		if err := pem.Encode(&out, block); err != nil {
			return nil, err
		}
	}

	if !foundBlock {
		return pemData, nil
	}

	return out.Bytes(), nil
}
