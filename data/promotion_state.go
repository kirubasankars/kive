// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	PromotionStateFileName      = "promotion-state.json"
	PromotionHistoryRetention   = 50
	PromotionStatusIdle         = "idle"
	PromotionStatusRunning      = "running"
	PromotionStatusSucceeded    = "succeeded"
	PromotionStatusFailed       = "failed"
)

// PromotionHistoryEntry is one promote attempt in promotion-state.json.
type PromotionHistoryEntry struct {
	RunID                string `json:"run_id"`
	SourceBucketID       string `json:"source_bucket_id"`
	SourceRevision       string `json:"source_revision"`
	SourceRevisionLabel  string `json:"source_revision_label,omitempty"`
	SourceRunID          string `json:"source_run_id,omitempty"`
	Force                bool   `json:"force,omitempty"`
	Status               string `json:"status"`
	StartedAt            string `json:"started_at"`
	EndedAt              string `json:"ended_at,omitempty"`
	Error                string `json:"error,omitempty"`
}

// PromotionState is runtime cursor/status under .kive/promotion-state.json.
type PromotionState struct {
	LastSourceRevision      string                  `json:"last_source_revision,omitempty"`
	LastSourceRevisionLabel string                  `json:"last_source_revision_label,omitempty"`
	LastSourceRunID         string                  `json:"last_source_run_id,omitempty"`
	LastAttemptAt           string                  `json:"last_attempt_at,omitempty"`
	LastSuccessAt           string                  `json:"last_success_at,omitempty"`
	LastStatus              string                  `json:"last_status,omitempty"`
	LastError               string                  `json:"last_error,omitempty"`
	History                 []PromotionHistoryEntry `json:"history,omitempty"`
}

func promotionStatePath(bucketDir string) string {
	return filepath.Join(bucketDir, ".kive", PromotionStateFileName)
}

// LoadPromotionState reads promotion-state.json from bucketDir.
func LoadPromotionState(bucketDir string) (PromotionState, error) {
	encoded, err := os.ReadFile(promotionStatePath(bucketDir))
	if err != nil {
		if os.IsNotExist(err) {
			return PromotionState{LastStatus: PromotionStatusIdle}, nil
		}
		return PromotionState{}, err
	}
	var state PromotionState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return PromotionState{}, fmt.Errorf("parse promotion state: %w", err)
	}
	if state.LastStatus == "" {
		state.LastStatus = PromotionStatusIdle
	}
	return state, nil
}

// SavePromotionState writes promotion-state.json atomically.
func SavePromotionState(bucketDir string, state PromotionState) error {
	if err := os.MkdirAll(filepath.Join(bucketDir, ".kive"), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := promotionStatePath(bucketDir) + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, promotionStatePath(bucketDir))
}

// TrimPromotionHistory caps history to PromotionHistoryRetention entries.
func TrimPromotionHistory(entries []PromotionHistoryEntry) []PromotionHistoryEntry {
	if len(entries) <= PromotionHistoryRetention {
		return entries
	}
	return entries[:PromotionHistoryRetention]
}

// TrimPromotionHistoryKeepingRevisions keeps all entries whose source_revision is
// empty or in keep, plus always keeps entries matching lastSourceRevision.
// Among entries for gone revisions, only the newest PromotionHistoryRetention are kept
// is not applied — gone-revision entries are dropped. Soft-cap applies only to
// empty-revision entries beyond PromotionHistoryRetention.
func TrimPromotionHistoryKeepingRevisions(entries []PromotionHistoryEntry, lastSourceRevision string, keep []string) []PromotionHistoryEntry {
	keepSet := make(map[string]struct{}, len(keep)+1)
	for _, hash := range keep {
		hash = strings.TrimSpace(hash)
		if hash != "" {
			keepSet[hash] = struct{}{}
		}
	}
	last := strings.TrimSpace(lastSourceRevision)
	if last != "" {
		keepSet[last] = struct{}{}
	}
	out := make([]PromotionHistoryEntry, 0, len(entries))
	emptyKept := 0
	for _, entry := range entries {
		hash := strings.TrimSpace(entry.SourceRevision)
		if hash == "" {
			if emptyKept >= PromotionHistoryRetention {
				continue
			}
			emptyKept++
			out = append(out, entry)
			continue
		}
		if _, ok := keepSet[hash]; ok {
			out = append(out, entry)
		}
	}
	return out
}

// PromotionHistorySourceRevisions returns non-empty source_revision hashes from retained history.
func PromotionHistorySourceRevisions(state PromotionState) []string {
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
	for _, entry := range state.History {
		add(entry.SourceRevision)
	}
	return out
}

// SourceRevisionPinsFromPeerPromotion collects source_revision pins from peer buckets
// whose promotion history references sourceBucketID.
func SourceRevisionPinsFromPeerPromotion(bucketDirs []string, sourceBucketID string) ([]string, error) {
	sourceBucketID = strings.TrimSpace(sourceBucketID)
	if sourceBucketID == "" {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, dir := range bucketDirs {
		state, err := LoadPromotionState(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range state.History {
			if !strings.EqualFold(strings.TrimSpace(entry.SourceBucketID), sourceBucketID) {
				continue
			}
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
	}
	return out, nil
}

// PrunePromotionState trims history to the legacy count cap and persists.
// Prefer PrunePromotionStateKeepingRevisions during GC.
func PrunePromotionState(bucketDir string) (PromotionState, error) {
	state, err := LoadPromotionState(bucketDir)
	if err != nil {
		return PromotionState{}, err
	}
	state.History = TrimPromotionHistory(state.History)
	if err := SavePromotionState(bucketDir, state); err != nil {
		return PromotionState{}, err
	}
	return state, nil
}

// PrunePromotionStateKeepingRevisions drops history entries for revisions that
// are no longer retained, then persists promotion-state.json.
func PrunePromotionStateKeepingRevisions(bucketDir string, keep []string) (PromotionState, error) {
	state, err := LoadPromotionState(bucketDir)
	if err != nil {
		return PromotionState{}, err
	}
	state.History = TrimPromotionHistoryKeepingRevisions(state.History, state.LastSourceRevision, keep)
	if err := SavePromotionState(bucketDir, state); err != nil {
		return PromotionState{}, err
	}
	return state, nil
}
