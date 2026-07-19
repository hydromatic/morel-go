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

// Package unify implements the Martelli-Montanari unification
// algorithm, with action hooks that fire as variables resolve
// and constraints that model overloaded operators.
package unify

import "strings"

// Term is a variable or a sequence.
type Term interface {
	String() string

	// apply substitutes variables throughout this term. It
	// returns the receiver if nothing changed, and callers rely
	// on that to detect change cheaply.
	apply(m map[*Var]Term) Term

	// apply1 substitutes a single variable; like apply, it
	// returns the receiver if nothing changed.
	apply1(v *Var, t Term) Term

	// contains returns whether this term references a variable.
	contains(v *Var) bool

	// couldUnifyWith returns whether this term could possibly
	// unify with another term.
	couldUnifyWith(other Term) bool

	// eq returns whether this term equals another. Variables
	// compare by pointer, sequences structurally.
	eq(other Term) bool
}

// Var is a variable; unification's task is to find a substitution
// for each variable. Variables are interned by a Unifier and
// compare by pointer.
type Var struct {
	Name string

	// Ordinal is n for a generated variable named "Tn", and -1
	// for a variable created with an explicit name.
	Ordinal int
}

// String returns the variable's name.
func (v *Var) String() string { return v.Name }

func (v *Var) apply(m map[*Var]Term) Term {
	if t, ok := m[v]; ok {
		return t
	}
	return v
}

func (v *Var) apply1(v2 *Var, t Term) Term {
	if v == v2 {
		return t
	}
	return v
}

func (v *Var) contains(v2 *Var) bool { return v == v2 }

func (v *Var) couldUnifyWith(Term) bool { return true }

func (v *Var) eq(other Term) bool {
	o, ok := other.(*Var)
	return ok && o == v
}

// varLess is the ordering of variables in printed substitutions:
// by ordinal, then by name.
func varLess(a, b *Var) bool {
	if a.Ordinal != b.Ordinal {
		return a.Ordinal < b.Ordinal
	}
	return a.Name < b.Name
}

// Sequence is an operator applied to terms; a sequence with no
// terms is an atom. A sequence [a b c] prints as "a(b, c)".
type Sequence struct {
	Op    string
	Terms []Term
}

// Apply returns the sequence "op(terms...)".
func Apply(op string, terms ...Term) *Sequence {
	return &Sequence{Op: op, Terms: terms}
}

// String returns the sequence in "op(t1, t2)" form, or just "op"
// for an atom.
func (s *Sequence) String() string {
	if len(s.Terms) == 0 {
		return s.Op
	}
	var b strings.Builder
	b.WriteString(s.Op)
	b.WriteString("(")
	for i, t := range s.Terms {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t.String())
	}
	b.WriteString(")")
	return b.String()
}

func (s *Sequence) apply(m map[*Var]Term) Term {
	if len(s.Terms) == 0 {
		return s
	}
	changed := false
	terms := make([]Term, len(s.Terms))
	for i, t := range s.Terms {
		terms[i] = t.apply(m)
		if terms[i] != t {
			changed = true
		}
	}
	if !changed {
		return s
	}
	return &Sequence{Op: s.Op, Terms: terms}
}

func (s *Sequence) apply1(v *Var, t Term) Term {
	if !s.contains(v) {
		return s
	}
	terms := make([]Term, len(s.Terms))
	for i, t1 := range s.Terms {
		terms[i] = t1.apply1(v, t)
	}
	return &Sequence{Op: s.Op, Terms: terms}
}

func (s *Sequence) contains(v *Var) bool {
	for _, t := range s.Terms {
		if t.contains(v) {
			return true
		}
	}
	return false
}

func (s *Sequence) couldUnifyWith(other Term) bool {
	o, ok := other.(*Sequence)
	if !ok {
		return true
	}
	if o.Op != s.Op || len(o.Terms) != len(s.Terms) {
		return false
	}
	for i, t := range s.Terms {
		if !t.couldUnifyWith(o.Terms[i]) {
			return false
		}
	}
	return true
}

func (s *Sequence) eq(other Term) bool {
	o, ok := other.(*Sequence)
	if !ok || o.Op != s.Op || len(o.Terms) != len(s.Terms) {
		return false
	}
	for i, t := range s.Terms {
		if !t.eq(o.Terms[i]) {
			return false
		}
	}
	return true
}

// Internal operators for the collection representation, known to
// the unifier only so that a list/bag clash renders in user terms.
const (
	collectionOp = "$collection"
	orderedOp    = "ordered"
	unorderedOp  = "unordered"
)

// orderednessConflict reports whether two terms are collections
// that differ only in orderedness — one a list, the other a bag.
// Such a pair shares the collection operator, so decomposition
// would report a confusing clash on the internal orderedness atom;
// the caller reports the collections themselves instead.
func orderednessConflict(left, right *Sequence) bool {
	o1 := orderednessOf(left)
	o2 := orderednessOf(right)
	return o1 != "" && o2 != "" && o1 != o2
}

// orderednessOf returns the concrete orderedness ("ordered" or
// "unordered") of a collection term, or "" if it is not a
// collection or its orderedness is not yet determined.
func orderednessOf(s *Sequence) string {
	if s.Op != collectionOp || len(s.Terms) != 2 {
		return ""
	}
	o, ok := s.Terms[1].(*Sequence)
	if !ok || len(o.Terms) != 0 {
		return ""
	}
	if o.Op == orderedOp || o.Op == unorderedOp {
		return o.Op
	}
	return ""
}

// renderCollection renders a term for an error message, decoding
// the internal collection representation to user-facing "list" and
// "bag": a collection term becomes "list(elem)" or "bag(elem)", a
// bare orderedness atom becomes "list" or "bag", and everything
// else keeps its ordinary term form.
func renderCollection(t Term) string {
	s, ok := t.(*Sequence)
	if !ok {
		return t.String()
	}
	if len(s.Terms) == 0 {
		switch s.Op {
		case orderedOp:
			return "list"
		case unorderedOp:
			return "bag"
		default:
			return s.Op
		}
	}
	if s.Op == collectionOp && len(s.Terms) == 2 {
		kind := "bag"
		if orderednessOf(s) == orderedOp {
			kind = "list"
		}
		return kind + "(" + renderCollection(s.Terms[0]) + ")"
	}
	var b strings.Builder
	b.WriteString(s.Op)
	b.WriteString("(")
	for i, term := range s.Terms {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(renderCollection(term))
	}
	b.WriteString(")")
	return b.String()
}

// TermPair is a pair of terms to be unified.
type TermPair struct {
	Left  Term
	Right Term
}
