// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

import (
	"fmt"
	"strings"
)

// Error is a user-facing configuration diagnostic with file location.
type Error struct {
	Path    string
	Line    int // 1-based; 0 if unknown
	Column  int // 1-based; 0 if unknown
	Message string
	Snippet string
	Hint    string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	if e.Path != "" {
		b.WriteString(e.Path)
		if e.Line > 0 {
			fmt.Fprintf(&b, ":%d", e.Line)
			if e.Column > 0 {
				fmt.Fprintf(&b, ":%d", e.Column)
			}
		}
		b.WriteString(": ")
	} else if e.Line > 0 {
		fmt.Fprintf(&b, "line %d", e.Line)
		if e.Column > 0 {
			fmt.Fprintf(&b, ", column %d", e.Column)
		}
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Snippet != "" {
		b.WriteByte('\n')
		b.WriteString("    ")
		b.WriteString(e.Snippet)
		if e.Column > 0 {
			b.WriteByte('\n')
			b.WriteString("    ")
			b.WriteString(strings.Repeat(" ", e.Column-1))
			b.WriteByte('^')
		}
	}
	if e.Hint != "" {
		b.WriteByte('\n')
		b.WriteString("hint: ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Err creates a configuration error at the given position.
func Err(path string, line, col int, message string) *Error {
	return &Error{Path: path, Line: line, Column: col, Message: message}
}

// Errf creates a formatted configuration error.
func Errf(path string, line, col int, format string, args ...any) *Error {
	return Err(path, line, col, fmt.Sprintf(format, args...))
}

// WithHint returns a copy of e with Hint set.
func (e *Error) WithHint(hint string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Hint = hint
	return &cp
}

// WithCause returns a copy of e with Cause set.
func (e *Error) WithCause(cause error) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Cause = cause
	return &cp
}

// WithSnippet returns a copy of e with Snippet set.
func (e *Error) WithSnippet(snippet string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Snippet = snippet
	return &cp
}

// SuggestClosest returns a "did you mean" hint when name is close to a candidate.
func SuggestClosest(name string, candidates []string) string {
	best := ""
	bestDist := 3
	for _, c := range candidates {
		d := levenshtein(strings.ToLower(name), strings.ToLower(c))
		if d < bestDist || (d == bestDist && (best == "" || c < best)) {
			bestDist = d
			best = c
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("did you mean %q?", best)
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			cur[j] = del
			if ins < cur[j] {
				cur[j] = ins
			}
			if sub < cur[j] {
				cur[j] = sub
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
