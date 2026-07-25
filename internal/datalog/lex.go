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

package datalog

import "strconv"

// tokKind identifies a token. The declaration order matches the
// reference grammar's token order, because "Was expecting" lists
// enumerate expected tokens in that order.
type tokKind int

const (
	tkEOF tokKind = iota
	tkDecl
	tkInput
	tkOutput
	tkInt
	tkString
	tkLparen
	tkRparen
	tkComma
	tkDot
	tkColon
	tkImplies
	tkBang
	tkLe
	tkGe
	tkNe
	tkLt
	tkGt
	tkEq
	tkPlus
	tkMinus
	tkStar
	tkSlash
	tkIntLit
	tkStrLit
	tkIdent
)

// images is each kind's display form in "Was expecting" lists, as
// javacc's tokenImage array renders it.
var images = [...]string{
	tkEOF:     "<EOF>",
	tkDecl:    `".decl"`,
	tkInput:   `".input"`,
	tkOutput:  `".output"`,
	tkInt:     `"int"`,
	tkString:  `"string"`,
	tkLparen:  `"("`,
	tkRparen:  `")"`,
	tkComma:   `","`,
	tkDot:     `"."`,
	tkColon:   `":"`,
	tkImplies: `":-"`,
	tkBang:    `"!"`,
	tkLe:      `"<="`,
	tkGe:      `">="`,
	tkNe:      `"!="`,
	tkLt:      `"<"`,
	tkGt:      `">"`,
	tkEq:      `"="`,
	tkPlus:    `"+"`,
	tkMinus:   `"-"`,
	tkStar:    `"*"`,
	tkSlash:   `"/"`,
	tkIntLit:  "<INTEGER_LITERAL>",
	tkStrLit:  "<STRING_LITERAL>",
	tkIdent:   "<IDENTIFIER>",
}

// token is a lexed token: its kind, raw image, unescaped string
// value (string literals only), and the position of its first
// character (1-based). The EOF token's position is that of the
// last character of the input, as javacc reports it.
type token struct {
	kind tokKind
	text string
	val  string
	line int
	col  int
}

// lexer scans a Datalog source string into tokens.
type lexer struct {
	src      string
	pos      int
	line     int
	col      int
	lastLine int
	lastCol  int
}

// punct maps single- and double-character punctuation to kinds;
// longer spellings must be tried before their prefixes.
var punct = []struct {
	text string
	kind tokKind
}{
	{".decl", tkDecl},
	{".input", tkInput},
	{".output", tkOutput},
	{":-", tkImplies},
	{"<=", tkLe},
	{">=", tkGe},
	{"!=", tkNe},
	{"(", tkLparen},
	{")", tkRparen},
	{",", tkComma},
	{".", tkDot},
	{":", tkColon},
	{"!", tkBang},
	{"<", tkLt},
	{">", tkGt},
	{"=", tkEq},
	{"+", tkPlus},
	{"-", tkMinus},
	{"*", tkStar},
	{"/", tkSlash},
}

// tokenize scans the whole input, ending with an EOF token.
func tokenize(src string) ([]token, error) {
	l := &lexer{src: src, line: 1, col: 1, lastLine: 1}
	var toks []token
	for {
		l.skip()
		if l.pos >= len(l.src) {
			toks = append(toks, token{
				kind: tkEOF,
				line: l.lastLine,
				col:  l.lastCol,
			})
			return toks, nil
		}
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
	}
}

// skip consumes whitespace and comments: "//" and "#" to end of
// line, and "/* ... */" blocks.
func (l *lexer) skip() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.advance()
		case c == '#' || l.lookingAt("//"):
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.advance()
			}
		case l.lookingAt("/*"):
			l.advance()
			l.advance()
			for l.pos < len(l.src) && !l.lookingAt("*/") {
				l.advance()
			}
			if l.pos < len(l.src) {
				l.advance()
				l.advance()
			}
		default:
			return
		}
	}
}

// next scans one token; skip has already run.
func (l *lexer) next() (token, error) {
	c := l.src[l.pos]
	switch {
	case isIdentStart(c):
		return l.ident(), nil
	case c >= '0' && c <= '9':
		return l.integer(), nil
	case c == '"':
		return l.stringLit()
	}
	for _, p := range punct {
		if l.lookingAt(p.text) {
			tok := token{
				kind: p.kind, text: p.text,
				line: l.line, col: l.col,
			}
			for range len(p.text) {
				l.advance()
			}
			return tok, nil
		}
	}
	return token{}, errorf(
		"Lexical error at line %d, column %d.  Encountered: %q",
		l.line, l.col, rune(c),
	)
}

// ident scans an identifier or the keywords "int" and "string".
func (l *lexer) ident() token {
	tok := token{kind: tkIdent, line: l.line, col: l.col}
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.advance()
	}
	tok.text = l.src[start:l.pos]
	switch tok.text {
	case typeInt:
		tok.kind = tkInt
	case typeString:
		tok.kind = tkString
	}
	return tok
}

// integer scans an integer literal (digits only; no sign).
func (l *lexer) integer() token {
	tok := token{kind: tkIntLit, line: l.line, col: l.col}
	start := l.pos
	for l.pos < len(l.src) &&
		l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.advance()
	}
	tok.text = l.src[start:l.pos]
	return tok
}

// stringLit scans a double-quoted string with the escapes
// \\ \" \n \r \t.
func (l *lexer) stringLit() (token, error) {
	tok := token{kind: tkStrLit, line: l.line, col: l.col}
	start := l.pos
	l.advance()
	var val []byte
	for {
		if l.pos >= len(l.src) {
			return token{}, errorf(
				"Lexical error at line %d, column %d.  "+
					"Encountered: <EOF>", l.lastLine, l.lastCol,
			)
		}
		c := l.src[l.pos]
		l.advance()
		if c == '"' {
			tok.text = l.src[start:l.pos]
			tok.val = string(val)
			return tok, nil
		}
		if c == '\\' && l.pos < len(l.src) {
			e := l.src[l.pos]
			l.advance()
			// lint: sort until '^\t\t\t}' where '^\t\t\tcase '
			switch e {
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			default:
				c = e
			}
		}
		val = append(val, c)
	}
}

// advance consumes one character, tracking line and column.
func (l *lexer) advance() {
	l.lastLine, l.lastCol = l.line, l.col
	if l.src[l.pos] == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	l.pos++
}

// lookingAt reports whether the input continues with a literal.
func (l *lexer) lookingAt(s string) bool {
	return len(l.src)-l.pos >= len(s) &&
		l.src[l.pos:l.pos+len(s)] == s
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// escapeImage renders a token image inside an "Encountered"
// report, escaping as javacc's add_escapes does.
func escapeImage(s string) string {
	var b []byte
	for i := range len(s) {
		c := s[i]
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

// itoa is strconv.Itoa under a short local name.
func itoa(n int) string { return strconv.Itoa(n) }
