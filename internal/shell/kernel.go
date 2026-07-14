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
	"strings"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/token"
)

// Config holds the session properties.
type Config struct{}

// Kernel executes statements and holds the state that persists
// between them: the configuration, and (in time) the environment
// and session.
type Kernel struct {
	config Config
	name   string
}

// NewKernel returns a kernel; name (e.g. "stdIn" or a file name)
// is used in error messages.
func NewKernel(name string) *Kernel {
	return &Kernel{name: name}
}

// Config returns the kernel's configuration; the kernel is its
// sole owner.
func (k *Kernel) Config() *Config {
	return &k.config
}

// Execute runs one complete statement and returns its output.
// Until the full parser and evaluator exist, only built-in calls
// of the shape `A.b "str";` are evaluated; any other statement is
// lexically validated only, producing no output.
func (k *Kernel) Execute(stmt string) string {
	fn, arg, ok := parse.MicroCall(k.name, stmt)
	if ok {
		if f, found := eval.Builtins[fn]; found {
			return callString(f, arg)
		}
	}
	l := parse.NewLexer(k.name, stmt)
	for {
		tok, err := l.Next()
		if err != nil {
			return err.Error()
		}
		if tok.Kind == token.EOF {
			return ""
		}
	}
}

// callString invokes a built-in whose result is a string, and
// formats the result as the shell prints it.
func callString(f eval.Fn, arg string) string {
	v, err := f(arg)
	if err != nil {
		return err.Error()
	}
	s, ok := v.(string)
	if !ok {
		return "unexpected result"
	}
	return `val it = "` + escapeString(s) + `" : string`
}

// escapeString renders a string value's characters as they
// appear in a string literal.
func escapeString(s string) string {
	var b strings.Builder
	for _, r := range s {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Blank reports whether src contains only whitespace and
// comments.
func Blank(name, src string) bool {
	l := parse.NewLexer(name, src)
	tok, err := l.Next()
	return err == nil && tok.Kind == token.EOF
}
