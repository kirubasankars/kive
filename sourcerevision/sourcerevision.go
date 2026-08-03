// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package sourcerevision manages immutable push artifacts under .kive/source-revisions/.
package sourcerevision

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	KiveDir             = ".kive"
	DirName             = "source-revisions"
	StateFile           = "state.json"
	BundleName          = "bundle.db"
	TreeDirName         = "tree"
	MetaFile            = "meta.json"
	KindBundle          = "bundle"
	KindTree            = "tree"
	// RetentionCount is the newest-N floor for revision artifacts when no
	// consumer markers exist on the bucket.
	RetentionCount = 50
)

const (
	// MaxLabelLen is the maximum allowed length of a revision label after trim.
	MaxLabelLen = 128
)

// Entry is one retained source revision in state.json.
type Entry struct {
	PushHash          string `json:"push_hash"`
	ProducerBuildHash string `json:"producer_build_hash,omitempty"`
	BundleVersion     int    `json:"bundle_version,omitempty"`
	Label             string `json:"label,omitempty"`
	CreatedAt         string `json:"created_at"`
	BuildResult       string `json:"build_result"`
	Kind              string `json:"kind"`
}

// State tracks current/last_successful pointers and retained revision entries.
type State struct {
	Current        string  `json:"current"`
	LastSuccessful string  `json:"last_successful"`
	Revisions      []Entry `json:"revisions"`
}

func Root(bucketDir string) string {
	return filepath.Join(bucketDir, KiveDir, DirName)
}

func StatePath(bucketDir string) string {
	return filepath.Join(Root(bucketDir), StateFile)
}

func Dir(bucketDir, hash string) string {
	return filepath.Join(Root(bucketDir), hash)
}

// LoadState reads state.json; missing file yields an empty state.
func LoadState(bucketDir string) (State, error) {
	path := StatePath(bucketDir)
	encoded, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(encoded, &state); err != nil {
		return State{}, fmt.Errorf("parse source revision state: %w", err)
	}
	return state, nil
}

// SaveState writes state.json atomically.
func SaveState(bucketDir string, state State) error {
	root := Root(bucketDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := StatePath(bucketDir) + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, StatePath(bucketDir))
}

// PruneOptions controls source revision pruning.
type PruneOptions struct {
	// ExtraPins are additional push hashes that must be kept (promotion/deploy/peer pins).
	ExtraPins []string
	// KeepNewest, when > 0, also retains the newest N revisions by CreatedAt
	// (in addition to current, last_successful, and ExtraPins).
	KeepNewest int
}

// Prune removes unreferenced revision dirs and trims state.json entries.
// Pins: current, last_successful, ExtraPins, and optionally the newest KeepNewest.
func Prune(bucketDir string, opts PruneOptions) error {
	state, err := LoadState(bucketDir)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	if state.Current != "" {
		keep[state.Current] = true
	}
	if state.LastSuccessful != "" {
		keep[state.LastSuccessful] = true
	}
	for _, hash := range opts.ExtraPins {
		if h := strings.TrimSpace(hash); h != "" {
			keep[h] = true
		}
	}
	if opts.KeepNewest > 0 {
		newest := newestRevisionHashes(state.Revisions, opts.KeepNewest)
		for _, hash := range newest {
			keep[hash] = true
		}
	}
	order := make([]Entry, 0, len(state.Revisions))
	seen := map[string]bool{}
	for _, entry := range state.Revisions {
		if !keep[entry.PushHash] {
			_ = os.RemoveAll(Dir(bucketDir, entry.PushHash))
			continue
		}
		if seen[entry.PushHash] {
			continue
		}
		order = append(order, entry)
		seen[entry.PushHash] = true
	}
	state.Revisions = order
	if err := saveStateAndOrphans(bucketDir, state, keep); err != nil {
		return err
	}
	return nil
}

// newestRevisionHashes returns up to n push hashes ordered newest-first by CreatedAt.
func newestRevisionHashes(entries []Entry, n int) []string {
	if n <= 0 || len(entries) == 0 {
		return nil
	}
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt > sorted[j].CreatedAt
	})
	out := make([]string, 0, n)
	seen := map[string]bool{}
	for _, entry := range sorted {
		hash := strings.TrimSpace(entry.PushHash)
		if hash == "" || seen[hash] {
			continue
		}
		seen[hash] = true
		out = append(out, hash)
		if len(out) >= n {
			break
		}
	}
	return out
}

// RetainedHashes returns push hashes still listed in state.json after load.
func RetainedHashes(bucketDir string) ([]string, error) {
	state, err := LoadState(bucketDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(state.Revisions))
	seen := map[string]bool{}
	for _, entry := range state.Revisions {
		hash := strings.TrimSpace(entry.PushHash)
		if hash == "" || seen[hash] {
			continue
		}
		seen[hash] = true
		out = append(out, hash)
	}
	return out, nil
}

func saveStateAndOrphans(bucketDir string, state State, keep map[string]bool) error {
	if err := removeOrphanDirs(bucketDir, keep); err != nil {
		return err
	}
	return SaveState(bucketDir, state)
}

func removeOrphanDirs(bucketDir string, keep map[string]bool) error {
	root := Root(bucketDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == StateFile || name == MarkersDirName || strings.HasSuffix(name, ".tmp") {
			continue
		}
		if keep[name] {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, name))
	}
	return nil
}

// ArtifactPresent reports whether bundle.db or tree/ exists for hash.
func ArtifactPresent(bucketDir, hash string) bool {
	dir := Dir(bucketDir, hash)
	if _, err := os.Stat(filepath.Join(dir, BundleName)); err == nil {
		return true
	}
	if info, err := os.Stat(filepath.Join(dir, TreeDirName)); err == nil && info.IsDir() {
		return true
	}
	return false
}

// NormalizeLabel trims a revision label. Empty input yields "".
func NormalizeLabel(label string) string {
	return strings.TrimSpace(label)
}

// ValidateLabel checks a non-empty revision label. Pass the already-normalized
// value from NormalizeLabel. Empty labels are valid (optional).
func ValidateLabel(label string) error {
	if label == "" {
		return nil
	}
	if len(label) > MaxLabelLen {
		return fmt.Errorf("revision label exceeds %d characters", MaxLabelLen)
	}
	for _, r := range label {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("revision label must not contain control characters")
		}
	}
	return nil
}

// LabelInUse reports whether another retained revision (not excludeHash) already
// has the given non-empty label. Comparison is case-sensitive.
func LabelInUse(state State, label, excludeHash string) bool {
	label = NormalizeLabel(label)
	if label == "" {
		return false
	}
	excludeHash = strings.TrimSpace(excludeHash)
	for _, entry := range state.Revisions {
		if entry.PushHash == excludeHash {
			continue
		}
		if NormalizeLabel(entry.Label) == label {
			return true
		}
	}
	return false
}

// LabelForRevision returns the label for hash from state, or "".
func LabelForRevision(state State, hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return ""
	}
	for _, entry := range state.Revisions {
		if entry.PushHash == hash {
			return NormalizeLabel(entry.Label)
		}
	}
	return ""
}

// LabelMap returns push_hash → label for revisions that have a non-empty label.
func LabelMap(state State) map[string]string {
	out := make(map[string]string)
	for _, entry := range state.Revisions {
		if label := NormalizeLabel(entry.Label); label != "" {
			out[entry.PushHash] = label
		}
	}
	return out
}
