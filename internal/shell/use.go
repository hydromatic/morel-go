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
	"os"
	"path/filepath"
	"strings"

	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/eval"
)

// The use and useSilently built-ins: execute a file of Morel
// statements in the current session. Their extra output —
// "[opening f]" and any failure report — reaches the calling
// statement's output through the kernel's pending lines.

// useBuiltins returns the Interact implementations, and their
// top-level aliases, for NewKernel to inject.
func (k *Kernel) useBuiltins() map[string]eval.Val {
	use := eval.Fn(func(arg eval.Val) (eval.Val, error) {
		return k.interactUse(arg, false)
	})
	useSilently := eval.Fn(func(arg eval.Val) (eval.Val, error) {
		return k.interactUse(arg, true)
	})
	return map[string]eval.Val{
		"Interact.use":         use,
		"Interact.useSilently": useSilently,
		"use":                  use,
		"useSilently":          useSilently,
	}
}

// interactUse is "use fileName" (echoing each statement and its
// output) or "useSilently fileName" (executing without output).
// The file resolves against the "scriptDirectory" property; a
// missing file, or exceeding the maximum use depth, reports a
// "[use failed: ...]" line and raises Error. Bindings the file
// makes persist in the session.
func (k *Kernel) interactUse(arg eval.Val, silent bool) (eval.Val,
	error,
) {
	fileName, _ := arg.(string)
	lines := []string{"[opening " + fileName + "]"}
	fail := func(reason string) (eval.Val, error) {
		lines = append(lines, "[use failed: Io: openIn failed on "+
			fileName+", "+reason+"]")
		k.pendingLines = append(k.pendingLines, lines...)
		return nil, &eval.MorelError{Exn: "Error"}
	}
	path := fileName
	if !filepath.IsAbs(path) {
		path = filepath.Join(k.scriptDirectory(), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail("No such file or directory")
	}
	if k.config.MaxUseDepth >= 0 &&
		k.useDepth >= k.config.MaxUseDepth {
		return fail("Too many open files")
	}
	k.useDepth++
	// The inner statements drain the pending lines into their own
	// output, so this call's lines stay local until the file is
	// done.
	saved := k.pendingLines
	k.pendingLines = nil
	lines = k.runUsedFile(string(data), silent, lines)
	saved = append(saved, lines...)
	k.pendingLines = saved
	k.useDepth--
	return core.Unit{}, nil
}

// runUsedFile executes a file's statements, skipping
// expected-output ("> ") lines so an idempotent script can be
// used; unless silent, each statement and its output join lines.
func (k *Kernel) runUsedFile(src string, silent bool,
	lines []string,
) []string {
	buf := ""
	for _, line := range strings.SplitAfter(src, "\n") {
		if line == "" {
			continue
		}
		if isOutputLine(line) && !Unclosed(k.name, buf) {
			continue
		}
		buf += line
		stmts, rest, err := Split(k.name, buf)
		if err != nil {
			if !silent {
				lines = appendLines(lines, err.Error())
			}
			buf = ""
			continue
		}
		for _, stmt := range stmts {
			out := k.Execute(stmt)
			if !silent {
				lines = appendLines(lines,
					strings.TrimSuffix(stmt, "\n"))
				lines = appendLines(lines, out)
			}
		}
		buf = rest
	}
	return lines
}

// appendLines splits a block on newlines and appends each line;
// an empty block appends nothing.
func appendLines(lines []string, block string) []string {
	if block == "" {
		return lines
	}
	return append(lines, strings.Split(block, "\n")...)
}

// scriptDirectory is the directory use-files resolve against: the
// "scriptDirectory" property, defaulting to the working directory.
func (k *Kernel) scriptDirectory() string {
	if k.config.ScriptDirectory != "" {
		return k.config.ScriptDirectory
	}
	return k.config.Directory
}
