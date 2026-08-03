// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"sort"

	"kive/conf"
)

// ParseBucketJobsConf parses bucket.jobs.conf into per-job string maps.
func ParseBucketJobsConf(filePath string, data []byte) (map[string]map[string]string, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	for _, stmt := range f.Stmts {
		b, ok := stmt.(*conf.Block)
		if !ok || b.Name != "job" {
			return nil, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column,
				`bucket.jobs.conf: expected job <name> { ... } blocks`)
		}
		if b.ID == "" {
			return nil, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
				`job block requires a name`).WithHint(`write job api { memory("512 mb"); };`)
		}
		settings := map[string]string{}
		for _, body := range b.Body {
			c, ok := body.(*conf.Call)
			if !ok {
				return nil, conf.Err(filePath, body.Pos().Line, body.Pos().Column,
					"job settings only allow calls")
			}
			if len(c.Args) != 1 {
				return nil, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
					fmt.Sprintf("%s expects one value", c.Name))
			}
			s, err := conf.AsString(c.Args[0].Value, filePath)
			if err != nil {
				// allow numbers/bools as strings
				switch v := c.Args[0].Value.(type) {
				case *conf.NumberLit:
					s = v.Text
				case *conf.BoolLit:
					if v.Value {
						s = "true"
					} else {
						s = "false"
					}
				default:
					return nil, err
				}
			}
			settings[c.Name] = s
		}
		out[b.ID] = settings
	}
	return out, nil
}

// ParseVarsConf parses a flat vars.conf into a string map.
func ParseVarsConf(filePath string, data []byte) (map[string]string, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, stmt := range f.Stmts {
		c, ok := stmt.(*conf.Call)
		if !ok {
			return nil, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column,
				"vars.conf only allows setting calls")
		}
		if len(c.Args) != 1 {
			return nil, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
				fmt.Sprintf("%s expects one value", c.Name))
		}
		s, err := conf.AsString(c.Args[0].Value, filePath)
		if err != nil {
			switch v := c.Args[0].Value.(type) {
			case *conf.NumberLit:
				s = v.Text
			case *conf.BoolLit:
				if v.Value {
					s = "true"
				} else {
					s = "false"
				}
			default:
				return nil, err
			}
		}
		out[c.Name] = s
	}
	return out, nil
}

// EmitVarsConf emits a flat string map.
func EmitVarsConf(vars map[string]string) string {
	return EmitBucketConfRaw(vars)
}

// EmitBucketJobsConf serializes per-job string maps as bucket.jobs.conf.
func EmitBucketJobsConf(jobs map[string]map[string]string) string {
	f := &conf.File{}
	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		settings := jobs[name]
		b := &conf.Block{Name: "job", ID: name}
		keys := make([]string, 0, len(settings))
		for k := range settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.Body = append(b.Body, conf.CallStmt(k, settings[k]))
		}
		f.Stmts = append(f.Stmts, b)
	}
	return conf.Emit(f, conf.EmitOptions{})
}
