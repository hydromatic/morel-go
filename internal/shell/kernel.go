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

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/compile"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/sig"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/lib"
)

// Config holds the session properties.
type Config struct{}

// Kernel executes statements and holds the state that persists
// between them: the configuration, the type system, and the
// bindings made by earlier statements.
type Kernel struct {
	config   Config
	name     string
	sys      *types.System
	bindings []compile.Binding
}

// NewKernel returns a kernel; name (e.g. "stdIn" or a file name)
// is used in error messages.
func NewKernel(name string) *Kernel {
	sys := types.NewSystem()
	result, err := sig.Load(sys, lib.FS)
	if err != nil {
		// The signature files are embedded and tested, so they
		// always load.
		panic(err)
	}
	bindings := compile.TopBindings(sys)
	bindings = append(bindings, result.Bindings...)
	return &Kernel{
		name:     name,
		sys:      sys,
		bindings: bindings,
	}
}

// Config returns the kernel's configuration; the kernel is its
// sole owner.
func (k *Kernel) Config() *Config {
	return &k.config
}

// Execute runs one complete statement and returns its output. A
// statement marked ":t" is type-checked but not evaluated. Until
// the evaluator exists, other statements are evaluated only if
// they are built-in calls of the shape `A.b arg;`; anything else
// is lexically validated, producing no output.
func (k *Kernel) Execute(stmt string) string {
	if rest, ok := typeOnlyRest(stmt); ok {
		return k.executeTypeOnly(rest)
	}
	n, err := parse.Stmt(k.name, stmt)
	if err != nil {
		return k.lexValidate(stmt)
	}
	e, isExpr := n.(ast.Expr)
	if !isExpr {
		return ""
	}
	fn, arg, ok := builtinCall(e)
	if !ok {
		return ""
	}
	if fn == "Sys.set" {
		// Session properties do not yet affect anything, so
		// setting one is a no-op.
		return "val it = () : unit"
	}
	lit, isString := arg.(*ast.Literal)
	if !isString || lit.Kind != ast.StringLiteralOp {
		return ""
	}
	f, found := eval.Builtins[fn]
	if !found {
		return ""
	}
	return callString(f, lit.Value)
}

// executeTypeOnly type-checks a statement, prints each binding as
// "val name : type", and adds the bindings to the environment.
func (k *Kernel) executeTypeOnly(src string) string {
	n, err := parse.Stmt(k.name, src)
	if err != nil {
		return err.Error()
	}
	var decl ast.Decl
	switch node := n.(type) {
	case ast.Decl:
		decl = node
	case ast.Expr:
		decl = compile.ItValDecl(node)
	}
	resolved, err := compile.Deduce(k.sys, k.bindings, decl)
	if err != nil {
		return err.Error()
	}
	valDecl, ok := resolved.Decl.(*ast.ValDecl)
	if !ok {
		return ""
	}
	var lines []string
	for _, b := range valDecl.Binds {
		pat, isID := b.Pat.(*ast.IDPat)
		if !isID {
			continue
		}
		typ, err := resolved.TypeMap.TypeOf(pat)
		if err != nil {
			return err.Error()
		}
		lines = append(lines,
			"val "+pat.Name+" : "+typ.String())
		k.bind(pat.Name, typ)
	}
	return strings.Join(lines, "\n")
}

// bind adds a binding to the environment, replacing any previous
// binding of the same name.
func (k *Kernel) bind(name string, t types.Type) {
	for i := range k.bindings {
		if k.bindings[i].Name == name {
			k.bindings[i].Type = t
			return
		}
	}
	k.bindings = append(k.bindings,
		compile.Binding{Name: name, Type: t})
}

// lexValidate reports lexical errors in a statement the parser
// cannot yet handle.
func (k *Kernel) lexValidate(stmt string) string {
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

// builtinCall matches the expression shape of a call to a
// built-in: a selector applied to a structure name, applied to
// an argument (e.g. `Sys.parseTree "str"`), returning the dotted
// name and the argument expression.
func builtinCall(e ast.Expr) (string, ast.Expr, bool) {
	outer, ok := e.(*ast.Apply)
	if !ok {
		return "", nil, false
	}
	inner, ok := outer.Fn.(*ast.Apply)
	if !ok {
		return "", nil, false
	}
	sel, ok := inner.Fn.(*ast.RecordSelector)
	if !ok {
		return "", nil, false
	}
	id, ok := inner.Arg.(*ast.ID)
	if !ok {
		return "", nil, false
	}
	return id.Name + "." + sel.Name, outer.Arg, true
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

// typeOnlyRest looks for the ":t" marker that makes a statement
// type-only: at the start of a line, preceded only by whitespace
// and comments, and followed by a space or newline. It returns
// the statement text after the marker.
func typeOnlyRest(stmt string) (string, bool) {
	const markerLen = len(":t")
	i, n := 0, len(stmt)
	for i < n {
		switch {
		case stmt[i] == ' ' || stmt[i] == '\t' ||
			stmt[i] == '\r' || stmt[i] == '\n':
			i++
		case strings.HasPrefix(stmt[i:], "(*)"):
			// A "(*)" comment runs to the end of the line.
			j := strings.IndexByte(stmt[i:], '\n')
			if j < 0 {
				return "", false
			}
			i += j + 1
		case strings.HasPrefix(stmt[i:], "(*"):
			i = skipBlockComment(stmt, i+len("(*"))
		default:
			if !strings.HasPrefix(stmt[i:], ":t") {
				return "", false
			}
			if i > 0 && stmt[i-1] != '\n' {
				return "", false
			}
			j := i + markerLen
			if j < n && stmt[j] != ' ' && stmt[j] != '\n' {
				return "", false
			}
			return stmt[j:], true
		}
	}
	return "", false
}

// skipBlockComment returns the position after the "*)" that
// closes a block comment, accounting for nested comments; "(*)"
// within a block comment is not a nested comment. pos is the
// position after the opening "(*".
func skipBlockComment(s string, pos int) int {
	n := len(s)
	for pos < n {
		switch {
		case strings.HasPrefix(s[pos:], "(*)"):
			pos += len("(*)")
		case strings.HasPrefix(s[pos:], "(*"):
			pos = skipBlockComment(s, pos+len("(*"))
		case strings.HasPrefix(s[pos:], "*)"):
			return pos + len("*)")
		default:
			pos++
		}
	}
	return n
}

// Blank reports whether src contains only whitespace and
// comments.
func Blank(name, src string) bool {
	l := parse.NewLexer(name, src)
	tok, err := l.Next()
	return err == nil && tok.Kind == token.EOF
}
