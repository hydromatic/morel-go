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

// Package types defines Morel's types and the system that
// interns them. Types are immutable after interning, and interned
// types compare by pointer.
package types

import (
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
)

// Type is a Morel type. String returns its description (e.g.
// "int * bool -> string list"), which is also its interning key.
type Type interface {
	String() string
	Mark() string
	typ()
}

type typeBase struct {
	desc string
	mark string
}

func (t *typeBase) String() string { return t.desc }
func (t *typeBase) Mark() string   { return t.mark }
func (*typeBase) typ()             {}

// marks concatenates the marks of the given types. A type is
// interned by what it prints, and an alias prints as its name
// alone; but a name may be redeclared, and the two declarations
// abbreviate different types. The mark says which of them a type
// is built over, so "emp bag" before a redeclaration and "emp bag"
// after it are two types, as they are in morel-java, where types
// are interned by structural keys rather than by how they print.
func marks(ts ...Type) string {
	var b strings.Builder
	for _, t := range ts {
		b.WriteString(t.Mark())
	}
	return b.String()
}

// fieldMarks is marks over the types of a record's fields.
func fieldMarks(fields []Field) string {
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(f.Type.Mark())
	}
	return b.String()
}

// Primitive is a built-in atomic type: bool, char, int, real,
// string, or unit.
type Primitive struct {
	typeBase
}

// Var is a type variable; its description ("'a", "'b", ...)
// derives from its ordinal.
type Var struct {
	typeBase

	Ordinal int
}

// List is "elem list".
type List struct {
	typeBase

	Elem Type
}

// Collection is a list or a bag of elem, with orderedness left
// free. It is the parameter type of a built-in that works on both,
// such as "elem" or "Relational.only"; at a use site it unifies
// with whichever the argument is.
type Collection struct {
	typeBase

	Elem Type
}

// Fn is "param -> result".
type Fn struct {
	typeBase

	Param  Type
	Result Type
}

// Tuple is "t1 * t2 * ...".
type Tuple struct {
	typeBase

	Args []Type
}

// Field is one field of a record type.
type Field struct {
	Label string
	Type  Type
}

// Record is "{a:t1, b:t2, ...}", with fields in label order:
// numeric labels first, in numeric order, then names
// alphabetically.
type Record struct {
	typeBase

	Fields []Field

	// Progressive says that the field list is partial: more fields
	// may be discovered later, and the type prints with a trailing
	// ", ...". It distinguishes the file system's record types,
	// whose fields appear as directories are browsed, from ordinary
	// records. Fields are never lost, so a program that has been
	// typed stays valid.
	Progressive bool
}

// Predicate is one overload constraint of a qualified type: the
// overloaded name Name must have an instance whose type is Type (a
// function type over the enclosing type's variables). Candidates
// records the instance types that were in scope when the predicate
// was formed, so the constraint can be re-created (with fresh
// variables) each time the qualified type is instantiated.
type Predicate struct {
	Name       string
	Type       Type
	Candidates []Type
}

// Qualified is a type qualified by one or more overload constraints
// (predicates), following "A Second Look at Overloading" (Odersky,
// Wadler, Wehr 1995). For example
// "(second : 'a -> 'b, first : 'a -> 'c) => 'a -> 'b * 'c" is the
// type of a function that, for any 'a, 'b, 'c such that there are
// instances of the overloaded names "second" of type 'a -> 'b and
// "first" of type 'a -> 'c, maps 'a to 'b * 'c. An overloaded
// application whose argument type is not yet known records a
// predicate rather than resolving eagerly.
type Qualified struct {
	typeBase

	Predicates []Predicate
	Type       Type
}

// qualifiedDesc renders a qualified type: a single predicate in
// braces ("{foo : 'a -> 'b} => ..."), several in parentheses
// ("(first : ..., second : ...) => ...").
func qualifiedDesc(predicates []Predicate, base Type) string {
	var b strings.Builder
	single := len(predicates) == 1
	if single {
		b.WriteString("{")
	} else {
		b.WriteString("(")
	}
	for i, p := range predicates {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name + " : " + p.Type.String())
	}
	if single {
		b.WriteString("}")
	} else {
		b.WriteString(")")
	}
	b.WriteString(" => " + base.String())
	return b.String()
}

// AliasType is a use of a transparent type alias: the name it was
// written as, and the type it abbreviates. An alias reaches only
// the type that is displayed -- everything that reads a type
// structurally reads Base -- so that nothing has to know an alias
// exists.
//
// An alias that carries Checks is a checked type: its base type
// plus conditions that every value of it satisfies, e.g.
// "type nat = int check i => i >= 0". A checked type is erased --
// the conditions do not survive Unalias -- which is what makes
// widening free and narrowing checked. A checked type need not be
// named; one that is not is written in full, and two are the same
// type when their conditions are textually equal.
type AliasType struct {
	typeBase

	Args   []Type
	Name   string
	Base   Type
	Checks []*ast.Fn
	// Gen is which declaration of Name this type is; see
	// Alias.Gen.
	Gen int
}

// Unalias returns the type an alias abbreviates, and any other
// type unchanged. Every read of a type's structure goes through
// it.
func Unalias(t Type) Type {
	for {
		a, ok := t.(*AliasType)
		if !ok {
			return t
		}
		t = a.Base
	}
}

// Named is an instance of a datatype, e.g. "color" or
// "int option".
type Named struct {
	typeBase

	Args []Type
	Name string
}

// namedDesc returns the description of a datatype instance:
// "color", "int option", "(int,bool) pair".
func namedDesc(name string, args []Type) string {
	switch len(args) {
	case 0:
		return name
	case 1:
		return descArg(args[0]) + " " + name
	default:
		var b strings.Builder
		b.WriteString("(")
		for i, arg := range args {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(arg.String())
		}
		b.WriteString(") " + name)
		return b.String()
	}
}

// varOrdinal is the inverse of varName: the ordinal whose name is
// the given one, or -1 if no ordinal has that name. "'a" is 0,
// "'z" is 25, "'ba" is 26; a descriptive name such as "'left",
// which a signature may use, is none of them.
func varOrdinal(name string) int {
	const letters = 26
	bare := strings.TrimPrefix(name, "'")
	if len(bare) != 1 {
		// Only a single letter names an ordinal: 'a is 0 and 'z is
		// 25. A longer name is the writer's own -- "'left", say, in
		// a signature -- and is numbered as it is met. Decoding it
		// as base 26 would make "'right" ordinal 7,913,457, and
		// anything that sizes an array by an ordinal would then
		// allocate eight million types to renumber one member.
		return -1
	}
	ordinal := 0
	for _, c := range bare {
		if c < 'a' || c > 'z' {
			return -1
		}
		ordinal = ordinal*letters + int(c-'a')
	}
	// Only a name that renders back is one of ours: varName emits
	// no leading "a", so "aa" is not the name of any ordinal.
	if varName(ordinal) != "'"+bare {
		return -1
	}
	return ordinal
}

// varName returns the description of the type variable with the
// given ordinal: 'a, 'b, ..., 'z, 'ba, 'bb, ..., 'zz, 'baa, ...
// It is a base-26 number with 'a' as 0 and 'z' as 25.
func varName(ordinal int) string {
	const letters = 26
	var b []byte
	for {
		b = append(b, byte('a'+ordinal%letters))
		ordinal /= letters
		if ordinal == 0 {
			break
		}
	}
	// The digits were generated least-significant first.
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return "'" + string(b)
}

// descArg parenthesizes a type used as a list element or tuple
// component, where a function or tuple type would be ambiguous. A
// checked type with no name is written in full, and its condition
// runs to the end, so it needs parentheses in the same places.
func descArg(t Type) string {
	switch t.(type) {
	case *Fn, *Tuple:
		return "(" + t.String() + ")"
	default:
		if written(t) {
			return "(" + t.String() + ")"
		}
		return t.String()
	}
}

// descParam parenthesizes a function type used as a function
// parameter, and a checked type that is written in full.
func descParam(t Type) string {
	if _, ok := t.(*Fn); ok {
		return "(" + t.String() + ")"
	}
	if written(t) {
		return "(" + t.String() + ")"
	}
	return t.String()
}

// descResult parenthesizes a function's result where it is a
// checked type written in full: "->" is right-associative, so a
// function result needs no parentheses, but a condition there
// would otherwise read as being on the whole function type.
func descResult(t Type) string {
	if written(t) {
		return "(" + t.String() + ")"
	}
	return t.String()
}

// written reports whether a type is a checked type that has no
// name, and so is written as its body followed by its conditions.
func written(t Type) bool {
	return WrittenChecked(t)
}

// StripWrittenChecks returns a type with every checked type the
// annotation *writes out* replaced by what it abbreviates.
//
// It is what a pattern annotation displays. A name is worth
// keeping -- it is what the user called the type -- but a
// condition written in full says nothing a reader of the binding
// needs, and the value has been checked against it already.
//
// A condition inside a type the annotation merely *names* is
// untouched: "hr2" means what "type hr2 = ..." declared, however
// deep its conditions go, and it is the type's own business how it
// is written. So the annotation's syntax is walked beside the
// type, and only where it says "check" is anything dropped.
func StripWrittenChecks(s *System, written ast.Type, t Type) Type {
	if a, isAlias := t.(*AliasType); isAlias && a.Name != "" {
		// The annotation named this type, so what is inside it is
		// the name's own business, however deep.
		return t
	}
	if w, isChecked := written.(*ast.CheckedType); isChecked {
		return StripWrittenChecks(s, w.Type, stripAliases(t))
	}
	return t
}

// stripAliases removes the checked types that have no name from
// the head of a type; a named one stops it, because a name is what
// the type is written as.
func stripAliases(t Type) Type {
	for {
		a, ok := t.(*AliasType)
		if !ok || a.Name != "" || len(a.Checks) == 0 {
			return t
		}
		t = a.Base
	}
}

// WrittenChecked reports whether a type is a checked type with no
// name, which is written as its body followed by its conditions.
// A condition runs to the end of what is written, so such a type
// needs parentheses wherever a function type does.
func WrittenChecked(t Type) bool {
	a, ok := t.(*AliasType)
	return ok && a.Name == "" && len(a.Checks) > 0
}

// LabelLess is the ordering of record labels: numeric labels
// first, in numeric order, then names alphabetically.
func LabelLess(a, b string) bool {
	return ast.LabelLess(a, b)
}

// recordDesc describes a record type: "{a:int, b:string}", or
// "{a:int, ...}" if progressive, or "{...}" if progressive with no
// fields yet. The description is the interning key, so a
// progressive record is never confused with the plain record that
// happens to have the same fields.
func recordDesc(fields []Field, progressive bool) string {
	var b strings.Builder
	b.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Label + ":" + f.Type.String())
	}
	if progressive {
		if len(fields) > 0 {
			b.WriteString(", ")
		}
		b.WriteString("...")
	}
	b.WriteString("}")
	return b.String()
}
