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
	sys.DeclareDatatype("option", 1)
	a := sys.Var(0)
	option := sys.Named("option", a)
	sys.DeclareTyCon("NONE", nil, option)
	sys.DeclareTyCon("SOME", a, option)
	bindings := []compile.Binding{
		{Name: "true", Type: sys.Bool},
		{Name: "false", Type: sys.Bool},
		{Name: "id", Type: idType},
		{Name: "NONE", Type: option},
		{Name: "SOME", Type: sys.Fn(a, option)},
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
		{"(1, true)", "int * bool"},
		{
			"(false, 1, (true, false))",
			"bool * int * (bool * bool)",
		},
		{"[1]", "int list"},
		{"[[1]]", "int list list"},
		{"[(1, true), (2, false)]", "(int * bool) list"},
		{"[]", "'a list"},
		{"(1, [2], 3)", "int * int list * int"},
		{"{a=1, b=\"two\"}", "{a:int, b:string}"},
		{"{c=1, b=true}", "{b:bool, c:int}"},
		{"{1 = true, 2 = 0}", "bool * int"},
		{"{2=0,1=true}", "bool * int"},
		{"{3=0,1=true,11=false}", "{1:bool, 3:int, 11:bool}"},
		{"{}", "unit"},
		{"#1 (true, 0)", "bool"},
		{"#2 (true, 0)", "int"},
		{"#1 {1=true,2=0}", "bool"},
		{"#b {a=1, b=\"two\"}", "string"},
		{
			"fn (f, g) => fn x => f (g x)",
			"('a -> 'b) * ('c -> 'a) -> 'c -> 'b",
		},
		{"fn {f, g} => true", "{f:'a, g:'b} -> bool"},
		{"fn (x, y) => x", "'a * 'b -> 'a"},
		{"(fn {f, ...} => f) {f=1, g=true}", "int"},
		{"{{a=1,b=true} with b=false}", "{a:int, b:bool}"},
		{"let val x = {a=1, b=true} in #b x end", "bool"},
		{"fun id x = x", "'a -> 'a"},
		{"fun first x y = x", "'a -> 'b -> 'a"},
		{
			"fun choose b x y = if b then x else y",
			"bool -> 'a -> 'a -> 'a",
		},
		{
			"fun choose b (x, y) = if b then x else y",
			"bool -> 'a * 'a -> 'a",
		},
		{"fun f 0 = true | f _ = false", "int -> bool"},
		{"fun f 0 y = y | f x _ = x", "int -> int -> int"},
		{"val rec f = fn x => f x", "'a -> 'b"},
		{"fn x => case x of 0 => 1 | _ => 2", "int -> int"},
		{
			"fn x => case x of 0 => \"zero\" | _ => \"nonzero\"",
			"int -> string",
		},
		{"let fun id x = x in id end", "'a -> 'a"},
		{
			"fn r => case r of {a=1, ...} => 1 | {b=2, ...} => 2",
			"'a -> int",
		},
		{"NONE", "'a option"},
		{"SOME 4", "int option"},
		{"SOME (SOME true)", "bool option option"},
		{"SOME (SOME [1, 2])", "int list option option"},
		{
			"fn x => case x of NONE => 0 | SOME y => y",
			"int option -> int",
		},
		{"fun f NONE = 0 | f (SOME x) = x", "int option -> int"},
		{"case SOME 4 of NONE => 0 | SOME y => y", "int"},
		{
			"let datatype color = RED | GREEN | BLUE in RED end",
			"color",
		},
		{"let datatype shape = CIRCLE of real | SQUARE of real" +
			" in CIRCLE 1.5 end", "shape"},
		{"let datatype tree = LEAF | NODE of tree * tree" +
			" in NODE (LEAF, NODE (LEAF, LEAF)) end", "tree"},
		{"let datatype 'a opt = NIL | JUST of 'a" +
			" in JUST 5 end", "int opt"},
		{
			"let datatype color = RED | GREEN" +
				" in fn c => case c of RED => 1 | GREEN => 2 end",
			"color -> int",
		},
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
			"Cannot deduce type: conflict: bool vs int",
		},
		{"fn x => x x", "Cannot deduce type: cycle"},
		{"#a", "unresolved flex record (can't tell what " +
			"fields there are besides #a)"},
		{"{a=1, b=true, a=3}", "duplicate field 'a' in record"},
		{
			// 'let' does not generalize: a let-bound
			// value has one type, so 'id' cannot be used at both
			// int and string.
			"let val id = fn x => x in (id 1, id \"a\") end",
			"Cannot deduce type: conflict",
		},
		{
			// Constructors declared in a 'let' are also
			// monomorphic within it.
			"let datatype 'a opt = NIL | JUST of 'a" +
				" in JUST (JUST 5) end",
			"Cannot deduce type: conflict",
		},
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
