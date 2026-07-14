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

package pp_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hydromatic/morel-go/internal/pp"
)

// TestFillNotExponential guards against a regression in which
// rendering a wrapped fill was exponential in the number of
// elements: a fill nests one union per element, and fits recursed
// into each union's wide branch (which contains the next union).
// With the fix, rendering is linear, so many elements render fast;
// the buggy form would take longer than the deadline well before
// this size.
func TestFillNotExponential(t *testing.T) {
	const n = 40
	docs := make([]pp.Doc, n)
	for i := range docs {
		docs[i] = pp.Text("int")
	}
	// The width must be wide enough that col does not exceed it
	// early: that is what let the buggy fits recurse to full depth
	// (2^n). With this config the buggy form runs for far longer
	// than the deadline, while the fixed form is instant.
	d := pp.Fill(pp.Text(" "), docs)
	done := make(chan string, 1)
	go func() { done <- pp.Render(200, d) }()
	select {
	case out := <-done:
		if strings.Count(out, "int") != n {
			t.Fatalf("got %d ints, want %d", strings.Count(out, "int"), n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Render did not finish in 5s: fill rendering is " +
			"exponential again")
	}
}

func TestGroup(t *testing.T) {
	// A group lays out flat when it fits, broken when it does
	// not.
	d := pp.Group(pp.Concat(pp.Text("aaa"), pp.Line(),
		pp.Text("bbb")))
	if got := pp.Render(10, d); got != "aaa bbb" {
		t.Errorf("got %q", got)
	}
	if got := pp.Render(5, d); got != "aaa\nbbb" {
		t.Errorf("got %q", got)
	}
}

func TestNestAndAlign(t *testing.T) {
	d := pp.Nest(2, pp.Concat(pp.Text("head"), pp.HardLine(),
		pp.Text("body")))
	if got := pp.Render(80, d); got != "head\n  body" {
		t.Errorf("got %q", got)
	}
	// Align sets the indent to the current column, so later
	// lines sit under the first element.
	d = pp.Concat(pp.Text("12"),
		pp.Align(pp.Concat(pp.Text("a"), pp.HardLine(),
			pp.Text("b"))))
	if got := pp.Render(80, d); got != "12a\n  b" {
		t.Errorf("got %q", got)
	}
}

func TestFill(t *testing.T) {
	// Fill packs as many elements per line as fit; each gap is
	// decided by whether the next element fits flat.
	docs := []pp.Doc{
		pp.Text("aa,"), pp.Text("bb,"), pp.Text("cc,"),
		pp.Text("dd"),
	}
	if got := pp.Render(7, pp.Fill(pp.Empty(), docs)); got !=
		"aa,bb,\ncc,dd" {
		t.Errorf("got %q", got)
	}
	if got := pp.Render(80, pp.Fill(pp.Empty(), docs)); got !=
		"aa,bb,cc,dd" {
		t.Errorf("got %q", got)
	}
}

func TestUnion(t *testing.T) {
	d := pp.Union(pp.Text("wide layout"),
		pp.Concat(pp.Text("narrow"), pp.HardLine(),
			pp.Text("layout")))
	if got := pp.Render(20, d); got != "wide layout" {
		t.Errorf("got %q", got)
	}
	if got := pp.Render(8, d); got != "narrow\nlayout" {
		t.Errorf("got %q", got)
	}
}
