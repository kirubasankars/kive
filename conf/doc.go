// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

// Package conf parses Kive’s block-dialect operator settings (*.conf).
//
// Every statement is either a Call (name(args)) or a Block (name [id] { stmts }).
//
//	Section (multi-field container, no id) → block:  ssh { … }, resources { … }
//	Named instance                         → block:  job api { … }, hook ready { … }
//	Setting (leaf values only)             → call:   port(22), path("/")
//
// No nested-call trees: multi-field objects are blocks whose bodies are setting
// calls or nested blocks. Blocks nest only as statements inside other blocks;
// calls do not hold nested sections. Schema lowerers decide which names are legal.
// Flat key/value files (bucket.conf, vars.conf, kive.server.conf) are call-only
// by design.
package conf
