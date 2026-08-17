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

package terminal_test

import (
	"testing"

	"github.com/hydromatic/morel-go/internal/shell/terminal"
)

// TestComplete checks the test that decides whether the line
// reader keeps reading. It is what puts "= " on the next line
// rather than executing what has been typed so far.
func TestComplete(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		// Nothing typed: accept, so that a blank line just
		// gives a fresh prompt.
		{"", true},
		{"   ", true},
		// A statement is complete at its ';'.
		{"1 + 2;", true},
		{"1 +", false},
		{"1 + 2", false},
		{"val x = 1;", true},
		{"val x =", false},
		// Two statements on one line are both complete.
		{"1; 2;", true},
		// A trailing partial statement is not.
		{"1; 2", false},
		// An unclosed comment or string keeps reading.
		{"(* hi", false},
		{"(* hi *) 1;", true},
		{`"abc`, false},
		{`"abc";`, true},
		// A backslash escapes within the literal, and nothing
		// unescapes the line before the lexer sees it: a string
		// ending in an escaped backslash is closed, and one whose
		// closing quote is escaped is not. morel-java had a line
		// reader that unescaped first, so both went wrong there
		// (morel-java#447); morel-go has no such layer.
		{`"a\\";`, true},
		{`#"\n";`, true},
		{`"a\";`, false},
		// Text that cannot lex is accepted, so that the error
		// is reported rather than the shell waiting for a
		// continuation that cannot come.
		{"1 ~~~ @@@ ;", true},
	} {
		got := terminal.CompleteForTest(tc.text)
		if got != tc.want {
			t.Errorf("complete(%q) = %v, want %v", tc.text,
				got, tc.want)
		}
	}
}
