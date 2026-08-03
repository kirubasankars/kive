// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package workspace

import (
	"fmt"
	"os"
	"path"
	"sort"

	"kive/bucket"
	"kive/conf"
)

const (
	WorkersConfName = "workers.conf"
	WorkersJSONName = "workers.json"
)

func workersConfPath() string {
	return path.Join(bucket.WorkspaceLocation, WorkersConfName)
}

func workersConfPathLegacy() string {
	return path.Join(bucket.WorkspaceLocation, WorkersJSONName)
}

// ParseWorkersConf lowers workers.conf into worker records.
func ParseWorkersConf(filePath string, data []byte) ([]WorkerRecord, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return nil, err
	}
	for _, b := range f.TopBlocks() {
		if b.Name != "worker" {
			return nil, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
				fmt.Sprintf(`workers.conf: expected "worker { ... }" blocks, found %q`, b.Name))
		}
	}
	for _, s := range f.Stmts {
		if _, ok := s.(*conf.Block); !ok {
			return nil, conf.Err(filePath, s.Pos().Line, s.Pos().Column,
				`workers.conf: expected "worker { ... }" blocks`)
		}
	}
	blocks := f.Blocks("worker")
	out := make([]WorkerRecord, 0, len(blocks))
	hosts := map[string]conf.Pos{}
	for _, b := range blocks {
		rec, err := lowerWorkerBlock(filePath, b)
		if err != nil {
			return nil, err
		}
		if prev, ok := hosts[rec.Host]; ok {
			return nil, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
				fmt.Sprintf("duplicate worker host %q", rec.Host)).
				WithHint(fmt.Sprintf("first defined at line %d", prev.Line))
		}
		hosts[rec.Host] = b.NamePos
		out = append(out, rec)
	}
	return out, nil
}

func lowerWorkerBlock(filePath string, b *conf.Block) (WorkerRecord, error) {
	var rec WorkerRecord
	known := []string{"host", "hostname", "labels", "memory", "cpu", "tags", "position"}
	for _, stmt := range b.Body {
		switch s := stmt.(type) {
		case *conf.Call:
			switch s.Name {
			case "host":
				sv, err := conf.SingleStringArg(s, filePath)
				if err != nil {
					return WorkerRecord{}, err
				}
				rec.Host = sv
			case "hostname":
				sv, err := conf.SingleStringArg(s, filePath)
				if err != nil {
					return WorkerRecord{}, err
				}
				rec.Hostname = sv
			case "labels":
				sels, err := conf.AsStrings(s, filePath)
				if err != nil {
					return WorkerRecord{}, err
				}
				rec.Labels = sels
			case "memory":
				sv, err := conf.SingleStringArg(s, filePath)
				if err != nil {
					return WorkerRecord{}, err
				}
				rec.Memory = sv
			case "cpu":
				sv, err := conf.SingleStringArg(s, filePath)
				if err != nil {
					return WorkerRecord{}, err
				}
				rec.CPU = sv
			case "position":
				n, err := conf.SingleIntArg(s, filePath)
				if err != nil {
					return WorkerRecord{}, err
				}
				rec.Position = &n
			default:
				return WorkerRecord{}, conf.UnknownSetting(filePath, s.NamePos, s.Name, known)
			}
		case *conf.Block:
			if s.Name != "tags" {
				return WorkerRecord{}, conf.Err(filePath, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf("unexpected block %q in worker", s.Name)).
					WithHint(`use tags { key("value"); };`)
			}
			if rec.Tags == nil {
				rec.Tags = map[string]string{}
			}
			for _, tagStmt := range s.Body {
				tc, ok := tagStmt.(*conf.Call)
				if !ok {
					return WorkerRecord{}, conf.Err(filePath, tagStmt.Pos().Line, tagStmt.Pos().Column,
						"tags body only allows calls")
				}
				sv, err := conf.SingleStringArg(tc, filePath)
				if err != nil {
					return WorkerRecord{}, err
				}
				rec.Tags[tc.Name] = sv
			}
		default:
			return WorkerRecord{}, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column,
				"worker body only allows calls and tags { ... }")
		}
	}
	if rec.Host == "" {
		return WorkerRecord{}, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
			`worker is missing host("...")`)
	}
	if !isValidWorkerHost(rec.Host) {
		return WorkerRecord{}, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
			fmt.Sprintf("invalid worker host %q: only letters, digits, and .-_: are allowed", rec.Host))
	}
	if rec.Hostname != "" && !isValidWorkerHost(rec.Hostname) {
		return WorkerRecord{}, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
			fmt.Sprintf("invalid worker hostname %q: only letters, digits, and .-_: are allowed", rec.Hostname))
	}
	return rec, nil
}

// isValidWorkerHost reports whether host is a plausible IP address or hostname
// that is safe to interpolate into SSH targets and shell wrappers. It rejects
// whitespace and shell metacharacters at ingestion so they can never reach the
// worker catalog or generated command scripts.
func isValidWorkerHost(host string) bool {
	if host == "" || len(host) > 255 {
		return false
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_' || r == ':':
		default:
			return false
		}
	}
	return true
}

// EmitWorkersConf serializes workers to block conf.
func EmitWorkersConf(workers []WorkerRecord) string {
	f := &conf.File{}
	for _, w := range workers {
		b := &conf.Block{Name: "worker"}
		b.Body = append(b.Body, conf.CallStmt("host", w.Host))
		if w.Hostname != "" {
			b.Body = append(b.Body, conf.CallStmt("hostname", w.Hostname))
		}
		if len(w.Labels) > 0 {
			b.Body = append(b.Body, conf.CallStmt("labels", w.Labels...))
		}
		if w.Memory != "" {
			b.Body = append(b.Body, conf.CallStmt("memory", w.Memory))
		}
		if w.CPU != "" {
			b.Body = append(b.Body, conf.CallStmt("cpu", w.CPU))
		}
		if w.Position != nil {
			b.Body = append(b.Body, conf.CallStmtInt("position", *w.Position))
		}
		if len(w.Tags) > 0 {
			keys := make([]string, 0, len(w.Tags))
			for k := range w.Tags {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			tagsBlock := &conf.Block{Name: "tags"}
			for _, k := range keys {
				tagsBlock.Body = append(tagsBlock.Body, conf.CallStmt(k, w.Tags[k]))
			}
			b.Body = append(b.Body, tagsBlock)
		}
		f.Stmts = append(f.Stmts, b)
	}
	return conf.Emit(f, conf.EmitOptions{})
}

func checkWorkersLegacyJSON() error {
	if _, err := os.Stat(workersConfPathLegacy()); err == nil {
		if _, err2 := os.Stat(workersConfPath()); err2 == nil {
			return conf.RejectBothFormats(WorkersJSONName, WorkersConfName)
		}
		return conf.RejectOldJSONFormat(WorkersJSONName, WorkersConfName)
	}
	return nil
}
