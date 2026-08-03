// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	"kive/bucket"

	_ "github.com/mattn/go-sqlite3"
)

const sqliteBusyTimeoutMS = 5000

// DatabasePath returns the path to kive.db under the bucket data directory.
func DatabasePath() string {
	return DatabasePathAt(bucket.Location)
}

// DatabasePathAt returns the path to kive.db under root's data directory.
func DatabasePathAt(root string) string {
	return path.Join(root, "data", "kive.db")
}

// DatabaseExists reports whether the bucket database file is present.
func DatabaseExists() bool {
	_, err := os.Stat(DatabasePath())
	return err == nil
}

// BucketFilesPresent reports whether both kive.conf and data/kive.db exist.
func BucketFilesPresent() bool {
	return bucket.KiveConfExists() && DatabaseExists()
}

// OpenSQLite opens a SQLite database with foreign-key enforcement enabled.
// extraQuery is an optional URL query string (without "?"), for example
// "_busy_timeout=5000&_journal_mode=WAL" or "mode=ro&_query_only=1".
func OpenSQLite(dbPath string, extraQuery string) (*sql.DB, error) {
	dsn, err := sqliteDSN(dbPath, extraQuery)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, bucket.DatabaseError(err)
	}
	return db, nil
}

func sqliteDSN(dbPath string, extraQuery string) (string, error) {
	q := url.Values{}
	q.Set("_foreign_keys", "on")
	if strings.TrimSpace(extraQuery) != "" {
		parsed, err := url.ParseQuery(extraQuery)
		if err != nil {
			return "", bucket.DatabaseError(fmt.Errorf("sqlite DSN query: %w", err))
		}
		for key, values := range parsed {
			for _, value := range values {
				q.Add(key, value)
			}
		}
		if q.Get("_foreign_keys") == "" {
			q.Set("_foreign_keys", "on")
		}
	}
	return fmt.Sprintf("file:%s?%s", dbPath, q.Encode()), nil
}

// OpenDatabase opens the bucket SQLite database for the process Location.
// When requireExists is true, returns bucket.ErrNotInitialized if kive.db is missing.
func OpenDatabase(requireExists bool) (*sql.DB, error) {
	return OpenDatabaseAt(bucket.Location, requireExists)
}

// OpenDatabaseAt opens the SQLite database under root without mutating bucket.Location.
// When requireExists is true, returns bucket.ErrNotInitialized if kive.db is missing.
func OpenDatabaseAt(root string, requireExists bool) (*sql.DB, error) {
	dbPath := DatabasePathAt(root)
	if requireExists {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			return nil, bucket.ErrNotInitialized
		}
	}

	return OpenSQLite(dbPath, fmt.Sprintf("_busy_timeout=%d&_journal_mode=WAL", sqliteBusyTimeoutMS))
}

// Vacuum rebuilds kive.db to reclaim free pages. Must not run inside an open transaction.
func Vacuum(db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		return bucket.DatabaseError(err)
	}
	return nil
}
