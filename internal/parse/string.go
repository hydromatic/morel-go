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

package parse

import "strings"

// Unquote returns the value of a string or char literal token:
// the text without its quotes (and "#" for a char), with escape
// sequences decoded. The text must have been produced by the
// lexer; malformed input panics.
func Unquote(text string) string {
	s := strings.TrimPrefix(text, "#")
	s = s[1 : len(s)-1]
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if r[i] != '\\' {
			b.WriteRune(r[i])
			continue
		}
		i++
		i += decodeEscape(&b, r[i:])
	}
	return b.String()
}

var simpleEscapes = map[rune]rune{
	'a': '\a', 'b': '\b', 't': '\t', 'n': '\n', 'v': '\v',
	'f': '\f', 'r': '\r', '"': '"', '\\': '\\',
}

// decodeEscape writes the value of one escape sequence starting
// at r (just after the backslash) and returns how many extra
// runes beyond the first it consumed.
func decodeEscape(b *strings.Builder, r []rune) int {
	if c, ok := simpleEscapes[r[0]]; ok {
		b.WriteRune(c)
		return 0
	}
	if r[0] == '^' {
		b.WriteRune(r[1] - '@')
		return 1
	}
	const digits, base = 3, 10
	n := rune(0)
	for k := range digits {
		n = n*base + (r[k] - '0')
	}
	b.WriteRune(n)
	return digits - 1
}

// unquoteIdent returns the name of a backtick-quoted identifier
// token: the text without its backticks, with doubled backticks
// undoubled.
func unquoteIdent(text string) string {
	s := text[1 : len(text)-1]
	return strings.ReplaceAll(s, "``", "`")
}

// QuoteIdent renders a binding name as it appears in printed
// output: a backtick-quoted identifier (doubling any internal
// backtick) when the name contains a backtick or a space,
// otherwise the name unchanged. A reserved word used as a name
// is not quoted here: the reference output prints "val val" for
// a binding named "val". Record labels are
// different: see QuoteLabel.
func QuoteIdent(id string) string {
	if strings.Contains(id, "`") {
		return "`" + strings.ReplaceAll(id, "`", "``") + "`"
	}
	if strings.Contains(id, " ") {
		return "`" + id + "`"
	}
	return id
}

// QuoteLabel renders a record label as printed output. It is like
// QuoteIdent but also back-tick-quotes a reserved word, as
// appendId does: a structure with a member named "exists" prints
// as "{`exists`=fn, ...}". (A binding name of the same spelling
// prints unquoted; only labels quote reserved words.)
func QuoteLabel(id string) string {
	if reservedWords[id] {
		return "`" + id + "`"
	}
	return QuoteIdent(id)
}

// reservedWords are morel's keywords, which may be used as an
// identifier only when back-tick quoted.
var reservedWords = map[string]bool{
	// lint: sort until '^}' where '^\t"'
	"and":       true,
	"andalso":   true,
	"as":        true,
	"case":      true,
	"compute":   true,
	"current":   true,
	"datatype":  true,
	"distinct":  true,
	"div":       true,
	"elem":      true,
	"elements":  true,
	"else":      true,
	"end":       true,
	"eqtype":    true,
	"except":    true,
	"exception": true,
	"exists":    true,
	"fn":        true,
	"forall":    true,
	"from":      true,
	"full":      true,
	"fun":       true,
	"group":     true,
	"if":        true,
	"implies":   true,
	"in":        true,
	"inst":      true,
	"intersect": true,
	"into":      true,
	"join":      true,
	"left":      true,
	"let":       true,
	"mod":       true,
	"notelem":   true,
	"o":         true,
	"of":        true,
	"on":        true,
	"op":        true,
	"order":     true,
	"ordinal":   true,
	"orelse":    true,
	"over":      true,
	"raise":     true,
	"rec":       true,
	"require":   true,
	"right":     true,
	"sig":       true,
	"signature": true,
	"skip":      true,
	"take":      true,
	"then":      true,
	"through":   true,
	"type":      true,
	"typeof":    true,
	"union":     true,
	"unorder":   true,
	"val":       true,
	"where":     true,
	"with":      true,
	"yield":     true,
}
