// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package conf

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokNumber
	tokTrue
	tokFalse
	tokLParen
	tokRParen
	tokLBrace
	tokRBrace
	tokComma
	tokSemicolon
	tokColon
	tokAtVersion // @version
)

type token struct {
	kind tokenKind
	text string
	pos  Pos
}

type lexer struct {
	path  string
	src   string
	i     int
	line  int
	col   int
	lines []string
}

func newLexer(path, src string) *lexer {
	lines := strings.Split(src, "\n")
	return &lexer{path: path, src: src, line: 1, col: 1, lines: lines}
}

func (l *lexer) snippet(line int) string {
	if line <= 0 || line > len(l.lines) {
		return ""
	}
	return strings.TrimRight(l.lines[line-1], "\r")
}

func (l *lexer) err(pos Pos, msg string) *Error {
	return Err(l.path, pos.Line, pos.Column, msg).WithSnippet(l.snippet(pos.Line))
}

func (l *lexer) peek() (rune, int) {
	if l.i >= len(l.src) {
		return 0, 0
	}
	return utf8.DecodeRuneInString(l.src[l.i:])
}

func (l *lexer) advance() rune {
	r, w := l.peek()
	if w == 0 {
		return 0
	}
	l.i += w
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

func (l *lexer) pos() Pos {
	return Pos{Line: l.line, Column: l.col}
}

func (l *lexer) skipSpaceAndComments() *Error {
	for {
		r, w := l.peek()
		if w == 0 {
			return nil
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			l.advance()
			continue
		}
		if r == '#' {
			l.skipLineComment()
			continue
		}
		if r == '/' {
			r2, w2 := utf8.DecodeRuneInString(l.src[l.i+w:])
			if w2 > 0 && r2 == '/' {
				l.advance()
				l.advance()
				l.skipLineCommentRest()
				continue
			}
		}
		return nil
	}
}

func (l *lexer) skipLineComment() {
	l.advance() // #
	l.skipLineCommentRest()
}

func (l *lexer) skipLineCommentRest() {
	for {
		r, w := l.peek()
		if w == 0 || r == '\n' {
			return
		}
		l.advance()
	}
}

func (l *lexer) next() (token, *Error) {
	if err := l.skipSpaceAndComments(); err != nil {
		return token{}, err
	}
	start := l.pos()
	r, w := l.peek()
	if w == 0 {
		return token{kind: tokEOF, pos: start}, nil
	}
	if r == 0 {
		return token{}, l.err(start, "null byte in source").
			WithCause(ErrLimitExceeded).
			WithHint("conf files must be valid UTF-8 text without NUL")
	}

	switch r {
	case '(':
		l.advance()
		return token{kind: tokLParen, text: "(", pos: start}, nil
	case ')':
		l.advance()
		return token{kind: tokRParen, text: ")", pos: start}, nil
	case '{':
		l.advance()
		return token{kind: tokLBrace, text: "{", pos: start}, nil
	case '}':
		l.advance()
		return token{kind: tokRBrace, text: "}", pos: start}, nil
	case ',':
		l.advance()
		return token{kind: tokComma, text: ",", pos: start}, nil
	case ';':
		l.advance()
		return token{kind: tokSemicolon, text: ";", pos: start}, nil
	case ':':
		l.advance()
		return token{kind: tokColon, text: ":", pos: start}, nil
	case '@':
		return l.scanAt(start)
	case '"':
		return l.scanString(start)
	}

	if r == '-' || unicode.IsDigit(r) {
		return l.scanNumber(start)
	}
	if isIdentStart(r) {
		return l.scanIdent(start)
	}
	return token{}, l.err(start, fmt.Sprintf("unexpected character %q", r))
}

func (l *lexer) scanAt(start Pos) (token, *Error) {
	l.advance() // @
	var b strings.Builder
	for {
		r, w := l.peek()
		if w == 0 || !isIdentPart(r) {
			break
		}
		if b.Len()+utf8.RuneLen(r) > MaxIdentBytes {
			return token{}, l.err(start, fmt.Sprintf("identifier exceeds %d bytes", MaxIdentBytes)).
				WithCause(ErrLimitExceeded)
		}
		b.WriteRune(r)
		l.advance()
	}
	name := b.String()
	if name != "version" {
		return token{}, l.err(start, fmt.Sprintf("unknown directive @%s", name)).
			WithHint("only @version is supported")
	}
	return token{kind: tokAtVersion, text: "@version", pos: start}, nil
}

func (l *lexer) scanIdent(start Pos) (token, *Error) {
	var b strings.Builder
	for {
		r, w := l.peek()
		if w == 0 || !isIdentPart(r) {
			break
		}
		if b.Len()+utf8.RuneLen(r) > MaxIdentBytes {
			return token{}, l.err(start, fmt.Sprintf("identifier exceeds %d bytes", MaxIdentBytes)).
				WithCause(ErrLimitExceeded)
		}
		b.WriteRune(r)
		l.advance()
	}
	text := b.String()
	switch text {
	case "true":
		return token{kind: tokTrue, text: text, pos: start}, nil
	case "false":
		return token{kind: tokFalse, text: text, pos: start}, nil
	default:
		return token{kind: tokIdent, text: text, pos: start}, nil
	}
}

func (l *lexer) scanNumber(start Pos) (token, *Error) {
	var b strings.Builder
	r, _ := l.peek()
	if r == '-' {
		b.WriteRune(r)
		l.advance()
	}
	digits := 0
	for {
		r, w := l.peek()
		if w == 0 || !unicode.IsDigit(r) {
			break
		}
		b.WriteRune(r)
		l.advance()
		digits++
	}
	if digits == 0 {
		return token{}, l.err(start, "expected a number")
	}
	if r, w := l.peek(); w > 0 && r == '.' {
		b.WriteRune(r)
		l.advance()
		frac := 0
		for {
			r, w := l.peek()
			if w == 0 || !unicode.IsDigit(r) {
				break
			}
			b.WriteRune(r)
			l.advance()
			frac++
		}
		if frac == 0 {
			return token{}, l.err(start, "expected digits after decimal point")
		}
	}
	return token{kind: tokNumber, text: b.String(), pos: start}, nil
}

func (l *lexer) scanString(start Pos) (token, *Error) {
	l.advance() // opening "
	var b strings.Builder
	for {
		r, w := l.peek()
		if w == 0 {
			return token{}, l.err(start, "unclosed string").
				WithHint("add a closing double quote")
		}
		if r == 0 {
			return token{}, l.err(l.pos(), "null byte in string").
				WithCause(ErrLimitExceeded).
				WithHint("conf files must be valid UTF-8 text without NUL")
		}
		if r == '\n' {
			return token{}, l.err(start, "unclosed string (newline in string)").
				WithHint("add a closing double quote")
		}
		if r == '"' {
			l.advance()
			return token{kind: tokString, text: b.String(), pos: start}, nil
		}
		if r == '\\' {
			l.advance()
			esc, w := l.peek()
			if w == 0 {
				return token{}, l.err(l.pos(), "unterminated escape sequence")
			}
			l.advance()
			var out rune
			switch esc {
			case '"', '\\', '/':
				out = esc
			case 'n':
				out = '\n'
			case 't':
				out = '\t'
			case 'r':
				out = '\r'
			default:
				return token{}, l.err(Pos{Line: l.line, Column: l.col - 1},
					fmt.Sprintf("invalid escape \\%c", esc))
			}
			if b.Len()+utf8.RuneLen(out) > MaxStringBytes {
				return token{}, l.err(start, fmt.Sprintf("string exceeds %d bytes", MaxStringBytes)).
					WithCause(ErrLimitExceeded)
			}
			b.WriteRune(out)
			continue
		}
		if b.Len()+utf8.RuneLen(r) > MaxStringBytes {
			return token{}, l.err(start, fmt.Sprintf("string exceeds %d bytes", MaxStringBytes)).
				WithCause(ErrLimitExceeded)
		}
		b.WriteRune(r)
		l.advance()
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
