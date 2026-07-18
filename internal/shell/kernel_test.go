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

// runSession runs statements through one kernel, checking each
// statement's output; a statement may use the bindings of
// earlier ones.
func runSession(t *testing.T, stmts [][2]string) {
	t.Helper()
	k := shell.NewKernel("test")
	for _, stmt := range stmts {
		if got := k.Execute(stmt[0]); got != stmt[1] {
			t.Errorf("%q: got %q, want %q", stmt[0], got,
				stmt[1])
		}
	}
}

func TestExecuteLiterals(t *testing.T) {
	runSession(t, [][2]string{
		{"1;", "val it = 1 : int"},
		{`"a";`, `val it = "a" : string`},
		{`#"a";`, `val it = #"a" : char`},
		{"();", "val it = () : unit"},
		{"true;", "val it = true : bool"},
	})
}

func TestExecuteValAndReuse(t *testing.T) {
	runSession(t, [][2]string{
		{"val x = 2;", "val x = 2 : int"},
		{"x;", "val it = 2 : int"},
		{"it;", "val it = 2 : int"},
		{"val x = \"b\";", `val x = "b" : string`},
		{"x;", `val it = "b" : string`},
	})
}

func TestExecuteBuiltins(t *testing.T) {
	runSession(t, [][2]string{
		{"not true;", "val it = false : bool"},
		{"not (not true);", "val it = true : bool"},
		{`size "abc";`, "val it = 3 : int"},
		{`ord #"A";`, "val it = 65 : int"},
		{"chr 66;", `val it = #"B" : char`},
		{`str #"c";`, `val it = "c" : string`},
	})
}

func TestExecuteLet(t *testing.T) {
	runSession(t, [][2]string{
		{"let val x = 1 in x end;", "val it = 1 : int"},
		{
			"let val x = 1 val y = x in y end;",
			"val it = 1 : int",
		},
		{
			`let val s = "morel" in size s end;`,
			"val it = 5 : int",
		},
	})
}

func TestExecuteIfAndCase(t *testing.T) {
	runSession(t, [][2]string{
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
	})
}

func TestExecuteFunctions(t *testing.T) {
	runSession(t, [][2]string{
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
	})
}

func TestExecuteMultiClause(t *testing.T) {
	runSession(t, [][2]string{
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
	})
}

func TestExecuteClosures(t *testing.T) {
	runSession(t, [][2]string{
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
	})
}

func TestExecuteLists(t *testing.T) {
	runSession(t, [][2]string{
		{"[1, 2, 3];", "val it = [1,2,3] : int list"},
		{"nil;", "val it = [] : 'a list"},
		{"1 :: [2, 3];", "val it = [1,2,3] : int list"},
		{"1 :: 2 :: nil;", "val it = [1,2] : int list"},
		{"[1] @ [2, 3];", "val it = [1,2,3] : int list"},
		{`"foo" ^ "bar";`, `val it = "foobar" : string`},
		{"hd [7, 8];", "val it = 7 : int"},
		{"tl [7, 8];", "val it = [8] : int list"},
		{"length [1, 2, 3];", "val it = 3 : int"},
		{"rev [1, 2];", "val it = [2,1] : int list"},
		{"null nil;", "val it = true : bool"},
		{"null [1];", "val it = false : bool"},
		{
			"map (fn x => x * 2) [1, 2];",
			"val it = [2,4] : int list",
		},
		{
			`explode "ab";`,
			`val it = [#"a",#"b"] : char list`,
		},
		{`implode [#"a", #"b"];`, `val it = "ab" : string`},
		{
			`concat ["con", "cat"];`,
			`val it = "concat" : string`,
		},
		{"hd nil;", "uncaught exception Empty\n" +
			"  raised at: stdIn:1.1-1.7"},
	})
}

func TestExecuteListPatterns(t *testing.T) {
	runSession(t, [][2]string{
		{
			"fun sum nil = 0 | sum (x :: xs) = x + sum xs;",
			"val sum = fn : int list -> int",
		},
		{"sum [1, 2, 3, 4];", "val it = 10 : int"},
		{
			"case [1] of [x] => x | _ => 0;",
			"val it = 1 : int",
		},
		{
			"case [1, 2] of [x] => x | _ => 0;",
			"val it = 0 : int",
		},
		{
			"fun len nil = 0 | len (_ :: xs) = 1 + len xs;",
			"val len = fn : 'a list -> int",
		},
		{`len ["a", "b", "c"];`, "val it = 3 : int"},
		{
			"val (h :: rest) = [5, 6, 7];",
			"val h = 5 : int\nval rest = [6,7] : int list",
		},
	})
}

func TestExecuteEquality(t *testing.T) {
	runSession(t, [][2]string{
		{"1 = 1;", "val it = true : bool"},
		{"1 = 2;", "val it = false : bool"},
		{"(1, 2) = (1, 3);", "val it = false : bool"},
		{"[1, 2] = [1, 2];", "val it = true : bool"},
		{`"a" <> "b";`, "val it = true : bool"},
		{"1 < 2;", "val it = true : bool"},
		{`"abc" < "abd";`, "val it = true : bool"},
		{`#"a" < #"b";`, "val it = true : bool"},
		{"2.5 >= 2.5;", "val it = true : bool"},
		{"3 > 4;", "val it = false : bool"},
	})
}

func TestExecuteTuplesAndRecords(t *testing.T) {
	runSession(t, [][2]string{
		{"(1, true);", "val it = (1,true) : int * bool"},
		{
			"{a=1, b=true};",
			"val it = {a=1,b=true} : {a:int, b:bool}",
		},
		{
			"{b=2, a=1};",
			"val it = {a=1,b=2} : {a:int, b:int}",
		},
		{
			"(1, (2, 3));",
			"val it = (1,(2,3)) : int * (int * int)",
		},
	})
}

func TestExecuteValPatterns(t *testing.T) {
	runSession(t, [][2]string{
		{
			"val (x, y) = (1, true);",
			"val x = 1 : int\nval y = true : bool",
		},
		{"x;", "val it = 1 : int"},
		{
			"val (a, (b, c)) = (1, (2, 3));",
			"val a = 1 : int\nval b = 2 : int\n" +
				"val c = 3 : int",
		},
		{
			"val {a=p, b=q} = {a=4, b=5};",
			"val p = 4 : int\nval q = 5 : int",
		},
		{"val _ = 9;", ""},
		{
			"let val (i, j) = (2, 3) in (j, i) end;",
			"val it = (3,2) : int * int",
		},
	})
}

func TestExecuteArithmetic(t *testing.T) {
	runSession(t, [][2]string{
		{"1 + 2;", "val it = 3 : int"},
		{"1.5 + 2.25;", "val it = 3.75 : real"},
		{"10 - 3;", "val it = 7 : int"},
		{"5 * 6;", "val it = 30 : int"},
		{"7 div 2;", "val it = 3 : int"},
		{"~7 div 2;", "val it = ~4 : int"},
		{"7 mod ~2;", "val it = ~1 : int"},
		{"1.0 / 4.0;", "val it = 0.25 : real"},
		{"true andalso false;", "val it = false : bool"},
		{"false orelse true;", "val it = true : bool"},
		{"1 + 2 * 3;", "val it = 7 : int"},
		{"7 div 0;", "uncaught exception Div" +
			" [divide by zero]\n  raised at: stdIn:1.1-1.8"},
	})
}

func TestExecuteCurrying(t *testing.T) {
	runSession(t, [][2]string{
		{
			"fun add x y = x + y;",
			"val add = fn : int -> int -> int",
		},
		{"add 3 4;", "val it = 7 : int"},
		{"val inc = add 1;", "val inc = fn : int -> int"},
		{"inc 10;", "val it = 11 : int"},
		{
			"fun fact 0 = 1 | fact n = n * fact (n - 1);",
			"val fact = fn : int -> int",
		},
		{"fact 5;", "val it = 120 : int"},
		{
			"fun fib 0 = 0 | fib 1 = 1" +
				" | fib n = fib (n - 1) + fib (n - 2);",
			"val fib = fn : int -> int",
		},
		{"fib 10;", "val it = 55 : int"},
	})
}

func TestExecuteRecursion(t *testing.T) {
	runSession(t, [][2]string{
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
	})
}

// TestExecuteStructures checks structure member access — a
// structure is a record value, so "String.size" is field
// selection — and bare selectors on records and tuples.
func TestExecuteStructures(t *testing.T) {
	runSession(t, [][2]string{
		{`String.size "abc";`, "val it = 3 : int"},
		{
			`String.concat ["a", "bc"];`,
			`val it = "abc" : string`,
		},
		{"Int.abs (~7);", "val it = 7 : int"},
		{"List.length [1, 2, 3];", "val it = 3 : int"},
		{
			"List.map (fn x => x * 2) [3, 4];",
			"val it = [6,8] : int list",
		},
		{"List.rev [1, 2, 3];", "val it = [3,2,1] : int list"},
		{"Bool.not false;", "val it = true : bool"},
		{`Char.ord #"A";`, "val it = 65 : int"},
		{"#1 (8, 9);", "val it = 8 : int"},
		{"#2 (8, 9);", "val it = 9 : int"},
		{"#b {a=1, b=true};", "val it = true : bool"},
		{
			"map #1 [(1, true), (2, false)];",
			"val it = [1,2] : int list",
		},
	})
}

// TestExecuteCrossStatement stress-tests values that flow across
// statement boundaries — the class of bugs that plagued
// morel-rust. Every expected output was probed against the java
// binary.
func TestExecuteCrossStatement(t *testing.T) {
	runSession(t, [][2]string{
		// An escaping lambda calls its sibling recursive
		// function; the pair binds two names in one statement.
		{
			"fun a x = fn () => b x and b x = x + 1;",
			"val a = fn : int -> unit -> int\n" +
				"val b = fn : int -> int",
		},
		{"val g = a 41;", "val g = fn : unit -> int"},
		{"g ();", "val it = 42 : int"},
		// A lambda escaping from inside a recursive function
		// still reaches the function.
		{
			"fun countdown n = if n = 0 then (fn () => 0)" +
				" else (fn () => countdown (n - 1) ());",
			"val countdown = fn : int -> unit -> int",
		},
		{"(countdown 3) ();", "val it = 0 : int"},
		// The recursive reference is created inside the init
		// expression (not the init's own closure), so it must
		// see the final value however deeply it was captured.
		{
			"val rec f = (fn g => g)" +
				" (fn x => if x = 0 then 1 else f (x - 1));",
			"val f = fn : int -> int",
		},
		{"f 3;", "val it = 1 : int"},
		// A local recursive function captured by an escaping
		// lambda, with a captured outer variable.
		{
			"fun outer n = let fun inner 0 = n" +
				" | inner k = inner (k - 1)" +
				" in fn () => inner 5 end;",
			"val outer = fn : 'a -> unit -> 'a",
		},
		{"(outer 7) ();", "val it = 7 : int"},
		// Closures stored in a list, extracted and applied.
		{
			"val fs = [fn x => x + 1, fn x => x * 2];",
			"val fs = [fn,fn] : (int -> int) list",
		},
		{"hd fs 10;", "val it = 11 : int"},
		// A closure held in 'it' and applied from 'it'.
		{"fn x => x + 1;", "val it = fn : int -> int"},
		{"it 5;", "val it = 6 : int"},
		// Mutual recursion used two statements later.
		{
			"fun isEven 0 = true | isEven n = isOdd (n - 1)" +
				" and isOdd 0 = false | isOdd n = isEven (n - 1);",
			"val isEven = fn : int -> bool\n" +
				"val isOdd = fn : int -> bool",
		},
		{
			"val checker = isOdd;",
			"val checker = fn : int -> bool",
		},
		{"checker 9;", "val it = true : bool"},
		{"isEven 10;", "val it = true : bool"},
	})
}

// TestExecuteDatatypes pins datatype-value sessions, including
// constructor-argument parenthesization and the echo of a
// datatype declaration.
func TestExecuteDatatypes(t *testing.T) {
	runSession(t, [][2]string{
		{"SOME 4;", "val it = SOME 4 : int option"},
		{"NONE;", "val it = NONE : 'a option"},
		{
			"SOME (SOME 4);",
			"val it = SOME (SOME 4) : int option option",
		},
		{
			"SOME (1, 2);",
			"val it = SOME (1,2) : (int * int) option",
		},
		{
			"SOME [1, 2];",
			"val it = SOME [1,2] : int list option",
		},
		{"SOME ~1;", "val it = SOME ~1 : int option"},
		{`SOME "a";`, `val it = SOME "a" : string option`},
		{
			"datatype color = RED | GREEN | BLUE;",
			"datatype color = RED | GREEN | BLUE",
		},
		{"RED;", "val it = RED : color"},
		{"SOME RED;", "val it = SOME RED : color option"},
		{
			"datatype shape = CIRCLE of real | SQUARE of real;",
			"datatype shape = CIRCLE of real | SQUARE of real",
		},
		{"CIRCLE 1.5;", "val it = CIRCLE 1.5 : shape"},
		{
			"case SOME 3 of NONE => 0 | SOME n => n + 1;",
			"val it = 4 : int",
		},
		{
			"case RED of RED => 1 | _ => 2;",
			"val it = 1 : int",
		},
		{
			"case GREEN of RED => 1 | _ => 2;",
			"val it = 2 : int",
		},
		{"val SOME z = SOME 9;", "val z = 9 : int"},
		{
			"map SOME [1, 2];",
			"val it = [SOME 1,SOME 2] : int option list",
		},
		{
			"datatype 'a tree = LEAF" +
				" | NODE of 'a tree * 'a * 'a tree;",
			"datatype 'a tree = LEAF" +
				" | NODE of 'a tree * 'a * 'a tree",
		},
		{
			"NODE (LEAF, 5, LEAF);",
			"val it = NODE (LEAF,5,LEAF) : int tree",
		},
		{
			"fun depth LEAF = 0" +
				" | depth (NODE (l, _, r)) = 1 + depth l;",
			"val depth = fn : 'a tree -> int",
		},
		{
			"depth (NODE (NODE (LEAF, 1, LEAF), 2, LEAF));",
			"val it = 2 : int",
		},
		{"LESS;", "val it = LESS : order"},
		{"LESS = EQUAL;", "val it = false : bool"},
		{"SOME 1 = SOME 1;", "val it = true : bool"},
		{"SOME 1 = SOME 2;", "val it = false : bool"},
		{"SOME 1 = NONE;", "val it = false : bool"},
	})
}

// TestExecuteExceptions pins exception reports probed against
// the java binary: the bracketed description, and "raised at"
// spans (an application's own span; a match list's span for
// Bind in case or fn; the pattern-through-expression span for
// Bind in val).
func TestExecuteExceptions(t *testing.T) {
	runSession(t, [][2]string{
		{"chr 999;", "uncaught exception Chr\n" +
			"  raised at: stdIn:1.1-1.8"},
		{"hd (tl [1]);", "uncaught exception Empty\n" +
			"  raised at: stdIn:1.1-1.11"},
		{"(fn 0 => 1) 5;", "uncaught exception Bind" +
			" [nonexhaustive binding failure]\n" +
			"  raised at: stdIn:1.5-1.11"},
		{"case 5 of 0 => 1;", "uncaught exception Bind" +
			" [nonexhaustive binding failure]\n" +
			"  raised at: stdIn:1.11-1.17"},
		{"val (1, x) = (2, 3);", "uncaught exception Bind" +
			" [nonexhaustive binding failure]\n" +
			"  raised at: stdIn:1.5-1.20"},
		// After a failed statement, the session continues and
		// 'it' is untouched.
		{"1;", "val it = 1 : int"},
		{"hd nil;", "uncaught exception Empty\n" +
			"  raised at: stdIn:1.1-1.7"},
		{"it;", "val it = 1 : int"},
	})
}

func TestExecuteNegate(t *testing.T) {
	runSession(t, [][2]string{
		{"~5;", "val it = ~5 : int"},
		{"~2.5;", "val it = ~2.5 : real"},
		{"abs (~5);", "val it = 5 : int"},
		{"if true then ~1 else 1;", "val it = ~1 : int"},
	})
}

func TestExecuteItOnlyOnSuccess(t *testing.T) {
	runSession(t, [][2]string{
		{"val y = 7;", "val y = 7 : int"},
		{"y;", "val it = 7 : int"},
		// The next statement fails to compile (queries
		// arrive later), so 'it' keeps its value.
		{"from i in [1] yield i;", ""},
		{"it;", "val it = 7 : int"},
	})
}

// TestExecuteBasis checks the General, Bool, and Option
// structures.
func TestExecuteBasis(t *testing.T) {
	runSession(t, [][2]string{
		{"General.ignore 42;", "val it = () : unit"},
		{"ignore 42;", "val it = () : unit"},
	})
}
