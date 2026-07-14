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

// recorder is an Executor that brackets each statement it is
// given.
type recorder struct {
	count int
}

func (r *recorder) Execute(stmt string) string {
	r.count++
	return "<" + stmt + ">"
}

func run(t *testing.T, input string) (string, int, error) {
	t.Helper()
	rec := &recorder{}
	var out strings.Builder
	r := shell.NewRunner(rec, strings.NewReader(input), &out,
		"stdIn")
	err := r.Run()
	return out.String(), rec.count, err
}

func TestRunnerAssemblesStatements(t *testing.T) {
	out, count, err := run(t, "val x =\n1;\nval y = 2;\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "<val x =\n1;>\n<\nval y = 2;>\n"
	if out != want || count != 2 {
		t.Errorf("got %q (count %d), want %q (count 2)",
			out, count, want)
	}
}

func TestRunnerCommentSpansLines(t *testing.T) {
	out, count, err := run(t,
		"val x = 1 (* comment\nstill; comment *) + 2;\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "<val x = 1 (* comment\nstill; comment *) + 2;>\n"
	if out != want || count != 1 {
		t.Errorf("got %q (count %d), want %q (count 1)",
			out, count, want)
	}
}

func TestRunnerLexError(t *testing.T) {
	out, count, err := run(t, "val x = ?;\nval y = 2;\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "stdIn:1.9-1.10: illegal character\n<val y = 2;>\n"
	if out != want || count != 1 {
		t.Errorf("got %q (count %d), want %q (count 1)",
			out, count, want)
	}
}

func TestRunnerIncompleteInput(t *testing.T) {
	_, _, err := run(t, "val x = (\n")
	if err == nil {
		t.Fatal("expected error")
	}
	want := "stdIn: unexpected end of input"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestRunnerTrailingComment(t *testing.T) {
	out, count, err := run(t, "val x = 1;\n(* the end *)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "<val x = 1;>\n" || count != 1 {
		t.Errorf("got %q (count %d)", out, count)
	}
}

func TestKernelExecute(t *testing.T) {
	k := shell.NewKernel("stdIn")
	if got := k.Execute("val x = 1;"); got != "" {
		t.Errorf("got %q", got)
	}
	want := "stdIn:1.9-1.10: illegal character"
	if got := k.Execute("val x = ?;"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestKernelParseTree(t *testing.T) {
	k := shell.NewKernel("stdIn")
	got := k.Execute(`Sys.parseTree "f 1";`)
	want := `val it = "(apply (id f) (int_literal 1))" : string`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A result containing quotes is escaped.
	got = k.Execute(`Sys.parseTree "\"x\"";`)
	want = `val it = "(string_literal \"x\")" : string`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// A parse error in the argument is output.
	got = k.Execute(`Sys.parseTree "1 +";`)
	want = "parseTree:1.4: expected expression, found EOF"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// An unknown builtin falls back to validation.
	if got := k.Execute(`Sys.nope "x";`); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestScriptParseTree(t *testing.T) {
	k := shell.NewKernel("t.smli")
	src := "Sys.parseTree \"f 1\";\n> stale\n"
	want := "Sys.parseTree \"f 1\";\n" +
		"> val it = \"(apply (id f) (int_literal 1))\" : string\n"
	got, err := shell.RunScript(k, "t.smli", src)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	again, err := shell.RunScript(k, "t.smli", got)
	if err != nil || again != got {
		t.Errorf("not idempotent: %q -> %q (%v)", got, again, err)
	}
}

func TestBlank(t *testing.T) {
	for src, want := range map[string]bool{
		"":                true,
		"  \n\t":          true,
		"(* comment *)\n": true,
		"(*) line\n":      true,
		"val":             false,
		"(* unterminated": false,
		"1":               false,
	} {
		if got := shell.Blank("stdIn", src); got != want {
			t.Errorf("Blank(%q) = %v, want %v", src, got, want)
		}
	}
}
