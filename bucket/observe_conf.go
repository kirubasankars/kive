// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os"

	"kive/conf"
)

// ObserveConf is the parsed observe.conf at the bucket root.
type ObserveConf struct {
	ClickHouseDSN           string
	HasClickHouseDSN        bool
	PrometheusURL           string
	HasPrometheusURL        bool
	PrometheusTLSSkipVerify bool
	HasPrometheusTLSSkip    bool
}

// ParseObserveConf lowers observe.conf into ObserveConf.
func ParseObserveConf(filePath string, data []byte) (ObserveConf, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return ObserveConf{}, fmt.Errorf("parse %s: %w", filePath, err)
	}
	if len(f.Stmts) != 1 {
		pos := conf.Pos{Line: 1, Column: 1}
		if len(f.Stmts) > 0 {
			pos = f.Stmts[0].Pos()
		}
		return ObserveConf{}, conf.Err(filePath, pos.Line, pos.Column,
			`observe.conf: expected exactly one observe { ... } block`)
	}
	blocks := f.Blocks("observe")
	if len(blocks) != 1 {
		return ObserveConf{}, conf.Err(filePath, f.Stmts[0].Pos().Line, f.Stmts[0].Pos().Column,
			`observe.conf: expected exactly one observe { ... } block`)
	}
	var cfg ObserveConf
	for _, stmt := range blocks[0].Body {
		b, ok := stmt.(*conf.Block)
		if !ok {
			return ObserveConf{}, conf.Err(filePath, stmt.Pos().Line, stmt.Pos().Column,
				"observe body only allows clickhouse { ... } and prometheus { ... }")
		}
		switch b.Name {
		case "clickhouse":
			for _, body := range b.Body {
				c, ok := body.(*conf.Call)
				if !ok || c.Name != "dsn" {
					if ok {
						return ObserveConf{}, conf.UnknownSetting(filePath, c.NamePos, c.Name, []string{"dsn"})
					}
					return ObserveConf{}, conf.Err(filePath, body.Pos().Line, body.Pos().Column,
						"clickhouse body only allows dsn(...)")
				}
				s, err := conf.SingleStringArg(c, filePath)
				if err != nil {
					return ObserveConf{}, err
				}
				cfg.HasClickHouseDSN = true
				cfg.ClickHouseDSN = s
			}
		case "prometheus":
			for _, body := range b.Body {
				c, ok := body.(*conf.Call)
				if !ok {
					return ObserveConf{}, conf.Err(filePath, body.Pos().Line, body.Pos().Column,
						"prometheus body only allows calls")
				}
				switch c.Name {
				case "url":
					s, err := conf.SingleStringArg(c, filePath)
					if err != nil {
						return ObserveConf{}, err
					}
					cfg.HasPrometheusURL = true
					cfg.PrometheusURL = s
				case "tls_skip_verify":
					v, err := conf.SingleBoolArg(c, filePath)
					if err != nil {
						return ObserveConf{}, err
					}
					cfg.HasPrometheusTLSSkip = true
					cfg.PrometheusTLSSkipVerify = v
				default:
					return ObserveConf{}, conf.UnknownSetting(filePath, c.NamePos, c.Name, []string{"url", "tls_skip_verify"})
				}
			}
		default:
			return ObserveConf{}, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
				fmt.Sprintf("unexpected block %q in observe", b.Name))
		}
	}
	return cfg, nil
}

// EmitObserveConf serializes observe.conf.
func EmitObserveConf(cfg ObserveConf) string {
	observeBody := make([]conf.Stmt, 0, 2)
	if cfg.HasClickHouseDSN {
		observeBody = append(observeBody, &conf.Block{Name: "clickhouse", Body: []conf.Stmt{
			conf.CallStmt("dsn", cfg.ClickHouseDSN),
		}})
	}
	if cfg.HasPrometheusURL || cfg.HasPrometheusTLSSkip {
		promBody := make([]conf.Stmt, 0, 2)
		if cfg.HasPrometheusURL {
			promBody = append(promBody, conf.CallStmt("url", cfg.PrometheusURL))
		}
		if cfg.HasPrometheusTLSSkip {
			promBody = append(promBody, conf.CallStmtBool("tls_skip_verify", cfg.PrometheusTLSSkipVerify))
		}
		observeBody = append(observeBody, &conf.Block{Name: "prometheus", Body: promBody})
	}
	b := &conf.Block{Name: "observe", Body: observeBody}
	return conf.Emit(&conf.File{Stmts: []conf.Stmt{b}}, conf.EmitOptions{})
}

// LoadObserveConfOptional reads observe.conf when present.
func LoadObserveConfOptional() (cfg ObserveConf, present bool, err error) {
	path := ObserveConfPath()
	raw, err := conf.ReadBytes(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ObserveConf{}, false, nil
		}
		return ObserveConf{}, false, err
	}
	cfg, err = ParseObserveConf(path, raw)
	if err != nil {
		return ObserveConf{}, true, err
	}
	return cfg, true, nil
}

// WriteObserveConf writes observe.conf.
func WriteObserveConf(cfg ObserveConf) error {
	return os.WriteFile(ObserveConfPath(), []byte(EmitObserveConf(cfg)), 0o644)
}

// RemoveObserveConf deletes observe.conf if present.
func RemoveObserveConf() error {
	err := os.Remove(ObserveConfPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove observe.conf: %w", err)
	}
	return nil
}
