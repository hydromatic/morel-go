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
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/shell"
)

// scripted is an Executor with a fixed response per statement,
// keyed by the statement's trimmed text.
type scripted map[string]string

func (s scripted) Execute(stmt string) string {
	return s[strings.TrimSpace(stmt)]
}

func checkScript(t *testing.T, exec shell.Executor, src,
	want string,
) {
	t.Helper()
	got, err := shell.RunScript(exec, "t.smli", src)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got != want {
		t.Errorf("RunScript:\n got %q\nwant %q", got, want)
	}
	// A correct script is a fixed point.
	again, err := shell.RunScript(exec, "t.smli", got)
	if err != nil {
		t.Fatalf("RunScript (again): %v", err)
	}
	if again != got {
		t.Errorf("not idempotent:\n got %q\nthen %q", got, again)
	}
}

func TestScriptReplacesExpectedOutput(t *testing.T) {
	exec := scripted{
		"val x = 1;": "val x = 1 : int",
		"val y = 2;": "val y = 2 : int",
	}
	checkScript(t, exec,
		"val x = 1;\n> stale\n\nval y = 2;\n",
		"val x = 1;\n> val x = 1 : int\n\nval y = 2;\n"+
			"> val y = 2 : int\n")
}

func TestScriptMultiLineStatementAndOutput(t *testing.T) {
	exec := scripted{
		"val x =\n  1;": "val x = 1\n  : int",
	}
	checkScript(t, exec,
		"val x =\n  1;\n> old\n> old2\n",
		"val x =\n  1;\n> val x = 1\n>   : int\n")
}

func TestScriptEmptyOutputLine(t *testing.T) {
	exec := scripted{"f ();": "a\n\nb"}
	checkScript(t, exec, "f ();\n",
		"f ();\n> a\n>\n> b\n")
}

func TestScriptOutputLineInComment(t *testing.T) {
	exec := scripted{}
	src := "(* comment with\n> output-like line\n*)\nval x = 1;\n"
	checkScript(t, exec, src, src)
}

func TestScriptOutputLineInString(t *testing.T) {
	exec := scripted{}
	src := "val s = \"text\n> not output\";\n"
	checkScript(t, exec, src, src)
}

func TestScriptCommentsAndTrailerEchoed(t *testing.T) {
	// The statement's verbatim slice includes the comment that
	// precedes it.
	exec := scripted{"(*) leading comment\nval x = 1;": "ok"}
	src := "(*) leading comment\nval x = 1;\n\n(*) End t.smli\n"
	want := "(*) leading comment\nval x = 1;\n> ok\n\n" +
		"(*) End t.smli\n"
	checkScript(t, exec, src, want)
}

func TestScriptLexErrorIsOutput(t *testing.T) {
	exec := scripted{"val y = 2;": "fine"}
	checkScript(t, exec, "val x = ?;\nval y = 2;\n",
		"val x = ?;\n> t.smli:1.9-1.10: illegal character\n"+
			"val y = 2;\n> fine\n")
}

func TestScriptIncompleteAtEOF(t *testing.T) {
	_, err := shell.RunScript(scripted{}, "t.smli",
		"val x = (\n")
	if err == nil {
		t.Fatal("expected error")
	}
}
