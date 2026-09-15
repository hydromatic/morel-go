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
	"strconv"
	"strings"
)

// Args is the parsed command line: the subset of shell options
// that morel-go supports.
type Args struct {
	// Eval, if HasEval, is the expression to evaluate and print
	// before exiting ("-e"/"--eval"/"--eval=").
	Eval    string
	HasEval bool

	// Directory overrides the working directory ("--directory=").
	Directory string

	// ScriptDirectory overrides the directory "use" resolves
	// relative file names against ("--scriptDirectory="); when
	// empty, each script's own directory is used.
	ScriptDirectory string

	// MaxUseDepth caps nested "use" calls ("--maxUseDepth=");
	// negative (the default) means no limit.
	MaxUseDepth int

	// Files are the scripts to run, in order; "-" means standard
	// input. Empty means read standard input.
	Files []string

	// Help requests the usage message ("-h"/"--help").
	Help bool

	// Echo sends script output to standard output as well as the
	// echoed script ("--echo").
	Echo bool

	// Idempotent engages the script harness ("--idempotent"), and
	// reads standard input as SMLI format. A first file ending
	// ".smli" engages it too. Within the harness the extension
	// picks the form; see Args.FormOf.
	Idempotent bool

	// script is whether the script harness is engaged: morel-java's
	// "smli" sub-command. Without it every source runs as an
	// ordinary program.
	script bool

	// Banner controls the startup banner; false suppresses it
	// ("--banner=false"). Default true.
	Banner bool

	// Dumb disables interactive terminal features
	// ("--terminal=dumb").
	Dumb bool

	// ColorScheme names the syntax-highlighting color scheme
	// ("--color-scheme="); empty means deduce one from the
	// terminal's background.
	ColorScheme string
}

// ParseArgs parses a command line into Args. An unrecognized flag
// is ignored rather than rejected, so that options morel-go does
// not implement (--foreign, --color-scheme, --system) are
// tolerated. A bare "execute" is the default command and is
// accepted; "--build" and "--no-build" are accepted no-ops (there
// is nothing to build).
func ParseArgs(argv []string) *Args {
	a := &Args{Banner: true, MaxUseDepth: -1}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "execute", arg == "--build", arg == "--no-build":
			// Accepted; no effect.
		case arg == "-h" || arg == "--help":
			a.Help = true
		case arg == "-e" || arg == "--eval":
			if i+1 < len(argv) {
				i++
				a.Eval, a.HasEval = argv[i], true
			}
		case strings.HasPrefix(arg, "--eval="):
			a.Eval, a.HasEval = arg[len("--eval="):], true
		case arg == "--echo":
			a.Echo = true
		case arg == "--idempotent":
			a.Idempotent = true
		case strings.HasPrefix(arg, "--directory="):
			a.Directory = arg[len("--directory="):]
		case strings.HasPrefix(arg, "--scriptDirectory="):
			a.ScriptDirectory = arg[len("--scriptDirectory="):]
		case strings.HasPrefix(arg, "--maxUseDepth="):
			n, err := strconv.Atoi(arg[len("--maxUseDepth="):])
			if err == nil {
				a.MaxUseDepth = n
			}
		case arg == "--banner=false":
			a.Banner = false
		case arg == "--terminal=dumb":
			a.Dumb = true
		case strings.HasPrefix(arg, "--color-scheme="):
			a.ColorScheme = arg[len("--color-scheme="):]
		case arg == "-":
			a.Files = append(a.Files, "-")
		case strings.HasPrefix(arg, "-"):
			// Unknown flag; ignored.
		default:
			a.Files = append(a.Files, arg)
		}
	}
	// The script harness is engaged by the flag, or by a first file
	// that is plainly a script. Java's "morel" wrapper chooses its
	// "smli" sub-command the same way.
	a.script = a.Idempotent ||
		len(a.Files) > 0 && strings.HasSuffix(a.Files[0], ".smli")
	return a
}

// usageText is the help printed by "-h"/"--help".
const usageText = `Usage: morel [option...] [file...]

Evaluate Morel statements from the given files, or from standard
input if no file is given.

Options:
  -e, --eval <expr>   Evaluate expression and exit.
  --echo              Echo script output to standard output.
  --idempotent        Run through the script harness, reading
                      standard input as SMLI (idempotent) format;
                      implicit when the first file ends in '.smli'.
                      Within the harness a '.smli' file is rewritten
                      and any other file's transcript is written.
  --directory=DIR     Set the working directory.
  --scriptDirectory=DIR
                      Set the directory 'use' resolves against
                      (default: the script's own directory).
  --maxUseDepth=N     Limit nested 'use' calls to N levels.
  --banner=false      Suppress the startup banner.
  --terminal=dumb     Disable interactive terminal features.
  --color-scheme=NAME Syntax-highlighting scheme: "dark", "light"
                      or "none" (default: deduced from the
                      terminal's background).
  -h, --help          Print this help, then exit.

A file argument of '-' means standard input.
`

// Usage writes the help message.
func Usage(out io.Writer) error {
	_, err := io.WriteString(out, usageText)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
