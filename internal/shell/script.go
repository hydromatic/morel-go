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

// RunScript runs an idempotent script: statements interleaved
// with their expected output on "> "-prefixed lines. Each input
// line is echoed verbatim, except expected-output lines, which
// are replaced by the actual output of the statement they follow.
// Running a correct script reproduces it exactly.
//
// A line is expected output if it is ">" or starts with "> " —
// unless it lies inside an unclosed comment or string, where it
// is content.
func RunScript(exec Executor, name, src string) (string, error) {
	var out strings.Builder
	buf := ""
	for _, line := range strings.SplitAfter(src, "\n") {
		if line == "" {
			continue
		}
		if isOutputLine(line) && !Unclosed(name, buf) {
			continue
		}
		out.WriteString(line)
		buf += line
		stmts, rest, err := Split(name, buf)
		if err != nil {
			writeOutput(&out, err.Error())
			buf = ""
			continue
		}
		for _, stmt := range stmts {
			writeOutput(&out, exec.Execute(stmt))
		}
		buf = rest
	}
	if !Blank(name, buf) {
		return out.String(),
			fmt.Errorf("%s: unexpected end of input", name)
	}
	return out.String(), nil
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
