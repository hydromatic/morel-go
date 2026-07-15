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
	"bufio"
	"fmt"
	"io"
)

// SML-NJ prompts: "- " begins a statement, "= " continues one
// whose text so far is incomplete.
const (
	primaryPrompt      = "- "
	continuationPrompt = "= "
)

// WantsRepl reports whether the command is an interactive session
// rather than a batch: no help, no "-e", no files, and the
// terminal is not forced dumb. The caller additionally checks
// that standard input is a terminal.
func (a *Args) WantsRepl() bool {
	return !a.Help && !a.HasEval && len(a.Files) == 0 && !a.Dumb
}

// Repl runs an interactive read-eval-print loop: it prints a
// banner (unless "--banner=false"), then reads lines, assembling
// them into statements with the same splitter the batch runner
// uses, and prints each statement's result as it completes. The
// primary prompt "- " precedes a fresh statement and the
// continuation prompt "= " a partial one. End-of-input (ctrl-D)
// ends the session.
func (a *Args) Repl(in io.Reader, out io.Writer) error {
	// write appends to out, latching the first error so the loop
	// can keep to plain calls and report it at the end.
	var writeErr error
	write := func(s string) {
		if writeErr != nil {
			return
		}
		_, writeErr = io.WriteString(out, s)
	}
	if a.Banner {
		write(bannerText() + "\n")
	}
	kernel := NewKernel("stdIn")
	if a.Directory != "" {
		kernel.Config().Directory = a.Directory
	}
	scanner := bufio.NewScanner(in)
	buf := ""
	write(primaryPrompt)
	for writeErr == nil && scanner.Scan() {
		buf += scanner.Text() + "\n"
		stmts, rest, err := Split("stdIn", buf)
		if err != nil {
			// The statement boundary is unknown; report the
			// error and discard the buffer.
			write(err.Error() + "\n")
			buf = ""
		} else {
			for _, stmt := range stmts {
				result := kernel.Execute(stmt)
				if result != "" {
					write(result + "\n")
				}
			}
			buf = rest
		}
		if Blank("stdIn", buf) {
			write(primaryPrompt)
		} else {
			write(continuationPrompt)
		}
	}
	if writeErr != nil {
		return fmt.Errorf("write: %w", writeErr)
	}
	err := scanner.Err()
	if err != nil {
		return fmt.Errorf("stdIn: read: %w", err)
	}
	// End the prompt line cleanly after ctrl-D.
	write("\n")
	if writeErr != nil {
		return fmt.Errorf("write: %w", writeErr)
	}
	return nil
}
