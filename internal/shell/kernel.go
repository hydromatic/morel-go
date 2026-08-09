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
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/compile"
	"github.com/hydromatic/morel-go/internal/core"
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
		MaxUseDepth: -1,
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
	// property reports it.
	Directory string

	// ScriptDirectory is where "use" resolves relative file names
	// (the "scriptDirectory" property); empty falls back to
	// Directory.
	ScriptDirectory string

	// MaxUseDepth caps nested "use" calls; negative means no
	// limit.
	MaxUseDepth int

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
	// inlineExps holds, per top-level binding, the expression
	// that defined it, so later statements can inline it.
	inlineExps map[string]core.Exp
	// recFns holds, per recursive top-level function, its
	// defining expression — not for inlining, but so the
	// grounding pass can invert a recursive predicate into a
	// fixed-point iteration.
	recFns map[string]*core.Fn
	// planExDecl is the most recent statement's resolved (not yet
	// optimized) declaration; Sys.planEx re-plans it.
	planExDecl core.Decl
	// bootBindings and bootValues snapshot the environment at the
	// end of boot; the Datalog built-ins compile their translated
	// programs against it, so user bindings are not visible to
	// them.
	bootBindings []compile.Binding
	bootValues   map[string]eval.Val
	// initBindings and initValues snapshot the freshly initialized
	// environment — the built-ins plus the global datasets, but no
	// user bindings; Sys.clearEnv restores the session to them.
	// They hold every structure, including the ones the
	// "excludeStructures" property hides, so that clearing the
	// property makes those visible without reloading them.
	initBindings []compile.Binding
	initValues   map[string]eval.Val
	// structureNames are the structures the signature files
	// declare, which is what "excludeStructures" chooses from.
	structureNames []string
	// gaps counts the not-yet-implemented errors and panics that
	// suppression hid, by message — the inventory of what running
	// the input would need. Gaps reports it; gapSample keeps the
	// first statement that produced each message, and currentStmt
	// is the statement being executed, for that sample.
	gaps        map[string]int
	gapSample   map[string]string
	currentStmt string
	// pendingLines is extra output the current statement's
	// built-ins have produced (use's "[opening f]"); runStatement
	// drains it into the statement's output.
	pendingLines []string
	// useDepth is the current nesting depth of "use" calls.
	useDepth int
	// overloads is the registry of overloaded names ("over") and
	// their instances ("val inst"), carried across statements so a
	// use resolves against every instance declared so far.
	overloads *compile.OverloadEnv
	// fileRoot is the file system that "file" browses, and fileDir
	// the directory it was built for. It is built on first use and
	// rebuilt if the "directory" property has changed since, so
	// that a harness that sets the directory after the kernel is
	// made still gets the directory it asked for. It accumulates
	// what browsing has discovered, so it must outlive a statement.
	fileRoot *eval.File
	fileDir  string
	// fileValues records, per top-level name, the file it is bound
	// to — so that "val s = file.scott" lets a later statement
	// discover fields on "s". A name bound to something that is not
	// a file maps to nil, marking it as shadowing any file of that
	// name.
	fileValues map[string]*eval.File
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
		adaptSysFile(sys, &result.Bindings[i])
	}
	bindings = append(bindings, result.Bindings...)
	config := DefaultConfig()
	config.sys = sys
	dir, err := os.Getwd()
	if err == nil {
		config.Directory = dir
	}
	k := &Kernel{
		name:       name,
		config:     config,
		sys:        sys,
		bindings:   bindings,
		gaps:       map[string]int{},
		inlineExps: map[string]core.Exp{},
		recFns:     map[string]*core.Fn{},
		gapSample:  map[string]string{},
		overloads:  compile.NewOverloadEnv(),
		fileValues: map[string]*eval.File{},
	}
	// Method overloads that the signature files cannot express (a
	// record has one field per name): Range.contains dispatched on
	// the set types, targeting hidden bindings.
	methods, overloadBindings := compile.OverloadMethods(sys)
	k.bindings = append(k.bindings, overloadBindings...)
	k.methods = compile.NewMethodRegistry(
		append(result.Methods, methods...), bindings,
		k.globalType)
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
	for name, v := range eval.PPFunctions() {
		values["PP."+name] = v
	}
	// The Sys implementations read and write session state, so
	// the kernel supplies them.
	maps.Copy(values, k.sysBuiltins())
	// The Datalog implementations compile and evaluate the
	// translated programs, so the kernel supplies them too.
	maps.Copy(values, k.datalogBuiltins())
	// The use implementations execute files in this session.
	maps.Copy(values, k.useBuiltins())
	// The internal extent builtin backs sourceless query scans.
	values[compile.ExtentName] = eval.Fn(eval.ExtentValues)
	// The internal raise builtin backs the "raise" expression.
	values[compile.RaiseName] = eval.Fn(eval.RaiseFn)
	// The Range.contains overloads on sets (OverloadMethods).
	values[compile.CsContainsName] = eval.RangeSetContainsFn
	values[compile.DsContainsName] = eval.RangeSetContainsFn
	// Build the structure records first, so that a structure defined
	// in embedded Morel source can reference any other structure;
	// then evaluate those sources and rebuild, wiring in the
	// members they derive.
	buildStructureRecords(values, result.Bindings)
	for _, l := range structLibs() {
		k.loadStructLib(l, values, result.Bindings)
	}
	buildStructureRecords(values, result.Bindings)
	k.values = values
	k.bootBindings = slices.Clone(k.bindings)
	k.bootValues = maps.Clone(values)
	k.loadScott()
	// Snapshot the initialized environment — built-ins plus the
	// global datasets, no user bindings or overloads — so
	// Sys.clearEnv can restore exactly it.
	k.initBindings = slices.Clone(k.bindings)
	k.initValues = maps.Clone(k.values)
	for _, b := range result.Bindings {
		k.structureNames = append(k.structureNames, b.Name)
	}
	k.hideExcludedStructures()
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

// SetProp sets a session property, as "Sys.set" does from Morel
// source. It is how a front end tells the kernel something only it
// can know, such as the terminal's background color.
func (c *Config) SetProp(name, value string) {
	if _, ok := sysProps[name]; !ok {
		panic("unknown property: " + name)
	}
	c.props[name] = value
}

// GapSamples reports the first statement that produced each gap
// message, truncated to one line.
func (k *Kernel) GapSamples() map[string]string {
	return k.gapSample
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
	k.currentStmt = stmt
	if typeOnly {
		return k.executeTypeOnly(stmt)
	}
	n, err := parse.Stmt(k.name, stmt)
	if err != nil {
		// A parse failure is silent in script mode (the grammar
		// gap inventory records it); the interactive shell shows
		// it.
		k.recordGap(err.Error())
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

// clearEnv resets the session to its freshly initialized state:
// it drops every user binding, value, and overload, restoring
// the built-ins and global datasets snapshotted at construction.
func (k *Kernel) clearEnv() {
	k.bindings = slices.Clone(k.initBindings)
	k.values = maps.Clone(k.initValues)
	k.hideExcludedStructures()
	k.overloads = compile.NewOverloadEnv()
	k.inlineExps = map[string]core.Exp{}
	k.recFns = map[string]*core.Fn{}
	k.lastCode = nil
	k.planExDecl = nil
}

// recordGap counts a suppressed message and keeps the statement
// that first produced it.
func (k *Kernel) recordGap(msg string) {
	k.gaps[msg]++
	if _, ok := k.gapSample[msg]; !ok {
		sample := strings.ReplaceAll(k.currentStmt, "\n", " ")
		const maxSample = 120
		if len(sample) > maxSample {
			sample = sample[:maxSample] + "..."
		}
		k.gapSample[msg] = sample
	}
}

// hideExcludedStructures removes from the environment the
// structures that the "excludeStructures" property names — by
// default "Test", which exists to be called from scripts and
// would otherwise clutter every listing of the environment.
//
// It reads from the snapshot rather than from what is in scope, so
// that clearing the property brings a hidden structure back. The
// property is consulted whenever the environment is built, which
// is at startup and at every Sys.clearEnv.
func (k *Kernel) hideExcludedStructures() {
	pattern, _ := k.showProp("excludeStructures")
	if pattern == "" {
		return
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// A pattern that does not compile hides nothing, rather
		// than failing a session that is merely misconfigured.
		return
	}
	hidden := map[string]bool{}
	for _, name := range k.structureNames {
		if re.MatchString(name) {
			hidden[name] = true
		}
	}
	if len(hidden) == 0 {
		return
	}
	k.bindings = slices.DeleteFunc(slices.Clone(k.bindings),
		func(b compile.Binding) bool { return hidden[b.Name] })
	for name := range k.values {
		structure, _, isMember := strings.Cut(name, ".")
		if hidden[name] || isMember && hidden[structure] {
			delete(k.values, name)
		}
	}
}

// adaptSysFile rewrites the "Sys" structure's "file" member to the
// type it really has, "{...}" — a record whose fields are not
// known until the file system is browsed. The signature file
// writes it as "{}", the nearest thing Morel's type syntax can
// say, and "{}" is unit. Other structures and members are left
// unchanged.
func adaptSysFile(sys *types.System, b *compile.Binding) {
	if b.Name != "Sys" {
		return
	}
	record, ok := b.Type.(*types.Record)
	if !ok {
		return
	}
	fields := make([]types.Field, len(record.Fields))
	for i, f := range record.Fields {
		fields[i] = f
		if f.Label == compile.FileName {
			fields[i].Type = sys.ProgressiveRecord(nil)
		}
	}
	b.Type = sys.Record(fields)
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
	decl := parseDecl(k.name, scottSrc)
	for _, b := range k.evalDecl(k.bindings, k.values, decl) {
		// scott's collections are foreign relations: they behave
		// as their rows but print opaquely as "<relation>".
		if fields, ok := b.val.([]eval.Val); ok {
			for i, f := range fields {
				if rows, ok := f.([]eval.Val); ok {
					fields[i] = eval.Relation{Rows: rows}
				}
			}
		}
		k.bind(b.name, b.typ)
		k.values[b.name] = b.val
	}
}

// files returns the session's view of the file system, building
// it on first use and rebuilding it if the "directory" property
// has changed. It also binds the name "file" to the root, so that
// a statement that mentions it can be evaluated.
func (k *Kernel) files() *compile.Files {
	dir := k.config.Directory
	if dir == "" {
		dir = "."
	}
	if k.fileRoot == nil || k.fileDir != dir {
		k.fileRoot = eval.NewFile(dir)
		k.fileDir = dir
		// "Sys.file" is the same file system as "file"; its value
		// is per-session, so it is filled in here rather than at
		// boot, once the directory is known.
		k.values["Sys.file"] = k.fileRoot
		k.setStructureField("Sys", compile.FileName, k.fileRoot)
		// So that "Sys.file.scott" browses, as "file.scott" does.
		k.fileValues["Sys."+compile.FileName] = k.fileRoot
	}
	if _, shadowed := k.fileValues[compile.FileName]; !shadowed {
		k.values[compile.FileName] = k.fileRoot
	}
	return &compile.Files{Root: k.fileRoot, Bound: k.fileValues}
}

// setStructureField sets one member of a structure's record value,
// finding the member's slot from the structure's type. It does
// nothing if there is no such structure or member.
func (k *Kernel) setStructureField(structure, member string,
	v eval.Val,
) {
	for _, b := range k.bindings {
		if b.Name != structure {
			continue
		}
		record, isRecord := b.Type.(*types.Record)
		if !isRecord {
			return
		}
		fields, isRecordVal := k.values[structure].([]eval.Val)
		if !isRecordVal {
			return
		}
		for i, f := range record.Fields {
			if f.Label == member && i < len(fields) {
				fields[i] = v
				return
			}
		}
		return
	}
}

// recordFileBinding notes whether a top-level name is bound to a
// file, so that fields can be discovered on it later. A name bound
// to anything else is remembered as shadowing — but only if it
// could have denoted a file, so that ordinary bindings do not
// accumulate.
func (k *Kernel) recordFileBinding(name string, v eval.Val) {
	if f, isFile := v.(*eval.File); isFile {
		k.fileValues[name] = f
		return
	}
	_, tracked := k.fileValues[name]
	if tracked || name == compile.FileName {
		k.fileValues[name] = nil
	}
}

// scopeBind is a binding produced by evaluating a declaration at
// boot: a name, its type, and its runtime value.
type scopeBind struct {
	name string
	typ  types.Type
	val  eval.Val
}

// evalDecl compiles and evaluates one declaration against the given
// type and value environments and returns the bindings it makes. It
// is the boot-time path shared by loadScott and loadStructLib; the
// embedded source is tested, so any failure is a programming error
// and panics.
func (k *Kernel) evalDecl(bindings []compile.Binding,
	values map[string]eval.Val, decl ast.Decl,
) []scopeBind {
	resolved, err := compile.Deduce(k.sys, bindings, nil, decl)
	if err != nil {
		panic(err)
	}
	coreDecl, err := compile.Resolve(resolved, nil)
	if err != nil {
		panic(err)
	}
	compiled, err := compile.Statement(coreDecl, values, k.sys)
	if err != nil {
		panic(err)
	}
	frame := eval.NewFrame(compiled.Slots)
	_, err = compiled.Code.Eval(frame)
	if err != nil {
		panic(err)
	}
	binds := make([]scopeBind, len(compiled.Binds))
	for i, b := range compiled.Binds {
		binds[i] = scopeBind{
			name: b.Pat.Name,
			typ:  b.Pat.T,
			val:  frame.Slots[b.Slot],
		}
	}
	return binds
}

// parseDecl parses a single embedded declaration; a parse failure
// is a programming error and panics.
func parseDecl(name, src string) ast.Decl {
	n, err := parse.Stmt(name, src)
	if err != nil {
		panic(err)
	}
	decl, ok := n.(ast.Decl)
	if !ok {
		panic(name + ": not a declaration")
	}
	return decl
}

// buildStructureRecords sets values[Name] to each structure's
// record value: a slice of its members' implementations, with a
// placeholder for any member that has none, so unpulled corpus
// statements stay silent rather than wrong. It is idempotent, and
// is run again after the structLib sources so the members they
// derive replace their placeholders.
func buildStructureRecords(values map[string]eval.Val,
	bindings []compile.Binding,
) {
	for _, b := range bindings {
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
}

// structLib is a built-in structure whose derivable members are
// defined in an embedded Morel source file (lib/<file>), evaluated
// at boot with the structure's native members in scope.
type structLib struct {
	name string
	file string
}

// mustReadLib reads a library Morel source (lib/*.sml) from the
// embedded library; the files are embedded and tested, so a failure
// is a programming error and panics.
func mustReadLib(name string) string {
	data, err := lib.FS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// structLibs returns the structures whose members are defined this
// way. Each keeps a few members native — recursive, hot, or
// primitive — and derives the rest in Morel.
func structLibs() []structLib {
	return []structLib{
		// native: not, and the comparison operators.
		{name: "Bool", file: "bool.sml"},
		{name: "Either", file: "either.sml"}, // no native members
		{name: "Fn", file: "fn.sml"},         // native: id, o, repeat
		{name: "Option", file: "option.sml"}, // native: getOpt, isSome, valOf
		// native: the doc constructors, fillSep/fillCat, render.
		{name: "PP", file: "pp.sml"},
	}
}

// loadStructLib evaluates a structure's embedded Morel source and
// wires the members it defines into values["Name.member"]. The
// structure's already-bound native members are first placed in
// scope under their bare names — an implicit "open" — so the source
// can build on them; those bare bindings are local and never leak
// to the global namespace.
func (k *Kernel) loadStructLib(l structLib,
	values map[string]eval.Val, bindings []compile.Binding,
) {
	var record *types.Record
	for i := range bindings {
		if bindings[i].Name == l.name {
			record, _ = bindings[i].Type.(*types.Record)
			break
		}
	}
	if record == nil {
		panic(l.name + ": not a structure")
	}
	localBindings := slices.Clone(k.bindings)
	localValues := maps.Clone(values)
	// Seed the native members under their bare names.
	for _, f := range record.Fields {
		if v, ok := values[l.name+"."+f.Label]; ok {
			localValues[f.Label] = v
			localBindings = append(localBindings,
				compile.Binding{Name: f.Label, Type: f.Type})
		}
	}
	stmts, _, err := Split(l.name, mustReadLib(l.file))
	if err != nil {
		panic(err)
	}
	provided := map[string]bool{}
	for _, stmt := range stmts {
		decl := parseDecl(l.name, stmt)
		for _, b := range k.evalDecl(localBindings, localValues, decl) {
			localBindings = append(localBindings,
				compile.Binding{Name: b.name, Type: b.typ})
			localValues[b.name] = b.val
			provided[b.name] = true
		}
	}
	// Wire the derived members into their structure slots.
	for _, f := range record.Fields {
		if provided[f.Label] {
			values[l.name+"."+f.Label] = localValues[f.Label]
		}
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
				// A blanket recover keeps the session alive, but it
				// also hides evaluator bugs as "no output". Setting
				// MOREL_DEBUG re-panics so the failure is visible.
				if os.Getenv("MOREL_DEBUG") != "" {
					panic(r)
				}
				msg := fmt.Sprintf("internal error: %v", r)
				k.recordGap(msg)
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

// qualifiedOrPlain returns surfaceT when it is a qualified type (so
// a later use of the binding re-emits its overload constraints),
// otherwise plainT.
func qualifiedOrPlain(plainT, surfaceT types.Type) types.Type {
	if _, ok := surfaceT.(*types.Qualified); ok {
		return surfaceT
	}
	return plainT
}

// overDeclEcho handles an "over name" declaration: it introduces an
// overloaded name, binds nothing at runtime, and echoes "over name".
// Later "val inst" declarations add instances to it.
func (k *Kernel) overDeclEcho(coreDecl core.Decl) (string, bool) {
	overDecl, isOver := coreDecl.(*core.OverDecl)
	if !isOver {
		return "", false
	}
	k.overloads.Declare(overDecl.Name)
	return "over " + overDecl.Name, true
}

func (k *Kernel) runStatement(n ast.Node) string {
	k.pendingLines = nil
	var decl ast.Decl
	switch node := n.(type) {
	case ast.Decl:
		decl = node
	case ast.Expr:
		decl = compile.ItValDecl(node)
	}
	if sigDecl, isSig := decl.(*ast.SignatureDecl); isSig {
		// A signature declaration only echoes; nothing ascribes
		// signatures yet.
		return ast.UnparseSignatureDecl(sigDecl)
	}
	k.methods.RewriteDecl(decl)
	resolved, err := compile.DeduceFiles(k.sys, k.bindings, k.overloads, decl,
		k.files())
	if err != nil {
		return k.formatCompileError(err)
	}
	datatypeDecl, isDatatype := resolved.Decl.(*ast.DatatypeDecl)
	if isDatatype {
		// The declaration registered its datatype and
		// constructors in the type system. Re-bind each
		// constructor name, so that it shadows a same-named
		// built-in binding (a user datatype's Empty must hide
		// exn's); the shell then echoes the declaration.
		k.bindDatatypeCons(datatypeDecl)
		return ast.UnparseDatatypeDecl(datatypeDecl)
	}
	if typeDecl, isType := resolved.Decl.(*ast.TypeDecl); isType {
		// The declaration registered its type aliases; echo it,
		// mapping any datatype whose name has since been displaced to
		// "?.d". Copy the binds so the stored alias body is untouched.
		binds := make([]ast.TypeBind, len(typeDecl.Binds))
		for i, b := range typeDecl.Binds {
			binds[i] = b
			binds[i].Type = ast.MapNamedTypeNames(b.Type,
				k.sys.DatatypeDisplay)
		}
		return ast.UnparseTypeDecl(ast.NewTypeDecl(typeDecl.Span(),
			binds))
	}
	coreDecl, err := compile.Resolve(resolved, k.overloads)
	if err != nil {
		return k.formatCompileError(err)
	}
	if s, isOver := k.overDeclEcho(coreDecl); isOver {
		return s
	}
	warnings, covErr := compile.CheckCoverage(k.sys, coreDecl)
	if covErr != nil {
		return k.formatCompileError(covErr)
	}
	// A "val inst" instance binds a generated hidden name but echoes
	// under the overloaded name; capture it before optimization,
	// which does not carry the marker.
	instName := ""
	if nv, ok := coreDecl.(*core.NonRecValDecl); ok {
		instName = nv.Overload
	}
	if !isPlanCall(coreDecl) {
		// A Sys.plan or Sys.planEx call must not replace the
		// statement it describes.
		k.planExDecl = coreDecl
	}
	coreDecl = compile.Inline(
		coreDecl, k.inlineEnv(), k.inlinePassCount(),
	)
	// Later statements inline the pre-grounding form: grounding
	// specializes queries to this statement's bindings, but the
	// logical form is what a later query's own grounding wants.
	inlined := coreDecl
	// Ground unbounded query variables, to a fixpoint: expanding
	// one query can expose another.
	for range k.inlinePassCount() {
		if !compile.ContainsUnbounded(coreDecl) {
			break
		}
		coreDecl2, gerr := compile.Ground(coreDecl, k.sys,
			k.recFns)
		if gerr != nil {
			return k.formatCompileError(gerr)
		}
		if coreDecl2 == coreDecl {
			break
		}
		coreDecl = coreDecl2
	}
	compiled, err := compile.Statement(coreDecl, k.values, k.sys)
	if err != nil {
		return k.formatCompileError(err)
	}
	var lines []string
	for _, w := range warnings {
		lines = append(lines, w.String())
	}
	frame := eval.NewFrame(compiled.Slots)
	_, err = compiled.Code.Eval(frame)
	// Record the statement's code for Sys.plan, after evaluating —
	// so a Sys.plan call sees the previous statement. A declaration
	// with no bound expression (a datatype or type) has no plan; it
	// leaves the previous plan in place rather than clearing it.
	if compiled.Plan != nil {
		k.lastCode = compiled.Plan
	}
	// Output that built-ins produced (use's "[opening f]") comes
	// before the statement's own report.
	lines = append(lines, k.pendingLines...)
	k.pendingLines = nil
	if err != nil {
		// A nonexhaustive-match warning still precedes the exception
		// its unmatched value raises.
		var msg string
		var morelErr *eval.MorelError
		if errors.As(err, &morelErr) {
			msg = morelErr.Describe()
		} else {
			msg = err.Error()
		}
		return strings.Join(append(lines, msg), "\n")
	}
	for _, b := range compiled.Binds {
		v := frame.Slots[b.Slot]
		if instName != "" {
			// The hidden binding is needed at runtime (a use compiles
			// to a reference to it), but it is not a user-visible name;
			// echo the overloaded name instead.
			k.values[b.Pat.Name] = v
			lines = append(lines,
				k.config.prettyBinding(instName, v, b.Pat.T,
					b.Pat.SurfaceT))
			continue
		}
		// A binding whose value is used at an abstract type has a
		// qualified surface type; bind that (not the plain function
		// type) so a later use re-emits the overload constraints.
		k.bind(b.Pat.Name, qualifiedOrPlain(b.Pat.T, b.Pat.SurfaceT))
		k.values[b.Pat.Name] = v
		k.recordFileBinding(b.Pat.Name, v)
		lines = append(lines,
			k.config.prettyBinding(b.Pat.Name, v, b.Pat.T,
				b.Pat.SurfaceT))
	}
	k.recordInlineExp(inlined)
	return strings.Join(lines, "\n")
}

// defaultInlinePassCount is the number of inlining passes to run
// when the "inlinePassCount" property is unset.
const defaultInlinePassCount = 5

// isPlanCall reports whether a declaration is a call of Sys.plan
// or Sys.planEx.
func isPlanCall(decl core.Decl) bool {
	d, ok := decl.(*core.NonRecValDecl)
	if !ok {
		return false
	}
	apply, ok := d.Exp.(*core.Apply)
	if !ok {
		return false
	}
	name := compile.BuiltinName(apply.Fn)
	return name == "Sys.plan" || name == "Sys.planEx"
}

// inlinePassCount returns the number of inlining passes to run: the
// "inlinePassCount" property, or its default.
func (k *Kernel) inlinePassCount() int {
	if s, ok := k.config.props["inlinePassCount"]; ok {
		n, err := strconv.Atoi(s)
		if err == nil {
			return n
		}
	}
	return defaultInlinePassCount
}

// inlineEnv returns the cross-statement inlining context: the
// defining expressions of earlier top-level bindings, and which
// names are resolvable at evaluation time.
func (k *Kernel) inlineEnv() *compile.InlineEnv {
	return &compile.InlineEnv{
		Exps: k.inlineExps,
		Known: func(name string) bool {
			_, ok := k.values[name]
			return ok
		},
	}
}

// recordInlineExp keeps the expression that defined a top-level
// single-variable binding, so later statements can inline it.
// Stored expressions that use a name this declaration rebinds are
// dropped: they captured the name's previous binding, and
// inlining them would resolve it to the new one.
func (k *Kernel) recordInlineExp(decl core.Decl) {
	var names []string
	switch d := decl.(type) {
	case *core.NonRecValDecl:
		for _, id := range core.PatIDs(d.Pat) {
			names = append(names, id.Name)
		}
	case *core.RecValDecl:
		for _, b := range d.Binds {
			for _, id := range core.PatIDs(b.Pat) {
				names = append(names, id.Name)
			}
		}
	}
	for _, name := range names {
		delete(k.inlineExps, name)
		delete(k.recFns, name)
		for stored, exp := range k.inlineExps {
			if slices.Contains(compile.FreeNames(exp), name) {
				delete(k.inlineExps, stored)
			}
		}
		for stored, fn := range k.recFns {
			if slices.Contains(compile.FreeNames(fn), name) {
				delete(k.recFns, stored)
			}
		}
	}
	switch d := decl.(type) {
	case *core.NonRecValDecl:
		if pat, ok := d.Pat.(*core.IDPat); ok {
			k.inlineExps[pat.Name] = d.Exp
		}
	case *core.RecValDecl:
		if len(d.Binds) == 1 {
			pat, okPat := d.Binds[0].Pat.(*core.IDPat)
			fn, okFn := d.Binds[0].Exp.(*core.Fn)
			if okPat && okFn {
				k.recFns[pat.Name] = fn
			}
		}
	}
}

// globalType is the declared type of a top-level binding, or nil;
// the method registry dispatches identifier receivers by it.
func (k *Kernel) globalType(name string) types.Type {
	for i := range k.bindings {
		if k.bindings[i].Name == name {
			return k.bindings[i].Type
		}
	}
	return nil
}

// bindDatatypeCons binds a datatype declaration's constructors as
// values, so later statements resolve them ahead of any
// same-named built-in constructor binding. Their types come from
// the type system, where the declaration just registered them.
func (k *Kernel) bindDatatypeCons(decl *ast.DatatypeDecl) {
	for _, b := range decl.Binds {
		for _, c := range b.Cons {
			tc, ok := k.sys.LookupTyCon(c.Name)
			if !ok {
				continue
			}
			t := tc.Result
			if tc.Arg != nil {
				t = k.sys.Fn(tc.Arg, tc.Result)
			}
			k.bind(c.Name, t)
		}
	}
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
		k.recordGap(err.Error())
		return ""
	}
	if unsupported(compileErr.Msg) {
		k.recordGap(compileErr.Msg)
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
	if sigDecl, isSig := decl.(*ast.SignatureDecl); isSig {
		// A signature declaration only echoes; nothing ascribes
		// signatures yet.
		return ast.UnparseSignatureDecl(sigDecl)
	}
	k.methods.RewriteDecl(decl)
	resolved, err := compile.DeduceFiles(k.sys, k.bindings, k.overloads, decl,
		k.files())
	if err != nil {
		return k.formatCompileError(err)
	}
	valDecl, ok := resolved.Decl.(*ast.ValDecl)
	if !ok {
		return ""
	}
	// A ":t" statement reports match-coverage warnings too, before
	// its type; it is not evaluated, so a redundant match is still
	// an error.
	var lines []string
	coreDecl, cerr := compile.Resolve(resolved, k.overloads)
	if cerr == nil {
		warnings, covErr := compile.CheckCoverage(k.sys, coreDecl)
		if covErr != nil {
			return k.formatCompileError(covErr)
		}
		for _, w := range warnings {
			lines = append(lines, w.String())
		}
	}
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
