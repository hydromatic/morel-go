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
	"github.com/hydromatic/morel-go/internal/types"
)

// The Sys structure. Its implementations live on the kernel,
// because they read and write session state; NewKernel injects
// them alongside the pure built-ins.

// The product name and version.
const (
	productName    = "morel-go"
	productVersion = "0.1.0"
)

// bannerText is the shell's banner.
func bannerText() string {
	return productName + " version " + productVersion +
		" (go version " + runtime.Version() + ", " +
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
// top-level aliases, for NewKernel to inject. Sys.plan stays a
// placeholder for now.
func (k *Kernel) sysBuiltins() map[string]eval.Val {
	m := map[string]eval.Val{
		"Sys.env":     eval.Fn(k.sysEnv),
		"Sys.set":     eval.Fn(k.sysSet),
		"Sys.show":    eval.Fn(k.sysShow),
		"Sys.showAll": eval.Fn(k.sysShowAll),
		"Sys.unset":   eval.Fn(k.sysUnset),
		"Variant.print": eval.Fn(func(arg eval.Val) (eval.Val, error) {
			return compile.VariantPrint(arg, k.sys), nil
		}),
		"Variant.parse": eval.Fn(func(arg eval.Val) (eval.Val, error) {
			s, _ := arg.(string)
			return compile.VariantParse(s, k.sys)
		}),
		"Time.now": eval.Fn(k.timeNow),
	}
	m["env"] = m["Sys.env"]
	m["plan"] = notImplemented("Sys.plan")
	m["set"] = m["Sys.set"]
	m["show"] = m["Sys.show"]
	m["showAll"] = m["Sys.showAll"]
	m["unset"] = m["Sys.unset"]
	return m
}

// timeNow is "Time.now ()": the current time as nanoseconds. It
// reads the "now" property, an ISO-8601 instant, so tests are
// deterministic; absent or unparsable, it uses the wall clock.
func (k *Kernel) timeNow(eval.Val) (eval.Val, error) {
	t, err := time.Parse(time.RFC3339, k.config.props["now"])
	if err != nil {
		//nolint:nilerr // fall back to the wall clock
		return time.Now().UnixNano(), nil
	}
	return t.UnixNano(), nil
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
		out[i] = []eval.Val{name, envTypeString(k.sys,
			byName[name])}
	}
	return out, nil
}

// envTypeString renders a binding's type as Sys.env shows it:
// "forall 'a 'b. " precedes a polymorphic type.
func envTypeString(sys *types.System, t types.Type) string {
	n := 0
	countTypeVars(t, &n)
	if n == 0 {
		return t.String()
	}
	var b strings.Builder
	b.WriteString("forall")
	for i := range n {
		b.WriteString(" " + sys.Var(i).String())
	}
	b.WriteString(". " + t.String())
	return b.String()
}

// countTypeVars sets n to one more than the highest type-variable
// ordinal in t, so that ordinals 0..n-1 quantify it.
func countTypeVars(t types.Type, n *int) {
	// lint: sort until '^	}' where '^	case '
	switch t := t.(type) {
	case *types.Fn:
		countTypeVars(t.Param, n)
		countTypeVars(t.Result, n)
	case *types.List:
		countTypeVars(t.Elem, n)
	case *types.Named:
		for _, arg := range t.Args {
			countTypeVars(arg, n)
		}
	case *types.Record:
		for _, f := range t.Fields {
			countTypeVars(f.Type, n)
		}
	case *types.Tuple:
		for _, arg := range t.Args {
			countTypeVars(arg, n)
		}
	case *types.Var:
		if t.Ordinal >= *n {
			*n = t.Ordinal + 1
		}
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
