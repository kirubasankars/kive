// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package initialize creates or upgrades a kive bucket in the current directory.
package initialize

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"path"
	"strings"
	"time"

	"kive/bucket"
	"kive/buildinfo"
	"kive/certs"
	"kive/data"
	"kive/kv"
)

// Execute initializes a new bucket or upgrades an existing one to the latest schema.
func Execute() error {
	return ExecuteContext(context.Background())
}

// ExecuteContext initializes or upgrades a bucket with cancellation checkpoints.
func ExecuteContext(ctx context.Context) error {
	return ExecuteWithBucketIDContext(ctx, "")
}

// ExecuteWithBucketID is like Execute but uses preferredID for a new bucket when non-empty.
func ExecuteWithBucketID(preferredID string) error {
	return ExecuteWithBucketIDContext(context.Background(), preferredID)
}

// ExecuteWithBucketIDContext initializes or upgrades using preferredID with cancellation.
func ExecuteWithBucketIDContext(ctx context.Context, preferredID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureBucketDirectories(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	isNewDatabase := !data.DatabaseExists()

	db, err := data.OpenDatabase(false)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	tx, err := db.Begin()
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	previousVersion, err := data.SchemaVersion(tx)
	if err != nil {
		return err
	}
	if err := data.MigrateSchema(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	schemaCreated := previousVersion == 0

	bucketInitialized, err := data.BucketInitialized(tx)
	if err != nil {
		return err
	}

	var bucketID string
	if isNewDatabase || !bucketInitialized {
		bucketID = strings.TrimSpace(preferredID)
		if bucketID == "" {
			id, err := data.NewPublicID()
			if err != nil {
				return err
			}
			bucketID = id
		} else if !data.IsPublicID(bucketID) {
			return fmt.Errorf("bucket id must be a 6-character [0-9a-z] id")
		}
		if err := data.InsertBucketRecord(tx, bucketID); err != nil {
			return err
		}
		if err := ensureDefaultKiveConfig(); err != nil {
			return err
		}
	} else {
		bucketID, err = data.GetBucketID(tx)
		if err != nil {
			return err
		}
	}

	if err := data.SetInitGitHash(tx, buildinfo.Hash()); err != nil {
		return err
	}

	if err := ensureWorkspaceFiles(); err != nil {
		return err
	}
	if err := ensureGitignore(); err != nil {
		return err
	}
	if err := ensureCA(ctx, bucketID); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := kv.EnsureEncryptionKeyTx(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return bucket.DatabaseError(err)
	}

	if !bucket.QuietCLIOutput() {
		if isNewDatabase || !bucketInitialized {
			fmt.Println("kive bucket initialized")
		} else if schemaCreated {
			fmt.Printf("kive bucket schema ready (version %d)\n", data.LatestSchemaVersion)
		}
	}
	return nil
}

func ensureBucketDirectories() error {
	// templates/ is optional (only `kive job create --template` reads it) and
	// tmp/ is scratch that commands create on demand and prune when done.
	dirs := []string{
		path.Join(bucket.Location, "data"),
		path.Join(bucket.Location, "workspace", "jobs"),
		path.Join(bucket.Location, "logs"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	if err := ensureSecureDirectory(bucket.SecretLocation, 0o700); err != nil {
		return fmt.Errorf("secure secrets directory: %w", err)
	}
	if err := os.MkdirAll(path.Join(bucket.Location, ".ssh"), 0o700); err != nil {
		return fmt.Errorf("create directory .ssh: %w", err)
	}
	knownHosts := path.Join(bucket.Location, ".ssh", "known_hosts")
	if _, err := os.Stat(knownHosts); os.IsNotExist(err) {
		if err := os.WriteFile(knownHosts, nil, 0o600); err != nil {
			return fmt.Errorf("create known_hosts: %w", err)
		}
	}
	return nil
}

func ensureSecureDirectory(dir string, mode os.FileMode) error {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, mode); err != nil {
			return err
		}
		info, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if err := os.Chmod(dir, mode); err != nil {
		return err
	}
	info, err = os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("%s permissions are %04o, want %04o", dir, info.Mode().Perm(), mode.Perm())
	}
	return nil
}

func ensureWorkspaceFiles() error {
	workersConf := path.Join(bucket.WorkspaceLocation, "workers.conf")
	if _, err := os.Stat(workersConf); os.IsNotExist(err) {
		if err := os.WriteFile(workersConf, []byte(""), 0o644); err != nil {
			return fmt.Errorf("create workers.conf: %w", err)
		}
	}

	bucketConf := path.Join(bucket.WorkspaceLocation, "bucket.conf")
	if _, err := os.Stat(bucketConf); os.IsNotExist(err) {
		if err := os.WriteFile(bucketConf, []byte(bucket.DefaultBucketConf()), 0o644); err != nil {
			return fmt.Errorf("create bucket.conf: %w", err)
		}
	}

	bucketJobsConf := path.Join(bucket.WorkspaceLocation, "bucket.jobs.conf")
	if _, err := os.Stat(bucketJobsConf); os.IsNotExist(err) {
		if err := os.WriteFile(bucketJobsConf, []byte(bucket.DefaultBucketJobsConf()), 0o644); err != nil {
			return fmt.Errorf("create bucket.jobs.conf: %w", err)
		}
	}
	return nil
}

const defaultGitignore = "data\nlogs\nsecrets\ntmp\n.ssh\n"

func ensureGitignore() error {
	gitignorePath := path.Join(bucket.Location, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte(defaultGitignore), 0o644); err != nil {
			return fmt.Errorf("create .gitignore: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat .gitignore: %w", err)
	}
	return nil
}

func ensureDefaultKiveConfig() error {
	conf := bucket.KiveConf{
		UseSUDO:                   true,
		SSHUser:                   "agent",
		SSHKeyFile:                "worker.key",
		SSHPort:                   22,
		PortRange:                 "30000,39999",
		CertsTTL:                  60,
		CertsRenewalBuffer:        10,
		Iptables:                  false,
		JobsProfile:               "",
		Timezone:                  bucket.DefaultTimezone,
		HealthPollIntervalSeconds: bucket.DefaultHealthPollIntervalSeconds,
	}
	if err := bucket.WriteKiveConf(&conf); err != nil {
		return fmt.Errorf("write kive.conf: %w", err)
	}
	return nil
}

func ensureCA(ctx context.Context, bucketID string) error {
	caCertPath := path.Join(bucket.SecretLocation, "ca.crt")
	caKeyPath := path.Join(bucket.SecretLocation, "ca.key")
	_, certErr := os.Lstat(caCertPath)
	_, keyErr := os.Lstat(caKeyPath)
	certMissing := os.IsNotExist(certErr)
	keyMissing := os.IsNotExist(keyErr)
	if certMissing && keyMissing {
		if err := generateCA(ctx, bucket.SecretLocation, pkix.Name{CommonName: bucketID}, 10*365); err != nil {
			return err
		}
		return certs.EnsureCATrustFile()
	}
	if certMissing || keyMissing {
		return fmt.Errorf("bucket CA is incomplete: secrets must contain both ca.crt and ca.key")
	}
	if certErr != nil {
		return fmt.Errorf("stat ca.crt: %w", certErr)
	}
	if keyErr != nil {
		return fmt.Errorf("stat ca.key: %w", keyErr)
	}
	if err := secureExistingFile(caCertPath, 0o644); err != nil {
		return fmt.Errorf("secure ca.crt: %w", err)
	}
	if err := secureExistingFile(caKeyPath, 0o600); err != nil {
		return fmt.Errorf("secure ca.key: %w", err)
	}
	return certs.EnsureCATrustFile()
}

func secureExistingFile(filePath string, mode os.FileMode) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a regular file", filePath)
	}
	if err := os.Chmod(filePath, mode); err != nil {
		return err
	}
	info, err = os.Lstat(filePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("%s permissions are %04o, want %04o", filePath, info.Mode().Perm(), mode.Perm())
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func generateCA(ctx context.Context, secretsDir string, subject pkix.Name, ttlDays int) error {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               subject,
		NotBefore:             now,
		NotAfter:              now.Add(time.Duration(ttlDays) * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	random := contextReader{ctx: ctx, r: rand.Reader}
	privateKey, err := rsa.GenerateKey(random, bucket.RSAKeyBits())
	if err != nil {
		return err
	}

	certBytes, err := x509.CreateCertificate(random, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}

	keyPath := path.Join(secretsDir, "ca.key")
	if err := writePEMExclusive(
		keyPath,
		0o600,
		&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)},
	); err != nil {
		return fmt.Errorf("create ca.key: %w", err)
	}

	certPath := path.Join(secretsDir, "ca.crt")
	if err := writePEMExclusive(certPath, 0o644, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		_ = os.Remove(keyPath)
		return fmt.Errorf("create ca.crt: %w", err)
	}
	return nil
}

func writePEMExclusive(filePath string, mode os.FileMode, block *pem.Block) error {
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		_ = os.Remove(filePath)
		return err
	}
	if err := pem.Encode(file, block); err != nil {
		_ = file.Close()
		_ = os.Remove(filePath)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(filePath)
		return err
	}
	return nil
}
