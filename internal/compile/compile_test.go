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

package compile_test

import (
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/compile"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/types"
)

// deduce parses a statement (a declaration, or an expression
// that becomes "val it = exp"), and deduces its types in an
// environment with a few test bindings.
func deduce(t *testing.T, src string) (*compile.Resolved, error) {
	t.Helper()
	sys := types.NewSystem()
	node, err := parse.DeclOrExpr("test", src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	var decl ast.Decl
	switch n := node.(type) {
	case ast.Decl:
		decl = n
	case ast.Expr:
		decl = compile.ItValDecl(n)
	}
	idType, err := sys.Parse("'a -> 'a")
	if err != nil {
		t.Fatal(err)
	}
	bindings := []compile.Binding{
		{Name: "true", Type: sys.Bool},
		{Name: "false", Type: sys.Bool},
		{Name: "id", Type: idType},
	}
	return compile.Deduce(sys, bindings, decl)
}

// firstPat returns the pattern of the first binding of a val
// declaration, whose deduced type the tests inspect.
func firstPat(t *testing.T, resolved *compile.Resolved) ast.Pat {
	t.Helper()
	valDecl, ok := resolved.Decl.(*ast.ValDecl)
	if !ok {
		t.Fatalf("not a val decl: %v", resolved.Decl.Op())
	}
	return valDecl.Binds[0].Pat
}

func TestDeduce(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"1", "int"},
		{"2.5", "real"},
		{`"a"`, "string"},
		{`#"a"`, "char"},
		{"()", "unit"},
		{"true", "bool"},
		{"if true then 1 else 2", "int"},
		{"fn x => x", "'a -> 'a"},
		{"fn x => if x then 1 else 2", "bool -> int"},
		{"(fn x => x) 1", "int"},
		{"id 1", "int"},
		{"id (id true)", "bool"},
		{"let val x = 1 in x end", "int"},
		{
			"let val x = 1 val y = x in if true then x else y end",
			"int",
		},
		{"let val f = fn x => x in f 1 end", "int"},
		{"val x = if false then 2.5 else 3.5", "real"},
		{"val _ = 1", "int"},
	} {
		t.Run(tc.src, func(t *testing.T) {
			resolved, err := deduce(t, tc.src)
			if err != nil {
				t.Fatalf("deduce %q: %v", tc.src, err)
			}
			typ, err := resolved.TypeMap.TypeOf(
				firstPat(t, resolved))
			if err != nil {
				t.Fatal(err)
			}
			if got := typ.String(); got != tc.want {
				t.Errorf("%q: got %s, want %s", tc.src, got,
					tc.want)
			}
		})
	}
}

func TestDeduceError(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"y", "unbound variable or constructor: y"},
		{
			"if 1 then 2 else 3",
			"Cannot deduce type: conflict: int vs bool",
		},
		{
			"if true then 1 else 2.5",
			"Cannot deduce type: conflict: int vs real",
		},
		{
			"(fn x => if x then 1 else 2) 3",
			"Cannot deduce type: conflict: int vs bool",
		},
		{"fn x => x x", "Cannot deduce type: cycle"},
		{"3000000000", "literal '3000000000' is too large" +
			" for type int"},
	} {
		t.Run(tc.src, func(t *testing.T) {
			_, err := deduce(t, tc.src)
			if err == nil {
				t.Fatalf("%q: expected error", tc.src)
			}
			got := err.Error()
			if !strings.Contains(got, tc.want) {
				t.Errorf("%q: got %q, want %q", tc.src, got,
					tc.want)
			}
		})
	}
}

// as asserts that a value has a given type.
func as[T any](t *testing.T, v any) T {
	t.Helper()
	r, ok := v.(T)
	if !ok {
		t.Fatalf("got %T, want %T", v, r)
	}
	return r
}

// resolve type-checks a statement and converts it to Core.
func resolve(t *testing.T, src string) core.Decl {
	t.Helper()
	resolved, err := deduce(t, src)
	if err != nil {
		t.Fatalf("deduce %q: %v", src, err)
	}
	decl, err := compile.Resolve(resolved)
	if err != nil {
		t.Fatalf("resolve %q: %v", src, err)
	}
	return decl
}

func TestResolve(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		decl := resolve(t, "val x = 1")
		valDecl := as[*core.NonRecValDecl](t, decl)
		if valDecl.Pat.Name != "x" {
			t.Errorf("got %s", valDecl.Pat.Name)
		}
		literal := as[*core.Literal](t, valDecl.Exp)
		if literal.Value != int32(1) {
			t.Errorf("got %v", literal.Value)
		}
		if literal.Type().String() != "int" {
			t.Errorf("got %s", literal.Type())
		}
	})
	t.Run("ifBecomesCase", func(t *testing.T) {
		decl := resolve(t, "if true then 1 else 2")
		valDecl := as[*core.NonRecValDecl](t, decl)
		caseExp := as[*core.Case](t, valDecl.Exp)
		if len(caseExp.Matches) != 2 {
			t.Fatalf("got %d matches", len(caseExp.Matches))
		}
		truePat := as[*core.LiteralPat](t, caseExp.Matches[0].Pat)
		if truePat.Value != true {
			t.Errorf("got %v", truePat.Value)
		}
		if _, ok := caseExp.Matches[1].Pat.(*core.WildcardPat); !ok {
			t.Errorf("second match is not a wildcard")
		}
		if caseExp.Type().String() != "int" {
			t.Errorf("got %s", caseExp.Type())
		}
	})
	t.Run("fnSharesIDPat", func(t *testing.T) {
		decl := resolve(t, "fn x => x")
		valDecl := as[*core.NonRecValDecl](t, decl)
		fn := as[*core.Fn](t, valDecl.Exp)
		if fn.Type().String() != "'a -> 'a" {
			t.Errorf("got %s", fn.Type())
		}
		// The body's reference resolves to the parameter's
		// declaration.
		id := as[*core.ID](t, fn.Exp)
		if id.Pat != fn.IDPat {
			t.Error("body does not reference the parameter")
		}
	})
	t.Run("letFlattens", func(t *testing.T) {
		decl := resolve(t, "let val x = 1 val y = 2 in y end")
		valDecl := as[*core.NonRecValDecl](t, decl)
		let1 := as[*core.Let](t, valDecl.Exp)
		let2 := as[*core.Let](t, let1.Exp)
		yDecl := as[*core.NonRecValDecl](t, let2.Decl)
		if yDecl.Pat.Name != "y" {
			t.Errorf("got %s", yDecl.Pat.Name)
		}
		id := as[*core.ID](t, let2.Exp)
		if id.Pat != yDecl.Pat {
			t.Error("body does not reference y's declaration")
		}
	})
	t.Run("apply", func(t *testing.T) {
		decl := resolve(t, "(fn x => x) 1")
		valDecl := as[*core.NonRecValDecl](t, decl)
		apply := as[*core.Apply](t, valDecl.Exp)
		if apply.Type().String() != "int" {
			t.Errorf("got %s", apply.Type())
		}
		fn := as[*core.Fn](t, apply.Fn)
		if fn.Type().String() != "int -> int" {
			t.Errorf("got %s", fn.Type())
		}
	})
}
