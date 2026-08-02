// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package cat

import (
	"kive/buildinfo"
	"kive/data"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
)

func Info() error {
	db, err := data.OpenDatabase(true)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	bucketID, err := data.GetBucketID(tx)
	if err != nil {
		return err
	}

	generation, err := data.GetBucketGeneration(tx)
	if err != nil {
		return err
	}

	workers, err := data.GetWorkers(tx, nil)
	if err != nil {
		return err
	}

	jobs, err := data.GetJobs(tx)
	if err != nil {
		return err
	}

	allocations, err := data.CountAllocations(tx, true)
	if err != nil {
		return err
	}
	bundleMeta, err := data.GetBundleMeta(tx)
	if err != nil {
		return err
	}
	timestamps, err := data.GetBucketTimestamps(tx)
	if err != nil {
		return err
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Description", "Value"})
	t.SetStyle(table.StyleRounded)
	t.AppendRow(table.Row{"Kive Build", buildinfo.Hash()})
	t.AppendRow(table.Row{"Bucket ID", bucketID})
	t.AppendRow(table.Row{"Created At", timestamps.CreatedAt})
	t.AppendRow(table.Row{"Initialized At", timestamps.InitializedAt})
	t.AppendRow(table.Row{"Initialized By", bundleMeta.InitGitHash})
	t.AppendRow(table.Row{"Content Built By", bundleMeta.BuildGitHash})
	t.AppendRow(table.Row{"Bundle Version", bundleMeta.BundleVersion})
	t.AppendRow(table.Row{"Generation", generation})
	t.AppendRow(table.Row{"Number of Workers", len(workers)})
	t.AppendRow(table.Row{"Number of Jobs", len(jobs)})
	t.AppendRow(table.Row{"Number of Allocations", allocations})
	t.Render()

	return nil
}
