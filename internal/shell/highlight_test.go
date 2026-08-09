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

package shell_test

import (
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/shell"
)

// catLetter renders a category as one character, so that a whole
// scan can be written as a string under the source it classifies.
var catLetter = map[shell.Category]byte{
	shell.CatNone:       '.',
	shell.CatComment:    'c',
	shell.CatConstant:   'k',
	shell.CatError:      'e',
	shell.CatIdentifier: 'i',
	shell.CatKeyword:    'K',
	shell.CatNumeric:    'n',
	shell.CatString:     's',
	shell.CatSymbol:     'y',
	shell.CatTypeVar:    't',
}

// scanString renders Scan's output as one letter per byte.
func scanString(t *testing.T, src string) string {
	t.Helper()
	spans := shell.Scan(src)
	var b strings.Builder
	prev := 0
	for _, s := range spans {
		if s.Start != prev {
			t.Errorf("%q: span starts at %d, want %d",
				src, s.Start, prev)
		}
		if s.End <= s.Start {
			t.Fatalf("%q: empty span at %d", src, s.Start)
		}
		letter, ok := catLetter[s.Category]
		if !ok {
			t.Fatalf("%q: unknown category %d", src, s.Category)
		}
		b.WriteString(strings.Repeat(string(letter),
			s.End-s.Start))
		prev = s.End
	}
	if prev != len(src) {
		t.Errorf("%q: spans end at %d, want %d", src, prev,
			len(src))
	}
	return b.String()
}

func TestScan(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// Keywords, identifiers, symbols, numbers.
		{"val x = 1;", "KKK.i.y.ny"},
		{"fun f x = x + 1", "KKK.i.i.y.i.y.n"},
		// true, false and nil are constants; other identifiers
		// keep the default style.
		{"true", "kkkk"},
		{"nil", "kkk"},
		{"truest", "iiiiii"},
		// A structure name highlights like a type variable.
		{"List.map", "ttttyiii"},
		// "list" is a type constructor, not a reserved word, so
		// it keeps the default style.
		{"'a list", "tt.iiii"},
		// A lone quote is a symbol, not a type variable.
		{"'", "y"},
		// Literals: int, real, scientific, word, hex word.
		{"1", "n"},
		{"1.5", "nnn"},
		{"1e~7", "nnnn"},
		{"0w7", "nnn"},
		{"0wx1F", "nnnnn"},
		// "1.map" is 1, then '.', then map: the '.' does not
		// start a fractional part unless a digit follows.
		{"1.map", "nyiii"},
		// "0w" with no digit is the integer 0 then the
		// identifier w.
		{"0w", "ni"},
		// Comments, block and line, and nesting.
		{"(* hi *)", "cccccccc"},
		{"(* a (* b *) c *)", "ccccccccccccccccc"},
		{"(*) to end", "cccccccccc"},
		{"1 (*) two", "n.ccccccc"},
		// Strings. An escaped quote does not end the string.
		{`"abc"`, "sssss"},
		{`"a\"b"`, "ssssss"},
	} {
		if got := scanString(t, tc.src); got != tc.want {
			t.Errorf("Scan(%q):\n got %s\nwant %s",
				tc.src, got, tc.want)
		}
	}
}

// What the scanner makes of incomplete input and of quoted
// identifiers is tested by "highlight.smli", via "Test.highlight",
// which reports the finer classification and covers more cases
// than the tests that used to be here.

// TestScanEveryPrefix checks that scanning any prefix of a
// statement terminates and tiles the input, as the shell does on
// each keystroke. It stays here because it is a property over
// every prefix, which a script cannot express.
func TestScanEveryPrefix(t *testing.T) {
	const src = `val s = "a\"b" (* c *) 0wx1F ~1.5e~7 'a List.map;`
	for i := 0; i <= len(src); i++ {
		scanString(t, src[:i])
	}
}
