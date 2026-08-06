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

// Package terminal is the shell's interactive front end: the
// prompt-driven session a user gets on a terminal, with
// command-line editing and history that persists across sessions.
//
// It is separate from the batch path in package shell, which reads
// a plain io.Reader and is what a pipe, a script file, or
// "--terminal=dumb" gets. Only this package knows about the
// terminal.
package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/reeflective/readline"

	"github.com/hydromatic/morel-go/internal/shell"
)

// Prompts, as in SML/NJ: "- " begins a statement, "= " continues
// one whose text so far is incomplete.
const (
	primaryPrompt      = "- "
	continuationPrompt = "= "
)

// emacsKeymap is the keymap the shell uses.
const emacsKeymap = "emacs"

// extraBinds are the keys the Emacs keymap leaves unbound. The
// library binds them in its Vi keymap only, so in the default Emacs
// keymap they did nothing — and worse, a terminal that sends the
// "\e[N~" form of a key left the '~' behind in the line, because
// the prefix was swallowed and the terminator was not.
//
// A terminal sends Home and End as "\eOH"/"\eOF" or "\e[H"/"\e[F"
// (both already bound), as "\e[1~"/"\e[4~" (xterm's other form,
// and what tmux, screen and the Linux console send), or as
// "\e[7~"/"\e[8~" (rxvt).
var extraBinds = []struct{ seq, action string }{
	{"\x1b[3~", "delete-char"},          // Delete
	{"\x1b[1~", "beginning-of-line"},    // Home
	{"\x1b[7~", "beginning-of-line"},    // Home, rxvt
	{"\x1b[4~", "end-of-line"},          // End
	{"\x1b[8~", "end-of-line"},          // End, rxvt
	{"\x1b[5~", "up-line-or-history"},   // Page Up
	{"\x1b[6~", "down-line-or-history"}, // Page Down
}

// The history file. morel-java writes ~/.morel/history and
// morel-rust ~/.morel/history-rust; the three formats are not
// compatible, so each implementation keeps its own.
const (
	morelDir     = ".morel"
	historyFile  = "history-go"
	historyName  = "morel"
	dirPerm      = 0o700
	sourceStdIn  = "stdIn"
	writeErrorFm = "write: %w"
)

// Repl runs an interactive session on the terminal until the user
// ends it with ctrl-D. Statements are read with editing and
// history; each is executed as soon as it is complete, and its
// result printed.
func Repl(a *shell.Args, out io.Writer) error {
	rl := readline.NewShell()
	rl.Prompt.Primary(func() string { return primaryPrompt })
	rl.Prompt.Secondary(func() string { return continuationPrompt })
	// The secondary prompt is only drawn when a multiline column
	// is asked for; without this the continuation lines of a
	// statement have no prompt at all.
	_ = rl.Config.Set("multiline-column", true)
	for _, b := range extraBinds {
		_ = rl.Config.Bind(emacsKeymap, b.seq, b.action, false)
	}

	// A statement, however many lines it spans, is one unit: the
	// line reader keeps reading until Split says the text holds
	// no incomplete trailing statement. So the whole statement is
	// edited, and recalled, as one.
	rl.AcceptMultiline = func(line []rune) bool {
		return complete(string(line))
	}

	path, err := historyPath()
	if err != nil {
		// A missing history file is not worth failing over; the
		// session works, it just does not remember.
		fmt.Fprintln(os.Stderr, "morel: "+err.Error())
	} else {
		rl.History.AddFromFile(historyName, path)
	}

	if a.Banner {
		err = write(out, shell.Banner()+"\n")
		if err != nil {
			return err
		}
	}

	kernel := shell.NewKernel(sourceStdIn)
	if a.Directory != "" {
		kernel.Config().Directory = a.Directory
	}
	for {
		line, readErr := rl.Readline()
		switch {
		case errors.Is(readErr, readline.ErrInterrupt):
			// ctrl-C abandons the statement in progress.
			continue
		case errors.Is(readErr, io.EOF):
			// ctrl-D ends the session.
			return nil
		case readErr != nil:
			return fmt.Errorf("stdIn: read: %w", readErr)
		}
		err = execute(kernel, line, out)
		if err != nil {
			return err
		}
	}
}

// complete reports whether text holds no incomplete trailing
// statement, and so can be executed. Text that does not lex is
// complete, so that the error is reported rather than the shell
// waiting for a continuation that cannot come.
func complete(text string) bool {
	_, rest, err := shell.Split(sourceStdIn, text)
	if err != nil {
		return true
	}
	return shell.Blank(sourceStdIn, rest)
}

// execute runs every statement in text and prints the results.
func execute(kernel *shell.Kernel, text string, out io.Writer) error {
	stmts, _, err := shell.Split(sourceStdIn, text)
	if err != nil {
		return write(out, err.Error()+"\n")
	}
	for _, stmt := range stmts {
		result := kernel.Execute(stmt)
		if result == "" {
			continue
		}
		err = write(out, result+"\n")
		if err != nil {
			return err
		}
	}
	return nil
}

// write sends s to out.
func write(out io.Writer, s string) error {
	_, err := io.WriteString(out, s)
	if err != nil {
		return fmt.Errorf(writeErrorFm, err)
	}
	return nil
}

// historyPath is the file that command history is saved to,
// creating its directory if necessary.
func historyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no home directory, so no "+
			"command history: %w", err)
	}
	dir := filepath.Join(home, morelDir)
	err = os.MkdirAll(dir, dirPerm)
	if err != nil {
		return "", fmt.Errorf("cannot create %s, so no "+
			"command history: %w", dir, err)
	}
	return filepath.Join(dir, historyFile), nil
}
