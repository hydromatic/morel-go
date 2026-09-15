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
	"path/filepath"
	"sort"
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
	kernel.Config().ScriptDirectory = a.ScriptDirectory
	if a.ColorScheme != "" {
		kernel.Config().SetProp("colorScheme", a.ColorScheme)
	}
	kernel.Config().MaxUseDepth = a.MaxUseDepth
	if a.HasEval {
		kernel.Config().ShowUnsupported = true
		return runEval(kernel, a.Eval, out)
	}
	if len(a.Files) == 0 {
		return a.runReader(kernel, "stdIn", in, out)
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
		if a.ScriptDirectory == "" {
			// "use" resolves against the script's own directory
			// (the scriptDirectory property).
			kernel.Config().ScriptDirectory = filepath.Dir(file)
		}
	} else {
		name = "stdIn"
	}
	return a.runReader(kernel, name, reader, out)
}

// Form is how a source is read and its output written.
type Form int

const (
	// FormBatch streams each statement's result as it completes.
	// This is how an ordinary program runs.
	FormBatch Form = iota
	// FormIdempotent reads a ".smli" script, which carries its own
	// expected output on "> "-prefixed lines, and writes the script
	// back with that output refreshed.
	FormIdempotent
	// FormTranscript reads a ".sml" script and writes the transcript
	// that its companion ".sml.out" file holds: every input line
	// echoed, and after each statement the output it produced.
	FormTranscript
)

// FormOf returns how to read the source named `name`.
//
// Two things decide, as they do in morel-java. First, whether the
// script harness is engaged at all: "--idempotent", or a first file
// ending ".smli", asks for it, and without it every source is an
// ordinary program, streamed. `morel prog.sml` therefore runs a
// program, which is what ".sml" ordinarily means.
//
// Then, within the harness, the extension picks the form: a ".smli"
// script carries its own expected output, and anything else has its
// transcript in a companion ".sml.out" file. Standard input has no
// name to go by and is read as ".smli" would be, which is what
// "--idempotent" means for it.
func (a *Args) FormOf(name string) Form {
	switch {
	case !a.script:
		return FormBatch
	case name == "-" || name == "stdIn":
		return FormIdempotent
	case strings.HasSuffix(name, ".smli"):
		return FormIdempotent
	default:
		return FormTranscript
	}
}

// runReader runs the statements read from reader, in whichever form
// the source's name calls for. A script form reads the whole source
// and writes the file the test harness would compare against; a batch
// streams each statement's result as it completes.
func (a *Args) runReader(kernel *Kernel, name string,
	reader io.Reader, out io.Writer,
) error {
	form := a.FormOf(name)
	if form == FormBatch {
		// Interactive and batch modes surface not-implemented
		// errors; only the script forms keep them silent, so that
		// an unpulled corpus statement replays quietly.
		kernel.Config().ShowUnsupported = true
		return NewRunner(kernel, reader, out, name).Run()
	}
	src, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	var result string
	if form == FormTranscript {
		result, err = RunSmlScript(kernel, name, string(src))
	} else {
		result, err = RunScript(kernel, name, string(src))
	}
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, result)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	reportGaps(kernel)
	return nil
}

// reportGaps prints the suppressed not-implemented tally to
// standard error when MOREL_GAPS is set, one "gap: N x msg" line
// per distinct message, so a corpus sweep can rank what running
// the inputs would need.
func reportGaps(kernel *Kernel) {
	if os.Getenv("MOREL_GAPS") == "" {
		return
	}
	gaps := kernel.Gaps()
	msgs := make([]string, 0, len(gaps))
	for msg := range gaps {
		msgs = append(msgs, msg)
	}
	sort.Strings(msgs)
	samples := kernel.GapSamples()
	for _, msg := range msgs {
		fmt.Fprintf(os.Stderr, "gap: %d x %s\n", gaps[msg], msg)
		if sample, ok := samples[msg]; ok {
			fmt.Fprintf(os.Stderr, "gap-sample: %s\n", sample)
		}
	}
}
