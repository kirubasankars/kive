// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

import (
	"fmt"
	"strconv"
	"strings"
)

// Blocks returns all top-level blocks with the given name.
func (f *File) Blocks(name string) []*Block {
	if f == nil {
		return nil
	}
	var out []*Block
	for _, s := range f.Stmts {
		if b, ok := s.(*Block); ok && b.Name == name {
			out = append(out, b)
		}
	}
	return out
}

// Calls returns all top-level calls with the given name.
func (f *File) Calls(name string) []*Call {
	if f == nil {
		return nil
	}
	var out []*Call
	for _, s := range f.Stmts {
		if c, ok := s.(*Call); ok && c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// TopCalls returns all top-level call statements.
func (f *File) TopCalls() []*Call {
	if f == nil {
		return nil
	}
	var out []*Call
	for _, s := range f.Stmts {
		if c, ok := s.(*Call); ok {
			out = append(out, c)
		}
	}
	return out
}

// TopBlocks returns all top-level blocks.
func (f *File) TopBlocks() []*Block {
	if f == nil {
		return nil
	}
	var out []*Block
	for _, s := range f.Stmts {
		if b, ok := s.(*Block); ok {
			out = append(out, b)
		}
	}
	return out
}

// BodyCalls returns call statements in a block body.
func (b *Block) BodyCalls() []*Call {
	if b == nil {
		return nil
	}
	var out []*Call
	for _, s := range b.Body {
		if c, ok := s.(*Call); ok {
			out = append(out, c)
		}
	}
	return out
}

// BodyBlocks returns nested blocks in a block body.
func (b *Block) BodyBlocks() []*Block {
	if b == nil {
		return nil
	}
	var out []*Block
	for _, s := range b.Body {
		if nb, ok := s.(*Block); ok {
			out = append(out, nb)
		}
	}
	return out
}

// AsString returns the string value or an error.
func AsString(v Value, path string) (string, error) {
	switch t := v.(type) {
	case *StringLit:
		return t.Value, nil
	case *IdentLit:
		return t.Name, nil
	default:
		return "", Err(path, v.Pos().Line, v.Pos().Column, "expected a string").
			WithHint("use a quoted string, e.g. \"value\"")
	}
}

// AsBool returns the bool value or an error.
func AsBool(v Value, path string) (bool, error) {
	switch t := v.(type) {
	case *BoolLit:
		return t.Value, nil
	default:
		return false, Err(path, v.Pos().Line, v.Pos().Column, "expected true or false")
	}
}

// AsInt returns an integer value or an error.
func AsInt(v Value, path string) (int, error) {
	switch t := v.(type) {
	case *NumberLit:
		if t.IsFloat {
			return 0, Err(path, t.At.Line, t.At.Column, "expected an integer, got a float").
				WithHint("use a whole number without a decimal point")
		}
		if t.Int > int64(^uint(0)>>1) || t.Int < int64(^int(0)) {
			return 0, Err(path, t.At.Line, t.At.Column, "integer out of range")
		}
		return int(t.Int), nil
	case *StringLit:
		n, err := strconv.Atoi(strings.TrimSpace(t.Value))
		if err != nil {
			return 0, Err(path, t.At.Line, t.At.Column, fmt.Sprintf("expected an integer, got string %q", t.Value))
		}
		return n, nil
	default:
		return 0, Err(path, v.Pos().Line, v.Pos().Column, "expected an integer")
	}
}

// AsStrings collects string args from a call.
func AsStrings(c *Call, path string) ([]string, error) {
	out := make([]string, 0, len(c.Args))
	for _, a := range c.Args {
		s, err := AsString(a.Value, path)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// SingleStringArg requires exactly one string argument.
func SingleStringArg(c *Call, path string) (string, error) {
	if len(c.Args) != 1 {
		return "", Err(path, c.NamePos.Line, c.NamePos.Column,
			fmt.Sprintf("%s expects one string argument, got %d", c.Name, len(c.Args)))
	}
	return AsString(c.Args[0].Value, path)
}

// SingleBoolArg requires exactly one bool argument.
func SingleBoolArg(c *Call, path string) (bool, error) {
	if len(c.Args) != 1 {
		return false, Err(path, c.NamePos.Line, c.NamePos.Column,
			fmt.Sprintf("%s expects one boolean argument, got %d", c.Name, len(c.Args)))
	}
	return AsBool(c.Args[0].Value, path)
}

// SingleIntArg requires exactly one integer argument.
func SingleIntArg(c *Call, path string) (int, error) {
	if len(c.Args) != 1 {
		return 0, Err(path, c.NamePos.Line, c.NamePos.Column,
			fmt.Sprintf("%s expects one integer argument, got %d", c.Name, len(c.Args)))
	}
	return AsInt(c.Args[0].Value, path)
}

// NestedCall returns the value as a nested call.
func NestedCall(v Value, path string) (*Call, error) {
	cv, ok := v.(*CallValue)
	if !ok || cv.Call == nil {
		return nil, Err(path, v.Pos().Line, v.Pos().Column, "expected a nested call").
			WithHint("use a nested call value, e.g. zone(\"a\")")
	}
	return cv.Call, nil
}

// UnknownSetting builds an error for an unknown field with optional suggestion.
func UnknownSetting(path string, pos Pos, name string, known []string) *Error {
	msg := fmt.Sprintf("unknown setting %q", name)
	err := Err(path, pos.Line, pos.Column, msg)
	if hint := SuggestClosest(name, known); hint != "" {
		return err.WithHint(hint)
	}
	if len(known) > 0 && len(known) <= 12 {
		return err.WithHint("known settings: " + strings.Join(known, ", "))
	}
	return err
}

// RejectOldJSONFormat returns an error when a leftover JSON file is found.
func RejectOldJSONFormat(oldPath, newPath string) *Error {
	return Err(oldPath, 0, 0, fmt.Sprintf("found %s; use %s (block syntax)", oldPath, newPath)).
		WithHint("JSON settings files are no longer supported")
}

// RejectBothFormats returns an error when both old and new files exist.
func RejectBothFormats(oldPath, newPath string) *Error {
	return Err(newPath, 0, 0, fmt.Sprintf("both %s and %s exist; remove the old JSON/TOML file", oldPath, newPath))
}
