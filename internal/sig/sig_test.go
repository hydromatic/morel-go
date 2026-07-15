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

package sig_test

import (
	"testing"

	"github.com/hydromatic/morel-go/internal/sig"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/lib"
)

// TestLoad checks that every signature file loads, that no value
// is skipped, and that a sample of the bindings have the
// expected types.
func TestLoad(t *testing.T) {
	sys := types.NewSystem()
	result, err := sig.Load(sys, lib.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skipped) > 0 {
		t.Errorf("skipped values: %v", result.Skipped)
	}
	const minBindings = 80
	if len(result.Bindings) < minBindings {
		t.Errorf("got %d bindings, want at least %d",
			len(result.Bindings), minBindings)
	}
	byName := map[string]types.Type{}
	for _, b := range result.Bindings {
		if _, dup := byName[b.Name]; dup {
			t.Errorf("duplicate binding %s", b.Name)
		}
		byName[b.Name] = b.Type
	}
	for _, tc := range []struct {
		name string
		want string
	}{
		{"true", "bool"},
		{"false", "bool"},
		{"NONE", "'a option"},
		{"SOME", "'a -> 'a option"},
		{"nil", "'a list"},
		{"LESS", "order"},
		{"o", "('a -> 'b) * ('c -> 'a) -> 'c -> 'b"},
		{"ignore", "'a -> unit"},
	} {
		typ, ok := byName[tc.name]
		if !ok {
			t.Errorf("no binding for %s", tc.name)
			continue
		}
		if got := typ.String(); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got,
				tc.want)
		}
	}
	// A structure binds as a record whose fields are its values.
	for _, tc := range []struct {
		structure string
		field     string
		want      string
	}{
		{"Bool", "not", "bool -> bool"},
		{"Int", "+", "int * int -> int"},
		{"Int", "div", "int * int -> int"},
		{"Real", "+", "real * real -> real"},
		{"String", "^", "string * string -> string"},
		{"String", "size", "string -> int"},
		{"List", "null", "'a list -> bool"},
		{"General", "ignore", "'a -> unit"},
		{"Math", "pi", "real"},
	} {
		record, ok := byName[tc.structure].(*types.Record)
		if !ok {
			t.Errorf("%s is not a record: %v", tc.structure,
				byName[tc.structure])
			continue
		}
		var got string
		for _, field := range record.Fields {
			if field.Label == tc.field {
				// A structure's members are numbered into one
				// shared sequence, so a member's variables carry an
				// offset; normalize it away, as the display does.
				got = normVars(sys, field.Type).String()
			}
		}
		if got != tc.want {
			t.Errorf("%s.%s: got %s, want %s", tc.structure,
				tc.field, got, tc.want)
		}
	}

	// The datatypes and constructors are registered.
	if arity, ok := sys.DatatypeArity("option"); !ok || arity != 1 {
		t.Error("option not registered")
	}
	if arity, ok := sys.DatatypeArity("bag"); !ok || arity != 1 {
		t.Error("bag not registered")
	}
	// LESS belongs to "order", not to IEEEReal's "real_order":
	// the first registration wins.
	tc, ok := sys.LookupTyCon("LESS")
	if !ok || tc.Result.String() != "order" {
		t.Errorf("LESS: got %v", tc.Result)
	}
}

// normVars renames a type's variables to 'a, 'b, ... in
// first-encounter (left-to-right) order, the form types display
// in, so a member type can be compared regardless of the offset
// at which the structure's shared numbering placed it.
func normVars(sys *types.System, t types.Type) types.Type {
	order := map[int]int{}
	var walk func(types.Type)
	walk = func(t types.Type) {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch t := t.(type) {
		case *types.Fn:
			walk(t.Param)
			walk(t.Result)
		case *types.List:
			walk(t.Elem)
		case *types.Named:
			for _, a := range t.Args {
				walk(a)
			}
		case *types.Record:
			for _, f := range t.Fields {
				walk(f.Type)
			}
		case *types.Tuple:
			for _, a := range t.Args {
				walk(a)
			}
		case *types.Var:
			if _, ok := order[t.Ordinal]; !ok {
				order[t.Ordinal] = len(order)
			}
		}
	}
	walk(t)
	maxOrd := -1
	for old := range order {
		if old > maxOrd {
			maxOrd = old
		}
	}
	args := make([]types.Type, maxOrd+1)
	for i := range args {
		args[i] = sys.Var(i)
	}
	for old, rank := range order {
		args[old] = sys.Var(rank)
	}
	return sys.Substitute(t, args)
}
