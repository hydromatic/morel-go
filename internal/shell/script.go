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

package shell

import (
	"fmt"
	"strings"
)

// semanticMatcher is an optional Executor capability: it reports
// whether an actual output is semantically equivalent to an
// expected one (bag values as multisets, whitespace normalized).
// When available, RunScript preserves the expected output for a
// statement whose actual output matches it only semantically, so
// that unordered results do not diverge on element order.
type semanticMatcher interface {
	EquivalentOutput(actual, expected string) bool
}

// RunScript runs an idempotent script: statements interleaved
// with their expected output on "> "-prefixed lines. Each input
// line is echoed verbatim, except expected-output lines, which
// are replaced by the actual output of the statement they follow.
// Running a correct script reproduces it exactly.
//
// When the executor can compare output semantically, a statement's
// original expected output is kept if it matches the actual output
// as a bag or up to whitespace; otherwise the actual output is
// emitted. So a genuine mismatch still shows, but an unordered
// result does not diverge merely on element order.
//
// A line is expected output if it is ">" or starts with "> " —
// unless it lies inside an unclosed comment or string, where it
// is content.
func RunScript(exec Executor, name, src string) (string, error) {
	matcher, _ := exec.(semanticMatcher)
	var out strings.Builder
	buf := ""
	var actuals []string
	var expected strings.Builder
	flush := func() {
		if len(actuals) == 0 {
			return
		}
		out.WriteString(resolveOutput(matcher, actuals,
			expected.String()))
		actuals = actuals[:0]
		expected.Reset()
	}
	for _, line := range strings.SplitAfter(src, "\n") {
		if line == "" {
			continue
		}
		if isOutputLine(line) && !Unclosed(name, buf) {
			expected.WriteString(line)
			continue
		}
		flush()
		out.WriteString(line)
		buf += line
		stmts, rest, err := Split(name, buf)
		if err != nil {
			actuals = append(actuals, err.Error())
			flush()
			buf = ""
			continue
		}
		for _, stmt := range stmts {
			actuals = append(actuals, exec.Execute(stmt))
		}
		buf = rest
	}
	flush()
	if !Blank(name, buf) {
		return out.String(),
			fmt.Errorf("%s: unexpected end of input", name)
	}
	return out.String(), nil
}

// resolveOutput renders the output block for the statements
// executed since the last expected block. It keeps the original
// expected text when a single statement's actual output matches it
// semantically; otherwise it emits the actual output.
func resolveOutput(matcher semanticMatcher, actuals []string,
	expected string,
) string {
	var b strings.Builder
	for _, a := range actuals {
		writeOutput(&b, a)
	}
	actual := b.String()
	if actual != expected && expected != "" && len(actuals) == 1 &&
		matcher != nil &&
		matcher.EquivalentOutput(actuals[0], stripOutputPrefix(expected)) {
		return expected
	}
	return actual
}

// stripOutputPrefix removes the "> " (or ">") prefix from each line
// of an expected-output block, returning the underlying text.
func stripOutputPrefix(block string) string {
	var lines []string
	for line := range strings.SplitSeq(
		strings.TrimSuffix(block, "\n"), "\n") {
		switch {
		case line == ">":
			lines = append(lines, "")
		case strings.HasPrefix(line, "> "):
			lines = append(lines, line[2:])
		default:
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func isOutputLine(line string) bool {
	bare := strings.TrimSuffix(line, "\n")
	return bare == ">" || strings.HasPrefix(bare, "> ")
}

// writeOutput emits each line of s prefixed by "> "; an empty
// line becomes a bare ">". Empty output emits nothing.
func writeOutput(out *strings.Builder, s string) {
	if s == "" {
		return
	}
	for line := range strings.SplitSeq(s, "\n") {
		if line == "" {
			out.WriteString(">\n")
		} else {
			out.WriteString("> " + line + "\n")
		}
	}
}
