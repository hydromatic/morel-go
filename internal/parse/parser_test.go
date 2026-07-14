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
			"stdIn:1.6-1.7: expected expression, found ~",
		},
		{
			"f ~x",
			"stdIn:1.3-1.4: expected EOF, found ~",
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
