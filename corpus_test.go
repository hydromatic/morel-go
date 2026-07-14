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

package morel_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/shell"
)

// TestSplitCorpus splits every morel-java script, with expected
// output lines stripped as the script harness will strip them;
// the corpus must split into statements with nothing left over.
// Skipped when no java checkout is present.
func TestSplitCorpus(t *testing.T) {
	dir := os.Getenv("HOME") +
		"/dev/morel.1/src/test/resources/script"
	files, err := filepath.Glob(dir + "/*.smli")
	if err != nil || len(files) == 0 {
		t.Skipf("no corpus: %v", err)
	}
	total := 0
	for _, f := range files {
		name := filepath.Base(f)
		if name == "scott-queries.smli" ||
			name == "postfix.smli" {
			// Contain "?." (morel#378, post-endpoint).
			continue
		}
		total += splitFile(t, f, name)
	}
	t.Logf("%d files, %d statements", len(files), total)
}

func splitFile(t *testing.T, f, name string) int {
	t.Helper()
	data, err := os.ReadFile(f) //nolint:gosec // fixed test dir
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, ">") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	stmts, rest, err := shell.Split(name, b.String())
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return 0
	}
	if !shell.Blank(name, rest) {
		t.Errorf("%s: %d stmts, incomplete rest: %.60q",
			name, len(stmts), rest)
	}
	return len(stmts)
}

// parseFile parses one script's statements, recording failure
// reasons and a sample statement per reason.
func parseFile(t *testing.T, f string, reasons map[string]int,
	samples map[string]string,
) (int, int) {
	t.Helper()
	name := filepath.Base(f)
	data, err := os.ReadFile(f) //nolint:gosec // fixed test dir
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, ">") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	stmts, _, err := shell.Split(name, b.String())
	if err != nil {
		return 0, 0
	}
	failed := 0
	for _, stmt := range stmts {
		perr := parseErr(name, stmt)
		if perr == "" {
			continue
		}
		failed++
		reasons[perr]++
		if samples[perr] == "" {
			samples[perr] = name + ": " + sampleOf(stmt)
		}
	}
	return len(stmts), failed
}

// parseErr returns the failure reason for a statement, or "".
func parseErr(name, stmt string) string {
	_, err := parse.Stmt(name, stmt)
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return msg
}

// sampleOf returns the first non-comment line of a statement.
func sampleOf(stmt string) string {
	for l := range strings.SplitSeq(stmt, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "(*") &&
			!strings.HasPrefix(l, "*") {
			if len(l) > 70 {
				return l[:70]
			}
			return l
		}
	}
	return ""
}

// maxParseGap is the number of corpus statements that do not yet
// parse. All are feature-gated: ":t" lines (the harness directive),
// "op" (morel#311), range literals (morel#372), yieldAll
// (morel#257), type_string (morel#406), inst/over (morel#237),
// outer joins (morel#75), raise (morel#364), signatures, "[@"
// attributes (morel#369), word literals (morel#396), and a few
// negative-syntax tests that morel-java also rejects. Lower this
// as gaps close; a regression fails the test.
const maxParseGap = 415

// TestParseCorpus parses every statement of every morel-java
// script, reporting failures grouped by error.
func TestParseCorpus(t *testing.T) {
	dir := os.Getenv("HOME") +
		"/dev/morel.1/src/test/resources/script"
	files, err := filepath.Glob(dir + "/*.smli")
	if err != nil || len(files) == 0 {
		t.Skipf("no corpus: %v", err)
	}
	total, failed := 0, 0
	reasons := map[string]int{}
	samples := map[string]string{}
	for _, f := range files {
		n, nf := parseFile(t, f, reasons, samples)
		total += n
		failed += nf
	}
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return reasons[keys[i]] > reasons[keys[j]]
	})
	for _, k := range keys {
		t.Logf("%4d  %s\n      e.g. %s", reasons[k], k,
			samples[k])
	}
	t.Logf("parsed %d/%d statements (%d failures)",
		total-failed, total, failed)
	if failed > maxParseGap {
		t.Errorf("parse failures %d exceed known gap %d",
			failed, maxParseGap)
	}
}
