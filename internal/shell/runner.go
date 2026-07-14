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

// Executor runs one complete statement and returns its output.
type Executor interface {
	Execute(stmt string) string
}

// Runner reads lines, assembles them into statements, executes
// each complete statement, and writes the output.
type Runner struct {
	exec Executor
	in   io.Reader
	out  io.Writer
	name string
}

// NewRunner returns a runner that reads from in and writes to
// out; name is used in error messages.
func NewRunner(exec Executor, in io.Reader, out io.Writer,
	name string,
) *Runner {
	return &Runner{exec: exec, in: in, out: out, name: name}
}

// Run processes the input to its end. It returns an error if the
// input ends in the middle of a statement, or if writing fails.
func (r *Runner) Run() error {
	scanner := bufio.NewScanner(r.in)
	buf := ""
	for scanner.Scan() {
		buf += scanner.Text() + "\n"
		stmts, rest, err := Split(r.name, buf)
		if err != nil {
			// The statement boundary is unknown; report the
			// error and discard the buffer.
			werr := r.write(err.Error())
			if werr != nil {
				return werr
			}
			buf = ""
			continue
		}
		for _, stmt := range stmts {
			werr := r.write(r.exec.Execute(stmt))
			if werr != nil {
				return werr
			}
		}
		buf = rest
	}
	err := scanner.Err()
	if err != nil {
		return fmt.Errorf("%s: read: %w", r.name, err)
	}
	if !Blank(r.name, buf) {
		return fmt.Errorf("%s: unexpected end of input", r.name)
	}
	return nil
}

// write emits one output block, if non-empty, with a trailing
// newline.
func (r *Runner) write(s string) error {
	if s == "" {
		return nil
	}
	_, err := io.WriteString(r.out, s+"\n")
	if err != nil {
		return fmt.Errorf("%s: write: %w", r.name, err)
	}
	return nil
}
