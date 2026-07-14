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
	"math"
	"testing"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/shell"
	"github.com/hydromatic/morel-go/internal/types"
)

func defaultConfig() shell.Config {
	return shell.Config{
		LineWidth:   79,
		PrintLength: 12,
		PrintDepth:  5,
		StringDepth: 70,
	}
}

func ints(is ...int32) []eval.Val {
	vals := make([]eval.Val, len(is))
	for i, v := range is {
		vals[i] = v
	}
	return vals
}

// TestPrettyBinding pins layouts at lineWidth 20: the value
// moves to its own line (indented 2)
// when it does not fit after "val it =", elements fill across
// lines aligned under the first, and the type follows on its own
// line.
func TestPrettyBinding(t *testing.T) {
	sys := types.NewSystem()
	c := defaultConfig()
	c.LineWidth = 20

	intList := sys.List(sys.Int)
	got := shell.PrettyBindingForTest(c, "it",
		ints(100000, 200000, 300000, 400000), intList)
	want := "val it =\n" +
		"  [100000,200000,\n" +
		"   300000,400000]\n" +
		"  : int list"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	got = shell.PrettyBindingForTest(c, "it", ints(1, 2), intList)
	want = "val it = [1,2]\n  : int list"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	c2 := defaultConfig()
	got = shell.PrettyBindingForTest(c2, "it", ints(1, 2), intList)
	want = "val it = [1,2] : int list"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestPrintLength checks the "..." elision of long lists.
func TestPrintLength(t *testing.T) {
	sys := types.NewSystem()
	c := defaultConfig()
	c.PrintLength = 3
	got := shell.PrettyBindingForTest(c, "it", ints(1, 2, 3, 4, 5, 6),
		sys.List(sys.Int))
	want := "val it = [1,2,3,...] : int list"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPrintDepth checks the "#" elision of deep nesting, as
// probed: at printDepth 2, "(1,(2,(3,4)))" prints "(1,(#,#))".
func TestPrintDepth(t *testing.T) {
	sys := types.NewSystem()
	c := defaultConfig()
	c.PrintDepth = 2
	inner2, err := sys.Parse("int * int")
	if err != nil {
		t.Fatal(err)
	}
	inner1 := sys.Tuple(sys.Int, inner2)
	outer := sys.Tuple(sys.Int, inner1)
	v := []eval.Val{
		int32(1),
		[]eval.Val{int32(2), []eval.Val{int32(3), int32(4)}},
	}
	got := shell.PrettyBindingForTest(c, "it", v, outer)
	want := "val it = (1,(#,#)) : int * (int * (int * int))"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestStringDepth checks string truncation: at stringDepth 5,
// "abcdefghij" prints as "abcde#".
func TestStringDepth(t *testing.T) {
	sys := types.NewSystem()
	c := defaultConfig()
	c.StringDepth = 5
	got := shell.PrettyBindingForTest(c, "it", "abcdefghij", sys.String)
	want := `val it = "abcde#" : string`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatReal pins Real.toString formats.
func TestFormatReal(t *testing.T) {
	for _, tc := range []struct {
		f    float32
		want string
	}{
		{1.0, "1"},
		{0.0, "0"},
		{float32(math.Copysign(0, -1)), "~0"},
		{-2.0, "~2"},
		{-2.5, "~2.5"},
		{1.5, "1.5"},
		{0.1, "0.1"},
		{0.001, "0.001"},
		{0.0001, "1E~4"},
		{1234567.0, "1234567"},
		{12345678.0, "1.2345678E7"},
		{1e10, "1E10"},
		{-1e10, "~1E10"},
		{1e-10, "1E~10"},
		{6.02e23, "6.02E23"},
		{float32(math.Inf(1)), "inf"},
		{float32(math.Inf(-1)), "~inf"},
		{float32(math.NaN()), "nan"},
		{1.4e-45, "1.4E~45"},
	} {
		if got := shell.FormatRealForTest(tc.f); got != tc.want {
			t.Errorf("formatReal(%v): got %q, want %q", tc.f,
				got, tc.want)
		}
	}
}
