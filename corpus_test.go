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
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/shell"
	"github.com/hydromatic/morel-go/internal/token"
)

// blank reports whether src contains only whitespace and
// comments.
func blank(src string) bool {
	l := parse.NewLexer("rest", src)
	tok, err := l.Next()
	return err == nil && tok.Kind == token.EOF
}

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
	if !blank(rest) {
		t.Errorf("%s: %d stmts, incomplete rest: %.60q",
			name, len(stmts), rest)
	}
	return len(stmts)
}
