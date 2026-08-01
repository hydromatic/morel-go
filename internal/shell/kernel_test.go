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

// Session behavior belongs in the script corpus
// (testdata/script); the tests here pin only properties that a
// script cannot express.

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

func TestExecuteItOnlyOnSuccess(t *testing.T) {
	runSession(t, [][2]string{
		{"val y = 7;", "val y = 7 : int"},
		{"y;", "val it = 7 : int"},
		// The next statement does not evaluate yet (exception
		// handling arrives later), so 'it' keeps its value.
		{"1 handle _ => 2;", ""},
		{"it;", "val it = 7 : int"},
	})
}

func TestEquivalentOutputNoPanic(t *testing.T) {
	// The last top-level colon falls before the value start, so
	// splitOutput once inverted a slice and panicked (outside the
	// recover wrapper); it must return not-equivalent instead.
	k := shell.NewKernel("test")
	if k.EquivalentOutput("val a : b = c", "val a : b = c") {
		t.Error("expected not equivalent (line has no value/type)")
	}
}
