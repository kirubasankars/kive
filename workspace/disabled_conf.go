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
	DisabledConfName = "disabled.conf"
	DisabledJSONName = "disabled.json"
)

func disabledConfPath() string {
	return path.Join(bucket.WorkspaceLocation, DisabledConfName)
}

func disabledJSONPathLegacy() string {
	return path.Join(bucket.WorkspaceLocation, DisabledJSONName)
}

// ParseDisabledConf lowers disabled.conf.
func ParseDisabledConf(filePath string, data []byte) (DisabledAllocations, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return DisabledAllocations{}, err
	}
	blocks := f.Blocks("disabled")
	if len(blocks) == 0 {
		if len(f.Stmts) == 0 {
			return DisabledAllocations{}, nil
		}
		return DisabledAllocations{}, conf.Err(filePath, f.Stmts[0].Pos().Line, f.Stmts[0].Pos().Column,
			`disabled.conf: expected a disabled { ... } block`)
	}
	if len(blocks) > 1 || len(f.Stmts) != 1 {
		return DisabledAllocations{}, conf.Err(filePath, blocks[0].NamePos.Line, blocks[0].NamePos.Column,
			`disabled.conf: expected exactly one disabled { ... } block`)
	}
	out := DisabledAllocations{
		Jobs: map[string]struct {
			Allocations []string `json:"allocations"`
		}{},
	}
	for _, stmt := range blocks[0].Body {
		switch s := stmt.(type) {
		case *conf.Block:
			if s.Name != "job" {
				return DisabledAllocations{}, conf.Err(filePath, s.NamePos.Line, s.NamePos.Column,
					fmt.Sprintf(`unexpected block %q; use job <name> { ... }`, s.Name))
			}
			if s.ID == "" {
				return DisabledAllocations{}, conf.Err(filePath, s.NamePos.Line, s.NamePos.Column,
					`job block requires a name`).WithHint(`write job api { }; or job api { allocations("10.0.0.1"); };`)
			}
			entry := struct {
				Allocations []string `json:"allocations"`
			}{}
			for _, body := range s.Body {
				c, ok := body.(*conf.Call)
				if !ok {
					return DisabledAllocations{}, conf.Err(filePath, body.Pos().Line, body.Pos().Column,
						"job body only allows allocations(...)")
				}
				if c.Name != "allocations" {
					return DisabledAllocations{}, conf.UnknownSetting(filePath, c.NamePos, c.Name, []string{"allocations"})
				}
				allocs, err := conf.AsStrings(c, filePath)
				if err != nil {
					return DisabledAllocations{}, err
				}
				entry.Allocations = append(entry.Allocations, allocs...)
			}
			out.Jobs[s.ID] = entry
		case *conf.Call:
			if s.Name != "workers" {
				return DisabledAllocations{}, conf.UnknownSetting(filePath, s.NamePos, s.Name, []string{"workers"})
			}
			ws, err := conf.AsStrings(s, filePath)
			if err != nil {
				return DisabledAllocations{}, err
			}
			out.Workers = append(out.Workers, ws...)
		default:
			return DisabledAllocations{}, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column, "unexpected statement")
		}
	}
	return out, nil
}

// EmitDisabledConf serializes disabled allocations.
func EmitDisabledConf(d DisabledAllocations) string {
	b := &conf.Block{Name: "disabled"}
	names := make([]string, 0, len(d.Jobs))
	for name := range d.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		jb := &conf.Block{Name: "job", ID: name}
		if allocs := d.Jobs[name].Allocations; len(allocs) > 0 {
			jb.Body = append(jb.Body, conf.CallStmt("allocations", allocs...))
		}
		b.Body = append(b.Body, jb)
	}
	if len(d.Workers) > 0 {
		b.Body = append(b.Body, conf.CallStmt("workers", d.Workers...))
	}
	return conf.Emit(&conf.File{Stmts: []conf.Stmt{b}}, conf.EmitOptions{})
}

// WriteDisabledConf writes workspace/disabled.conf.
func WriteDisabledConf(d DisabledAllocations) error {
	return os.WriteFile(disabledConfPath(), []byte(EmitDisabledConf(d)), 0o644)
}

// RemoveDisabledConf deletes workspace/disabled.conf if present.
func RemoveDisabledConf() error {
	err := os.Remove(disabledConfPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func checkDisabledLegacyJSON() error {
	if _, err := os.Stat(disabledJSONPathLegacy()); err == nil {
		if _, err2 := os.Stat(disabledConfPath()); err2 == nil {
			return conf.RejectBothFormats(DisabledJSONName, DisabledConfName)
		}
		return conf.RejectOldJSONFormat(DisabledJSONName, DisabledConfName)
	}
	return nil
}
