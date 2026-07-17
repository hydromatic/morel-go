// Licensed to Julian Hyde under one or more contributor license
// agreements.  See the NOTICE file distributed with this work
// for additional information regarding copyright ownership.
// Julian Hyde licenses this file to you under the Apache
// License, Version 2.0 (the "License"); you may not use this
// file except in compliance with the License.  You may obtain a
// copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied.  See the License for the specific
// language governing permissions and limitations under the
// License.

// Package parse holds the Morel lexer and parser.
package parse

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/hydromatic/morel-go/internal/token"
)

// Error is a lexical or syntax error at a source position.
// Unclosed reports that the input ended inside a construct (a
// string, comment, or quoted identifier); more input could
// complete it.
type Error struct {
	Name     string
	Msg      string
	Span     token.Span
	Unclosed bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%s: %s", e.Name, e.Span, e.Msg)
}

// Lexer splits source text into tokens.
type Lexer struct {
	name   string
	src    []rune
	i      int
	pos    token.Pos
	start  token.Pos
	startI int
}

// NewLexer returns a lexer over src; name (e.g. "stdIn" or a file
// name) is used in error messages.
func NewLexer(name, src string) *Lexer {
	return &Lexer{
		name: name,
		src:  []rune(src),
		pos:  token.Pos{Line: 1, Col: 1},
	}
}

// Next returns the next token, skipping whitespace and comments.
// At end of input it returns an EOF token.
func (l *Lexer) Next() (token.Token, error) {
	err := l.skipTrivia()
	if err != nil {
		return token.Token{}, err
	}
	l.start = l.pos
	l.startI = l.i
	switch r := l.peek(0); {
	case r < 0:
		return l.token(token.EOF), nil
	case isLetter(r):
		return l.scanIdent(), nil
	case isDigit(r):
		return l.scanNumber(), nil
	case r == '~' && isDigit(l.peek(1)):
		l.advance()
		return l.scanNumber(), nil
	case r == '\'' && isLetter(l.peek(1)):
		return l.scanTyVar(), nil
	case r == '`':
		return l.scanQuotedIdent()
	case r == '"':
		return l.scanString(token.StringLit)
	case r == '#':
		return l.scanHash()
	default:
		return l.scanSymbol()
	}
}

// Offset returns the current scan position as a rune index into
// the source.
func (l *Lexer) Offset() int {
	return l.i
}

func (l *Lexer) errorAt(span token.Span, msg string) error {
	return &Error{Name: l.name, Span: span, Msg: msg}
}

func (l *Lexer) errorUnclosed(span token.Span, msg string) error {
	return &Error{
		Name: l.name, Span: span, Msg: msg, Unclosed: true,
	}
}

func (l *Lexer) errorHere(msg string) error {
	span := token.Span{Start: l.start, End: l.pos}
	return l.errorAt(span, msg)
}

// peek returns the rune at offset k from the current position, or
// -1 at end of input.
func (l *Lexer) peek(k int) rune {
	if l.i+k >= len(l.src) {
		return -1
	}
	return l.src[l.i+k]
}

func (l *Lexer) advance() {
	r := l.src[l.i]
	l.i++
	if r == '\n' {
		l.pos.Line++
		l.pos.Col = 1
	} else {
		l.pos.Col++
	}
}

func (l *Lexer) skipN(n int) {
	for range n {
		l.advance()
	}
}

// has reports whether the source at the current position starts
// with s.
func (l *Lexer) has(s string) bool {
	for k, r := range []rune(s) {
		if l.peek(k) != r {
			return false
		}
	}
	return true
}

// hasDigits reports whether the next n runes are all digits.
func (l *Lexer) hasDigits(n int) bool {
	for k := range n {
		if !isDigit(l.peek(k)) {
			return false
		}
	}
	return true
}

// token builds a token whose text is the source from the start of
// the current scan to the current position.
func (l *Lexer) token(k token.Kind) token.Token {
	return token.Token{
		Kind: k,
		Text: string(l.src[l.startI:l.i]),
		Span: token.Span{Start: l.start, End: l.pos},
	}
}

func (l *Lexer) skipTrivia() error {
	for {
		switch {
		case isSpace(l.peek(0)):
			l.advance()
		case l.has("(*)"):
			l.skipLineComment()
		case l.has("(*"):
			err := l.skipBlockComment()
			if err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func (l *Lexer) skipLineComment() {
	for l.peek(0) >= 0 && l.peek(0) != '\n' {
		l.advance()
	}
}

// skipBlockComment consumes a "(* ... *)" comment. Comments nest;
// a "(*)" inside a block comment starts a line comment within
// which "*)" does not close the block.
func (l *Lexer) skipBlockComment() error {
	start := l.pos
	l.skipN(len("(*"))
	depth := 1
	for depth > 0 {
		switch {
		case l.peek(0) < 0:
			span := token.Span{Start: start, End: l.pos}
			return l.errorUnclosed(span, "unclosed comment")
		case l.has("(*)"):
			l.skipLineComment()
		case l.has("(*"):
			l.skipN(len("(*"))
			depth++
		case l.has("*)"):
			l.skipN(len("*)"))
			depth--
		default:
			l.advance()
		}
	}
	return nil
}

func (l *Lexer) scanIdent() token.Token {
	for isIdentPart(l.peek(0)) {
		l.advance()
	}
	t := l.token(token.Ident)
	t.Kind = token.Lookup(t.Text)
	return t
}

// scanTyVar consumes a type variable such as "'a".
func (l *Lexer) scanTyVar() token.Token {
	l.advance()
	for isIdentPart(l.peek(0)) {
		l.advance()
	}
	return l.token(token.TyVar)
}

// scanQuotedIdent consumes a backtick-quoted identifier; a
// doubled backtick is an escape.
func (l *Lexer) scanQuotedIdent() (token.Token, error) {
	l.advance()
	for {
		switch {
		case l.peek(0) < 0:
			span := token.Span{Start: l.start, End: l.pos}
			return token.Token{}, l.errorUnclosed(span,
				"unclosed quoted identifier")
		case l.peek(0) == '`' && l.peek(1) == '`':
			l.skipN(len("``"))
		case l.peek(0) == '`':
			l.advance()
			return l.token(token.QuotedIdent), nil
		default:
			l.advance()
		}
	}
}

// scanNumber consumes an integer, real, or scientific literal; a
// leading "~" has already been consumed.
func (l *Lexer) scanNumber() token.Token {
	// A word literal is "0w" then decimal digits, or "0wx"/"0wX"
	// then hex digits.
	if l.peek(0) == '0' && l.peek(1) == 'w' {
		l.advance()
		l.advance()
		if l.peek(0) == 'x' || l.peek(0) == 'X' {
			l.advance()
			for isHexDigit(l.peek(0)) {
				l.advance()
			}
		} else {
			for isDigit(l.peek(0)) {
				l.advance()
			}
		}
		return l.token(token.WordLit)
	}
	for isDigit(l.peek(0)) {
		l.advance()
	}
	kind := token.IntLit
	if l.peek(0) == '.' && isDigit(l.peek(1)) {
		l.advance()
		for isDigit(l.peek(0)) {
			l.advance()
		}
		kind = token.RealLit
	}
	if l.hasExponent() {
		l.advance()
		if l.peek(0) == '~' {
			l.advance()
		}
		for isDigit(l.peek(0)) {
			l.advance()
		}
		kind = token.ScientificLit
	}
	return l.token(kind)
}

// hasExponent reports whether an exponent follows: "e" or "E",
// then digits with an optional "~" sign.
func (l *Lexer) hasExponent() bool {
	if l.peek(0) != 'e' && l.peek(0) != 'E' {
		return false
	}
	k := 1
	if l.peek(k) == '~' {
		k++
	}
	return isDigit(l.peek(k))
}

// scanString consumes a quoted string, validating escapes:
// \a \b \t \n \v \f \r \" \\, a control escape \^C for C in
// "@"–"_", or a three-digit decimal escape \ddd. A string may
// contain a raw newline.
func (l *Lexer) scanString(kind token.Kind) (token.Token, error) {
	l.advance()
	for {
		switch r := l.peek(0); {
		case r < 0:
			span := token.Span{Start: l.start, End: l.pos}
			return token.Token{},
				l.errorUnclosed(span, "unclosed string")
		case r == '"':
			l.advance()
			return l.token(kind), nil
		case r == '\\':
			err := l.scanEscape()
			if err != nil {
				return token.Token{}, err
			}
		default:
			l.advance()
		}
	}
}

const (
	controlEscapeLen = len(`^C`)
	decimalEscapeLen = len(`ddd`)
)

func (l *Lexer) scanEscape() error {
	start := l.pos
	l.advance()
	r := l.peek(0)
	switch {
	case r >= 0 && strings.ContainsRune(`abtnvfr"\`, r):
		l.advance()
		return nil
	case r == '^' && l.peek(1) >= '@' && l.peek(1) <= '_':
		l.skipN(controlEscapeLen)
		return nil
	case l.hasDigits(decimalEscapeLen):
		l.skipN(decimalEscapeLen)
		return nil
	default:
		span := token.Span{Start: start, End: l.pos}
		return l.errorAt(span, "illegal escape")
	}
}

// scanHash consumes a char literal #"c" or a record label #x.
func (l *Lexer) scanHash() (token.Token, error) {
	if l.peek(1) == '"' {
		l.advance()
		return l.scanString(token.CharLit)
	}
	if !isIdentPart(l.peek(1)) {
		l.advance()
		return token.Token{}, l.errorHere("illegal character")
	}
	l.advance()
	for isIdentPart(l.peek(0)) {
		l.advance()
	}
	return l.token(token.Label), nil
}

var symbols = []struct {
	text string
	kind token.Kind
}{
	{"...", token.Ellipsis},
	{"::", token.Cons},
	{"<=", token.Le},
	{">=", token.Ge},
	{"<>", token.Ne},
	{"=>", token.RArrow},
	{"->", token.RThinArrow},
	{"(", token.LParen},
	{")", token.RParen},
	{"{", token.LBrace},
	{"}", token.RBrace},
	{"[", token.LBracket},
	{"]", token.RBracket},
	{";", token.Semi},
	{"|", token.Bar},
	{".", token.Dot},
	{",", token.Comma},
	{"_", token.Underscore},
	{"=", token.Eq},
	{">", token.Gt},
	{"<", token.Lt},
	{":", token.Colon},
	{"+", token.Plus},
	{"-", token.Minus},
	{"^", token.Caret},
	{"*", token.Star},
	{"/", token.Slash},
	{"~", token.Tilde},
	{"@", token.At},
}

func (l *Lexer) scanSymbol() (token.Token, error) {
	for _, s := range symbols {
		if l.has(s.text) {
			l.skipN(len(s.text))
			return l.token(s.kind), nil
		}
	}
	l.advance()
	return token.Token{}, l.errorHere("illegal character")
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' ||
		r == '\f'
}

func isLetter(r rune) bool {
	return unicode.IsLetter(r)
}

func isDigit(r rune) bool {
	return r >= 0 && unicode.IsDigit(r)
}

func isHexDigit(r rune) bool {
	return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func isIdentPart(r rune) bool {
	return isLetter(r) || isDigit(r) || r == '_' || r == '\''
}
