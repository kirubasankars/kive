// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

import (
	"fmt"
	"strconv"
	"strings"
)

// EmitOptions controls pretty-printing.
type EmitOptions struct {
	Indent string // default two spaces
}

func (o EmitOptions) indent() string {
	if o.Indent == "" {
		return "  "
	}
	return o.Indent
}

// Emit formats a File as block-dialect conf text.
// Consecutive top-level blocks are separated by a blank line; call statements
// stay packed (no blank line between adjacent calls, or between a call and a block).
func Emit(f *File, opts EmitOptions) string {
	if f == nil {
		return ""
	}
	var b strings.Builder
	if f.Version > 0 {
		fmt.Fprintf(&b, "@version: %d\n\n", f.Version)
	}
	for i, stmt := range f.Stmts {
		emitStmt(&b, stmt, 0, opts)
		if i+1 < len(f.Stmts) {
			_, prevBlock := stmt.(*Block)
			_, nextBlock := f.Stmts[i+1].(*Block)
			if prevBlock && nextBlock {
				b.WriteByte('\n')
			}
		}
	}
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}

func emitStmt(b *strings.Builder, stmt Stmt, depth int, opts EmitOptions) {
	ind := strings.Repeat(opts.indent(), depth)
	switch s := stmt.(type) {
	case *Block:
		b.WriteString(ind)
		b.WriteString(s.Name)
		if s.ID != "" {
			b.WriteByte(' ')
			b.WriteString(s.ID)
		}
		b.WriteString(" {\n")
		for _, body := range s.Body {
			emitStmt(b, body, depth+1, opts)
		}
		b.WriteString(ind)
		b.WriteString("}\n")
	case *Call:
		b.WriteString(ind)
		emitCall(b, s)
		b.WriteString("\n")
	}
}

func emitCall(b *strings.Builder, c *Call) {
	b.WriteString(c.Name)
	b.WriteByte('(')
	for i, a := range c.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		emitValue(b, a.Value)
	}
	b.WriteByte(')')
}

func emitValue(b *strings.Builder, v Value) {
	switch t := v.(type) {
	case *StringLit:
		b.WriteByte('"')
		b.WriteString(escapeString(t.Value))
		b.WriteByte('"')
	case *NumberLit:
		b.WriteString(t.Text)
	case *BoolLit:
		if t.Value {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case *IdentLit:
		b.WriteString(t.Name)
	case *CallValue:
		emitCall(b, t.Call)
	}
}

func escapeString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CallStmt builds a call statement with string args.
func CallStmt(name string, args ...string) *Call {
	c := &Call{Name: name}
	for _, a := range args {
		c.Args = append(c.Args, Arg{Value: &StringLit{Value: a}})
	}
	return c
}

// CallStmtBool builds a bool call statement.
func CallStmtBool(name string, v bool) *Call {
	return &Call{Name: name, Args: []Arg{{Value: &BoolLit{Value: v}}}}
}

// CallStmtInt builds an int call statement.
func CallStmtInt(name string, v int) *Call {
	return &Call{Name: name, Args: []Arg{{Value: &NumberLit{
		Text: strconv.Itoa(v), Int: int64(v), Float: float64(v),
	}}}}
}

// Nested builds a nested call value.
func Nested(name string, args ...Value) *CallValue {
	c := &Call{Name: name}
	for _, a := range args {
		c.Args = append(c.Args, Arg{Value: a})
	}
	return &CallValue{Call: c}
}

// Str is a string literal value.
func Str(s string) Value { return &StringLit{Value: s} }

// Int is an integer literal value.
func Int(n int) Value {
	return &NumberLit{Text: strconv.Itoa(n), Int: int64(n), Float: float64(n)}
}

// Bool is a boolean literal value.
func Bool(v bool) Value { return &BoolLit{Value: v} }
