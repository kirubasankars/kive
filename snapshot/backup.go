// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package snapshot

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kive/bucket"
	"kive/data"

	"github.com/mattn/go-sqlite3"
)

const (
	kiveLabel       = "kive"
	backupFilePrefix = "kive-"
	backupFileSuffix = ".db"
)

// Backup pushes a consistent kive.db snapshot to workers labeled "kive", then
// prunes remote backups beyond backup_retention_count (by generation).
func Backup(ctx context.Context, rt *bucket.Runtime, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}

	bucketID, generation, targets, err := kiveTargets(db)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		return nil
	}

	retentionCount, err := bucket.BackupRetentionCountFromConfig()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(bucket.TempLocation, 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}
	defer bucket.PruneTempDir()
	localSnap := path.Join(bucket.TempLocation, "kive.db.snapshot")
	_ = os.Remove(localSnap)
	if err := SnapshotDatabaseContext(ctx, db, localSnap); err != nil {
		return err
	}
	defer func() { _ = os.Remove(localSnap) }()

	remoteName := backupFileName(generation)
	var errs []error
	for _, workerIP := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := pushAndPrune(ctx, rt, bucketID, workerIP, localSnap, remoteName, retentionCount); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			errs = append(errs, fmt.Errorf("worker %s: %w", workerIP, err))
		}
	}
	return joinErrors("snapshot backup failed", errs)
}

func pushAndPrune(ctx context.Context, rt *bucket.Runtime, bucketID, workerIP, localSnap, remoteName string, retentionCount int) error {
	backupsDir := bucket.WorkerBackupsPath(bucketID)
	mkdirCmd := fmt.Sprintf("mkdir -p %s", backupsDir)
	if err := runWorkerCommand(ctx, rt, workerIP, bucket.CommandContext{
		Phase:  "backup",
		Action: "mkdir",
		Cmd:    mkdirCmd,
	}, []string{mkdirCmd}, nil); err != nil {
		return err
	}

	remotePath := path.Join(backupsDir, remoteName)
	if err := rsyncBackupFile(ctx, rt, workerIP, localSnap, remotePath); err != nil {
		return err
	}
	log.Printf("snapshot backup: copied %s to worker %s (%s)", remoteName, workerIP, remotePath)

	return pruneRemoteBackups(ctx, rt, workerIP, backupsDir, retentionCount)
}

func pruneRemoteBackups(ctx context.Context, rt *bucket.Runtime, workerIP, backupsDir string, retentionCount int) error {
	if retentionCount <= 0 {
		return nil
	}

	names, err := listRemoteBackupNames(ctx, rt, workerIP, backupsDir)
	if err != nil {
		return err
	}
	toDelete := backupFilesToDelete(names, retentionCount)
	if len(toDelete) == 0 {
		return nil
	}

	args := make([]string, 0, len(toDelete))
	for _, name := range toDelete {
		args = append(args, shellQuote(path.Join(backupsDir, name)))
	}
	rmCmd := "rm -f " + strings.Join(args, " ")
	return runWorkerCommand(ctx, rt, workerIP, bucket.CommandContext{
		Phase:  "backup",
		Action: "prune",
		Cmd:    rmCmd,
	}, []string{rmCmd}, nil)
}

func listRemoteBackupNames(ctx context.Context, rt *bucket.Runtime, workerIP, backupsDir string) ([]string, error) {
	listCmd := fmt.Sprintf("ls -1 %s 2>/dev/null || true", shellQuote(backupsDir))
	out, err := runWorkerCommandOutput(ctx, rt, workerIP, bucket.CommandContext{
		Phase:  "backup",
		Action: "list",
		Cmd:    listCmd,
	}, []string{listCmd})
	if err != nil {
		return nil, err
	}

	var names []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		base := filepath.Base(name)
		if !strings.HasPrefix(base, backupFilePrefix) || !strings.HasSuffix(base, backupFileSuffix) {
			continue
		}
		names = append(names, base)
	}
	return names, nil
}

func kiveTargets(db *sql.DB) (bucketID string, generation int, targets []string, err error) {
	if db == nil {
		return "", 0, nil, fmt.Errorf("database is required")
	}
	tx, err := db.Begin()
	if err != nil {
		return "", 0, nil, bucket.DatabaseError(err)
	}
	defer func() { _ = tx.Rollback() }()

	bucketID, err = data.GetBucketID(tx)
	if err != nil {
		return "", 0, nil, err
	}
	generation, err = data.GetBucketGeneration(tx)
	if err != nil {
		return "", 0, nil, err
	}
	targets, err = data.GetWorkers(tx, []string{kiveLabel})
	if err != nil {
		return "", 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, nil, bucket.DatabaseError(err)
	}
	return bucketID, generation, targets, nil
}

func backupFileName(generation int) string {
	return backupFilePrefix + strconv.Itoa(generation) + backupFileSuffix
}

// backupFilesToDelete returns kive-*.db names to remove when keeping the highest
// `keep` generation backups. Non-seq kive-*.db files (e.g. old timestamp names)
// are always deleted when keep > 0.
func backupFilesToDelete(names []string, keep int) []string {
	if keep <= 0 {
		return nil
	}

	type seqName struct {
		seq  int
		name string
	}
	var (
		seqFiles []seqName
		out      []string
	)
	for _, name := range names {
		seq, ok := parseBackupSeq(name)
		if !ok {
			base := filepath.Base(name)
			if strings.HasPrefix(base, backupFilePrefix) && strings.HasSuffix(base, backupFileSuffix) {
				out = append(out, name)
			}
			continue
		}
		seqFiles = append(seqFiles, seqName{seq: seq, name: name})
	}
	sort.Slice(seqFiles, func(i, j int) bool {
		if seqFiles[i].seq != seqFiles[j].seq {
			return seqFiles[i].seq > seqFiles[j].seq
		}
		return seqFiles[i].name > seqFiles[j].name
	})
	if len(seqFiles) > keep {
		for _, sn := range seqFiles[keep:] {
			out = append(out, sn.name)
		}
	}
	return out
}

func parseBackupSeq(name string) (int, bool) {
	base := filepath.Base(name)
	if !strings.HasPrefix(base, backupFilePrefix) || !strings.HasSuffix(base, backupFileSuffix) {
		return 0, false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(base, backupFilePrefix), backupFileSuffix)
	if mid == "" {
		return 0, false
	}
	for _, r := range mid {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	seq, err := strconv.Atoi(mid)
	if err != nil {
		return 0, false
	}
	return seq, true
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func joinErrors(prefix string, errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", prefix, strings.Join(parts, "; "))
}

// SnapshotDatabase writes a consistent copy of the open catalog DB to dstPath
// using the SQLite online backup API.
func SnapshotDatabase(db *sql.DB, dstPath string) error {
	return SnapshotDatabaseContext(context.Background(), db, dstPath)
}

// SnapshotDatabaseContext writes a consistent copy in cancellable page batches.
func SnapshotDatabaseContext(ctx context.Context, db *sql.DB, dstPath string) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if strings.TrimSpace(dstPath) == "" {
		return fmt.Errorf("destination path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return bucket.UnexpectedError(err)
	}
	_ = os.Remove(dstPath)

	dstDB, err := data.OpenSQLite(dstPath, "")
	if err != nil {
		return err
	}
	defer func() { _ = dstDB.Close() }()

	srcConn, err := db.Conn(ctx)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() { _ = srcConn.Close() }()

	dstConn, err := dstDB.Conn(ctx)
	if err != nil {
		return bucket.DatabaseError(err)
	}
	defer func() { _ = dstConn.Close() }()

	err = srcConn.Raw(func(srcDC any) error {
		return dstConn.Raw(func(dstDC any) error {
			srcSQLite, ok := srcDC.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected sqlite source driver type %T", srcDC)
			}
			dstSQLite, ok := dstDC.(*sqlite3.SQLiteConn)
			if !ok {
				return fmt.Errorf("unexpected sqlite dest driver type %T", dstDC)
			}
			bk, err := dstSQLite.Backup("main", srcSQLite, "main")
			if err != nil {
				return err
			}
			defer func() { _ = bk.Finish() }()
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				done, stepErr := bk.Step(128)
				if stepErr != nil {
					return stepErr
				}
				if done {
					break
				}
			}
			return nil
		})
	})
	if err != nil {
		_ = os.Remove(dstPath)
		return bucket.DatabaseError(err)
	}
	return nil
}
