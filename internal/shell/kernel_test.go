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

// TestExecute runs statements through one kernel, checking each
// statement's output; a statement may use the bindings of
// earlier ones.
func TestExecute(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stmts [][2]string
	}{
		{"literals", [][2]string{
			{"1;", "val it = 1 : int"},
			{`"a";`, `val it = "a" : string`},
			{`#"a";`, `val it = #"a" : char`},
			{"();", "val it = () : unit"},
			{"true;", "val it = true : bool"},
		}},
		{"valAndReuse", [][2]string{
			{"val x = 2;", "val x = 2 : int"},
			{"x;", "val it = 2 : int"},
			{"it;", "val it = 2 : int"},
			{"val x = \"b\";", `val x = "b" : string`},
			{"x;", `val it = "b" : string`},
		}},
		{"builtins", [][2]string{
			{"not true;", "val it = false : bool"},
			{"not (not true);", "val it = true : bool"},
			{`size "abc";`, "val it = 3 : int"},
			{`ord #"A";`, "val it = 65 : int"},
			{"chr 66;", `val it = #"B" : char`},
			{`str #"c";`, `val it = "c" : string`},
		}},
		{"let", [][2]string{
			{"let val x = 1 in x end;", "val it = 1 : int"},
			{
				"let val x = 1 val y = x in y end;",
				"val it = 1 : int",
			},
			{
				`let val s = "morel" in size s end;`,
				"val it = 5 : int",
			},
		}},
		{"itOnlyOnSuccess", [][2]string{
			{"val y = 7;", "val y = 7 : int"},
			{"y;", "val it = 7 : int"},
			// The next statement fails to compile (functions
			// arrive later), so 'it' keeps its value.
			{"fn x => x;", ""},
			{"it;", "val it = 7 : int"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := shell.NewKernel("test")
			for _, stmt := range tc.stmts {
				if got := k.Execute(stmt[0]); got != stmt[1] {
					t.Errorf("%q: got %q, want %q", stmt[0],
						got, stmt[1])
				}
			}
		})
	}
}
