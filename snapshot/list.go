// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package snapshot

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path"
	"sort"
	"strconv"

	"kive/bucket"
	"kive/utils"

	"github.com/jedib0t/go-pretty/v6/table"
)

// Entry is one remote catalog backup file on a kive-labeled worker.
type Entry struct {
	WorkerIP   string
	Name       string
	Generation int // 0 if the filename is not kive-<digits>.db
	Path       string
}

// List returns generation-named kive.db backups on workers labeled "kive",
// highest seq first.
func List(ctx context.Context, rt *bucket.Runtime, db *sql.DB) ([]Entry, error) {
	bucketID, _, targets, err := kiveTargets(db)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		log.Printf("snapshot list: no workers with label %q", kiveLabel)
		return nil, nil
	}

	backupsDir := bucket.WorkerBackupsPath(bucketID)
	var (
		entries []Entry
		errs    []error
	)
	for _, workerIP := range targets {
		names, err := listRemoteBackupNames(ctx, rt, workerIP, backupsDir)
		if err != nil {
			errs = append(errs, fmt.Errorf("worker %s: %w", workerIP, err))
			continue
		}
		for _, name := range names {
			e := Entry{
				WorkerIP: workerIP,
				Name:     name,
				Path:     path.Join(backupsDir, name),
			}
			if seq, ok := parseBackupSeq(name); ok {
				e.Generation = seq
			}
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		ai, aj := entries[i], entries[j]
		if ai.Generation != aj.Generation {
			return ai.Generation > aj.Generation
		}
		if ai.Name != aj.Name {
			return ai.Name > aj.Name
		}
		return ai.WorkerIP < aj.WorkerIP
	})
	if err := joinErrors("snapshot list failed", errs); err != nil {
		return entries, err
	}
	return entries, nil
}

// Print writes entries as a table to stdout.
func Print(entries []Entry) {
	t := utils.GetTable(table.Row{"Worker", "Generation", "Name", "Path"})
	for _, e := range entries {
		seq := "-"
		if e.Generation > 0 || (e.Generation == 0 && parseBackupSeqOk(e.Name)) {
			seq = strconv.Itoa(e.Generation)
		}
		t.AppendRow(table.Row{e.WorkerIP, seq, e.Name, e.Path})
	}
	t.Render()
}

func parseBackupSeqOk(name string) bool {
	_, ok := parseBackupSeq(name)
	return ok
}
