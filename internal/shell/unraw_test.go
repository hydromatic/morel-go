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
	"testing"

	"github.com/hydromatic/morel-go/internal/shell"
)

// TestUnraw checks that a raw string literal in a script's expected
// output is read as the escaped literal with the same content, and
// that nothing else is touched -- in particular that a fence
// written inside a quoted literal is content, not syntax.
func TestUnraw(t *testing.T) {
	cases := []struct{ in, want string }{
		// A raw literal becomes the escaped literal.
		{
			"val it = {|a\nb|} : string",
			`val it = "a\nb" : string`,
		},
		// A tag lets the content hold "|}".
		{
			"val it = {x|a|}b|x} : string",
			`val it = "a|}b" : string`,
		},
		// A tag beginning "_" means the content starts on the next
		// line, and that newline is not content.
		{
			"val it = {_|\np|_} : string",
			`val it = "p" : string`,
		},
		// A fence inside a quoted literal is three characters of a
		// string, not a fence.
		{
			`val it = "{a|x|a}" : string`,
			`val it = "{a|x|a}" : string`,
		},
		{
			`val it = ["{|p|}","q"] : string list`,
			`val it = ["{|p|}","q"] : string list`,
		},
		// An escaped quote does not end the literal.
		{
			`val it = "say \"{|x|}\"" : string`,
			`val it = "say \"{|x|}\"" : string`,
		},
		// A record is not a fence: "{" that no "|" follows.
		{
			`val it = {a=1,b=2} : {a:int, b:int}`,
			`val it = {a=1,b=2} : {a:int, b:int}`,
		},
		// An unclosed fence is not a literal.
		{"val it = {|a : string", "val it = {|a : string"},
	}
	for _, c := range cases {
		if got := shell.UnrawForTest(c.in); got != c.want {
			t.Errorf("unraw(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}
