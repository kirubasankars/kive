// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package gc

import (
	"database/sql"
	"log"
	"strings"

	"kive/bucket"
	"kive/data"
	"kive/sourcerevision"
)

// HistoryOptions carries optional pins for source-revision GC.
type HistoryOptions struct {
	SourceRevisionPins []string
}

// CollectSourceRevisionHistoryPins returns merged pins used to retain source
// revision artifacts. It does not prune history or revisions; callers that need
// full GC should use pruneHistory / PruneSourceRevisionsAndHistory.
//
// Deploy and promotion history contribute at most the newest RetentionCount
// entries' source_revision values (plus last_source_revision). Marker pins and
// (when any fifo marker exists) all promotable deploy revisions are included in full.
func CollectSourceRevisionHistoryPins(tx *sql.Tx, bucketDir string, peerPins []string) ([]string, error) {
	state, err := data.LoadPromotionState(bucketDir)
	if err != nil {
		return nil, err
	}
	promotionPins := promotionHistoryPinRevisions(state, sourcerevision.RetentionCount)

	deployPins, err := deployHistoryPinRevisions(tx, sourcerevision.RetentionCount)
	if err != nil {
		return nil, err
	}
	markerPins, err := sourcerevision.MarkerSourceRevisions(bucketDir)
	if err != nil {
		return nil, err
	}

	pins := MergeSourceRevisionPins(promotionPins, deployPins, peerPins, markerPins)

	hasFifo, err := sourcerevision.HasFifoMarker(bucketDir)
	if err != nil {
		return nil, err
	}
	if hasFifo {
		promotable, err := data.PromotableSourceRevisions(tx)
		if err != nil {
			return nil, err
		}
		pins = MergeSourceRevisionPins(pins, promotable)
	}
	return pins, nil
}

func promotionHistoryPinRevisions(state data.PromotionState, newest int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	add := func(hash string) {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			return
		}
		if _, ok := seen[hash]; ok {
			return
		}
		seen[hash] = struct{}{}
		out = append(out, hash)
	}
	add(state.LastSourceRevision)
	limit := newest
	if limit < 1 {
		limit = data.PromotionHistoryRetention
	}
	for i, entry := range state.History {
		if i >= limit {
			break
		}
		add(entry.SourceRevision)
	}
	return out
}

func deployHistoryPinRevisions(tx *sql.Tx, newest int) ([]string, error) {
	if newest < 1 {
		newest = data.DeployHistoryRetention
	}
	entries, err := data.ListDeployHistory(tx, newest, nil)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, entry := range entries {
		hash := strings.TrimSpace(entry.SourceRevision)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		out = append(out, hash)
	}
	return out, nil
}

// pruneHistory reconciles source-revision artifacts then prunes deploy/promotion
// history for revisions that no longer exist.
func pruneHistory(tx *sql.Tx, bucketDir string, opts HistoryOptions) error {
	return PruneSourceRevisionsAndHistory(tx, bucketDir, opts.SourceRevisionPins)
}

// PruneSourceRevisionsAndHistory computes pins, prunes revision dirs, then
// drops deploy/promotion history rows for gone revisions.
func PruneSourceRevisionsAndHistory(tx *sql.Tx, bucketDir string, peerPins []string) error {
	pins, err := CollectSourceRevisionHistoryPins(tx, bucketDir, peerPins)
	if err != nil {
		return err
	}

	hasMarkers, err := sourcerevision.HasAnyMarkers(bucketDir)
	if err != nil {
		return err
	}
	keepNewest := 0
	if !hasMarkers {
		keepNewest = sourcerevision.RetentionCount
	}
	log.Printf("gc: source-revisions pins merged=%d keep_newest=%d", len(pins), keepNewest)

	if err := sourcerevision.Prune(bucketDir, sourcerevision.PruneOptions{
		ExtraPins:  pins,
		KeepNewest: keepNewest,
	}); err != nil {
		return err
	}

	kept, err := sourcerevision.RetainedHashes(bucketDir)
	if err != nil {
		return err
	}
	if err := data.PruneDeployHistoryKeepingRevisions(tx, kept, data.DeployHistoryRetention); err != nil {
		return err
	}
	if _, err := data.PrunePromotionStateKeepingRevisions(bucketDir, kept); err != nil {
		return err
	}
	log.Printf("gc: source-revisions and history prune done kept=%d", len(kept))
	return nil
}

// MergeSourceRevisionPins deduplicates source-revision hashes from pin groups.
func MergeSourceRevisionPins(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range groups {
		for _, hash := range group {
			if hash == "" {
				continue
			}
			if _, ok := seen[hash]; ok {
				continue
			}
			seen[hash] = struct{}{}
			out = append(out, hash)
		}
	}
	return out
}

// pruneHistoryForBucket runs history GC using bucket.Location as the bucket root.
func pruneHistoryForBucket(tx *sql.Tx, opts HistoryOptions) error {
	bucketDir := bucket.Location
	if bucketDir == "" {
		return nil
	}
	return pruneHistory(tx, bucketDir, opts)
}
