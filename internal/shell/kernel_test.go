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
		{"ifAndCase", [][2]string{
			{"if true then 1 else 2;", "val it = 1 : int"},
			{"if false then 1 else 2;", "val it = 2 : int"},
			{"if not true then 1 else 2;", "val it = 2 : int"},
			{
				"if true then (if false then 1 else 2) else 3;",
				"val it = 2 : int",
			},
			{
				`case 3 of 0 => "zero" | _ => "nonzero";`,
				`val it = "nonzero" : string`,
			},
			{
				`case 0 of 0 => "zero" | _ => "nonzero";`,
				`val it = "zero" : string`,
			},
			{"case 42 of n => n;", "val it = 42 : int"},
			{"case 7 of 0 => 0 | n => n;", "val it = 7 : int"},
			{
				`case "a" of "a" => 1 | _ => 2;`,
				"val it = 1 : int",
			},
			{"val n = 5;", "val n = 5 : int"},
			{
				"case n of 5 => true | _ => false;",
				"val it = true : bool",
			},
		}},
		{"functions", [][2]string{
			{"val f = fn x => x;", "val f = fn : 'a -> 'a"},
			{"f 3;", "val it = 3 : int"},
			{`f "a";`, `val it = "a" : string`},
			{"(fn x => x) 5;", "val it = 5 : int"},
			{
				"val g = fn s => size s;",
				"val g = fn : string -> int",
			},
			{`g "abc";`, "val it = 3 : int"},
			{"(fn () => 3) ();", "val it = 3 : int"},
			{"(fn _ => 9) 1;", "val it = 9 : int"},
		}},
		{"multiClause", [][2]string{
			{
				`val h = fn 0 => "zero" | _ => "other";`,
				"val h = fn : int -> string",
			},
			{"h 0;", `val it = "zero" : string`},
			{"h 5;", `val it = "other" : string`},
			{
				"(fn 1 => true | n => false) 3;",
				"val it = false : bool",
			},
		}},
		{"closures", [][2]string{
			{
				"val k = let val a = 7 in fn x => a end;",
				"val k = fn : 'a -> int",
			},
			{"k 100;", "val it = 7 : int"},
			{
				"val add = fn x => fn y => x;",
				"val add = fn : 'a -> 'b -> 'a",
			},
			{"add 1 2;", "val it = 1 : int"},
			{"val p = add true;", "val p = fn : 'a -> bool"},
			{"p ();", "val it = true : bool"},
			// Transitive capture: the innermost function reaches
			// a variable two scopes up.
			{
				"val t = let val a = 4 in" +
					" fn x => fn y => a end;",
				"val t = fn : 'a -> 'b -> int",
			},
			{"t 1 2;", "val it = 4 : int"},
			{
				"case (fn x => x) of f => f 8;",
				"val it = 8 : int",
			},
		}},
		{"recursion", [][2]string{
			{
				`fun f 0 = "done" | f n = f 0;`,
				"val f = fn : int -> string",
			},
			{"f 5;", `val it = "done" : string`},
			{"fun id2 x = x;", "val id2 = fn : 'a -> 'a"},
			{"id2 8;", "val it = 8 : int"},
			{
				"val rec r = fn 0 => 1 | _ => r 0;",
				"val r = fn : int -> int",
			},
			{"r 9;", "val it = 1 : int"},
			{"let val rec g = fn 0 => 2 | _ => g 0" +
				" in g 7 end;", "val it = 2 : int"},
			// Mutual recursion: f calls its sibling g.
			{"let val rec fm = fn () => gm 0" +
				" and gm = fn 0 => 6 | _ => fm ()" +
				" in fm () end;", "val it = 6 : int"},
			// A recursive function stored and called from a
			// later statement keeps working.
			{
				"val keep = f;",
				"val keep = fn : int -> string",
			},
			{"keep 2;", `val it = "done" : string`},
		}},
		{"negate", [][2]string{
			{"~5;", "val it = ~5 : int"},
			{"~2.5;", "val it = ~2.5 : real"},
			{"abs (~5);", "val it = 5 : int"},
			{"if true then ~1 else 1;", "val it = ~1 : int"},
		}},
		{"itOnlyOnSuccess", [][2]string{
			{"val y = 7;", "val y = 7 : int"},
			{"y;", "val it = 7 : int"},
			// The next statement fails to compile (lists
			// arrive later), so 'it' keeps its value.
			{"[1,2];", ""},
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
