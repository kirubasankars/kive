// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package kv

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"

	"kive/bucket"
	"kive/data"
)

const encryptionKeyFile = "kv.key"
const encryptionKeySize = 32

var encryptionKeyCache struct {
	sync.Mutex
	key    []byte
	path   string
	loaded bool
}

// EnsureEncryptionKey creates secrets/kv.key when missing (32-byte AES-256 key).
// If encrypted values already exist in kive.db, a missing key returns ErrEncryptionKeyMissing
// instead of generating a new key that could not decrypt existing secrets.
func EnsureEncryptionKey() error {
	return EnsureEncryptionKeyTx(nil)
}

// EnsureEncryptionKeyTx is like EnsureEncryptionKey but reads encrypted-value state from tx
// when provided. Pass the open init transaction so an uncommitted schema is visible.
func EnsureEncryptionKeyTx(tx *sql.Tx) error {
	keyPath := path.Join(bucket.SecretLocation, encryptionKeyFile)
	if _, err := os.Lstat(keyPath); err == nil {
		return secureEncryptionKeyFile(keyPath)
	} else if !os.IsNotExist(err) {
		return bucket.UnexpectedError(err)
	}

	hasEncrypted, err := HasPersistedEncryptedValuesTx(tx)
	if err != nil {
		return err
	}
	if hasEncrypted {
		return ErrEncryptionKeyMissing
	}

	key := make([]byte, encryptionKeySize)
	if _, err := rand.Read(key); err != nil {
		return bucket.UnexpectedError(err)
	}
	if err := writeEncryptionKeyExclusive(keyPath, key); err != nil {
		return bucket.UnexpectedError(err)
	}
	if err := secureEncryptionKeyFile(keyPath); err != nil {
		return err
	}
	return nil
}

func writeEncryptionKeyExclusive(keyPath string, key []byte) error {
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(keyPath)
		return err
	}
	n, err := file.Write(key)
	if err != nil || n != len(key) {
		_ = file.Close()
		_ = os.Remove(keyPath)
		if err != nil {
			return err
		}
		return fmt.Errorf("short write: wrote %d of %d bytes", n, len(key))
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(keyPath)
		return err
	}
	return nil
}

func secureEncryptionKeyFile(keyPath string) error {
	info, err := os.Lstat(keyPath)
	if err != nil {
		return bucket.UnexpectedError(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return bucket.UnexpectedError(fmt.Errorf("%s is not a regular file", keyPath))
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return bucket.UnexpectedError(err)
	}
	info, err = os.Lstat(keyPath)
	if err != nil {
		return bucket.UnexpectedError(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return bucket.UnexpectedError(fmt.Errorf("%s permissions are %04o, want 0600", keyPath, info.Mode().Perm()))
	}
	return nil
}

func loadEncryptionKey() ([]byte, error) {
	encryptionKeyCache.Lock()
	defer encryptionKeyCache.Unlock()

	keyPath := path.Join(bucket.SecretLocation, encryptionKeyFile)
	if encryptionKeyCache.loaded && encryptionKeyCache.path == keyPath {
		if len(encryptionKeyCache.key) != encryptionKeySize {
			return nil, ErrEncryptionKeyMissing
		}
		return encryptionKeyCache.key, nil
	}

	key, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrEncryptionKeyMissing
		}
		return nil, bucket.UnexpectedError(err)
	}
	if len(key) != encryptionKeySize {
		return nil, ErrEncryptionKeyMissing
	}

	encryptionKeyCache.key = key
	encryptionKeyCache.path = keyPath
	encryptionKeyCache.loaded = true
	return key, nil
}

// HasPersistedEncryptedValues reports whether kive.db contains a non-deleted
// key_value row whose latest version uses the encrypted value prefix.
func HasPersistedEncryptedValues() (bool, error) {
	return HasPersistedEncryptedValuesTx(nil)
}

// HasPersistedEncryptedValuesTx is like HasPersistedEncryptedValues but reads from tx when provided.
func HasPersistedEncryptedValuesTx(tx *sql.Tx) (bool, error) {
	if tx == nil && !data.DatabaseExists() {
		return false, nil
	}

	const query = `
		SELECT 1
		FROM key_value AS outer_kv
		WHERE outer_kv.value LIKE ?
		AND outer_kv.deleted = 0
		AND outer_kv.version = (
			SELECT MAX(inner_kv.version)
			FROM key_value AS inner_kv
			WHERE inner_kv.namespace = outer_kv.namespace AND inner_kv.key = outer_kv.key
		)
		LIMIT 1`

	if tx != nil {
		var exists int
		err := tx.QueryRow(query, encryptedValuePrefix+"%").Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, bucket.DatabaseError(err)
		}
		return true, nil
	}

	db, err := data.OpenDatabase(true)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = db.Close()
	}()

	var exists int
	err = db.QueryRow(query, encryptedValuePrefix+"%").Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, bucket.DatabaseError(err)
	}
	return true, nil
}

// ResetEncryptionKeyCacheForTest clears the in-process encryption key cache.
func ResetEncryptionKeyCacheForTest() {
	encryptionKeyCache.Lock()
	encryptionKeyCache.key = nil
	encryptionKeyCache.path = ""
	encryptionKeyCache.loaded = false
	encryptionKeyCache.Unlock()
}
