// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

// Pos is a 1-based line/column location in a source file.
type Pos struct {
	Line   int
	Column int
}

// File is a parsed conf document.
type File struct {
	Path    string
	Version int // 0 if @version omitted
	Stmts   []Stmt
}

// Stmt is a top-level statement: a block or a bare call.
type Stmt interface {
	stmtNode()
	Pos() Pos
}

// Block is `name [id] { stmts }`.
type Block struct {
	Name   string
	ID     string // optional identifier after name
	Body   []Stmt
	NamePos Pos
}

func (b *Block) stmtNode() {}
func (b *Block) Pos() Pos  { return b.NamePos }

// Call is `name(args...)` / `name(args...);` as a statement, or nested `name(args)` as a value.
type Call struct {
	Name   string
	Args   []Arg
	NamePos Pos
	// TrailingSem true when this call was a statement that included an optional `;`.
	TrailingSem bool
}

func (c *Call) stmtNode() {}
func (c *Call) Pos() Pos  { return c.NamePos }

// Arg is a positional value or a keyword argument `name(value)` / nested call.
type Arg struct {
	// Name is set for keyword-style nested calls used as args when Ambiguous —
	// in this dialect, args are Values. Keyword form is Call with nested structure.
	Value Value
}

// Value is a literal or nested call.
type Value interface {
	valueNode()
	Pos() Pos
}

// StringLit is a quoted string.
type StringLit struct {
	Value string
	At    Pos
}

func (s *StringLit) valueNode() {}
func (s *StringLit) Pos() Pos   { return s.At }

// NumberLit is an integer or float token preserved as text plus parsed forms.
type NumberLit struct {
	Text  string
	Int   int64
	Float float64
	IsFloat bool
	At    Pos
}

func (n *NumberLit) valueNode() {}
func (n *NumberLit) Pos() Pos   { return n.At }

// BoolLit is true or false.
type BoolLit struct {
	Value bool
	At    Pos
}

func (b *BoolLit) valueNode() {}
func (b *BoolLit) Pos() Pos   { return b.At }

// IdentLit is a bare identifier used as a value (rare; usually calls).
type IdentLit struct {
	Name string
	At   Pos
}

func (i *IdentLit) valueNode() {}
func (i *IdentLit) Pos() Pos   { return i.At }

// CallValue wraps a nested call used as a value (no trailing semicolon).
type CallValue struct {
	*Call
}

func (c *CallValue) valueNode() {}
