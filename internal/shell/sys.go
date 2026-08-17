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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hydromatic/morel-go/internal/compile"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/types"
)

// The Sys structure. Its implementations live on the kernel,
// because they read and write session state; NewKernel injects
// them alongside the pure built-ins.

// The product name and version.
const (
	productName    = "morel-go"
	productVersion = "0.8.0"
)

// Banner is the shell's startup banner, for a front end that
// prints it before the first prompt.
func Banner() string { return bannerText() }

// bannerText is the shell's banner. Go version numbers start with
// 'v', which the banner supplies; productVersion does not carry it.
func bannerText() string {
	return productName + " v" + productVersion +
		" (" + runtime.Version() + ", " +
		runtime.GOOS + "/" + runtime.GOARCH + ")"
}

// propKind says what values a property accepts.
type propKind int

const (
	intProp propKind = iota
	boolProp
	stringProp
	// outputProp accepts an output-mode name, shown uppercase
	// ("CLASSIC", "TABULAR").
	outputProp
	// dynamicProp is computed from the session (banner,
	// directory) and cannot be set.
	dynamicProp
)

// sysProp describes a session property: its kind and its
// default rendering (nil means NONE).
type sysProp struct {
	dflt *string
	kind propKind
}

func text(s string) *string { return &s }

// Names of the integer printing properties.
const (
	lineWidthProp   = "lineWidth"
	printDepthProp  = "printDepth"
	printLengthProp = "printLength"
	stringDepthProp = "stringDepth"
)

// sysProps is the property table: every property is
// accepted, shown, and unset; the printing properties (and
// later "output") change behavior.
var sysProps = map[string]sysProp{
	// lint: sort until '^}' where '^\t"'
	"banner":               {nil, dynamicProp},
	"colorScheme":          {nil, stringProp},
	"directory":            {nil, dynamicProp},
	"excludeStructures":    {text("^Test$"), stringProp},
	"hybrid":               {text("false"), boolProp},
	"inlinePassCount":      {text("5"), intProp},
	lineWidthProp:          {nil, intProp},
	"matchCoverageEnabled": {text("true"), boolProp},
	"matchStrict":          {text("false"), boolProp},
	"now":                  {nil, stringProp},
	"optionalInt":          {nil, intProp},
	"output":               {text("CLASSIC"), outputProp},
	printDepthProp:         {nil, intProp},
	printLengthProp:        {nil, intProp},
	"productName":          {nil, dynamicProp},
	"productVersion":       {nil, dynamicProp},
	"relationalize":        {text("false"), boolProp},
	"scriptDirectory":      {nil, dynamicProp},
	stringDepthProp:        {nil, intProp},
	"stringFold":           {nil, stringProp},
	"terminalBackground":   {nil, stringProp},
	"timeZone":             {nil, stringProp},
}

// intPropField returns the config field backing an integer
// printing property, or nil for the others.
func (c *Config) intPropField(name string) *int {
	// lint: sort until '^	}' where '^	case '
	switch name {
	case lineWidthProp:
		return &c.LineWidth
	case printDepthProp:
		return &c.PrintDepth
	case printLengthProp:
		return &c.PrintLength
	case stringDepthProp:
		return &c.StringDepth
	default:
		return nil
	}
}

// intPropDefault returns an integer printing property's
// default.
func intPropDefault(name string) int {
	// lint: sort until '^	}' where '^	case '
	switch name {
	case lineWidthProp:
		return defaultLineWidth
	case printDepthProp:
		return defaultPrintDepth
	case printLengthProp:
		return defaultPrintLength
	case stringDepthProp:
		return defaultStringDepth
	default:
		return 0
	}
}

// sysBuiltins returns the Sys implementations, and their
// top-level aliases, for NewKernel to inject.
func (k *Kernel) sysBuiltins() map[string]eval.Val {
	m := map[string]eval.Val{
		"Sys.clearEnv":          eval.Fn(k.sysClearEnv),
		"Sys.colorSchemes":      eval.Fn(k.sysColorSchemes),
		"Sys.deduceColorScheme": eval.Fn(k.sysDeduceColorScheme),
		"Sys.env":               eval.Fn(k.sysEnv),
		"Sys.plan":              eval.Fn(k.sysPlan),
		"Sys.planEx":            eval.Fn(k.sysPlanEx),
		"Sys.set":               eval.Fn(k.sysSet),
		"Sys.show":              eval.Fn(k.sysShow),
		"Sys.showAll":           eval.Fn(k.sysShowAll),
		"Sys.unset":             eval.Fn(k.sysUnset),
		"Variant.print": eval.Fn(func(arg eval.Val) (eval.Val, error) {
			return compile.VariantPrint(arg, k.sys), nil
		}),
		"Variant.parse": eval.Fn(func(arg eval.Val) (eval.Val, error) {
			s, _ := arg.(string)
			return compile.VariantParse(s, k.sys)
		}),
		"Test.highlight": eval.Fn(func(arg eval.Val) (eval.Val, error) {
			s, _ := arg.(string)
			return Highlight(s), nil
		}),
		"Time.now": eval.Fn(k.timeNow),
		"Date.date": eval.Fn(func(arg eval.Val) (eval.Val, error) {
			rec, _ := arg.([]eval.Val)
			return eval.DateConstructRecord(rec, k.timeZone())
		}),
		"Date.fromTimeLocal": eval.Fn(func(arg eval.Val) (eval.Val, error) {
			return eval.DateFromTimeLocal(arg, k.timeZone()), nil
		}),
		"Date.localOffset": eval.Fn(func(eval.Val) (eval.Val, error) {
			return eval.DateLocalOffset(k.timeZone(), k.nowTime()), nil
		}),
	}
	m["clearEnv"] = m["Sys.clearEnv"]
	m["env"] = m["Sys.env"]
	m["plan"] = m["Sys.plan"]
	m["planEx"] = m["Sys.planEx"]
	m["set"] = m["Sys.set"]
	m["show"] = m["Sys.show"]
	m["showAll"] = m["Sys.showAll"]
	m["unset"] = m["Sys.unset"]
	return m
}

// sysPlanEx is "Sys.planEx phase": the most recent statement's
// declaration re-planned to the numbered pass, rendered as
// source.
func (k *Kernel) sysPlanEx(arg eval.Val) (eval.Val, error) {
	phase, ok := arg.(string)
	if !ok {
		return "Error re-planning: not a phase", nil
	}
	if k.planExDecl == nil {
		return "No previous command to re-plan", nil
	}
	d := compile.Replan(k.planExDecl, k.inlineEnv(), k.sys,
		k.recFns, k.inlinePassCount(), phase)
	return compile.UnparseDecl(k.sys, d), nil
}

// sysClearEnv is "Sys.clearEnv ()": it resets the session
// environment to its freshly initialized state — dropping every
// user binding, value, and overload — and returns unit.
func (k *Kernel) sysClearEnv(eval.Val) (eval.Val, error) {
	k.clearEnv()
	return core.Unit{}, nil
}

// sysPlan is "Sys.plan ()": the compiled plan of the most
// recently executed statement, as a string.
func (k *Kernel) sysPlan(eval.Val) (eval.Val, error) {
	if k.lastCode == nil {
		return "", nil
	}
	return k.lastCode.Describe(), nil
}

// timeNow is "Time.now ()": the current time as nanoseconds. It
// reads the "now" property, an ISO-8601 instant, so tests are
// deterministic; absent or unparsable, it uses the wall clock.
func (k *Kernel) timeNow(eval.Val) (eval.Val, error) {
	return k.nowTime().UnixNano(), nil
}

// nowTime is the reference instant: the "now" property parsed as an
// ISO-8601 instant, or the wall clock when absent or unparsable.
func (k *Kernel) nowTime() time.Time {
	t, err := time.Parse(time.RFC3339, k.config.props["now"])
	if err != nil {
		return time.Now()
	}
	return t
}

// timeZone is the session's zone from the "timeZone" property, or
// the local zone when absent or unknown.
func (k *Kernel) timeZone() *time.Location {
	name := k.config.props["timeZone"]
	if name == "" {
		return time.Local //nolint:gosmopolitan // the session default
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local //nolint:gosmopolitan // fallback default
	}
	return loc
}

// sysEnv is "Sys.env ()": the environment's bindings as (name,
// type) pairs, sorted by name, with polymorphic types
// forall-quantified.
func (k *Kernel) sysEnv(eval.Val) (eval.Val, error) {
	names := make([]string, len(k.bindings))
	byName := make(map[string]types.Type, len(k.bindings))
	for i, b := range k.bindings {
		names[i] = b.Name
		byName[b.Name] = b.Type
	}
	slices.Sort(names)
	out := make([]eval.Val, len(names))
	for i, name := range names {
		out[i] = []eval.Val{
			name,
			k.envTypeString(name, byName[name]),
		}
	}
	return out, nil
}

// envTypeString renders a binding's type as Sys.env shows it:
// "forall 'a 'b. " precedes a polymorphic type.
//
// A nullary datatype constructor is not quantified: NONE is one
// value of the option datatype, not a family of them, so it shows
// as "'a option". "nil" is not one of these -- the empty list is a
// value of the built-in list type, not a constructor of a declared
// datatype -- so it keeps its "forall".
func (k *Kernel) envTypeString(name string, t types.Type) string {
	t = userFacing(k.sys, t)
	if rec, isRecord := t.(*types.Record); isRecord {
		return k.envRecordString(rec)
	}
	n := 0
	countTypeVars(t, &n)
	if n == 0 || k.isDatatypeCon(name) {
		return t.String()
	}
	var b strings.Builder
	b.WriteString("forall")
	for i := range n {
		b.WriteString(" " + k.sys.Var(i).String())
	}
	b.WriteString(". " + t.String())
	return b.String()
}

// envRecordString renders a record -- a structure is one, its
// members its fields -- with each member quantified on its own.
// morel-java quantifies inside the record rather than outside it,
// because each member is independently polymorphic: two members
// that both mention "'a" do not thereby share a type.
func (k *Kernel) envRecordString(rec *types.Record) string {
	var b strings.Builder
	b.WriteString("{")
	for i, f := range rec.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		// A member whose name is a keyword is quoted, as it must
		// be written and as the pretty-printer writes it.
		b.WriteString(parse.QuoteIdent(f.Label) + ":" +
			k.memberTypeString(f.Type))
	}
	if rec.Progressive {
		if len(rec.Fields) > 0 {
			b.WriteString(", ")
		}
		b.WriteString("...")
	}
	b.WriteString("}")
	return b.String()
}

// memberTypeString renders one member of a record: its type
// variables renumbered from "'a" -- the record numbers them
// across all its members, so one member alone may start at "'c"
// -- and quantified if it has any.
func (k *Kernel) memberTypeString(t types.Type) string {
	ordinals := varOrdinals(t)
	if len(ordinals) == 0 {
		return t.String()
	}
	args := make([]types.Type, ordinals[len(ordinals)-1]+1)
	for i := range args {
		args[i] = k.sys.Var(i)
	}
	for i, ordinal := range ordinals {
		args[ordinal] = k.sys.Var(i)
	}
	var b strings.Builder
	b.WriteString("forall")
	for i := range ordinals {
		b.WriteString(" " + k.sys.Var(i).String())
	}
	b.WriteString(". " + k.sys.Substitute(t, args).String())
	return b.String()
}

// varOrdinals are the distinct type-variable ordinals in a type,
// in increasing order.
func varOrdinals(t types.Type) []int {
	seen := map[int]bool{}
	forEachVar(t, func(ordinal int) { seen[ordinal] = true })
	ordinals := make([]int, 0, len(seen))
	for ordinal := range seen {
		ordinals = append(ordinals, ordinal)
	}
	slices.Sort(ordinals)
	return ordinals
}

// isDatatypeCon reports whether a name is a nullary constructor
// of a declared datatype, such as NONE or ALL.
func (k *Kernel) isDatatypeCon(name string) bool {
	tc, ok := k.sys.LookupTyCon(name)
	if !ok || tc.Arg != nil {
		return false
	}
	named, ok := tc.Result.(*types.Named)
	if !ok {
		return false
	}
	_, isDatatype := k.sys.DatatypeArity(named.Name)
	return isDatatype
}

// userFacing rewrites a type for display, replacing the internal
// collection type — a list or a bag with its orderedness still
// free, which is what an aggregate such as "count" accepts — with
// a bag, the spelling a user can write. "$collection" is internal
// and its "$" says so; it must not reach the user.
func userFacing(sys *types.System, t types.Type) types.Type {
	// lint: sort until '^	}' where '^	case '
	switch t := t.(type) {
	case *types.Collection:
		return sys.Named(bagType, userFacing(sys, t.Elem))
	case *types.Fn:
		return sys.Fn(userFacing(sys, t.Param),
			userFacing(sys, t.Result))
	case *types.List:
		return sys.List(userFacing(sys, t.Elem))
	case *types.Named:
		args := make([]types.Type, len(t.Args))
		for i, arg := range t.Args {
			args[i] = userFacing(sys, arg)
		}
		return sys.Named(t.Name, args...)
	case *types.Record:
		fields := make([]types.Field, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = types.Field{
				Label: f.Label, Type: userFacing(sys, f.Type),
			}
		}
		if t.Progressive {
			// A progressive record keeps its "..."; rebuilding it as
			// an ordinary record would drop it, and an empty one
			// would become unit.
			return sys.ProgressiveRecord(fields)
		}
		return sys.Record(fields)
	case *types.Tuple:
		args := make([]types.Type, len(t.Args))
		for i, arg := range t.Args {
			args[i] = userFacing(sys, arg)
		}
		return sys.Tuple(args...)
	default:
		return t
	}
}

// countTypeVars sets n to one more than the highest type-variable
// ordinal in t, so that ordinals 0..n-1 quantify it.
func countTypeVars(t types.Type, n *int) {
	forEachVar(t, func(ordinal int) {
		if ordinal >= *n {
			*n = ordinal + 1
		}
	})
}

// forEachVar calls action for every type variable in t, including
// repeats.
func forEachVar(t types.Type, action func(ordinal int)) {
	// lint: sort until '^	}' where '^	case '
	switch t := t.(type) {
	case *types.Collection:
		forEachVar(t.Elem, action)
	case *types.Fn:
		forEachVar(t.Param, action)
		forEachVar(t.Result, action)
	case *types.List:
		forEachVar(t.Elem, action)
	case *types.Named:
		for _, arg := range t.Args {
			forEachVar(arg, action)
		}
	case *types.Record:
		for _, f := range t.Fields {
			forEachVar(f.Type, action)
		}
	case *types.Tuple:
		for _, arg := range t.Args {
			forEachVar(arg, action)
		}
	case *types.Var:
		action(t.Ordinal)
	}
}

// sysSet is "Sys.set (name, value)". An unknown property, or a
// value of the wrong type, panics, so the statement produces
// no output.
func (k *Kernel) sysSet(arg eval.Val) (eval.Val, error) {
	vals, _ := arg.([]eval.Val)
	name, _ := vals[0].(string)
	prop, ok := sysProps[name]
	if !ok {
		panic("unknown property: " + name)
	}
	value := vals[1]
	// lint: sort until '^	}' where '^	case '
	switch prop.kind {
	case boolProp:
		b, isBool := value.(bool)
		if !isBool {
			panic("property " + name + " requires a bool")
		}
		k.config.props[name] = strconv.FormatBool(b)
	case dynamicProp:
		panic("cannot set property: " + name)
	case intProp:
		i, isInt := value.(int32)
		if !isInt {
			panic("property " + name + " requires an int")
		}
		if field := k.config.intPropField(name); field != nil {
			*field = int(i)
		} else {
			k.config.props[name] = strconv.Itoa(int(i))
		}
	case outputProp:
		s, isString := value.(string)
		mode := strings.ToUpper(s)
		if !isString ||
			mode != "CLASSIC" && mode != "TABULAR" {
			panic("bad output mode")
		}
		k.config.props[name] = mode
	case stringProp:
		s, isString := value.(string)
		if !isString {
			panic("property " + name + " requires a string")
		}
		k.config.props[name] = s
	}
	return unitResult()
}

// sysShow is "Sys.show name": SOME of the property's current
// value rendered as a string, or NONE if it has no value.
func (k *Kernel) sysShow(arg eval.Val) (eval.Val, error) {
	name, _ := arg.(string)
	if _, ok := sysProps[name]; !ok {
		panic("unknown property: " + name)
	}
	if s, ok := k.showProp(name); ok {
		return eval.SomeVal(s), nil
	}
	return eval.NoneVal, nil
}

// showProp gives a property's current rendering, or false for
// NONE.
func (k *Kernel) showProp(name string) (string, bool) {
	if field := k.config.intPropField(name); field != nil {
		return strconv.Itoa(*field), true
	}
	// lint: sort until '^	}' where '^	case '
	switch name {
	case "banner":
		return bannerText(), true
	case "directory", "scriptDirectory":
		return k.config.Directory, true
	case "productName":
		return productName, true
	case "productVersion":
		return productVersion, true
	}
	if s, ok := k.config.props[name]; ok {
		return s, true
	}
	if d := sysProps[name].dflt; d != nil {
		return *d, true
	}
	return "", false
}

// sysColorSchemes is "Sys.colorSchemes ()": the built-in
// syntax-highlighting color schemes, each a record whose "name"
// field is the scheme name and whose remaining fields give the
// style of each token category.
func (k *Kernel) sysColorSchemes(eval.Val) (eval.Val, error) {
	out := make([]eval.Val, len(colorSchemes))
	for i, scheme := range colorSchemes {
		record := make([]eval.Val, len(schemeFields))
		for j, f := range schemeFields {
			if f.label == "name" {
				record[j] = scheme.Name
			} else {
				record[j] = scheme.Style(f.category)
			}
		}
		out[i] = record
	}
	return out, nil
}

// sysDeduceColorScheme is "Sys.deduceColorScheme ()": the name of
// the color scheme in effect.
func (k *Kernel) sysDeduceColorScheme(eval.Val) (eval.Val, error) {
	return k.DeduceColorScheme().Name, nil
}

// sysShowAll is "Sys.showAll ()": every property and its
// current value, sorted by name.
func (k *Kernel) sysShowAll(eval.Val) (eval.Val, error) {
	names := make([]string, 0, len(sysProps))
	for name := range sysProps {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]eval.Val, len(names))
	for i, name := range names {
		var v eval.Val = eval.NoneVal
		if s, ok := k.showProp(name); ok {
			v = eval.SomeVal(s)
		}
		out[i] = []eval.Val{name, v}
	}
	return out, nil
}

// sysUnset is "Sys.unset name": restores the property's
// default.
func (k *Kernel) sysUnset(arg eval.Val) (eval.Val, error) {
	name, _ := arg.(string)
	if _, ok := sysProps[name]; !ok {
		panic("unknown property: " + name)
	}
	if field := k.config.intPropField(name); field != nil {
		*field = intPropDefault(name)
	} else {
		delete(k.config.props, name)
	}
	return unitResult()
}

func unitResult() (eval.Val, error) {
	return core.Unit{}, nil
}
