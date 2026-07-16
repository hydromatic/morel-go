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
	"strings"
)

// Args is the parsed command line.
type Args struct {
	// Eval, if HasEval, is the expression to evaluate and print
	// before exiting ("-e"/"--eval"/"--eval=").
	Eval    string
	HasEval bool

	// Directory overrides the working directory ("--directory=").
	Directory string

	// Files are the scripts to run, in order; "-" means standard
	// input. Empty means read standard input.
	Files []string

	// Help requests the usage message ("-h"/"--help").
	Help bool

	// Echo sends script output to standard output as well as the
	// echoed script ("--echo").
	Echo bool

	// Idempotent treats input as SMLI format ("--idempotent");
	// implicit when the first file ends in ".smli".
	Idempotent bool

	// Banner controls the startup banner; false suppresses it
	// ("--banner=false"). Default true.
	Banner bool

	// Dumb disables interactive terminal features
	// ("--terminal=dumb").
	Dumb bool
}

// ParseArgs parses a command line into Args. An unrecognized
// flag is ignored rather than rejected, so that options
// morel-go does not implement (--foreign,
// --color-scheme, --maxUseDepth, --system) are tolerated. A bare
// "execute" is the default command and is accepted; "--build" and
// "--no-build" are accepted no-ops (there is nothing to build).
func ParseArgs(argv []string) *Args {
	a := &Args{Banner: true}
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
		case arg == "--banner=false":
			a.Banner = false
		case arg == "--terminal=dumb":
			a.Dumb = true
		case arg == "-":
			a.Files = append(a.Files, "-")
		case strings.HasPrefix(arg, "-"):
			// Unknown flag; ignored.
		default:
			a.Files = append(a.Files, arg)
		}
	}
	if len(a.Files) > 0 &&
		strings.HasSuffix(a.Files[0], ".smli") {
		a.Idempotent = true
	}
	return a
}

// usageText is the help printed by "-h"/"--help".
const usageText = `Usage: morel [option...] [file...]

Evaluate Morel statements from the given files, or from standard
input if no file is given.

Options:
  -e, --eval <expr>   Evaluate expression and exit.
  --echo              Echo script output to standard output.
  --idempotent        Treat input as SMLI (idempotent) format;
                      implicit when the first file ends in '.smli'.
  --directory=DIR     Set the working directory.
  --banner=false      Suppress the startup banner.
  --terminal=dumb     Disable interactive terminal features.
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
