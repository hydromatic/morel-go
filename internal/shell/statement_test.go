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
	"slices"
	"testing"

	"github.com/hydromatic/morel-go/internal/shell"
)

func checkSplit(t *testing.T, src string, want []string,
	wantRest string,
) {
	t.Helper()
	stmts, rest, err := shell.Split("stdIn", src)
	if err != nil {
		t.Fatalf("Split(%q): %v", src, err)
	}
	if !slices.Equal(stmts, want) || rest != wantRest {
		t.Errorf("Split(%q):\n got %q rest %q\nwant %q rest %q",
			src, stmts, rest, want, wantRest)
	}
}

const valX = "val x = 1;"

func TestSplit(t *testing.T) {
	checkSplit(t, valX, []string{valX}, "")
	checkSplit(t, "val x = 1; val y = 2;",
		[]string{valX, " val y = 2;"}, "")
	checkSplit(t, "val x = 1;\nval y = 2;\n",
		[]string{valX, "\nval y = 2;"}, "\n")
	checkSplit(t, "1 + 2", nil, "1 + 2")
	checkSplit(t, "", nil, "")
}

func TestSplitSemicolonInComment(t *testing.T) {
	checkSplit(t, "val (* ; *) x = 1;",
		[]string{"val (* ; *) x = 1;"}, "")
	checkSplit(t, "val x = 1 (*) semicolon ;\n+ 2;",
		[]string{"val x = 1 (*) semicolon ;\n+ 2;"}, "")
	checkSplit(t, "(* ; *) (*) ;\n", nil, "(* ; *) (*) ;\n")
	checkSplit(t, "(* a (* b; *) c; *) 1;",
		[]string{"(* a (* b; *) c; *) 1;"}, "")
}

func TestSplitSemicolonInString(t *testing.T) {
	checkSplit(t, `val s = ";";`, []string{`val s = ";";`}, "")
	checkSplit(t, `val s = "a;b" ^ ";";`,
		[]string{`val s = "a;b" ^ ";";`}, "")
}

func TestSplitCloseCommentInLineComment(t *testing.T) {
	// "(*)" inside a block comment hides "*)" to end of line.
	checkSplit(t, "(* a (*) hidden *)\nb *) val x = 1;",
		[]string{"(* a (*) hidden *)\nb *) val x = 1;"}, "")
}

func TestSplitOpStar(t *testing.T) {
	// "(op *)" is not a comment.
	checkSplit(t, "val f = (op *);", []string{"val f = (op *);"},
		"")
	checkSplit(t, "foldl (op +) 0 [1, 2, 3];",
		[]string{"foldl (op +) 0 [1, 2, 3];"}, "")
}

func TestSplitSemicolonInLet(t *testing.T) {
	checkSplit(t, "val a = let val y = 1; val z = 2 in y + z end;",
		[]string{"val a = let val y = 1; val z = 2 in y + z end;"},
		"")
	checkSplit(t, "let\n  val y = 1;\n  val z = 2\nin y + z end;",
		[]string{"let\n  val y = 1;\n  val z = 2\nin y + z end;"},
		"")
	// An unclosed let is incomplete even after a semicolon.
	checkSplit(t, "let val y = 1;", nil, "let val y = 1;")
	checkSplit(t, "(1; 2);", []string{"(1; 2);"}, "")
	checkSplit(t, "[1; 2];", []string{"[1; 2];"}, "")
}

func TestSplitIncomplete(t *testing.T) {
	checkSplit(t, "val x = (* unclosed", nil,
		"val x = (* unclosed")
	checkSplit(t, `val s = "unclosed`, nil, `val s = "unclosed`)
	checkSplit(t, "val q = `unclosed", nil, "val q = `unclosed")
	checkSplit(t, "val x = 1; val y = (2", []string{valX},
		" val y = (2")
}

func TestSplitError(t *testing.T) {
	_, _, err := shell.Split("stdIn", "val x = ?;")
	if err == nil {
		t.Fatal("expected error")
	}
	want := "stdIn:1.9-1.10: illegal character"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestSplitMultiLineString(t *testing.T) {
	// A string may contain raw newlines, and a semicolon within
	// it does not end the statement.
	checkSplit(t, "val s = \"abc;\ndef\";",
		[]string{"val s = \"abc;\ndef\";"}, "")
	checkSplit(t, "val s = \"abc\nval t = 1;", nil,
		"val s = \"abc\nval t = 1;")
}
