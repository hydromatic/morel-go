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

package eval_test

import (
	"path/filepath"
	"testing"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/types"
)

// dataDir is the sample file system that the script tests browse.
const dataDir = "../../testdata/data"

// TestFileKind checks that a file's kind comes from its name, and
// that ".csv.gz" wins over ".csv".
func TestFileKind(t *testing.T) {
	for _, c := range []struct {
		path string
		kind eval.FileKind
		base string
	}{
		{dataDir, eval.DirKind, "data"},
		{dataDir + "/scott", eval.DirKind, "scott"},
		{dataDir + "/scott/depts.csv", eval.CSVKind, "depts"},
		{dataDir + "/scott/emps.csv.gz", eval.CSVGzKind, "emps"},
		{dataDir + "/nonesuch.txt", eval.PlainKind, "nonesuch.txt"},
	} {
		f := eval.NewFile(c.path)
		if f.Kind != c.kind {
			t.Errorf("%s: kind %v, want %v", c.path, f.Kind,
				c.kind)
		}
		if f.BaseName != c.base {
			t.Errorf("%s: base name %q, want %q", c.path,
				f.BaseName, c.base)
		}
	}
}

// TestFileTypeProgresses checks the shape of a file's type before
// and after expansion: unknown until looked at, and a directory's
// entries themselves unknown until they are.
func TestFileTypeProgresses(t *testing.T) {
	sys := types.NewSystem()
	root := eval.NewFile(dataDir)
	if got := root.Type(sys).String(); got != "{...}" {
		t.Errorf("unexpanded root: got %q, want {...}", got)
	}
	root.Expand()
	want := "{map:{...}, scott:{...}, wordle:{...}, ...}"
	if got := root.Type(sys).String(); got != want {
		t.Errorf("expanded root:\n got %q\nwant %q", got, want)
	}
	// Discovering a field expands that entry, and only it.
	if !root.DiscoverField("scott") {
		t.Error("discovering scott did not widen the type")
	}
	want = "{map:{...}, " +
		"scott:{bonuses:{...} list, depts:{...} list, " +
		"emps:{...} list, salgrades:{...} list, ...}, " +
		"wordle:{...}, ...}"
	if got := root.Type(sys).String(); got != want {
		t.Errorf("after discovering scott:\n got %q\nwant %q", got,
			want)
	}
	// Discovering the same field again does not widen further.
	if root.DiscoverField("scott") {
		t.Error("rediscovering scott widened the type again")
	}
	// An entry that does not exist never widens.
	if root.DiscoverField("nonesuch") {
		t.Error("discovering a missing entry widened the type")
	}
}

// TestFileRowType checks that a data file's row type comes from its
// header: fields in label order, with the declared types, and an
// unrecognized type (emps' "hiredate:date") read as a string.
func TestFileRowType(t *testing.T) {
	sys := types.NewSystem()
	scott := eval.NewFile(filepath.Join(dataDir, "scott"))
	scott.Expand()
	for _, c := range []struct{ name, want string }{
		{"depts", "{deptno:int, dname:string, loc:string} list"},
		{"salgrades", "{grade:int, hisal:real, losal:real} list"},
		{
			"bonuses",
			"{bonus:real, ename:string, job:string, sal:real} list",
		},
		{
			"emps",
			"{comm:real, deptno:int, empno:int, ename:string, " +
				"hiredate:string, job:string, mgrno:int, " +
				"sal:real} list",
		},
	} {
		scott.DiscoverField(c.name)
		child := scott.Child(c.name)
		if child == nil {
			t.Fatalf("%s: no such entry", c.name)
		}
		if got := child.Type(sys).String(); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestFileReadRows checks that a data file's rows parse, that
// single quotes are stripped, that NULL reads as zero, and that a
// gzipped file is decompressed.
func TestFileReadRows(t *testing.T) {
	depts := eval.NewFile(
		filepath.Join(dataDir, "scott", "depts.csv"))
	rows := depts.ReadRows()
	if len(rows) != 4 {
		t.Fatalf("depts: got %d rows, want 4", len(rows))
	}
	// Fields are in label order: deptno, dname, loc.
	first, ok := rows[0].([]eval.Val)
	if !ok {
		t.Fatalf("depts: row is %T, want []eval.Val", rows[0])
	}
	if first[0] != int32(10) || first[1] != "ACCOUNTING" ||
		first[2] != "NEW YORK" {
		t.Errorf("depts first row: got %v", first)
	}
	// A gzipped file decompresses, and its NULL comm reads as 0.0.
	emps := eval.NewFile(
		filepath.Join(dataDir, "scott", "emps.csv.gz"))
	rows = emps.ReadRows()
	if len(rows) != 14 {
		t.Fatalf("emps: got %d rows, want 14", len(rows))
	}
	first, ok = rows[0].([]eval.Val)
	if !ok {
		t.Fatalf("emps: row is %T, want []eval.Val", rows[0])
	}
	// comm, deptno, empno, ename, hiredate, job, mgrno, sal.
	if first[0] != float32(0) {
		t.Errorf("emps: NULL comm read as %v, want 0", first[0])
	}
	if first[3] != "SMITH" {
		t.Errorf("emps: ename %v, want SMITH", first[3])
	}
	if first[4] != "1980-12-17" {
		t.Errorf("emps: hiredate %v, want 1980-12-17", first[4])
	}
	if first[7] != float32(800) {
		t.Errorf("emps: sal %v, want 800", first[7])
	}
	// A file holding only a header has no rows.
	bonuses := eval.NewFile(
		filepath.Join(dataDir, "scott", "bonuses.csv"))
	if rows := bonuses.ReadRows(); len(rows) != 0 {
		t.Errorf("bonuses: got %d rows, want 0", len(rows))
	}
}

// TestFileReadDoesNotExpand is the invariant that keeps a
// program's type stable: reading a file's rows, as printing its
// value does, must not widen its type. Only DiscoverField may.
func TestFileReadDoesNotExpand(t *testing.T) {
	sys := types.NewSystem()
	root := eval.NewFile(dataDir)
	root.DiscoverField("wordle")
	wordle := root.Child("wordle")
	if wordle == nil {
		t.Fatal("no wordle entry")
	}
	want := "{answers:{...} list, words:{...} list, ...}"
	if got := wordle.Type(sys).String(); got != want {
		t.Fatalf("wordle:\n got %q\nwant %q", got, want)
	}
	// Read both entries' rows, as printing the directory does.
	for _, child := range wordle.Children() {
		if rows := child.ReadRows(); len(rows) == 0 {
			t.Errorf("%s: no rows", child.BaseName)
		}
	}
	// The type is unchanged: the entries were read, not named.
	if got := wordle.Type(sys).String(); got != want {
		t.Errorf("wordle after reading:\n got %q\nwant %q", got,
			want)
	}
	// Naming one of them does widen it.
	wordle.DiscoverField("words")
	want = "{answers:{...} list, words:{word:string} list, ...}"
	if got := wordle.Type(sys).String(); got != want {
		t.Errorf("wordle after discovering words:\n got %q\nwant %q",
			got, want)
	}
}

// TestFileMissing checks that a file we cannot read stays
// unexpanded rather than failing.
func TestFileMissing(t *testing.T) {
	sys := types.NewSystem()
	f := eval.NewFile(filepath.Join(dataDir, "nonesuch"))
	f.Expand()
	if got := f.Type(sys).String(); got != "{...}" {
		t.Errorf("missing file: got %q, want {...}", got)
	}
	if rows := f.ReadRows(); rows != nil {
		t.Errorf("missing file: got %v rows, want none", rows)
	}
}

// End file_test.go
