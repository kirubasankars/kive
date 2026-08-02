// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package sourcerevision

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	MarkersDirName           = "markers"
	PromotionMarkerRetention = 50
)

// MarkerEntry is one successful promote recorded on the source bucket for a target.
type MarkerEntry struct {
	SourceRevision string `json:"source_revision"`
	SourceRunID    string `json:"source_run_id,omitempty"`
	PromoteRunID   string `json:"promote_run_id"`
	PromotedAt     string `json:"promoted_at"`
}

// MarkerFile is markers/<consumer-bucket-id>.json on a source bucket.
type MarkerFile struct {
	ConsumerBucketID string        `json:"consumer_bucket_id"`
	Selection        string        `json:"selection"`
	Entries          []MarkerEntry `json:"entries"`
}

func MarkersDir(bucketDir string) string {
	return filepath.Join(Root(bucketDir), MarkersDirName)
}

func MarkerPath(bucketDir, consumerBucketID string) string {
	return filepath.Join(MarkersDir(bucketDir), consumerBucketID+".json")
}

// LoadMarker reads one consumer marker file; missing file yields empty entries.
func LoadMarker(bucketDir, consumerBucketID string) (MarkerFile, error) {
	consumerBucketID = strings.TrimSpace(consumerBucketID)
	if consumerBucketID == "" {
		return MarkerFile{}, fmt.Errorf("consumer bucket id is required")
	}
	path := MarkerPath(bucketDir, consumerBucketID)
	encoded, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return MarkerFile{ConsumerBucketID: consumerBucketID}, nil
		}
		return MarkerFile{}, err
	}
	var file MarkerFile
	if err := json.Unmarshal(encoded, &file); err != nil {
		return MarkerFile{}, fmt.Errorf("parse promotion marker: %w", err)
	}
	file.ConsumerBucketID = consumerBucketID
	return file, nil
}

// SaveMarker writes markers/<consumer>.json atomically.
func SaveMarker(bucketDir string, file MarkerFile) error {
	consumerBucketID := strings.TrimSpace(file.ConsumerBucketID)
	if consumerBucketID == "" {
		return fmt.Errorf("consumer bucket id is required")
	}
	dir := MarkersDir(bucketDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file.ConsumerBucketID = consumerBucketID
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	path := MarkerPath(bucketDir, consumerBucketID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeleteMarker removes markers/<consumer>.json.
func DeleteMarker(bucketDir, consumerBucketID string) error {
	consumerBucketID = strings.TrimSpace(consumerBucketID)
	if consumerBucketID == "" {
		return fmt.Errorf("consumer bucket id is required")
	}
	err := os.Remove(MarkerPath(bucketDir, consumerBucketID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ListMarkers returns consumer bucket IDs with marker files on sourceBucketDir.
func ListMarkers(sourceBucketDir string) ([]string, error) {
	dir := MarkersDir(sourceBucketDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(entry.Name(), ".json"))
	}
	return out, nil
}

// AppendMarkerEntry prepends an entry and trims to PromotionMarkerRetention.
func AppendMarkerEntry(sourceBucketDir, consumerBucketID, selection string, entry MarkerEntry) error {
	file, err := LoadMarker(sourceBucketDir, consumerBucketID)
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(selection); s != "" {
		file.Selection = s
	}
	file.Entries = append([]MarkerEntry{entry}, file.Entries...)
	if len(file.Entries) > PromotionMarkerRetention {
		file.Entries = file.Entries[:PromotionMarkerRetention]
	}
	return SaveMarker(sourceBucketDir, file)
}

// HasAnyMarkers reports whether any consumer marker file exists on the bucket.
func HasAnyMarkers(sourceBucketDir string) (bool, error) {
	ids, err := ListMarkers(sourceBucketDir)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// HasFifoMarker reports whether any marker file has selection "fifo".
func HasFifoMarker(sourceBucketDir string) (bool, error) {
	consumerIDs, err := ListMarkers(sourceBucketDir)
	if err != nil {
		return false, err
	}
	for _, id := range consumerIDs {
		file, err := LoadMarker(sourceBucketDir, id)
		if err != nil {
			return false, err
		}
		if strings.EqualFold(strings.TrimSpace(file.Selection), "fifo") {
			return true, nil
		}
	}
	return false, nil
}

// MarkerSourceRevisions returns non-empty source_revision hashes from all marker files.
func MarkerSourceRevisions(sourceBucketDir string) ([]string, error) {
	consumerIDs, err := ListMarkers(sourceBucketDir)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, id := range consumerIDs {
		file, err := LoadMarker(sourceBucketDir, id)
		if err != nil {
			return nil, err
		}
		for _, entry := range file.Entries {
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
