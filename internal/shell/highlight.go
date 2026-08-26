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

package shell

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hydromatic/morel-go/internal/token"
)

// Scan classifies source for syntax highlighting. It is not the
// parser's lexer, and deliberately so: the shell highlights on
// every keystroke, so it must emit comments (which the lexer
// discards as trivia) and must never fail on input that is merely
// incomplete — a string is unterminated on the way to being
// terminated, and `"\` is a state that typing any escape passes
// through. morel-java and morel-rust each highlight with a
// separate scanner for the same reasons.

// Category is what a span of source highlights as. It is a
// simplification of the parser's token kinds: kinds that render
// alike collapse into one category, and an identifier that is
// neither keyword nor constant keeps the terminal's default style.
type Category int

// The categories a span can have. A color scheme gives each of them
// a style; the categories are named for the fields of the scheme
// records that Sys.colorSchemes returns.
const (
	// CatNone is whitespace, and anything else left unstyled.
	CatNone Category = iota
	CatComment
	CatConstant
	// CatError is never produced here; a color scheme carries a
	// style for it because the shell colors error text too.
	CatError
	CatIdentifier
	CatKeyword
	CatNumeric
	CatString
	CatSymbol
	// CatTypeVar covers type variables ('a) and, as in morel-java
	// and morel-rust, the structure name of a qualified reference
	// (List in List.map).
	CatTypeVar
)

// constants are the identifiers that highlight as constants rather
// than as plain identifiers.
var constants = map[string]bool{
	"false": true,
	"nil":   true,
	"true":  true,
}

// Span is a region of the source and what it highlights as. Spans
// tile the source: they are in order and abut, so concatenating
// src[s.Start:s.End] over the spans reproduces src.
type Span struct {
	Start    int
	End      int
	Category Category
}

// Scan divides src into spans. It never fails: an unterminated
// comment or string runs to the end of the input.
func Scan(src string) []Span {
	var spans []Span
	for i := 0; i < len(src); {
		start := i
		var cat Category
		r, w := utf8.DecodeRuneInString(src[i:])
		switch {
		case r == '(' && strings.HasPrefix(src[i+w:], "*"):
			i, cat = scanComment(src, i), CatComment
		case r == '"':
			i, cat = scanString(src, i), CatString
		case r == '`':
			i, cat = scanQuotedIdent(src, i), CatIdentifier
		case r == '\'' && startsIdent(src[i+w:]):
			i, cat = scanIdentRest(src, i+w), CatTypeVar
		case unicode.IsLetter(r) || r == '_':
			i = scanIdentRest(src, i+w)
			cat = wordCategory(src[start:i], src[i:])
		case r >= '0' && r <= '9':
			i, cat = scanNumber(src, i), CatNumeric
		case unicode.IsSpace(r):
			i, cat = scanSpace(src, i+w), CatNone
		default:
			i, cat = scanSymbol(src, i+w), CatSymbol
		}
		spans = append(spans, Span{start, i, cat})
	}
	return spans
}

// wordCategory classifies an identifier, given the text that
// follows it: a keyword, the structure name of a qualified
// reference, a constant, or a plain identifier.
func wordCategory(word, rest string) Category {
	switch {
	case token.Lookup(word) != token.Ident,
		token.NonReserved[word]:
		return CatKeyword
	case strings.HasPrefix(rest, "."):
		return CatTypeVar
	case constants[word]:
		return CatConstant
	default:
		return CatIdentifier
	}
}

// startsIdent reports whether s begins with a letter, so that "'a"
// is a type variable but a lone "'" is a symbol.
func startsIdent(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLetter(r)
}

// isIdentRune reports whether r continues an identifier or a type
// variable.
func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) ||
		r == '_' || r == '\''
}

// isSymbolRune reports whether r continues a run of operator
// characters: anything that does not start some other token.
func isSymbolRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
		!unicode.IsSpace(r) && r != '"' && r != '\'' &&
		r != '_' && r != '`'
}

// scanIdentRest returns the offset just past an identifier whose
// first rune ends at i.
func scanIdentRest(src string, i int) int {
	for i < len(src) {
		r, w := utf8.DecodeRuneInString(src[i:])
		if !isIdentRune(r) {
			break
		}
		i += w
	}
	return i
}

// scanSpace returns the offset just past a run of whitespace whose
// first rune ends at i.
func scanSpace(src string, i int) int {
	for i < len(src) {
		r, w := utf8.DecodeRuneInString(src[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += w
	}
	return i
}

// scanSymbol returns the offset just past a run of operator
// characters whose first rune ends at i.
func scanSymbol(src string, i int) int {
	for i < len(src) {
		r, w := utf8.DecodeRuneInString(src[i:])
		if !isSymbolRune(r) {
			break
		}
		i += w
	}
	return i
}

// scanComment returns the offset just past the comment starting at
// i. "(*)" starts a line comment, which runs to the end of the
// line; any other "(*" starts a block comment, which nests and runs
// to the matching "*)", or to the end of the input if it is
// unterminated.
func scanComment(src string, start int) int {
	if strings.HasPrefix(src[start:], "(*)") {
		if j := strings.IndexByte(src[start+3:], '\n'); j >= 0 {
			return start + 3 + j
		}
		return len(src)
	}
	depth := 0
	for i := start; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "(*"):
			depth++
			i += 2
		case strings.HasPrefix(src[i:], "*)"):
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(src)
}

// scanString returns the offset just past the string literal
// starting at i, or the end of the input if it is unterminated. A
// backslash escapes the next byte; the skip is clamped, so that the
// trailing backslash of a half-typed escape does not run past the
// end (morel-java #415).
func scanString(src string, start int) int {
	// An escape is a backslash and the byte it escapes.
	const escapeLen = len(`\x`)
	for i := start + 1; i < len(src); {
		switch src[i] {
		case '\\':
			i = min(i+escapeLen, len(src))
		case '"':
			return i + 1
		default:
			i++
		}
	}
	return len(src)
}

// scanQuotedIdent returns the offset just past the quoted
// identifier starting at start, or the end of the input if it is
// unterminated. A doubled backtick stands for a literal one, as in
// the parser's lexer, so it does not close the identifier.
//
// The whole of `let val` is one identifier, keywords and all, so
// none of it highlights as a keyword.
func scanQuotedIdent(src string, start int) int {
	for i := start + 1; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "``"):
			i += len("``")
		case src[i] == '`':
			return i + 1
		default:
			i++
		}
	}
	return len(src)
}

// scanNumber returns the offset just past the numeric literal
// starting at start: an integer, a word (0w7, 0wx1F), a real (1.5)
// or a scientific literal (1e~7).
func scanNumber(src string, start int) int {
	digit := func(i int) bool {
		return i < len(src) && src[i] >= '0' && src[i] <= '9'
	}
	hex := func(i int) bool {
		return i < len(src) &&
			strings.IndexByte("0123456789abcdefABCDEF",
				src[i]) >= 0
	}
	// Word literal: "0w" digits, or "0wx" hex digits. If no digit
	// follows, this is not a word literal, and the leading 0 is
	// scanned as an ordinary integer below.
	if strings.HasPrefix(src[start:], "0w") {
		j, isDigit := start+len("0w"), digit
		if strings.HasPrefix(src[j:], "x") ||
			strings.HasPrefix(src[j:], "X") {
			j, isDigit = start+len("0wx"), hex
		}
		if isDigit(j) {
			for isDigit(j) {
				j++
			}
			return j
		}
	}
	i := start
	for digit(i) {
		i++
	}
	// Fractional part: a '.' must be followed by a digit, so that
	// the '.' of "1.map" stays a symbol.
	if i < len(src) && src[i] == '.' && digit(i+1) {
		i += 2
		for digit(i) {
			i++
		}
	}
	// Exponent: [eE], an optional '~', then digits.
	if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
		j := i + 1
		if j < len(src) && src[j] == '~' {
			j++
		}
		if digit(j) {
			i = j
			for digit(i) {
				i++
			}
		}
	}
	return i
}
