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

package ast_test

import (
	"testing"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/token"
)

var noSpan = token.Span{}

func checkDump(t *testing.T, node ast.Node, want string) {
	t.Helper()
	if got := ast.Dump(node); got != want {
		t.Errorf("Dump:\n got %s\nwant %s", got, want)
	}
}

func TestDumpLiterals(t *testing.T) {
	checkDump(t,
		ast.NewLiteral(noSpan, ast.IntLiteralOp, "1"),
		"(int_literal 1)")
	checkDump(t,
		ast.NewLiteral(noSpan, ast.RealLiteralOp, "1.5"),
		"(real_literal 1.5)")
	checkDump(t,
		ast.NewLiteral(noSpan, ast.StringLiteralOp, "ab"),
		`(string_literal "ab")`)
	checkDump(t,
		ast.NewLiteral(noSpan, ast.CharLiteralOp, "a"),
		`(char_literal #"a")`)
}

func TestDumpApply(t *testing.T) {
	// f 1 "x" parses as ((f 1) "x").
	inner := ast.NewApply(noSpan,
		ast.NewID(noSpan, "f"),
		ast.NewLiteral(noSpan, ast.IntLiteralOp, "1"))
	outer := ast.NewApply(noSpan, inner,
		ast.NewLiteral(noSpan, ast.StringLiteralOp, "x"))
	checkDump(t, outer,
		`(apply (apply (id f) (int_literal 1)) `+
			`(string_literal "x"))`)
}

func TestSpan(t *testing.T) {
	span := token.Span{
		Start: token.Pos{Line: 1, Col: 1},
		End:   token.Pos{Line: 1, Col: 2},
	}
	n := ast.NewID(span, "x")
	if n.Span() != span {
		t.Errorf("got %v, want %v", n.Span(), span)
	}
}
