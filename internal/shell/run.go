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
	"io"
	"os"
	"strings"
)

// Run carries out the command described by a: print help,
// evaluate an expression, or run the files (standard input if
// none). in and out are standard input and output.
func (a *Args) Run(in io.Reader, out io.Writer) error {
	if a.Help {
		return Usage(out)
	}
	kernel := NewKernel("stdIn")
	if a.Directory != "" {
		kernel.Config().Directory = a.Directory
	}
	if a.HasEval {
		return runEval(kernel, a.Eval, out)
	}
	if len(a.Files) == 0 {
		return NewRunner(kernel, in, out, "stdIn").Run()
	}
	for _, file := range a.Files {
		err := a.runFile(kernel, file, in, out)
		if err != nil {
			return err
		}
	}
	return nil
}

// runEval evaluates a single expression and prints its result,
// as "morel -e" does. A trailing ";" is supplied if absent.
func runEval(kernel *Kernel, expr string, out io.Writer) error {
	stmt := expr
	if !strings.HasSuffix(strings.TrimRight(stmt, " \t\n"), ";") {
		stmt += ";"
	}
	result := kernel.Execute(stmt)
	if result == "" {
		return nil
	}
	_, err := io.WriteString(out, result+"\n")
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// runFile runs one script file (or standard input, for "-").
// Script format is elaborated in the next task; for now every
// file runs as a batch, echoing nothing.
func (a *Args) runFile(kernel *Kernel, file string,
	in io.Reader, out io.Writer,
) error {
	name := file
	reader := in
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("open %s: %w", file, err)
		}
		defer f.Close()
		reader = f
	} else {
		name = "stdIn"
	}
	return NewRunner(kernel, reader, out, name).Run()
}
