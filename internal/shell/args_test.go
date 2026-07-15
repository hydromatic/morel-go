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

	// A ".smli" first file makes idempotent implicit; a bare "-"
	// is standard input; unknown flags and "execute"/"--build"
	// are ignored.
	a = shell.ParseArgs([]string{
		"execute", "--build",
		"--foreign=X", "a.smli", "-",
	})
	if !a.Idempotent {
		t.Errorf(".smli should imply idempotent")
	}
	if len(a.Files) != 2 || a.Files[0] != "a.smli" ||
		a.Files[1] != "-" {
		t.Errorf("files: got %v", a.Files)
	}

	// A ".sml" first file does not imply idempotent.
	a = shell.ParseArgs([]string{"a.sml"})
	if a.Idempotent {
		t.Errorf(".sml should not imply idempotent")
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
