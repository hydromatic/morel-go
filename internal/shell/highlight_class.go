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

// A class is what a span of source is called in the highlighter's
// finer classification, the one that "Test.highlight" reports. The
// names are Rouge's CSS classes, as in morel-java.
//
// The classes are finer than the categories the shell colors by
// (see Category): they tell a name that is being bound from one
// that is being used, and punctuation from operators. The shell
// has no use for the distinction, but a test does: it pins what
// the scanner understood, not merely what color it chose.
type class int

// The classes a span can have.
const (
	// clsPlain is whitespace, written verbatim rather than wrapped.
	clsPlain class = iota
	// clsComment is the "(*" that opens a comment, clsCommentBody
	// the rest of it.
	clsComment
	clsCommentBody
	clsKeyword
	clsNumeric
	// clsIdent is a name being used; clsValName one being bound by
	// "val" or by a query generator; clsFunName one being bound by
	// "fun".
	clsIdent
	clsValName
	clsFunName
	// clsCtor is a type variable, or a structure name before ".".
	clsCtor
	clsOperator
	clsPunct
	clsString
)

// name is the class's Rouge name, as "Test.highlight" writes it.
func (c class) name() string {
	// lint: sort until '^\t}' where '^\tcase '
	switch c {
	case clsComment:
		return "c"
	case clsCommentBody:
		return "cm"
	case clsCtor:
		return "nn"
	case clsFunName:
		return "nf"
	case clsIdent:
		return "n"
	case clsKeyword:
		return "kr"
	case clsNumeric:
		return "mi"
	case clsOperator:
		return "o"
	case clsPlain:
		return ""
	case clsPunct:
		return "p"
	case clsString:
		return "s2"
	case clsValName:
		return "nv"
	}
	return ""
}

// classSpan is a region of the source and its class. Spans tile
// the source, as Scan's do.
type classSpan struct {
	start int
	end   int
	class class
}

// punctChars are the characters that group into a run of
// punctuation. Everything else that is not a letter, a digit or
// whitespace is an operator.
const punctChars = "()[]{}=,;|."

// Highlight tokenizes Morel source and writes each token as its
// class followed by the token's text in braces, so that
//
//	val x = 1
//
// becomes
//
//	kr{val} nv{x} p{=} mi{1}
//
// Whitespace is written verbatim. It is how "Test.highlight"
// reports what the scanner understood, so that the shell's
// highlighting can be tested from a script.
func Highlight(src string) string {
	var b strings.Builder
	for _, s := range scanClasses(src) {
		text := src[s.start:s.end]
		if s.class == clsPlain {
			b.WriteString(text)
			continue
		}
		b.WriteString(s.class.name())
		b.WriteString("{")
		b.WriteString(text)
		b.WriteString("}")
	}
	return b.String()
}

// scanClasses divides src into spans and gives each its class. It
// never fails, for the reasons Scan does not: the shell scans the
// buffer on every keystroke, and a partly typed line is not valid
// Morel.
//
// It shares its token boundaries with Scan — the same helpers find
// the end of a comment, a string, a quoted name and a number — and
// differs only in how finely it classifies what it finds, and in
// that it carries a context so that it can tell a name being bound
// from one being used.
func scanClasses(src string) []classSpan {
	var spans []classSpan
	// Not in a val pattern until "val" is seen.
	cx := highlightContext{valPatDepth: -1}
	add := func(start, end int, c class) {
		spans = append(spans, classSpan{start, end, c})
	}
	for i := 0; i < len(src); {
		start := i
		r, w := utf8.DecodeRuneInString(src[i:])
		switch {
		case r == '(' && strings.HasPrefix(src[i+w:], "*"):
			// "(*" opens the comment; the rest is its body.
			const openLen = len("(*")
			end := scanComment(src, i)
			add(i, min(i+openLen, end), clsComment)
			if i+openLen < end {
				add(i+openLen, end, clsCommentBody)
			}
			i = end
		case r == '"':
			i = scanString(src, i)
			add(start, i, clsString)
		case r == '`':
			// A quoted name is a name whatever it contains, so a
			// keyword inside backticks is not a keyword.
			i = scanQuotedIdent(src, i)
			add(start, i, cx.nameClass(src, i))
		case r == '\'' && startsIdent(src[i+w:]):
			i = scanIdentRest(src, i+w)
			add(start, i, clsCtor)
			cx.clearNames()
		case unicode.IsLetter(r) || r == '_':
			i = scanIdentRest(src, i+w)
			word := src[start:i]
			if token.Lookup(word) != token.Ident {
				add(start, i, clsKeyword)
				cx.keyword(word)
			} else {
				add(start, i, cx.nameClass(src, i))
			}
		case r >= '0' && r <= '9':
			i = scanNumber(src, i)
			add(start, i, clsNumeric)
		case unicode.IsSpace(r):
			i = scanSpace(src, i+w)
			add(start, i, clsPlain)
		default:
			i = scanOperator(src, i)
			switch {
			case strings.ContainsRune(punctChars, r):
				cx.punct(src[start:i])
				add(start, i, clsPunct)
			case src[start:i] == ":":
				// A lone ":" introduces a type annotation, so it
				// punctuates rather than operates; "::" and ":=" are
				// operators, and scanOperator has already taken them.
				add(start, i, clsPunct)
			default:
				add(start, i, clsOperator)
			}
		}
	}
	return spans
}

// scanOperator returns the end of the symbol beginning at i: a run
// of punctuation characters, or one of the two-character operators
// ("::", ":=", "=>", "->"), or a single symbol character.
//
// It splits where Scan's scanSymbol does not, because punctuation
// and operators are different classes here: "x=1" is one symbol to
// the shell but "p{=}" to a test.
func scanOperator(src string, i int) int {
	r, w := utf8.DecodeRuneInString(src[i:])
	if strings.ContainsRune(punctChars, r) {
		end := i + w
		for end < len(src) {
			r2, w2 := utf8.DecodeRuneInString(src[end:])
			if !strings.ContainsRune(punctChars, r2) {
				break
			}
			end += w2
		}
		return end
	}
	for _, op := range []string{"::", ":=", "=>", "->"} {
		if strings.HasPrefix(src[i:], op) {
			return i + len(op)
		}
	}
	return i + w
}

// highlightContext is what the scan carries so that it can tell a
// name being bound from one being used: which declaration it is
// inside, and how deeply bracketed.
type highlightContext struct {
	// awaitingFunName says the previous keyword was "fun", so the
	// next name is the function being declared.
	awaitingFunName bool
	// valPatDepth is -1 outside a "val" pattern; inside one it is
	// the bracket depth, and every name is being bound. The pattern
	// ends at an "=" at depth 0.
	valPatDepth int
	// fromState is 0 outside a query, 1 in a generator's pattern
	// (where names are bound), 2 in the expression after its "in".
	fromState int
	// fromDepth is the bracket depth while fromState is 2, so that
	// only a top-level "," starts another generator.
	fromDepth int
}

// nameClass is the class of the name ending at end: a structure
// name if "." follows, otherwise one of bound-by-val,
// bound-by-fun, or merely used.
func (cx *highlightContext) nameClass(src string, end int) class {
	if strings.HasPrefix(src[end:], ".") {
		cx.clearNames()
		return clsCtor
	}
	switch {
	case cx.valPatDepth >= 0:
		return clsValName
	case cx.awaitingFunName:
		cx.awaitingFunName = false
		return clsFunName
	case cx.fromState == 1:
		return clsValName
	default:
		return clsIdent
	}
}

// clearNames forgets that a name was expected, for a structure
// name binds nothing.
func (cx *highlightContext) clearNames() {
	cx.valPatDepth = -1
	cx.awaitingFunName = false
}

// keyword notes that a keyword has been scanned, and what it means
// for the names that follow.
func (cx *highlightContext) keyword(word string) {
	cx.awaitingFunName = false
	// lint: sort until '^\t}' where '^\tcase '
	switch {
	case (word == "where" || word == "yield" || word == "group" ||
		word == "order") &&
		(cx.fromState == 1 || cx.fromState == 2 && cx.fromDepth == 0):
		// The generators have ended; no more patterns are expected.
		cx.fromState = 0
	case word == "from":
		cx.fromState, cx.fromDepth, cx.valPatDepth = 1, 0, -1
	case word == "fun":
		cx.awaitingFunName = true
		cx.valPatDepth, cx.fromState = -1, 0
	case word == "in" && cx.fromState == 1:
		// The generator's pattern has ended; its source follows.
		cx.fromState, cx.fromDepth = 2, 0
	case word == "join" && cx.fromState == 2:
		cx.fromState = 1
	case word == "val":
		cx.valPatDepth, cx.fromState = 0, 0
	}
}

// punct notes that a run of punctuation has been scanned, tracking
// bracket depth so that a "," in a query's expression starts
// another generator, and an "=" at depth 0 ends a val pattern.
func (cx *highlightContext) punct(run string) {
	if cx.valPatDepth < 0 && cx.fromState != 2 {
		return
	}
	for _, p := range run {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch p {
		case '(', '[', '{':
			if cx.valPatDepth >= 0 {
				cx.valPatDepth++
			} else {
				cx.fromDepth++
			}
		case ')', ']', '}':
			if cx.valPatDepth > 0 {
				cx.valPatDepth--
			} else if cx.fromDepth > 0 {
				cx.fromDepth--
			}
		case ',':
			if cx.fromState == 2 && cx.fromDepth == 0 {
				cx.fromState = 1
			}
		case '=':
			if cx.valPatDepth == 0 {
				cx.valPatDepth = -1
			}
		}
	}
}

// End highlight_class.go
