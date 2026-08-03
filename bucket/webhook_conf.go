// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package bucket

import (
	"fmt"
	"os"
	"strings"
	"time"

	"kive/conf"
)

const (
	DefaultWebhookTimeout = 5 * time.Second
	MaxWebhookTimeout     = 60 * time.Second
	MaxWebhookRetry       = 10
	MaxWebhookEntries     = 8
)

// WebhookEvents is a bitmask of subscribed lifecycle events.
type WebhookEvents uint8

const (
	WebhookEventStarted   WebhookEvents = 1 << iota // "run.started"
	WebhookEventSucceeded                           // "run.succeeded"
	WebhookEventFailed                              // "run.failed"

	WebhookEventDefault = WebhookEventFailed
	WebhookEventAll     = WebhookEventStarted | WebhookEventSucceeded | WebhookEventFailed
)

const (
	WebhookEventNameStarted   = "run.started"
	WebhookEventNameSucceeded = "run.succeeded"
	WebhookEventNameFailed    = "run.failed"
)

// WebhookEntry is one destination in webhook.conf.
type WebhookEntry struct {
	URL     string
	Timeout time.Duration // 0 → DefaultWebhookTimeout
	Retry   int           // 0 = no retry
	Events  WebhookEvents // 0 → WebhookEventDefault (run.failed)
}

// EffectiveTimeout returns Timeout when set, else DefaultWebhookTimeout.
func (e WebhookEntry) EffectiveTimeout() time.Duration {
	if e.Timeout > 0 {
		return e.Timeout
	}
	return DefaultWebhookTimeout
}

// EffectiveEvents returns Events when set, else WebhookEventDefault.
func (e WebhookEntry) EffectiveEvents() WebhookEvents {
	if e.Events != 0 {
		return e.Events
	}
	return WebhookEventDefault
}

// Subscribes reports whether the entry should fire for the named event.
func (e WebhookEntry) Subscribes(event string) bool {
	bit, ok := webhookEventBit(event)
	if !ok {
		return false
	}
	return e.EffectiveEvents()&bit != 0
}

// WebhookConf is the parsed webhook.conf at the bucket root.
type WebhookConf struct {
	Entries []WebhookEntry // nil/empty = explicit disable
}

// ParseWebhookConf lowers webhook.conf into WebhookConf.
func ParseWebhookConf(filePath string, data []byte) (WebhookConf, error) {
	f, err := conf.Parse(filePath, data)
	if err != nil {
		return WebhookConf{}, err
	}
	if len(f.Stmts) == 0 {
		return WebhookConf{}, conf.Err(filePath, 1, 1,
			"webhook.conf: expected one or more webhook { ... } blocks").
			WithHint(`use webhook { url("https://…"); }`)
	}
	for _, s := range f.Stmts {
		b, ok := s.(*conf.Block)
		if !ok || b.Name != "webhook" {
			return WebhookConf{}, conf.Err(filePath, s.Pos().Line, s.Pos().Column,
				`webhook.conf: expected "webhook { ... }" blocks`)
		}
	}
	blocks := f.Blocks("webhook")
	if len(blocks) > MaxWebhookEntries {
		return WebhookConf{}, conf.Err(filePath, blocks[MaxWebhookEntries].NamePos.Line, blocks[MaxWebhookEntries].NamePos.Column,
			fmt.Sprintf("webhook.conf: at most %d webhook blocks allowed", MaxWebhookEntries))
	}

	out := make([]WebhookEntry, 0, len(blocks))
	seen := map[string]conf.Pos{}
	for _, b := range blocks {
		entry, err := lowerWebhookBlock(filePath, b)
		if err != nil {
			return WebhookConf{}, err
		}
		u := strings.TrimSpace(entry.URL)
		if u == "" {
			if len(blocks) != 1 {
				return WebhookConf{}, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
					`webhook.conf: empty url("") is only allowed as the sole webhook block`).
					WithHint("use a lone webhook { url(\"\"); } to disable")
			}
			return WebhookConf{Entries: nil}, nil
		}
		if prev, ok := seen[u]; ok {
			return WebhookConf{}, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
				fmt.Sprintf("duplicate webhook url %q", u)).
				WithHint(fmt.Sprintf("first defined at line %d", prev.Line))
		}
		seen[u] = b.NamePos
		out = append(out, entry)
	}
	return WebhookConf{Entries: out}, nil
}

func lowerWebhookBlock(filePath string, b *conf.Block) (WebhookEntry, error) {
	known := []string{"url", "timeout", "retry", "events"}
	var entry WebhookEntry
	var hasURL bool
	for _, body := range b.Body {
		c, ok := body.(*conf.Call)
		if !ok {
			return WebhookEntry{}, conf.Err(filePath, body.Pos().Line, body.Pos().Column,
				"webhook body only allows url(...), timeout(...), retry(...), events(...)")
		}
		switch c.Name {
		case "url":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return WebhookEntry{}, err
			}
			entry.URL = strings.TrimSpace(s)
			hasURL = true
		case "timeout":
			s, err := conf.SingleStringArg(c, filePath)
			if err != nil {
				return WebhookEntry{}, err
			}
			d, err := time.ParseDuration(strings.TrimSpace(s))
			if err != nil {
				return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
					fmt.Sprintf("timeout: invalid duration %q", s)).
					WithHint(`use a Go duration like "5s" or "10s"`)
			}
			if d <= 0 {
				return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
					"timeout must be greater than zero")
			}
			if d > MaxWebhookTimeout {
				return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
					fmt.Sprintf("timeout exceeds maximum %s", MaxWebhookTimeout))
			}
			entry.Timeout = d
		case "retry":
			n, err := conf.SingleIntArg(c, filePath)
			if err != nil {
				return WebhookEntry{}, err
			}
			if n < 0 {
				return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
					"retry must be >= 0")
			}
			if n > MaxWebhookRetry {
				return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
					fmt.Sprintf("retry exceeds maximum %d", MaxWebhookRetry))
			}
			entry.Retry = n
		case "events":
			names, err := conf.AsStrings(c, filePath)
			if err != nil {
				return WebhookEntry{}, err
			}
			if len(names) == 0 {
				return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
					"events expects one or more event names").
					WithHint(`use events("run.failed") or events("run.started", "run.succeeded", "run.failed")`)
			}
			var bits WebhookEvents
			seen := map[string]bool{}
			for _, name := range names {
				name = strings.TrimSpace(name)
				bit, ok := webhookEventBit(name)
				if !ok {
					return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
						fmt.Sprintf("unknown event %q", name)).
						WithHint(`known events: "run.started", "run.succeeded", "run.failed"`)
				}
				if seen[name] {
					return WebhookEntry{}, conf.Err(filePath, c.NamePos.Line, c.NamePos.Column,
						fmt.Sprintf("duplicate event %q", name))
				}
				seen[name] = true
				bits |= bit
			}
			entry.Events = bits
		default:
			return WebhookEntry{}, conf.UnknownSetting(filePath, c.NamePos, c.Name, known)
		}
	}
	if !hasURL {
		return WebhookEntry{}, conf.Err(filePath, b.NamePos.Line, b.NamePos.Column,
			`webhook is missing url("...")`)
	}
	return entry, nil
}

func webhookEventBit(name string) (WebhookEvents, bool) {
	switch name {
	case WebhookEventNameStarted:
		return WebhookEventStarted, true
	case WebhookEventNameSucceeded:
		return WebhookEventSucceeded, true
	case WebhookEventNameFailed:
		return WebhookEventFailed, true
	default:
		return 0, false
	}
}

// WebhookEventNames returns the conf/API names for the given bitmask (stable order).
func WebhookEventNames(events WebhookEvents) []string {
	out := make([]string, 0, 3)
	if events&WebhookEventStarted != 0 {
		out = append(out, WebhookEventNameStarted)
	}
	if events&WebhookEventSucceeded != 0 {
		out = append(out, WebhookEventNameSucceeded)
	}
	if events&WebhookEventFailed != 0 {
		out = append(out, WebhookEventNameFailed)
	}
	return out
}

// ParseWebhookEventNames lowers event name strings into a bitmask.
func ParseWebhookEventNames(names []string) (WebhookEvents, error) {
	if len(names) == 0 {
		return 0, fmt.Errorf("events requires at least one name")
	}
	var bits WebhookEvents
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		bit, ok := webhookEventBit(name)
		if !ok {
			return 0, fmt.Errorf("unknown event %q", name)
		}
		if seen[name] {
			return 0, fmt.Errorf("duplicate event %q", name)
		}
		seen[name] = true
		bits |= bit
	}
	return bits, nil
}

// EmitWebhookConf serializes webhook.conf.
func EmitWebhookConf(cfg WebhookConf) string {
	f := &conf.File{}
	if len(cfg.Entries) == 0 {
		b := &conf.Block{Name: "webhook"}
		b.Body = append(b.Body, conf.CallStmt("url", ""))
		f.Stmts = append(f.Stmts, b)
		return conf.Emit(f, conf.EmitOptions{})
	}
	for _, e := range cfg.Entries {
		b := &conf.Block{Name: "webhook"}
		b.Body = append(b.Body, conf.CallStmt("url", e.URL))
		if e.Timeout > 0 && e.Timeout != DefaultWebhookTimeout {
			b.Body = append(b.Body, conf.CallStmt("timeout", e.Timeout.String()))
		}
		if e.Retry > 0 {
			b.Body = append(b.Body, conf.CallStmtInt("retry", e.Retry))
		}
		eff := e.EffectiveEvents()
		if e.Events != 0 && eff != WebhookEventDefault {
			b.Body = append(b.Body, conf.CallStmt("events", WebhookEventNames(eff)...))
		}
		f.Stmts = append(f.Stmts, b)
	}
	return conf.Emit(f, conf.EmitOptions{})
}

// LoadWebhookConfOptional reads webhook.conf when present.
func LoadWebhookConfOptional() (cfg WebhookConf, present bool, err error) {
	path := WebhookConfPath()
	raw, err := conf.ReadBytes(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WebhookConf{}, false, nil
		}
		return WebhookConf{}, false, err
	}
	cfg, err = ParseWebhookConf(path, raw)
	if err != nil {
		return WebhookConf{}, true, err
	}
	return cfg, true, nil
}

// WriteWebhookConf writes webhook.conf.
func WriteWebhookConf(cfg WebhookConf) error {
	return os.WriteFile(WebhookConfPath(), []byte(EmitWebhookConf(cfg)), 0o644)
}

// RemoveWebhookConf deletes webhook.conf if present.
func RemoveWebhookConf() error {
	err := os.Remove(WebhookConfPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove webhook.conf: %w", err)
	}
	return nil
}
