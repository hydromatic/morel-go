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
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"os"
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

// Default values of the printing properties.
const (
	defaultLineWidth   = 79
	defaultPrintLength = 12
	defaultPrintDepth  = 5
	defaultStringDepth = 70
)

// DefaultConfig returns the default session properties.
func DefaultConfig() Config {
	return Config{
		LineWidth:   defaultLineWidth,
		PrintLength: defaultPrintLength,
		PrintDepth:  defaultPrintDepth,
		StringDepth: defaultStringDepth,
		props:       map[string]string{},
	}
}

// Config holds the session properties that control printing:
// the width at which lines wrap, the list length and value depth
// at which ellipsis begins, and the string length at which
// truncation begins. A negative value means no limit.
type Config struct {
	LineWidth   int
	PrintLength int
	PrintDepth  int
	StringDepth int

	// Directory is the working directory, as the "directory"
	// and "scriptDirectory" properties report it.
	Directory string

	// props holds the explicitly set values of the properties
	// that do not (yet) change behavior.
	props map[string]string

	// ShowUnsupported surfaces not-yet-implemented compile errors
	// and recovered panics instead of suppressing them. Script
	// (idempotent) mode keeps them silent, so unpulled corpus
	// statements replay quietly; the interactive shell and "-e"
	// turn this on, so a user sees why a statement produced
	// nothing.
	ShowUnsupported bool

	// sys resolves datatype constructors when printing their
	// values.
	sys *types.System
}

// Kernel executes statements and holds the state that persists
// between them: the configuration, the type system, and the
// bindings made by earlier statements.
type Kernel struct {
	config   Config
	name     string
	sys      *types.System
	bindings []compile.Binding
	methods  *compile.MethodRegistry
	values   map[string]eval.Val
	// lastCode is the compiled code of the most recently executed
	// statement's expression; Sys.plan describes it.
	lastCode eval.Code
	// gaps counts the not-yet-implemented errors and panics that
	// suppression hid, by message — the inventory of what running
	// the input would need. Gaps reports it.
	gaps map[string]int
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
	for i := range result.Bindings {
		adaptRelationalAggregates(sys, &result.Bindings[i])
	}
	bindings = append(bindings, result.Bindings...)
	config := DefaultConfig()
	config.sys = sys
	dir, err := os.Getwd()
	if err == nil {
		config.Directory = dir
	}
	k := &Kernel{
		name:     name,
		config:   config,
		sys:      sys,
		bindings: bindings,
		gaps:     map[string]int{},
		methods:  compile.NewMethodRegistry(result.Methods, bindings),
	}
	values := make(map[string]eval.Val, len(eval.Builtins))
	maps.Copy(values, eval.Builtins)
	// The relational aggregates and functions are bound both at top
	// level and as members of the Relational structure.
	for name, fn := range eval.RelationalAggregates() {
		values[name] = fn
		values["Relational."+name] = fn
	}
	for name, fn := range eval.RelationalFunctions() {
		values[name] = fn
		values["Relational."+name] = fn
	}
	// The Sys implementations read and write session state, so
	// the kernel supplies them.
	maps.Copy(values, k.sysBuiltins())
	// A structure is a record value whose fields are its
	// members' implementations. A member without one gets a
	// placeholder that fails if it is ever applied, so unpulled
	// corpus statements stay silent rather than wrong.
	for _, b := range result.Bindings {
		record, isRecord := b.Type.(*types.Record)
		if !isRecord {
			continue
		}
		fields := make([]eval.Val, len(record.Fields))
		for i, field := range record.Fields {
			qualified := b.Name + "." + field.Label
			if fn, ok := values[qualified]; ok {
				fields[i] = fn
			} else {
				fields[i] = notImplemented(qualified)
			}
		}
		values[b.Name] = fields
	}
	k.values = values
	k.loadScott()
	return k
}

// scottSrc defines the global "scott" dataset.
//
//go:embed scott.sml
var scottSrc string

// Gaps reports what suppression hid: each not-yet-implemented
// message with the number of statements it silenced.
func (k *Kernel) Gaps() map[string]int {
	return k.gaps
}

// Config returns the kernel's configuration; the kernel is its
// sole owner.
func (k *Kernel) Config() *Config {
	return &k.config
}

// EquivalentOutput reports whether an actual statement output is
// semantically equivalent to an expected one — bag values compared
// as multisets, whitespace normalized — unless the "matchStrict"
// property forces exact textual comparison.
func (k *Kernel) EquivalentOutput(actual, expected string) bool {
	if k.config.props["matchStrict"] == "true" {
		return false
	}
	return equivalentOutput(k.sys, actual, expected)
}

// Execute runs one complete statement and returns its output. A
// statement marked ":t" is type-checked but not evaluated. Until
// the evaluator exists, other statements are evaluated only if
// they are built-in calls of the shape `A.b arg;`; anything else
// is lexically validated, producing no output.
func (k *Kernel) Execute(stmt string) string {
	// A ":t" marker becomes the "(*TYPE_ONLY*)" comment before
	// anything looks at positions — so a type-only statement's
	// line-1 columns are shifted by
	// the ten extra characters.
	stmt, typeOnly := rewriteTypeOnly(stmt)
	// Positions in error reports are relative to the statement's
	// first token: its line renumbers to 1, but columns keep
	// their raw values. Blank out everything before the
	// first token (comments become spaces, so columns survive)
	// and drop the resulting blank lines.
	stmt = normalizeLeading(stmt)
	if typeOnly {
		return k.executeTypeOnly(stmt)
	}
	n, err := parse.Stmt(k.name, stmt)
	if err != nil {
		// A parse failure is silent in script mode (the grammar
		// gap inventory records it); the interactive shell shows
		// it.
		k.gaps[err.Error()]++
		if k.config.ShowUnsupported {
			return err.Error()
		}
		return k.lexValidate(stmt)
	}
	e, isExpr := n.(ast.Expr)
	if !isExpr {
		return k.executeStatement(n)
	}
	fn, arg, ok := builtinCall(e)
	if !ok {
		return k.executeStatement(n)
	}
	if fn == "Sys.parseTree" {
		lit, isString := arg.(*ast.Literal)
		if isString && lit.Kind == ast.StringLiteralOp {
			return callString(eval.Builtins[fn], lit.Value)
		}
	}
	return k.executeStatement(n)
}

// adaptRelationalAggregates rewrites the "Relational" structure's
// aggregate members (count, sum, only, ...) so that each takes a
// collection of free orderedness — a list or a bag — matching the
// top-level aliases. Other structures and members are left
// unchanged.
func adaptRelationalAggregates(sys *types.System, b *compile.Binding) {
	if b.Name != "Relational" {
		return
	}
	record, ok := b.Type.(*types.Record)
	if !ok {
		return
	}
	fields := make([]types.Field, len(record.Fields))
	for i, f := range record.Fields {
		fields[i] = f
		if t := compile.CollectionAggType(sys, f.Label, f.Type); t != nil {
			fields[i].Type = t
		}
	}
	b.Type = sys.Record(fields)
}

// loadScott binds the global "scott" dataset, a foreign relation
// available to every script. It is evaluated once, and its
// binding is added to the base environment so every statement
// sees it.
func (k *Kernel) loadScott() {
	n, err := parse.Stmt(k.name, scottSrc)
	if err != nil {
		panic(err) // the embedded source is tested, so it parses
	}
	decl, ok := n.(ast.Decl)
	if !ok {
		panic("scott.sml is not a declaration")
	}
	resolved, err := compile.Deduce(k.sys, k.bindings, decl)
	if err != nil {
		panic(err)
	}
	coreDecl, err := compile.Resolve(resolved)
	if err != nil {
		panic(err)
	}
	compiled, err := compile.Statement(coreDecl, k.values, k.sys)
	if err != nil {
		panic(err)
	}
	frame := eval.NewFrame(compiled.Slots)
	_, err = compiled.Code.Eval(frame)
	if err != nil {
		panic(err)
	}
	for _, b := range compiled.Binds {
		val := frame.Slots[b.Slot]
		// scott's collections are foreign relations: they behave
		// as their rows but print opaquely as "<relation>".
		if fields, ok := val.([]eval.Val); ok {
			for j, f := range fields {
				if rows, ok := f.([]eval.Val); ok {
					fields[j] = eval.Relation{Rows: rows}
				}
			}
		}
		k.bind(b.Pat.Name, b.Pat.T)
		k.values[b.Pat.Name] = val
	}
}

// executeStatement compiles and evaluates a statement, prints
// the binding it makes as "val name = value : type", and adds
// the binding to the environment. A statement that needs a
// not-yet-implemented feature produces no output — including one
// that panics the evaluator; the session must survive any
// single statement.
func (k *Kernel) executeStatement(n ast.Node) string {
	out := ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("internal error: %v", r)
				k.gaps[msg]++
				out = ""
				if k.config.ShowUnsupported {
					out = msg
				}
			}
		}()
		out = k.runStatement(n)
	}()
	return out
}

func (k *Kernel) runStatement(n ast.Node) string {
	var decl ast.Decl
	switch node := n.(type) {
	case ast.Decl:
		decl = node
	case ast.Expr:
		decl = compile.ItValDecl(node)
	}
	k.methods.RewriteDecl(decl)
	resolved, err := compile.Deduce(k.sys, k.bindings, decl)
	if err != nil {
		return k.formatCompileError(err)
	}
	datatypeDecl, isDatatype := resolved.Decl.(*ast.DatatypeDecl)
	if isDatatype {
		// The declaration registered its datatype and
		// constructors in the type system; the shell echoes it.
		return ast.UnparseDatatypeDecl(datatypeDecl)
	}
	if typeDecl, isType := resolved.Decl.(*ast.TypeDecl); isType {
		// The declaration registered its type aliases; echo it.
		return ast.UnparseTypeDecl(typeDecl)
	}
	coreDecl, err := compile.Resolve(resolved)
	if err != nil {
		return k.formatCompileError(err)
	}
	compiled, err := compile.Statement(coreDecl, k.values, k.sys)
	if err != nil {
		return k.formatCompileError(err)
	}
	frame := eval.NewFrame(compiled.Slots)
	_, err = compiled.Code.Eval(frame)
	// Record the statement's code for Sys.plan, after evaluating —
	// so a Sys.plan call sees the previous statement, as java does.
	k.lastCode = compiled.Plan
	if err != nil {
		var morelErr *eval.MorelError
		if errors.As(err, &morelErr) {
			return morelErr.Describe()
		}
		return err.Error()
	}
	var lines []string
	for _, b := range compiled.Binds {
		v := frame.Slots[b.Slot]
		k.bind(b.Pat.Name, b.Pat.T)
		k.values[b.Pat.Name] = v
		lines = append(lines,
			k.config.prettyBinding(b.Pat.Name, v, b.Pat.T))
	}
	return strings.Join(lines, "\n")
}

// notImplemented is the placeholder value of a built-in that has
// no implementation yet. Calling it reports an error — visible in
// the shell, unlike a swallowed panic — while the placeholder
// value itself still prints as "fn" in its structure's record.
func notImplemented(name string) eval.Fn {
	return func(eval.Val) (eval.Val, error) {
		return nil, errors.New("not implemented: " + name)
	}
}

// formatCompileError renders a compilation error:
//
//	stdIn:1.1-1.11 Error: literal '9999999999' is too large ...
//	  raised at: stdIn:1.1-1.11
//
// An error that means "not implemented yet" produces no output,
// so unpulled corpus statements stay silent.
func (k *Kernel) formatCompileError(err error) string {
	var compileErr *compile.Error
	if !errors.As(err, &compileErr) ||
		compileErr.Span == (token.Span{}) {
		k.gaps[err.Error()]++
		return ""
	}
	if unsupported(compileErr.Msg) {
		k.gaps[compileErr.Msg]++
		if !k.config.ShowUnsupported {
			return ""
		}
	}
	pos := "stdIn:" + compileErr.Span.String()
	return pos + " Error: " + compileErr.Msg +
		"\n  raised at: " + pos
}

// unsupported reports whether a compile error means that a
// feature is not implemented yet, rather than that the user's
// statement is wrong.
func unsupported(msg string) bool {
	for _, prefix := range []string{
		"cannot compile",
		"cannot convert to core",
		"cannot deduce type for",
		"cannot derive label",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
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
	k.methods.RewriteDecl(decl)
	resolved, err := compile.Deduce(k.sys, k.bindings, decl)
	if err != nil {
		return k.formatCompileError(err)
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
		lines = append(lines, k.config.prettyType(pat.Name, typ))
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
func callString(f eval.Val, arg string) string {
	v, err := eval.ApplyVal(f, arg)
	if err != nil {
		return err.Error()
	}
	s, ok := v.(string)
	if !ok {
		return "unexpected result"
	}
	return `val it = "` + escapeString(s) + `" : string`
}

// escapeString renders a string value's bytes as they appear in
// a string literal, escaping each byte the way a character
// literal escapes it, byte by byte. A string is byte-indexed,
// so it is iterated by byte,
// not by rune.
func escapeString(s string) string {
	var b strings.Builder
	for i := range len(s) {
		b.WriteString(eval.CharToString(rune(s[i])))
	}
	return b.String()
}

// normalizeLeading replaces the whitespace and comments before a
// statement's first token with spaces and removes the blank
// lines that result, so the first token is on line 1 at its
// original column.
func normalizeLeading(stmt string) string {
	i, n := 0, len(stmt)
scan:
	for i < n {
		switch {
		case stmt[i] == ' ' || stmt[i] == '\t' ||
			stmt[i] == '\r' || stmt[i] == '\n':
			i++
		case strings.HasPrefix(stmt[i:], "(*)"):
			j := strings.IndexByte(stmt[i:], '\n')
			if j < 0 {
				i = n
				break scan
			}
			i += j + 1
		case strings.HasPrefix(stmt[i:], "(*"):
			i = skipBlockComment(stmt, i+len("(*"))
		default:
			break scan
		}
	}
	prefix := []byte(stmt[:i])
	for j, c := range prefix {
		if c != '\n' {
			prefix[j] = ' '
		}
	}
	s := string(prefix) + stmt[i:]
	for {
		j := strings.IndexByte(s, '\n')
		if j < 0 || strings.TrimSpace(s[:j]) != "" {
			return s
		}
		s = s[j+1:]
	}
}

// rewriteTypeOnly looks for the ":t" marker that makes a
// statement type-only: at the start of a line, preceded only by
// whitespace and comments, and followed by a space or newline.
// The marker (and a following
// space) becomes the "(*TYPE_ONLY*)" comment, so positions in
// the statement's error reports count the comment's characters.
func rewriteTypeOnly(stmt string) (string, bool) {
	const marker = "(*TYPE_ONLY*)"
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
				return stmt, false
			}
			i += j + 1
		case strings.HasPrefix(stmt[i:], "(*"):
			i = skipBlockComment(stmt, i+len("(*"))
		default:
			if !strings.HasPrefix(stmt[i:], ":t") {
				return stmt, false
			}
			if i > 0 && stmt[i-1] != '\n' {
				return stmt, false
			}
			j := i + markerLen
			if j < n && stmt[j] != ' ' && stmt[j] != '\n' {
				return stmt, false
			}
			if j < n && stmt[j] == ' ' {
				j++
			}
			return stmt[:i] + marker + stmt[j:], true
		}
	}
	return stmt, false
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
