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
	"strconv"
	"strings"
)

// Type is a Morel type. String returns its description (e.g.
// "int * bool -> string list"), which is also its interning key.
type Type interface {
	String() string
	typ()
}

type typeBase struct {
	desc string
}

func (t *typeBase) String() string { return t.desc }
func (*typeBase) typ()             {}

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
// component, where a function or tuple type would be ambiguous.
func descArg(t Type) string {
	switch t.(type) {
	case *Fn, *Tuple:
		return "(" + t.String() + ")"
	default:
		return t.String()
	}
}

// descParam parenthesizes a function type used as a function
// parameter.
func descParam(t Type) string {
	if _, ok := t.(*Fn); ok {
		return "(" + t.String() + ")"
	}
	return t.String()
}

// LabelLess is the ordering of record labels: numeric labels
// first, in numeric order, then names alphabetically.
func LabelLess(a, b string) bool {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return an < bn
	}
	if aerr == nil || berr == nil {
		return aerr == nil
	}
	return a < b
}

func recordDesc(fields []Field) string {
	var b strings.Builder
	b.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Label + ":" + f.Type.String())
	}
	b.WriteString("}")
	return b.String()
}
