// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package jobsign signs and verifies immutable snapshots of workspace jobs.
package jobsign

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kive/bucket"
	"kive/workspace"
)

const (
	// SignatureFileName is the certificate bundle stored in each signed job.
	SignatureFileName = ".kive-job.crt"
	formatVersion     = 1
	hashAlgorithm     = "SHA-256"
	canonicalPrefix   = "kive-job-signature-v1\x00"
)

var digestExtensionOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}

// File is one captured job path. Path is relative to workspace/jobs and starts
// with the job name, matching the job_files catalog representation.
type File struct {
	Path    string
	Content []byte
	IsDir   bool
}

// Snapshot is a single, immutable read of a job directory.
type Snapshot struct {
	JobName string
	Files   []File
}

// Verification is the provenance extracted from a valid job certificate.
type Verification struct {
	Status string
	Signer string
	Digest string
}

type digestExtension struct {
	Version   int
	Job       string
	Algorithm string
	Digest    []byte
}

// Capture reads a job once, rejects links and special files, and applies the
// same dependency/cache exclusions used by normal workspace job walks.
func Capture(jobName string) (Snapshot, error) {
	if err := workspace.ValidateJobName(jobName); err != nil {
		return Snapshot{}, err
	}
	root := workspace.JobFilePath(jobName)
	files := make([]File, 0)
	err := filepath.WalkDir(root, func(absPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relToRoot, err := filepath.Rel(root, absPath)
		if err != nil {
			return err
		}
		if relToRoot != "." && workspace.ShouldSkipJobWalk(relToRoot, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			display := "workspace/jobs/" + jobName
			if relToRoot != "." {
				display += "/" + filepath.ToSlash(relToRoot)
			}
			return fmt.Errorf("symlink not allowed: %s", display)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("job %s contains unsupported non-regular file %s", jobName, filepath.ToSlash(relToRoot))
		}
		storePath := jobName
		if relToRoot != "." {
			storePath += "/" + filepath.ToSlash(relToRoot)
		}
		content := []byte{}
		if info.Mode().IsRegular() {
			content, err = os.ReadFile(absPath)
			if err != nil {
				return err
			}
		}
		files = append(files, File{Path: storePath, Content: content, IsDir: info.IsDir()})
		return nil
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("capture job %s: %w", jobName, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Snapshot{JobName: jobName, Files: files}, nil
}

// ManifestBytes returns the manifest from this snapshot, transpiling .bt in
// exactly the same way as the workspace loader.
func (s Snapshot) ManifestBytes() ([]byte, error) {
	plain, hasPlain := s.file(workspace.JobConfName)
	template, hasTemplate := s.file(workspace.JobConfBTName)
	if hasPlain && hasTemplate {
		return nil, fmt.Errorf("job %s has both job.conf and job.conf.bt", s.JobName)
	}
	if hasPlain {
		return append([]byte(nil), plain...), nil
	}
	if hasTemplate {
		return []byte(workspace.Transpile(string(template), workspace.JobTemplateVarsForName(s.JobName))), nil
	}
	return nil, fmt.Errorf("job %s job.conf not found", s.JobName)
}

// Signature returns the job's certificate bundle and whether the file exists.
func (s Snapshot) Signature() ([]byte, bool) {
	target := s.JobName + "/" + SignatureFileName
	for _, file := range s.Files {
		if !file.IsDir && file.Path == target {
			return append([]byte(nil), file.Content...), true
		}
	}
	return nil, false
}

func (s Snapshot) file(rel string) ([]byte, bool) {
	target := s.JobName + "/" + rel
	for _, file := range s.Files {
		if !file.IsDir && file.Path == target {
			return file.Content, true
		}
	}
	return nil, false
}

// Digest calculates the canonical SHA-256 digest of regular job files,
// excluding the signature bundle itself.
func (s Snapshot) Digest() ([sha256.Size]byte, error) {
	var canonical bytes.Buffer
	canonical.WriteString(canonicalPrefix)
	for _, file := range s.Files {
		if file.IsDir || file.Path == s.JobName+"/"+SignatureFileName {
			continue
		}
		rel := strings.TrimPrefix(file.Path, s.JobName+"/")
		if rel == file.Path || rel == "" {
			return [sha256.Size]byte{}, fmt.Errorf("job %s has invalid snapshot path %q", s.JobName, file.Path)
		}
		writeLengthPrefixed(&canonical, []byte(rel))
		writeLengthPrefixed(&canonical, file.Content)
	}
	return sha256.Sum256(canonical.Bytes()), nil
}

func writeLengthPrefixed(dst *bytes.Buffer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	dst.Write(length[:])
	dst.Write(value)
}

// Sign creates a per-job leaf certificate and returns a PEM bundle containing
// that leaf followed by the configured signer chain.
func Sign(snapshot Snapshot, signerCertPEM, signerKeyPEM []byte) ([]byte, Verification, error) {
	signerChain, err := parseCertificates(signerCertPEM)
	if err != nil {
		return nil, Verification{}, fmt.Errorf("parse signer certificate: %w", err)
	}
	signer := signerChain[0]
	if !signer.IsCA || signer.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, Verification{}, errors.New("signer certificate is not a certificate authority")
	}
	now := time.Now().UTC()
	if now.Before(signer.NotBefore) || now.After(signer.NotAfter) {
		return nil, Verification{}, fmt.Errorf("signer certificate is not valid at %s", now.Format(time.RFC3339))
	}
	privateKey, err := parseRSAPrivateKey(signerKeyPEM)
	if err != nil {
		return nil, Verification{}, err
	}
	if !publicKeysEqual(&privateKey.PublicKey, signer.PublicKey) {
		return nil, Verification{}, errors.New("signer private key does not match signer certificate")
	}
	digest, err := snapshot.Digest()
	if err != nil {
		return nil, Verification{}, err
	}
	extensionValue, err := asn1.Marshal(digestExtension{
		Version: formatVersion, Job: snapshot.JobName, Algorithm: hashAlgorithm, Digest: digest[:],
	})
	if err != nil {
		return nil, Verification{}, fmt.Errorf("encode job digest extension: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, Verification{}, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, Verification{}, fmt.Errorf("generate job certificate key: %w", err)
	}
	notBefore := now.Add(-5 * time.Minute)
	if signer.NotBefore.After(notBefore) {
		notBefore = signer.NotBefore
	}
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: snapshot.JobName},
		NotBefore:             notBefore,
		NotAfter:              signer.NotAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  false,
		ExtraExtensions: []pkix.Extension{{
			Id: digestExtensionOID, Value: extensionValue,
		}},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, signer, &leafKey.PublicKey, privateKey)
	if err != nil {
		return nil, Verification{}, fmt.Errorf("create job certificate: %w", err)
	}
	var bundle bytes.Buffer
	if err := pem.Encode(&bundle, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return nil, Verification{}, err
	}
	bundle.Write(signerCertPEM)
	return bundle.Bytes(), Verification{
		Status: "signed", Signer: signerName(signer), Digest: hex.EncodeToString(digest[:]),
	}, nil
}

// Verify checks the job certificate chain and compares its signed digest with
// the captured job bytes.
func Verify(snapshot Snapshot, rootsPEM []byte) (Verification, error) {
	return verifyAt(snapshot, rootsPEM, time.Now().UTC())
}

func verifyAt(snapshot Snapshot, rootsPEM []byte, currentTime time.Time) (Verification, error) {
	bundle, present := snapshot.Signature()
	if !present {
		return Verification{Status: "unsigned"}, nil
	}
	if len(bundle) == 0 {
		return Verification{}, fmt.Errorf("job %s signature file is empty", snapshot.JobName)
	}
	certificates, err := parseCertificates(bundle)
	if err != nil {
		return Verification{}, fmt.Errorf("job %s signature: %w", snapshot.JobName, err)
	}
	roots, err := certificatePool(rootsPEM)
	if err != nil {
		return Verification{}, fmt.Errorf("trusted signer certificates: %w", err)
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certificates[1:] {
		intermediates.AddCert(cert)
	}
	chains, err := certificates[0].Verify(x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		CurrentTime: currentTime,
	})
	if err != nil {
		return Verification{}, fmt.Errorf("job %s signer is not trusted: %w", snapshot.JobName, err)
	}
	metadata, err := signedMetadata(certificates[0])
	if err != nil {
		return Verification{}, fmt.Errorf("job %s signature metadata: %w", snapshot.JobName, err)
	}
	if metadata.Version != formatVersion {
		return Verification{}, fmt.Errorf("unsupported signing format %d; re-sign job %s", metadata.Version, snapshot.JobName)
	}
	if metadata.Algorithm != hashAlgorithm {
		return Verification{}, fmt.Errorf("unsupported signing hash algorithm %q", metadata.Algorithm)
	}
	if metadata.Job != snapshot.JobName {
		return Verification{}, fmt.Errorf("certificate is for job %q, not %q", metadata.Job, snapshot.JobName)
	}
	digest, err := snapshot.Digest()
	if err != nil {
		return Verification{}, err
	}
	if !bytes.Equal(metadata.Digest, digest[:]) {
		return Verification{}, fmt.Errorf("job %s content does not match its vendor signature", snapshot.JobName)
	}
	signer := certificates[0]
	if len(chains) > 0 && len(chains[0]) > 1 {
		signer = chains[0][1]
	}
	return Verification{
		Status: "signed", Signer: signerName(signer), Digest: hex.EncodeToString(digest[:]),
	}, nil
}

func signedMetadata(cert *x509.Certificate) (digestExtension, error) {
	var found *pkix.Extension
	for i := range cert.Extensions {
		if cert.Extensions[i].Id.Equal(digestExtensionOID) {
			if found != nil {
				return digestExtension{}, errors.New("duplicate digest extension")
			}
			found = &cert.Extensions[i]
		}
	}
	if found == nil {
		return digestExtension{}, errors.New("digest extension is missing")
	}
	var metadata digestExtension
	rest, err := asn1.Unmarshal(found.Value, &metadata)
	if err != nil || len(rest) != 0 {
		return digestExtension{}, errors.New("digest extension is malformed")
	}
	return metadata, nil
}

func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	var certificates []*x509.Certificate
	rest := data
	for len(bytes.TrimSpace(rest)) > 0 {
		block, next := pem.Decode(rest)
		if block == nil {
			return nil, errors.New("invalid PEM data")
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("unexpected PEM block %q", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, cert)
		rest = next
	}
	if len(certificates) == 0 {
		return nil, errors.New("no certificates found")
	}
	return certificates, nil
}

func certificatePool(data []byte) (*x509.CertPool, error) {
	certificates, err := parseCertificates(data)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	for _, cert := range certificates {
		pool.AddCert(cert)
	}
	return pool, nil
}

func parseRSAPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("signer key must contain exactly one PEM private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("signer key is not a valid RSA private key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("signer key is not an RSA private key")
	}
	return key, nil
}

func publicKeysEqual(expected *rsa.PublicKey, actual any) bool {
	key, ok := actual.(*rsa.PublicKey)
	return ok && key.E == expected.E && key.N.Cmp(expected.N) == 0
}

func signerName(cert *x509.Certificate) string {
	if name := strings.TrimSpace(cert.Subject.CommonName); name != "" {
		return name
	}
	return cert.Subject.String()
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	return serial, nil
}

// GenerateCA creates a self-signed RSA signing CA without overwriting an
// existing or incomplete certificate/key pair.
func GenerateCA(certPath, keyPath, commonName string, now time.Time) error {
	for _, filePath := range []string{certPath, keyPath} {
		if _, err := os.Lstat(filePath); err == nil {
			return fmt.Errorf("refusing to overwrite existing signer file %s", filePath)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, bucket.RSAKeyBits())
	if err != nil {
		return fmt.Errorf("generate signer key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	now = now.UTC()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("create signer certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}
	certTmp, err := writeTemp(filepath.Dir(certPath), ".job-signer-*.crt", certPEM, 0o644)
	if err != nil {
		return err
	}
	defer os.Remove(certTmp)
	keyTmp, err := writeTemp(filepath.Dir(keyPath), ".job-signer-*.key", keyPEM, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(keyTmp)
	if err := os.Rename(certTmp, certPath); err != nil {
		return err
	}
	if err := os.Rename(keyTmp, keyPath); err != nil {
		_ = os.Remove(certPath)
		return err
	}
	return nil
}

// AtomicWriteSignature safely replaces a job signature bundle.
func AtomicWriteSignature(jobName string, bundle []byte) (string, error) {
	dir := workspace.JobFilePath(jobName)
	tmp, err := writeTemp(dir, ".kive-job-*.crt", bundle, 0o644)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	target := filepath.Join(dir, SignatureFileName)
	if err := os.Rename(tmp, target); err != nil {
		return "", err
	}
	return target, nil
}

func writeTemp(dir, pattern string, content []byte, mode fs.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := file.Write(content); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
