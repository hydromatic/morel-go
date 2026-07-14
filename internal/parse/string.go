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
