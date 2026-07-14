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

package types

import (
	"sort"
	"strings"
)

// System interns types: equal types are the same pointer.
type System struct {
	byKey map[string]Type

	Bool   Type
	Char   Type
	Int    Type
	Real   Type
	String Type
	Unit   Type
}

// NewSystem returns a system with the primitive types
// registered.
func NewSystem() *System {
	s := &System{byKey: map[string]Type{}}
	prim := func(name string) Type {
		t := &Primitive{typeBase{name}}
		s.byKey[name] = t
		return t
	}
	s.Bool = prim("bool")
	s.Char = prim("char")
	s.Int = prim("int")
	s.Real = prim("real")
	s.String = prim("string")
	s.Unit = prim("unit")
	return s
}

// Lookup returns the type with the given name (a primitive, or
// in time a datatype or alias), or nil.
func (s *System) Lookup(name string) Type {
	return s.byKey[name]
}

// Var returns the type variable with the given ordinal.
func (s *System) Var(ordinal int) Type {
	name := varName(ordinal)
	return s.intern(name, func() Type {
		return &Var{typeBase{name}, ordinal}
	})
}

// List returns the type "elem list".
func (s *System) List(elem Type) Type {
	key := descArg(elem) + " list"
	return s.intern(key, func() Type {
		return &List{typeBase{key}, elem}
	})
}

// Fn returns the type "param -> result".
func (s *System) Fn(param, result Type) Type {
	key := descParam(param) + " -> " + result.String()
	return s.intern(key, func() Type {
		return &Fn{typeBase{key}, param, result}
	})
}

// Tuple returns the type "t1 * t2 * ...".
func (s *System) Tuple(args ...Type) Type {
	descs := make([]string, len(args))
	for i, a := range args {
		descs[i] = descArg(a)
	}
	key := strings.Join(descs, " * ")
	return s.intern(key, func() Type {
		return &Tuple{typeBase{key}, args}
	})
}

// Record returns the record type with the given fields, sorted
// into label order.
func (s *System) Record(fields []Field) Type {
	sorted := make([]Field, len(fields))
	copy(sorted, fields)
	sort.Slice(sorted, func(i, j int) bool {
		return LabelLess(sorted[i].Label, sorted[j].Label)
	})
	key := recordDesc(sorted)
	return s.intern(key, func() Type {
		return &Record{typeBase{key}, sorted}
	})
}

func (s *System) intern(key string, mk func() Type) Type {
	if t, ok := s.byKey[key]; ok {
		return t
	}
	t := mk()
	s.byKey[key] = t
	return t
}
