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

package types_test

import (
	"testing"

	"github.com/hydromatic/morel-go/internal/types"
)

const abRecord = "{a:int, b:string}"

func TestInterning(t *testing.T) {
	s := types.NewSystem()
	// Interned types compare by pointer.
	if s.List(s.Int) != s.List(s.Int) {
		t.Error("list types not interned")
	}
	if s.Fn(s.Int, s.Bool) != s.Fn(s.Int, s.Bool) {
		t.Error("fn types not interned")
	}
	if s.Tuple(s.Int, s.Bool) != s.Tuple(s.Int, s.Bool) {
		t.Error("tuple types not interned")
	}
	if s.List(s.Int) == s.List(s.Bool) {
		t.Error("distinct types interned together")
	}
	if s.Lookup("int") != s.Int {
		t.Error("Lookup(int) is not Int")
	}
}

func TestDescriptions(t *testing.T) {
	s := types.NewSystem()
	for _, tc := range []struct {
		typ  types.Type
		want string
	}{
		{s.Int, "int"},
		{s.List(s.Int), "int list"},
		{s.List(s.List(s.Int)), "int list list"},
		{s.List(s.Tuple(s.Int, s.Int)), "(int * int) list"},
		{
			s.Tuple(s.Int, s.Bool, s.String),
			"int * bool * string",
		},
		{
			s.Tuple(s.Tuple(s.Int, s.Int), s.Bool),
			"(int * int) * bool",
		},
		{
			s.Fn(s.Int, s.Fn(s.Int, s.Bool)),
			"int -> int -> bool",
		},
		{
			s.Fn(s.Fn(s.Int, s.Int), s.Bool),
			"(int -> int) -> bool",
		},
		{
			s.Tuple(s.Fn(s.Int, s.Int), s.Bool),
			"(int -> int) * bool",
		},
		{s.Var(0), "'a"},
		{s.Var(1), "'b"},
		{s.Var(26), "'ba"},
		{s.Var(51), "'bz"},
		{s.Var(52), "'ca"},
		{s.Record([]types.Field{
			{Label: "b", Type: s.String},
			{Label: "a", Type: s.Int},
		}), abRecord},
		{s.Record([]types.Field{
			{Label: "10", Type: s.Int},
			{Label: "2", Type: s.Int},
			{Label: "a", Type: s.Int},
		}), "{2:int, 10:int, a:int}"},
	} {
		if got := tc.typ.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestTyConOrdinals(t *testing.T) {
	s := types.NewSystem()
	s.DeclareDatatype("order", 0)
	order := s.Named("order")
	s.DeclareTyCon("LESS", nil, order)
	s.DeclareTyCon("EQUAL", nil, order)
	s.DeclareTyCon("GREATER", nil, order)
	s.DeclareDatatype("option", 1)
	option := s.Named("option", s.Var(0))
	s.DeclareTyCon("NONE", nil, option)
	s.DeclareTyCon("SOME", s.Var(0), option)
	for _, tc := range []struct {
		name    string
		ordinal int
	}{
		{"LESS", 0},
		{"EQUAL", 1},
		{"GREATER", 2},
		{"NONE", 0},
		{"SOME", 1},
	} {
		con, ok := s.LookupTyCon(tc.name)
		if !ok || con.Ordinal != tc.ordinal {
			t.Errorf("%s: got %d, want %d", tc.name,
				con.Ordinal, tc.ordinal)
		}
	}
}

func TestParse(t *testing.T) {
	s := types.NewSystem()
	for _, src := range []string{
		"int",
		"int list list",
		"(int * int) list",
		"int * bool -> string",
		abRecord,
		"'a -> 'b -> 'a",
	} {
		typ, err := s.Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		if got := typ.String(); got != src {
			t.Errorf("Parse(%q) = %q", src, got)
		}
	}
	// Variables are numbered by first occurrence; the same
	// variable is the same pointer.
	typ, err := s.Parse("'a -> 'a")
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := typ.(*types.Fn)
	if !ok || fn.Param != fn.Result {
		t.Errorf("'a -> 'a: param and result differ: %v", typ)
	}
	// Record fields sort into label order.
	typ, err = s.Parse("{b:string, a:int}")
	if err != nil {
		t.Fatal(err)
	}
	if got := typ.String(); got != abRecord {
		t.Errorf("got %q", got)
	}
	// Types parsed from equivalent text are the same pointer.
	t1, _ := s.Parse("int -> bool")
	if t1 != s.Fn(s.Int, s.Bool) {
		t.Error("parsed type not interned with constructed type")
	}
	_, err = s.Parse("wibble")
	if err == nil {
		t.Error("expected error for unknown type")
	}
}
