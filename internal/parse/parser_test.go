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

package parse_test

import (
	"testing"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/parse"
)

func checkExpr(t *testing.T, src, want string) {
	t.Helper()
	e, err := parse.Expr("stdIn", src)
	if err != nil {
		t.Fatalf("Expr(%q): %v", src, err)
	}
	if got := ast.Dump(e); got != want {
		t.Errorf("Expr(%q):\n got %s\nwant %s",
			src, got, want)
	}
}

func TestParseAtoms(t *testing.T) {
	checkExpr(t, "1", "(int_literal 1)")
	checkExpr(t, "~1", "(int_literal ~1)")
	checkExpr(t, "1.5", "(real_literal 1.5)")
	checkExpr(t, `"ab"`, `(string_literal "ab")`)
	checkExpr(t, `"a\nb"`, "(string_literal \"a\nb\")")
	checkExpr(t, `#"a"`, `(char_literal #"a")`)
	checkExpr(t, "x", "(id x)")
}

func TestParseApply(t *testing.T) {
	checkExpr(t, "f 1", "(apply (id f) (int_literal 1))")
	checkExpr(t, "f 1 2",
		"(apply (apply (id f) (int_literal 1)) (int_literal 2))")
}

func TestParseExprErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"", "stdIn:1.1: expected expression, found EOF"},
		{
			"1 +",
			"stdIn:1.4: expected expression, found EOF",
		},
		// "~" applies only to a literal or a parenthesized
		// expression, not to a bare identifier.
		{
			"~a * ~b",
			"stdIn:1.6: expected expression, found ~",
		},
		{
			"f ~x",
			"stdIn:1.3: expected EOF, found ~",
		},
	} {
		_, err := parse.Expr("stdIn", tc.src)
		if err == nil {
			t.Fatalf("Expr(%q): expected error", tc.src)
		}
		if err.Error() != tc.want {
			t.Errorf("Expr(%q):\n got %q\nwant %q",
				tc.src, err.Error(), tc.want)
		}
	}
}

func TestParseBrackets(t *testing.T) {
	checkExpr(t, "()", "(unit_literal ())")
	checkExpr(t, "(1)", "(int_literal 1)")
	checkExpr(t, "(1, 2)",
		"(tuple (int_literal 1) (int_literal 2))")
	checkExpr(t, "f (1, 2)",
		"(apply (id f) (tuple (int_literal 1) (int_literal 2)))")
	checkExpr(t, "[]", "(list)")
	checkExpr(t, "[1, 2]",
		"(list (int_literal 1) (int_literal 2))")
	checkExpr(t, "[[1], []]",
		"(list (list (int_literal 1)) (list))")
}

func TestParseRecords(t *testing.T) {
	checkExpr(t, "{a = 1, b = 2}",
		"(record (a (int_literal 1)) (b (int_literal 2)))")
	// Fields keep source order.
	checkExpr(t, "{b = 2, a = 1}",
		"(record (b (int_literal 2)) (a (int_literal 1)))")
	// An implicit label is empty until resolution.
	checkExpr(t, "{x, a = 1}",
		"(record ( (id x)) (a (int_literal 1)))")
	checkExpr(t, "{}", "(record)")
}

func TestParseSelectors(t *testing.T) {
	checkExpr(t, "#a", "(record_selector #a)")
	checkExpr(t, "x.a", "(apply (record_selector #a) (id x))")
	checkExpr(t, "x.a.b",
		"(apply (record_selector #b) "+
			"(apply (record_selector #a) (id x)))")
	checkExpr(t, `Sys.parseTree "x"`,
		"(apply (apply (record_selector #parseTree) (id Sys)) "+
			`(string_literal "x"))`)
}

func TestParseOperators(t *testing.T) {
	checkExpr(t, "1 + 2 * 3",
		"(plus (int_literal 1) "+
			"(times (int_literal 2) (int_literal 3)))")
	checkExpr(t, "1 - 2 - 3",
		"(minus (minus (int_literal 1) (int_literal 2)) "+
			"(int_literal 3))")
	checkExpr(t, "1 div 2 mod 3",
		"(mod (div (int_literal 1) (int_literal 2)) "+
			"(int_literal 3))")
	checkExpr(t, "x ^ y ^ z",
		"(caret (caret (id x) (id y)) (id z))")
	checkExpr(t, "f x + g y",
		"(plus (apply (id f) (id x)) (apply (id g) (id y)))")
	checkExpr(t, "1 < 2", "(lt (int_literal 1) (int_literal 2))")
	checkExpr(t, "1 = 2", "(eq (int_literal 1) (int_literal 2))")
	checkExpr(t, "x elem xs", "(elem (id x) (id xs))")
	checkExpr(t, "x notelem xs", "(not_elem (id x) (id xs))")
	checkExpr(t, "f o g o h",
		"(compose (compose (id f) (id g)) (id h))")
	checkExpr(t, "a andalso b orelse c",
		"(orelse (andalso (id a) (id b)) (id c))")
	checkExpr(t, "a implies b", "(implies (id a) (id b))")
}

func TestParseRightAssociative(t *testing.T) {
	checkExpr(t, "1 :: 2 :: xs",
		"(cons (int_literal 1) (cons (int_literal 2) (id xs)))")
	checkExpr(t, "[1] @ [2] @ [3]",
		"(at (list (int_literal 1)) "+
			"(at (list (int_literal 2)) (list (int_literal 3))))")
}

func TestParseNegate(t *testing.T) {
	checkExpr(t, "~x", "(negate (id x))")
	// The operand of "~" is a whole multiplicative chain.
	checkExpr(t, "~x * 2",
		"(negate (times (id x) (int_literal 2)))")
	checkExpr(t, "~a + b", "(plus (negate (id a)) (id b))")
	checkExpr(t, "1 + ~a", "(plus (int_literal 1) (negate (id a)))")
	checkExpr(t, "~f x", "(negate (apply (id f) (id x)))")
	checkExpr(t, "~(1 + 2)",
		"(negate (plus (int_literal 1) (int_literal 2)))")
	checkExpr(t, "~ 1", "(negate (int_literal 1))")
}

func TestParseIf(t *testing.T) {
	checkExpr(t, "if a then b else c",
		"(if (id a) (id b) (id c))")
	// A conditional can be an operand.
	checkExpr(t, "2 * if a then b else c",
		"(times (int_literal 2) (if (id a) (id b) (id c)))")
	checkExpr(t, "1 + case x of _ => 2",
		"(plus (int_literal 1) "+
			"(case (id x) (match wildcard (int_literal 2))))")
}

func TestParseFn(t *testing.T) {
	checkExpr(t, "fn x => x", "(fn (match (idPat x) (id x)))")
	checkExpr(t, "fn _ => 1",
		"(fn (match wildcard (int_literal 1)))")
	checkExpr(t, "fn 0 => 1 | x => x",
		"(fn (match (int_literal_pat 0) (int_literal 1)) "+
			"(match (idPat x) (id x)))")
	// The body extends as far right as possible.
	checkExpr(t, "fn x => x + 1",
		"(fn (match (idPat x) (plus (id x) (int_literal 1))))")
}

func TestParseCase(t *testing.T) {
	checkExpr(t, "case x of 0 => 1 | _ => 2",
		"(case (id x) "+
			"(match (int_literal_pat 0) (int_literal 1)) "+
			"(match wildcard (int_literal 2)))")
}

func TestParsePatterns(t *testing.T) {
	checkExpr(t, "fn (a, b) => a",
		"(fn (match (tuplePat (idPat a) (idPat b)) (id a)))")
	// The unit pattern is an empty tuple pattern.
	checkExpr(t, "fn () => 1",
		"(fn (match (tuplePat) (int_literal 1)))")
	checkExpr(t, "fn x :: xs => x",
		"(fn (match (cons_pat x :: xs) (id x)))")
	checkExpr(t, "fn [a, b] => a",
		"(fn (match (list_pat [a, b]) (id a)))")
	checkExpr(t, "fn {a, b} => a",
		"(fn (match (record_pat {a = a, b = b}) (id a)))")
	checkExpr(t, "fn {a = p} => p",
		"(fn (match (record_pat {a = p}) (id p)))")
	checkExpr(t, "fn {a, ...} => a",
		"(fn (match (record_pat {a = a, ...}) (id a)))")
	checkExpr(t, "fn all as (a, b) => a",
		"(fn (match (as_pat all as (a, b)) (id a)))")
	checkExpr(t, "fn (a :: b) :: c => 1",
		"(fn (match (cons_pat (a :: b) :: c) (int_literal 1)))")
}

func checkDecl(t *testing.T, src, want string) {
	t.Helper()
	n, err := parse.DeclOrExpr("stdIn", src)
	if err != nil {
		t.Fatalf("DeclOrExpr(%q): %v", src, err)
	}
	if got := ast.Dump(n); got != want {
		t.Errorf("DeclOrExpr(%q):\n got %s\nwant %s",
			src, got, want)
	}
}

func TestParseValDecl(t *testing.T) {
	checkDecl(t, "val x = 1",
		"(val (valBind (idPat x) (int_literal 1)))")
	checkDecl(t, "val rec f = fn x => x",
		"(val rec (valBind (idPat f) "+
			"(fn (match (idPat x) (id x)))))")
	checkDecl(t, "val x = 1 and y = 2",
		"(val (valBind (idPat x) (int_literal 1)) "+
			"(valBind (idPat y) (int_literal 2)))")
	checkDecl(t, "val (a, b) = p",
		"(val (valBind (tuplePat (idPat a) (idPat b)) (id p)))")
	// The first "=" binds; later ones are operators.
	checkDecl(t, "val b = 1 = 2",
		"(val (valBind (idPat b) "+
			"(eq (int_literal 1) (int_literal 2))))")
}

func TestParseFunDecl(t *testing.T) {
	checkDecl(t, "fun f x = x",
		"(fun (funBind (funMatch f (idPat x) (id x))))")
	checkDecl(t, "fun f 0 = 1 | f x = x + 1",
		"(fun (funBind "+
			"(funMatch f (int_literal_pat 0) (int_literal 1)) "+
			"(funMatch f (idPat x) "+
			"(plus (id x) (int_literal 1)))))")
	checkDecl(t, "fun f x y = x",
		"(fun (funBind (funMatch f (idPat x) (idPat y) (id x))))")
	checkDecl(t, "fun add (a, b) = a + b",
		"(fun (funBind (funMatch add "+
			"(tuplePat (idPat a) (idPat b)) "+
			"(plus (id a) (id b)))))")
	checkDecl(t, "fun f x = x and g y = y",
		"(fun (funBind (funMatch f (idPat x) (id x))) "+
			"(funBind (funMatch g (idPat y) (id y))))")
}

func TestParseLet(t *testing.T) {
	checkExpr(t, "let val x = 1 in x end",
		"(let (val (valBind (idPat x) (int_literal 1))) (id x))")
	// Declarations may be separated by ";" or nothing.
	checkExpr(t, "let val x = 1; val y = 2 in x + y end",
		"(let (val (valBind (idPat x) (int_literal 1))) "+
			"(val (valBind (idPat y) (int_literal 2))) "+
			"(plus (id x) (id y)))")
	checkExpr(t, "let val x = 1 val y = 2 in x end",
		"(let (val (valBind (idPat x) (int_literal 1))) "+
			"(val (valBind (idPat y) (int_literal 2))) (id x))")
	checkExpr(t, "let fun f x = x in f 1 end",
		"(let (fun (funBind (funMatch f (idPat x) (id x)))) "+
			"(apply (id f) (int_literal 1)))")
}

func TestStmt(t *testing.T) {
	e, err := parse.Stmt("stdIn", "f 1;")
	if err != nil {
		t.Fatal(err)
	}
	if got := ast.Dump(e); got != "(apply (id f) (int_literal 1))" {
		t.Errorf("got %s", got)
	}
	_, err = parse.Stmt("stdIn", "f 1")
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = parse.Stmt("stdIn", "f 1; 2")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUnquote(t *testing.T) {
	for text, want := range map[string]string{
		`"ab"`:      "ab",
		`""`:        "",
		`"a\nb"`:    "a\nb",
		`"q\"q\\q"`: `q"q\q`,
		`"\097"`:    "a",
		`"\^A"`:     "\x01",
		`#"a"`:      "a",
		`#"\t"`:     "\t",
	} {
		if got := parse.Unquote(text); got != want {
			t.Errorf("Unquote(%q) = %q, want %q",
				text, got, want)
		}
	}
}

func TestParseTypeAnnotations(t *testing.T) {
	checkExpr(t, "1 : int",
		"(annotatedExp (int_literal 1) (named int))")
	// ":" binds loosest.
	checkExpr(t, "1 + 2 : int",
		"(annotatedExp (plus (int_literal 1) (int_literal 2)) "+
			"(named int))")
	checkExpr(t, "x : int list",
		"(annotatedExp (id x) (named list (named int)))")
	checkExpr(t, "x : int list list",
		"(annotatedExp (id x) "+
			"(named list (named list (named int))))")
	checkExpr(t, "f : int -> int -> int",
		"(annotatedExp (id f) (fnType (named int) "+
			"(fnType (named int) (named int))))")
	checkExpr(t, "x : int * bool * int",
		"(annotatedExp (id x) (tupleType (named int) "+
			"(named bool) (named int)))")
	checkExpr(t, "x : {a: int, b: string}",
		"(annotatedExp (id x) (record_type {a: int, b: string}))")
	checkExpr(t, "x : 'a",
		"(annotatedExp (id x) (tyVar 'a))")
	checkExpr(t, "x : ('a, 'b) pair",
		"(annotatedExp (id x) (named pair (tyVar 'a) "+
			"(tyVar 'b)))")
	checkExpr(t, "x : (int -> int) list",
		"(annotatedExp (id x) (named list "+
			"(fnType (named int) (named int))))")
	checkExpr(t, "x : unit",
		"(annotatedExp (id x) (named unit))")
}

func TestParseAnnotatedPatterns(t *testing.T) {
	checkExpr(t, "fn (x : int) => x",
		"(fn (match (annotatedPat (idPat x) (named int)) "+
			"(id x)))")
	checkDecl(t, "val x : int = 1",
		"(val (valBind (annotatedPat (idPat x) (named int)) "+
			"(int_literal 1)))")
	checkDecl(t, "fun f (x : int) : int = x",
		"(fun (funBind (funMatch f "+
			"(annotatedPat (idPat x) (named int)) (named int) "+
			"(id x))))")
}

func TestParseDatatypeDecl(t *testing.T) {
	checkDecl(t, "datatype color = RED | GREEN",
		"(datatype_decl datatype color = RED | GREEN)")
	checkDecl(t, "datatype tree = LEAF of int | NODE of tree * tree",
		"(datatype_decl datatype tree = LEAF of int "+
			"| NODE of tree * tree)")
	checkDecl(t, "datatype 'a opt = N | S of 'a",
		"(datatype_decl datatype 'a opt = N | S of 'a)")
	checkDecl(t, "datatype ('a, 'b) pair = P of 'a * 'b",
		"(datatype_decl datatype ('a, 'b) pair = P of 'a * 'b)")
	checkDecl(t, "datatype d = A | B and e = C",
		"(datatype_decl datatype d = A | B and e = C)")
}

func TestParseTypeDecl(t *testing.T) {
	checkDecl(t, "type point = int * int",
		"(type_decl type point = int * int)")
}

func TestParseFrom(t *testing.T) {
	checkExpr(t, "from x in [1, 2]",
		"(from from x in [1, 2])")
	checkExpr(t, "from x in [1, 2] yield x + 1",
		"(from from x in [1, 2] yield x + 1)")
	checkExpr(t, "from x in xs where x > 1",
		"(from from x in xs where x > 1)")
	checkExpr(t, "from x in xs, y in ys",
		"(from from x in xs, y in ys)")
	checkExpr(t, "from x = 1", "(from from x = 1)")
	checkExpr(t, "from x", "(from from x)")
	checkExpr(t, "from (a, b) in ps",
		"(from from (a, b) in ps)")
	// A join unparses as a comma scan.
	checkExpr(t, "from x in xs join y in ys on x = y",
		"(from from x in xs, y in ys on x = y)")
	checkExpr(t, "from x in xs where x > 1 yield x * 2",
		"(from from x in xs where x > 1 yield x * 2)")
	// An implicit record field unparses bare.
	checkExpr(t, "from x in xs yield {x, y = x + 1}",
		"(from from x in xs yield {x, y = x + 1})")
	// A field selection unparses in selector form.
	checkExpr(t, "from e in emps yield e.name",
		"(from from e in emps yield #name e)")
}

func TestParseQuerySteps(t *testing.T) {
	checkExpr(t, "from x in xs group x mod 2",
		"(from from x in xs group x mod 2)")
	checkExpr(t, "from x in xs group {}",
		"(from from x in xs group {})")
	checkExpr(t,
		"from x in xs group {a = x} compute {c = count over ()}",
		"(from from x in xs group {a = x} "+
			"compute {c = count over ()})")
	checkExpr(t,
		"from x in xs group g = {a = x} compute {c = count over ()}",
		"(from from x in xs group g = {a = x} "+
			"compute {c = count over ()})")
	checkExpr(t, "from k in ks order DESC k",
		"(from from k in ks order DESC k)")
	checkExpr(t, "from k in ks order k",
		"(from from k in ks order k)")
	checkExpr(t, "from i in xs distinct",
		"(from from i in xs distinct)")
	checkExpr(t, "from x in xs unorder",
		"(from from x in xs unorder)")
	checkExpr(t, "from i in xs union ys",
		"(from from i in xs union ys)")
	checkExpr(t, "from i in xs intersect ys except zs",
		"(from from i in xs intersect ys except zs)")
	checkExpr(t, "from i in xs union distinct ys",
		"(from from i in xs union distinct ys)")
	checkExpr(t, "from i in xs skip 2 take 3",
		"(from from i in xs skip 2 take 3)")
	checkExpr(t, "from i in xs into f",
		"(from from i in xs into f)")
	checkExpr(t, "from i in xs through p in f",
		"(from from i in xs through p in f)")
	checkExpr(t, "from x in xs yield ordinal",
		"(from from x in xs yield ordinal)")
	checkExpr(t, "from x in xs yield current + 1",
		"(from from x in xs yield current + 1)")
}

func TestParseQuantifiers(t *testing.T) {
	checkExpr(t, "exists e in emps where e > 1",
		"(exists exists e in emps where e > 1)")
	checkExpr(t, "forall e in emps require e > 1",
		"(forall forall e in emps require e > 1)")
}

func TestParseConstructorPatterns(t *testing.T) {
	checkExpr(t, "fn SOME x => x | NONE => 0",
		"(fn (match (con_pat SOME x) (id x)) "+
			"(match (idPat NONE) (int_literal 0)))")
	checkExpr(t, "fn (x, SOME y) => y",
		"(fn (match (tuplePat (idPat x) (con_pat SOME y)) "+
			"(id y)))")
	// In a fun clause, a bare constructor stays a name pattern;
	// application happens only inside full patterns.
	checkDecl(t, "fun height Empty = 0 | height (Node (l, r)) = 1",
		"(fun (funBind (funMatch height (idPat Empty) "+
			"(int_literal 0)) (funMatch height "+
			"(con_pat Node (l, r)) (int_literal 1))))")
}

func TestParseHardening(t *testing.T) {
	checkExpr(t, "t.1", "(apply (record_selector #1) (id t))")
	checkExpr(t, "{1 = true, 2 = 0}",
		"(record (1 (id true)) (2 (int_literal 0)))")
	checkExpr(t, "`o`", "(id o)")
	checkDecl(t, "fun `<<` (a, b) = a",
		"(fun (funBind (funMatch << "+
			"(tuplePat (idPat a) (idPat b)) (id a))))")
	checkExpr(t, "from x in xs group {} compute elements",
		"(from from x in xs group {} compute elements)")
	checkExpr(t, "(from)", "(from from)")
	checkExpr(t, "not exists",
		"(apply (id not) (exists exists))")
	// The "with" source of a record update is not dumped.
	checkExpr(t, "{e with deptno = 10}",
		"(record (deptno (int_literal 10)))")
	checkExpr(t, "x : typeof y",
		"(annotatedExp (id x) (expression_type typeof y))")
	// A fn is a valid application argument.
	checkExpr(t, "iterate edges fn (a, b) => a",
		"(apply (apply (id iterate) (id edges)) "+
			"(fn (match (tuplePat (idPat a) (idPat b)) (id a))))")
	// A set-op step takes a list of arguments.
	checkExpr(t, "from i in xs except distinct ys, zs",
		"(from from i in xs except distinct ys, zs)")
}
