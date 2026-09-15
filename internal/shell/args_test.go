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

func TestParseArgs(t *testing.T) {
	a := shell.ParseArgs([]string{"-e", "1 + 2"})
	if !a.HasEval || a.Eval != "1 + 2" {
		t.Errorf("eval: got %+v", a)
	}
	if !a.Banner {
		t.Errorf("banner should default true")
	}

	a = shell.ParseArgs([]string{
		"--eval=1", "--echo",
		"--banner=false", "--terminal=dumb",
		"--directory=/tmp",
	})
	if a.Eval != "1" || !a.Echo || a.Banner || !a.Dumb ||
		a.Directory != "/tmp" {
		t.Errorf("flags: got %+v", a)
	}

	// A ".smli" first file engages the script harness; a bare "-"
	// is standard input; unknown flags and "execute"/"--build"
	// are ignored.
	a = shell.ParseArgs([]string{
		"execute", "--build",
		"--foreign=X", "a.smli", "-",
	})
	if a.FormOf("a.smli") != shell.FormIdempotent {
		t.Errorf(".smli should engage the script harness")
	}
	if len(a.Files) != 2 || a.Files[0] != "a.smli" ||
		a.Files[1] != "-" {
		t.Errorf("files: got %v", a.Files)
	}

	// A ".sml" first file does not engage it: ".sml" is the
	// ordinary extension for a program, so it runs as one.
	a = shell.ParseArgs([]string{"a.sml"})
	if a.Idempotent {
		t.Errorf(".sml should not imply idempotent")
	}
	if a.FormOf("a.sml") != shell.FormBatch {
		t.Errorf(".sml alone should run as a batch")
	}
}

// TestFormOf checks how a source is read.
//
// Two things decide, as in morel-java. The script harness is engaged
// by "--idempotent" or by a first file ending ".smli", and without it
// every source is an ordinary program. Within the harness the
// extension picks the form: ".smli" carries its own expected output,
// anything else has a transcript in a companion file, and standard
// input is read as ".smli" would be.
func TestFormOf(t *testing.T) {
	plain := shell.ParseArgs([]string{})
	sml := shell.ParseArgs([]string{"a.sml"})
	idem := shell.ParseArgs([]string{"--idempotent"})
	smli := shell.ParseArgs([]string{"a.smli", "b.sml"})
	cases := []struct {
		args *shell.Args
		name string
		want shell.Form
	}{
		// The harness is not engaged: everything is a program.
		{plain, "a.smli", shell.FormBatch},
		{plain, "a.sml", shell.FormBatch},
		{plain, "-", shell.FormBatch},
		{sml, "a.sml", shell.FormBatch},

		// "--idempotent" engages it, and reads standard input as
		// SMLI.
		{idem, "-", shell.FormIdempotent},
		{idem, "stdIn", shell.FormIdempotent},
		{idem, "a.smli", shell.FormIdempotent},
		{idem, "a.sml", shell.FormTranscript},
		{idem, "a.txt", shell.FormTranscript},

		// So does a first file ending ".smli" -- for every file in
		// the run, not only that one.
		{smli, "a.smli", shell.FormIdempotent},
		{smli, "b.sml", shell.FormTranscript},
	}
	for _, c := range cases {
		if got := c.args.FormOf(c.name); got != c.want {
			t.Errorf("FormOf(%q) (idempotent=%v): got %v, want %v",
				c.name, c.args.Idempotent, got, c.want)
		}
	}
}

func TestRunHelp(t *testing.T) {
	var out strings.Builder
	err := shell.ParseArgs([]string{"--help"}).
		Run(strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Usage: morel") {
		t.Errorf("help: got %q", out.String())
	}
}

func TestRunEval(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		{"1 + 2", "val it = 3 : int\n"},
		{"val x = 5", "val x = 5 : int\n"},
		{"2 * 3;", "val it = 6 : int\n"},
	} {
		var out strings.Builder
		err := shell.ParseArgs([]string{"-e", tc.expr}).
			Run(strings.NewReader(""), &out)
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != tc.want {
			t.Errorf("-e %q: got %q, want %q",
				tc.expr, out.String(), tc.want)
		}
	}
}

func TestRunMissingFile(t *testing.T) {
	var out strings.Builder
	err := shell.ParseArgs([]string{"/no/such/file.sml"}).
		Run(strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRunIdempotent(t *testing.T) {
	// A ".smli"/idempotent source goes through RunScript: the
	// script is echoed with each "> " line refreshed.
	in := "val x = 1;\n> stale\n1 + 2;\n"
	want := "val x = 1;\n> val x = 1 : int\n" +
		"1 + 2;\n> val it = 3 : int\n"
	var out strings.Builder
	err := shell.ParseArgs([]string{"--idempotent"}).
		Run(strings.NewReader(in), &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
	// The output is idempotent: running it again is a fixpoint.
	var out2 strings.Builder
	err = shell.ParseArgs([]string{"--idempotent"}).
		Run(strings.NewReader(out.String()), &out2)
	if err != nil {
		t.Fatal(err)
	}
	if out2.String() != out.String() {
		t.Errorf("not idempotent: %q -> %q",
			out.String(), out2.String())
	}
}

func TestRunBatch(t *testing.T) {
	// A non-idempotent source runs as a batch: results only.
	in := "val z = 9;\n1 + 1;\n"
	want := "val z = 9 : int\nval it = 2 : int\n"
	var out strings.Builder
	err := shell.ParseArgs(nil).Run(strings.NewReader(in), &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}
