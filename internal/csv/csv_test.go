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

package csv_test

import (
	"slices"
	"testing"

	"github.com/hydromatic/morel-go/internal/csv"
)

// TestParseHeader checks that a header's fields become columns in
// the order written, that a field may declare no type, and that a
// line with nothing in it has no columns.
func TestParseHeader(t *testing.T) {
	for _, c := range []struct {
		line string
		want []csv.Column
	}{
		{"", nil},
		{"   ", nil},
		{"word:string", []csv.Column{
			{Name: "word", Type: "string", Index: 0},
		}},
		// A field may declare no type.
		{"a,b:int", []csv.Column{
			{Name: "a", Type: "", Index: 0},
			{Name: "b", Type: "int", Index: 1},
		}},
		// Space around a name or a type is not part of it.
		{" a : int , b:string", []csv.Column{
			{Name: "a", Type: "int", Index: 0},
			{Name: "b", Type: "string", Index: 1},
		}},
		// Index is the column's position, whatever its name.
		{"deptno:int,dname:string,loc:string", []csv.Column{
			{Name: "deptno", Type: "int", Index: 0},
			{Name: "dname", Type: "string", Index: 1},
			{Name: "loc", Type: "string", Index: 2},
		}},
		// A type we do not know is passed through; the reader
		// decides what to make of it.
		{"hiredate:date", []csv.Column{
			{Name: "hiredate", Type: "date", Index: 0},
		}},
	} {
		got := csv.ParseHeader(c.line)
		if !slices.Equal(got, c.want) {
			t.Errorf("ParseHeader(%q):\n got %v\nwant %v", c.line,
				got, c.want)
		}
	}
}

// TestCells checks that a row splits on every comma, with no
// escaping, so that an empty cell survives and the count of cells
// is one more than the count of commas.
func TestCells(t *testing.T) {
	for _, c := range []struct {
		line string
		want []string
	}{
		{"", []string{""}},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a,,c", []string{"a", "", "c"}},
		{"7369,'SMITH','CLERK',7902", []string{
			"7369", "'SMITH'", "'CLERK'", "7902",
		}},
	} {
		got := csv.Cells(c.line)
		if !slices.Equal(got, c.want) {
			t.Errorf("Cells(%q):\n got %q\nwant %q", c.line, got,
				c.want)
		}
	}
}

// TestUnquote checks that one pair of surrounding single quotes is
// stripped, and that nothing else is.
func TestUnquote(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"'", "'"},
		{"''", ""},
		{"'SMITH'", "SMITH"},
		{"SMITH", "SMITH"},
		// Only the outermost pair goes.
		{"''SMITH''", "'SMITH'"},
		// An unbalanced quote is not a pair.
		{"'SMITH", "'SMITH"},
		{"SMITH'", "SMITH'"},
		// Quotes inside are left alone.
		{"a'b", "a'b"},
	} {
		if got := csv.Unquote(c.in); got != c.want {
			t.Errorf("Unquote(%q): got %q, want %q", c.in, got,
				c.want)
		}
	}
}

// End csv_test.go
