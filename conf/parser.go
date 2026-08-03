// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

import (
	"fmt"
	"strconv"
)

type parser struct {
	lex   *lexer
	cur   token
	peek  token
	depth int
	stmts int
}

// Parse parses a block-dialect conf document.
func Parse(path string, src []byte) (*File, error) {
	if len(src) > MaxSourceBytes {
		return nil, Err(path, 0, 0, fmt.Sprintf("source exceeds %d byte limit", MaxSourceBytes)).
			WithCause(ErrLimitExceeded).
			WithHint("split or reduce the conf file")
	}
	for i, b := range src {
		if b == 0 {
			// Approximate position for NUL (1-based byte index as column on line 1
			// is wrong for multi-line; count newlines for a usable diagnostic).
			line, col := 1, 1
			for j := 0; j < i; j++ {
				if src[j] == '\n' {
					line++
					col = 1
				} else {
					col++
				}
			}
			return nil, Err(path, line, col, "null byte in source").
				WithCause(ErrLimitExceeded).
				WithHint("conf files must be valid UTF-8 text without NUL")
		}
	}
	p := &parser{lex: newLexer(path, string(src))}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p.parseFile()
}

func (p *parser) advance() *Error {
	p.cur = p.peek
	tok, err := p.lex.next()
	if err != nil {
		return err
	}
	p.peek = tok
	return nil
}

func (p *parser) err(pos Pos, msg string) *Error {
	return Err(p.lex.path, pos.Line, pos.Column, msg).WithSnippet(p.lex.snippet(pos.Line))
}

func (p *parser) errf(pos Pos, format string, args ...any) *Error {
	return p.err(pos, fmt.Sprintf(format, args...))
}

func (p *parser) limitErr(pos Pos, msg string) *Error {
	return p.err(pos, msg).WithCause(ErrLimitExceeded)
}

func (p *parser) enterDepth(pos Pos) *Error {
	p.depth++
	if p.depth > MaxNestingDepth {
		return p.limitErr(pos, fmt.Sprintf("nesting depth exceeds %d", MaxNestingDepth)).
			WithHint("flatten nested blocks and calls")
	}
	return nil
}

func (p *parser) leaveDepth() {
	p.depth--
}

func (p *parser) countStmt(pos Pos) *Error {
	p.stmts++
	if p.stmts > MaxStatements {
		return p.limitErr(pos, fmt.Sprintf("statement count exceeds %d", MaxStatements)).
			WithHint("split or reduce the conf file")
	}
	return nil
}

func (p *parser) parseFile() (*File, error) {
	f := &File{Path: p.lex.path}
	if p.cur.kind == tokAtVersion {
		ver, err := p.parseVersion()
		if err != nil {
			return nil, err
		}
		f.Version = ver
	}
	for p.cur.kind != tokEOF {
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		f.Stmts = append(f.Stmts, stmt)
	}
	return f, nil
}

func (p *parser) parseVersion() (int, *Error) {
	atPos := p.cur.pos
	if err := p.advance(); err != nil {
		return 0, err
	}
	if p.cur.kind != tokColon {
		return 0, p.err(p.cur.pos, "expected ':' after @version").
			WithHint("write @version: 1")
	}
	if err := p.advance(); err != nil {
		return 0, err
	}
	if p.cur.kind != tokNumber {
		return 0, p.err(p.cur.pos, "expected version number after @version:").
			WithHint("write @version: 1")
	}
	n, err := strconv.Atoi(p.cur.text)
	if err != nil || n < 1 {
		return 0, p.err(p.cur.pos, "invalid @version number").
			WithHint("use a positive integer, e.g. @version: 1")
	}
	if err := p.advance(); err != nil {
		return 0, err
	}
	if p.cur.kind == tokSemicolon {
		if err := p.advance(); err != nil {
			return 0, err
		}
	}
	_ = atPos
	return n, nil
}

func (p *parser) parseStmt() (Stmt, *Error) {
	if p.cur.kind != tokIdent {
		return nil, p.err(p.cur.pos, fmt.Sprintf("expected a statement name, found %s", tokenDesc(p.cur))).
			WithHint("statements look like name(args) or name { ... } (; optional)")
	}
	if err := p.countStmt(p.cur.pos); err != nil {
		return nil, err
	}
	name := p.cur.text
	namePos := p.cur.pos
	if err := p.advance(); err != nil {
		return nil, err
	}

	switch p.cur.kind {
	case tokLBrace:
		return p.parseBlock(name, "", namePos)
	case tokIdent:
		id := p.cur.text
		idPos := p.cur.pos
		if err := p.advance(); err != nil {
			return nil, err
		}
		if p.cur.kind != tokLBrace {
			return nil, p.err(p.cur.pos, fmt.Sprintf("expected '{' after %s %s", name, id)).
				WithHint(fmt.Sprintf("write %s %s { ... }", name, id))
		}
		_ = idPos
		return p.parseBlock(name, id, namePos)
	case tokLParen:
		call, err := p.parseCallTail(name, namePos, true)
		if err != nil {
			return nil, err
		}
		return call, nil
	default:
		return nil, p.err(p.cur.pos, fmt.Sprintf("expected '(' or '{' after %q, found %s", name, tokenDesc(p.cur))).
			WithHint(fmt.Sprintf("write %s(...) or %s { ... }", name, name))
	}
}

func (p *parser) parseBlock(name, id string, namePos Pos) (*Block, *Error) {
	if p.cur.kind != tokLBrace {
		return nil, p.err(p.cur.pos, "expected '{'")
	}
	openPos := p.cur.pos
	if err := p.enterDepth(openPos); err != nil {
		return nil, err
	}
	defer p.leaveDepth()
	if err := p.advance(); err != nil {
		return nil, err
	}
	b := &Block{Name: name, ID: id, NamePos: namePos}
	for p.cur.kind != tokRBrace {
		if p.cur.kind == tokEOF {
			return nil, p.err(openPos, "unclosed '{'").
				WithHint("add a matching '}'")
		}
		stmt, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		b.Body = append(b.Body, stmt)
	}
	if err := p.advance(); err != nil { // }
		return nil, err
	}
	if p.cur.kind == tokSemicolon {
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func (p *parser) parseCallTail(name string, namePos Pos, asStmt bool) (*Call, *Error) {
	if p.cur.kind != tokLParen {
		return nil, p.err(p.cur.pos, "expected '('")
	}
	openPos := p.cur.pos
	if err := p.enterDepth(openPos); err != nil {
		return nil, err
	}
	defer p.leaveDepth()
	if err := p.advance(); err != nil {
		return nil, err
	}
	c := &Call{Name: name, NamePos: namePos}
	if p.cur.kind != tokRParen {
		for {
			if len(c.Args) >= MaxArgs {
				return nil, p.limitErr(p.cur.pos, fmt.Sprintf("argument count exceeds %d", MaxArgs)).
					WithHint("reduce the number of arguments")
			}
			val, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			c.Args = append(c.Args, Arg{Value: val})
			if p.cur.kind == tokComma {
				if err := p.advance(); err != nil {
					return nil, err
				}
				if p.cur.kind == tokRParen {
					return nil, p.err(p.cur.pos, "trailing comma in argument list").
						WithHint("remove the trailing comma")
				}
				continue
			}
			break
		}
	}
	if p.cur.kind != tokRParen {
		if p.cur.kind == tokEOF {
			return nil, p.err(openPos, "unclosed '('").
				WithHint("add a matching ')'")
		}
		return nil, p.err(p.cur.pos, fmt.Sprintf("expected ')' or ',', found %s", tokenDesc(p.cur)))
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if asStmt {
		if p.cur.kind == tokSemicolon {
			c.TrailingSem = true
			if err := p.advance(); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func (p *parser) parseValue() (Value, *Error) {
	switch p.cur.kind {
	case tokString:
		v := &StringLit{Value: p.cur.text, At: p.cur.pos}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return v, nil
	case tokNumber:
		n, err := parseNumberLit(p.cur)
		if err != nil {
			return nil, p.err(p.cur.pos, err.Error())
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return n, nil
	case tokTrue, tokFalse:
		v := &BoolLit{Value: p.cur.kind == tokTrue, At: p.cur.pos}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return v, nil
	case tokIdent:
		name := p.cur.text
		namePos := p.cur.pos
		if err := p.advance(); err != nil {
			return nil, err
		}
		if p.cur.kind == tokLParen {
			call, err := p.parseCallTail(name, namePos, false)
			if err != nil {
				return nil, err
			}
			return &CallValue{Call: call}, nil
		}
		return &IdentLit{Name: name, At: namePos}, nil
	default:
		return nil, p.err(p.cur.pos, fmt.Sprintf("expected a value, found %s", tokenDesc(p.cur))).
			WithHint("values are strings, numbers, true/false, or nested calls")
	}
}

func parseNumberLit(tok token) (*NumberLit, error) {
	n := &NumberLit{Text: tok.text, At: tok.pos}
	if stringsContainsDot(tok.text) {
		f, err := strconv.ParseFloat(tok.text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", tok.text)
		}
		n.IsFloat = true
		n.Float = f
		n.Int = int64(f)
		return n, nil
	}
	i, err := strconv.ParseInt(tok.text, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number %q", tok.text)
	}
	n.Int = i
	n.Float = float64(i)
	return n, nil
}

func stringsContainsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

func tokenDesc(tok token) string {
	switch tok.kind {
	case tokEOF:
		return "end of file"
	case tokIdent:
		return fmt.Sprintf("identifier %q", tok.text)
	case tokString:
		return "string"
	case tokNumber:
		return fmt.Sprintf("number %s", tok.text)
	case tokTrue:
		return "true"
	case tokFalse:
		return "false"
	case tokLParen:
		return "'('"
	case tokRParen:
		return "')'"
	case tokLBrace:
		return "'{'"
	case tokRBrace:
		return "'}'"
	case tokComma:
		return "','"
	case tokSemicolon:
		return "';'"
	case tokColon:
		return "':'"
	case tokAtVersion:
		return "@version"
	default:
		return tok.text
	}
}
