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
			"stdIn:1.3-1.4: expected EOF, found +",
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
