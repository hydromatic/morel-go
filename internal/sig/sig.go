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

// Package sig reads the subset of the built-in library's
// signature files (lib/*.sig) that gives the types of built-in
// values. Implementations are not needed to deduce types.
package sig

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/hydromatic/morel-go/internal/compile"
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/types"
)

// Result is what loading the signature files produced: the
// bindings, and the value specifications that could not be
// loaded yet (e.g. their types mention a structure's type, such
// as "StringCvt.radix").
type Result struct {
	Bindings []compile.Binding
	Methods  []compile.MethodInfo
	Skipped  []string
}

// Load reads every signature file and registers its datatypes in
// the type system. Each structure binds as a record whose fields
// are its values, so "String.size" is a field selection; the
// General structure's values and all datatype constructors also
// bind unqualified.
func Load(sys *types.System, fsys fs.FS) (*Result, error) {
	names, err := fs.Glob(fsys, "*.sig")
	if err != nil {
		return nil, fmt.Errorf("sig: %w", err)
	}
	sort.Strings(names)
	files := make([]*file, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("sig: %w", err)
		}
		f, err := parseFile(name, string(data))
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	result := &Result{}
	// Register every file's types before converting any value's
	// type, because values refer to types across files (e.g.
	// "Real.fromInt" mentions "real" and "int").
	for _, f := range files {
		f.declareTypes(sys)
	}
	for _, f := range files {
		err := f.declareCons(sys, result)
		if err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		f.bindVals(sys, result)
	}
	return result, nil
}

// file is one parsed signature file.
type file struct {
	name      string
	structure string
	typs      []typeSpec
	vals      []valSpec
}

// typeSpec is a "datatype", "type", or "eqtype" specification.
type typeSpec struct {
	name   string
	tyVars []string
	cons   []conSpec
}

// conSpec is one constructor of a datatype specification.
type conSpec struct {
	name string
	of   string
}

// valSpec is a "val name : type" specification.
type valSpec struct {
	name   string
	typ    string
	method bool
}

// structureName converts a file name such as "string-cvt.sig" to
// a structure name such as "StringCvt".
func structureName(fileName string) string {
	stem := strings.TrimSuffix(fileName, ".sig")
	var b strings.Builder
	for segment := range strings.SplitSeq(stem, "-") {
		// lint: sort until '^		}' where '^		case '
		switch segment {
		case "":
		case "ieee":
			b.WriteString("IEEE")
		case "pp":
			b.WriteString("PP")
		default:
			b.WriteString(strings.ToUpper(segment[:1]))
			b.WriteString(segment[1:])
		}
	}
	return b.String()
}

// declareTypes registers the file's datatypes and abstract types
// so that value types anywhere in the library can refer to them.
// A type that already exists (e.g. "bool", "unit") is not
// redeclared.
func (f *file) declareTypes(sys *types.System) {
	for _, t := range f.typs {
		if t.name == "list" || sys.Lookup(t.name) != nil {
			continue
		}
		if _, ok := sys.DatatypeArity(t.name); ok {
			continue
		}
		sys.DeclareDatatype(t.name, len(t.tyVars))
	}
}

// declareCons registers the constructors of the file's datatypes
// and binds each as a value. The first registration of a
// constructor name wins (e.g. LESS belongs to "order", not to
// IEEEReal's "real_order").
func (f *file) declareCons(sys *types.System,
	result *Result,
) error {
	for _, t := range f.typs {
		if len(t.cons) == 0 {
			continue
		}
		result2, tyVars := conResultType(sys, t)
		for _, c := range t.cons {
			if _, exists := sys.LookupTyCon(c.name); exists {
				continue
			}
			var argType types.Type
			if c.of != "" {
				var err error
				argType, err = parseType(sys, c.of, tyVars)
				if err != nil {
					return fmt.Errorf("sig: %s: constructor %s: %w",
						f.name, c.name, err)
				}
			}
			sys.DeclareTyCon(c.name, argType, result2)
			conType := result2
			if argType != nil {
				conType = sys.Fn(argType, result2)
			}
			result.Bindings = append(result.Bindings,
				compile.Binding{Name: c.name, Type: conType})
		}
	}
	return nil
}

// conResultType returns the type that a datatype specification's
// constructors construct, and the ordinals of its type variables.
func conResultType(sys *types.System, t typeSpec) (types.Type,
	map[string]int,
) {
	tyVars := map[string]int{}
	args := make([]types.Type, len(t.tyVars))
	for i, tv := range t.tyVars {
		tyVars[tv] = i
		args[i] = sys.Var(i)
	}
	if t.name == "list" {
		return sys.List(args[0]), tyVars
	}
	if prim := sys.Lookup(t.name); prim != nil &&
		len(t.tyVars) == 0 {
		if _, isNamed := prim.(*types.Named); !isNamed {
			return prim, tyVars
		}
	}
	return sys.Named(t.name, args...), tyVars
}

// member is a structure value with its type and the number of
// distinct type variables that type mentions.
type member struct {
	field types.Field
	nvars int
}

// bindVals binds the file's structure as a record whose fields
// are its values, so that "String.size" is a field selection.
// The General
// structure's values also bind unqualified. A value whose type
// cannot be converted yet is skipped.
func (f *file) bindVals(sys *types.System, result *Result) {
	var members []member
	for _, v := range f.vals {
		tyVars := map[string]int{}
		t, err := parseType(sys, v.typ, tyVars)
		if err != nil {
			result.Skipped = append(result.Skipped,
				f.structure+"."+v.name)
			continue
		}
		members = append(members, member{
			field: types.Field{Label: v.name, Type: t},
			nvars: len(tyVars),
		})
		if v.method {
			result.Methods = append(result.Methods, compile.MethodInfo{
				Structure: f.structure,
				Name:      v.name,
				Type:      t,
			})
		}
		if f.structure == "General" {
			result.Bindings = append(result.Bindings,
				compile.Binding{Name: v.name, Type: t})
		}
	}
	if len(members) == 0 {
		return
	}
	result.Bindings = append(result.Bindings,
		compile.Binding{
			Name: f.structure,
			Type: sys.Record(renumber(sys, members)),
		})
}

// renumber alpha-renames the members' type variables into one
// left-to-right sequence over the whole record, so that a
// structure printed as a record shows contiguous variables ('a,
// 'b, ...). Each member is independently
// polymorphic, so a variable shared by two members prints under
// two different names. Members are numbered in label order — the
// order the record displays its fields.
func renumber(sys *types.System, members []member) []types.Field {
	sort.Slice(members, func(i, j int) bool {
		return types.LabelLess(members[i].field.Label,
			members[j].field.Label)
	})
	fields := make([]types.Field, len(members))
	base := 0
	for i, m := range members {
		fields[i] = m.field
		if base > 0 && m.nvars > 0 {
			args := make([]types.Type, m.nvars)
			for j := range args {
				args[j] = sys.Var(base + j)
			}
			fields[i].Type = sys.Substitute(m.field.Type, args)
		}
		base += m.nvars
	}
	return fields
}

func parseType(sys *types.System, src string,
	tyVars map[string]int,
) (types.Type, error) {
	t, err := parse.TypeString("sig", src)
	if err != nil {
		return nil, err
	}
	return sys.FromAST(t, tyVars)
}
