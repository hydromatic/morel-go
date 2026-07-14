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

package unify_test

import (
	"testing"

	"github.com/hydromatic/morel-go/internal/unify"
)

// fixture provides fresh variables and term shorthands for one
// test.
type fixture struct {
	t          *testing.T
	u          *unify.Unifier
	x, y, w, z *unify.Var
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	u := unify.New()
	return &fixture{
		t: t,
		u: u,
		x: u.VariableNamed("X"),
		y: u.VariableNamed("Y"),
		w: u.VariableNamed("W"),
		z: u.VariableNamed("Z"),
	}
}

func (f *fixture) assertUnify(e1, e2 unify.Term, want string) {
	f.t.Helper()
	f.assertUnifyPairs([]unify.TermPair{{Left: e1, Right: e2}},
		want)
}

func (f *fixture) assertUnifyPairs(pairs []unify.TermPair,
	want string,
) {
	f.t.Helper()
	s, err := f.u.Unify(pairs, nil, nil)
	if err != nil {
		f.t.Fatalf("unify: %v", err)
	}
	if got := s.ResolveAll().String(); got != want {
		f.t.Errorf("got %s, want %s", got, want)
	}
}

func (f *fixture) assertCannotUnify(e1, e2 unify.Term) {
	f.t.Helper()
	f.assertCannotUnifyPairs([]unify.TermPair{
		{Left: e1, Right: e2},
	})
}

func (f *fixture) assertCannotUnifyPairs(pairs []unify.TermPair) {
	f.t.Helper()
	s, err := f.u.Unify(pairs, nil, nil)
	if err == nil {
		f.t.Errorf("expected failure, got %s", s)
	}
}

// pairs converts [a, b, c, d] to [(a, b), (c, d)].
func pairs(terms ...unify.Term) []unify.TermPair {
	result := make([]unify.TermPair, 0, len(terms)/2)
	for i := 1; i < len(terms); i += 2 {
		result = append(result, unify.TermPair{
			Left:  terms[i-1],
			Right: terms[i],
		})
	}
	return result
}

func TestNames(t *testing.T) {
	u := unify.New()
	a0 := u.AtomUnique("A")
	if a0.String() != "A0" {
		t.Errorf("got %s, want A0", a0)
	}
	if a1 := u.AtomUnique("A"); a1.String() != "A1" {
		t.Errorf("got %s, want A1", a1)
	}
	v0 := u.Variable()
	if v0.String() != "T0" {
		t.Errorf("got %s, want T0", v0)
	}
	// "T0" is taken by v0, so a unique atom skips to "T1".
	if a2 := u.AtomUnique("T"); a2.String() != "T1" {
		t.Errorf("got %s, want T1", a2)
	}
	if a3 := u.AtomUnique("T1"); a3.String() != "T10" {
		t.Errorf("got %s, want T10", a3)
	}
	v1 := u.Variable()
	if v1.String() != "T2" {
		t.Errorf("got %s, want T2", v1)
	}
	v2 := u.Variable()
	if v1b := u.VariableNamed("T2"); v1b != v1 {
		t.Error("VariableNamed(T2) is not v1")
	}
	if v1c := u.VariableOrdinal(2); v1c != v1 {
		t.Error("VariableOrdinal(2) is not v1")
	}
	if v2a := u.VariableOrdinal(3); v2a != v2 {
		t.Error("VariableOrdinal(3) is not v2")
	}
	var last *unify.Var
	for range 6 {
		last = u.Variable()
	}
	if last.String() != "T9" {
		t.Errorf("got %s, want T9", last)
	}
	// "T10" is taken by atom a3 above, so the next variable is
	// "T11".
	if v9 := u.Variable(); v9.String() != "T11" {
		t.Errorf("got %s, want T11", v9)
	}
}

func TestUnify(t *testing.T) {
	a := func(terms ...unify.Term) unify.Term {
		return unify.Apply("a", terms...)
	}
	b := func(terms ...unify.Term) unify.Term {
		return unify.Apply("b", terms...)
	}
	c := func(terms ...unify.Term) unify.Term {
		return unify.Apply("c", terms...)
	}
	d := func(terms ...unify.Term) unify.Term {
		return unify.Apply("d", terms...)
	}
	f := func(terms ...unify.Term) unify.Term {
		return unify.Apply("f", terms...)
	}
	g := func(terms ...unify.Term) unify.Term {
		return unify.Apply("g", terms...)
	}
	h := func(terms ...unify.Term) unify.Term {
		return unify.Apply("h", terms...)
	}
	p := func(terms ...unify.Term) unify.Term {
		return unify.Apply("p", terms...)
	}
	bill := func() unify.Term { return unify.Apply("bill") }
	bob := func() unify.Term { return unify.Apply("bob") }
	john := func() unify.Term { return unify.Apply("john") }
	tom := func() unify.Term { return unify.Apply("tom") }
	father := func(terms ...unify.Term) unify.Term {
		return unify.Apply("father", terms...)
	}
	mother := func(terms ...unify.Term) unify.Term {
		return unify.Apply("mother", terms...)
	}
	parents := func(terms ...unify.Term) unify.Term {
		return unify.Apply("parents", terms...)
	}
	parent := func(terms ...unify.Term) unify.Term {
		return unify.Apply("parent", terms...)
	}
	grandParent := func(terms ...unify.Term) unify.Term {
		return unify.Apply("grandParent", terms...)
	}
	connected := func(terms ...unify.Term) unify.Term {
		return unify.Apply("connected", terms...)
	}
	part := func(terms ...unify.Term) unify.Term {
		return unify.Apply("part", terms...)
	}

	t.Run("1", func(t *testing.T) {
		fx := newFixture(t)
		e1 := p(f(a()), g(b()), fx.y)
		if e1.String() != "p(f(a), g(b), Y)" {
			t.Errorf("got %s", e1)
		}
		fx.assertCannotUnify(e1, p(fx.z, g(d()), c()))
	})
	t.Run("2", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(p(f(a()), g(b()), fx.y),
			p(fx.z, g(fx.w), c()), "[b/W, c/Y, f(a)/Z]")
	})
	t.Run("3", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(p(f(f(b())), fx.x), p(f(fx.y), fx.x),
			"[f(b)/Y]")
	})
	t.Run("4", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(p(f(f(b())), c()), p(f(fx.y), fx.x),
			"[c/X, f(b)/Y]")
	})
	t.Run("5", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertCannotUnify(p(a(), fx.x), p(b(), fx.y))
	})
	t.Run("6", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(p(fx.x, a()), p(b(), fx.y),
			"[b/X, a/Y]")
	})
	t.Run("7", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(f(a(), fx.x), f(a(), b()), "[b/X]")
	})
	t.Run("8", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(f(fx.x), f(fx.y), "[Y/X]")
	})
	t.Run("9", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertCannotUnify(f(g(fx.x), fx.x), f(fx.y))
	})
	t.Run("10", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(f(g(fx.x)), f(fx.y), "[g(X)/Y]")
	})
	t.Run("11", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(f(g(fx.x), fx.x), f(fx.y, a()),
			"[a/X, g(a)/Y]")
	})
	t.Run("12", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(father(fx.x, fx.y), father(bob(), tom()),
			"[bob/X, tom/Y]")
	})
	t.Run("13", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(parents(fx.x, father(fx.x),
			mother(bill())),
			parents(bill(), father(bill()), fx.y),
			"[bill/X, mother(bill)/Y]")
	})
	t.Run("14", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(grandParent(fx.x, parent(parent(fx.x))),
			grandParent(john(), parent(fx.y)),
			"[john/X, parent(john)/Y]")
	})
	t.Run("15", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertCannotUnify(p(f(a(), g(fx.x))), p(fx.y, fx.y))
	})
	t.Run("16", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(p(a(), fx.x, h(g(fx.z))),
			p(fx.z, h(fx.y), h(fx.y)),
			"[h(g(a))/X, g(a)/Y, a/Z]")
	})
	t.Run("17", func(t *testing.T) {
		// The occurs check rejects X = f(X).
		fx := newFixture(t)
		fx.assertCannotUnify(p(fx.x, fx.x), p(fx.y, f(fx.y)))
	})
	t.Run("18", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertCannotUnify(part(fx.w, fx.x),
			connected(f(fx.w, fx.x), fx.w))
	})
	t.Run("19", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnify(p(f(fx.x), a(), fx.y),
			p(f(bill()), fx.z, g(b())),
			"[bill/X, g(b)/Y, a/Z]")
	})
	t.Run("atomEqAtom", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertCannotUnifyPairs(pairs(b(), fx.x, a(), fx.x))
	})
	t.Run("atomEqAtom2", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertCannotUnifyPairs(
			pairs(a(), fx.x, a(), fx.x, b(), fx.x))
	})
	t.Run("atomEqAtom3", func(t *testing.T) {
		fx := newFixture(t)
		fx.assertUnifyPairs(pairs(a(), fx.x, a(), fx.x),
			"[a/X]")
	})
}

// TestCombinator solves the equations arising from the S
// combinator, "fn x => fn y => fn z => x z (z y)", in Wand 87.
func TestCombinator(t *testing.T) {
	u := unify.New()
	v := make([]*unify.Var, 10)
	for i := range v {
		v[i] = u.VariableOrdinal(i)
	}
	arrow := func(t0, t1 unify.Term) unify.Term {
		return unify.Apply("->", t0, t1)
	}
	s, err := u.Unify(pairs(
		v[0], arrow(v[1], v[2]),
		v[2], arrow(v[3], v[4]),
		v[4], arrow(v[5], v[6]),
		v[1], arrow(v[8], arrow(v[7], v[6])),
		v[8], v[5],
		arrow(v[9], v[7]), v[3],
		v[9], v[5]), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "[->(T1, T2)/T0, ->(T8, ->(T7, T6))/T1," +
		" ->(T3, T4)/T2, ->(T9, T7)/T3, ->(T5, T6)/T4," +
		" T5/T8, T5/T9]"
	if got := s.String(); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestOverloadedTypes(t *testing.T) {
	u := unify.New()
	v := make([]*unify.Var, 16)
	for i := range v {
		v[i] = u.VariableOrdinal(i)
	}
	fn := func(t0, t1 unify.Term) unify.Term {
		return unify.Apply("fn", t0, t1)
	}
	intAtom := u.Atom("int")
	boolAtom := u.Atom("bool")
	fx := newFixture(t)
	fx.u = u
	fx.assertUnifyPairs(pairs(
		intAtom, v[5],
		v[5], v[4],
		fn(v[5], v[4]), v[3],
		fn(v[6], v[7]), v[3],
		fn(v[3], v[3]), v[2],
		boolAtom, v[11],
		v[11], v[10],
		fn(v[11], v[10]), v[9],
		fn(v[12], v[13]), v[9],
		fn(v[9], v[9]), v[8],
		fn(v[15], v[1]), v[14],
		boolAtom, v[15],
		fn(v[1], v[1]), v[0]),
		"[fn(T1, T1)/T0,"+
			" fn(fn(int, int), fn(int, int))/T2,"+
			" fn(int, int)/T3, int/T4, int/T5, int/T6, int/T7,"+
			" fn(fn(bool, bool), fn(bool, bool))/T8,"+
			" fn(bool, bool)/T9, bool/T10, bool/T11, bool/T12,"+
			" bool/T13, fn(bool, T1)/T14, bool/T15]")
}

func TestAction(t *testing.T) {
	u := unify.New()
	x := u.VariableNamed("X")
	y := u.VariableNamed("Y")
	a := u.Atom("a")
	b := u.Atom("b")
	fired := 0
	actions := []unify.VarAction{{
		Var: x,
		Action: func(_ *unify.Var, term unify.Term,
			_ *unify.Substitution,
			add func(left, right unify.Term),
		) {
			fired++
			if term.String() != "a" {
				t.Errorf("action got term %s, want a", term)
			}
			add(y, b)
		},
	}}
	s, err := u.Unify(pairs(x, a), actions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fired == 0 {
		t.Error("action did not fire")
	}
	if got := s.ResolveAll().String(); got != "[a/X, b/Y]" {
		t.Errorf("got %s", got)
	}
}

func TestConstraint(t *testing.T) {
	// An overloaded function has candidate types int -> istr and
	// bool -> bstr. Once the argument resolves to int, only one
	// candidate remains, so the result must be istr.
	overload := func(u *unify.Unifier) unify.Constraint {
		return unify.Overload(u.VariableNamed("A"),
			u.VariableNamed("R"),
			pairs(u.Atom("int"), u.Atom("istr"),
				u.Atom("bool"), u.Atom("bstr")))
	}
	t.Run("narrowToOne", func(t *testing.T) {
		u := unify.New()
		c := overload(u)
		s, err := u.Unify(
			pairs(u.VariableNamed("A"), u.Atom("int")),
			nil, []unify.Constraint{c})
		if err != nil {
			t.Fatal(err)
		}
		want := "[int/A, istr/R]"
		if got := s.ResolveAll().String(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	t.Run("noCandidates", func(t *testing.T) {
		u := unify.New()
		c := overload(u)
		_, err := u.Unify(
			pairs(u.VariableNamed("A"), u.Atom("string")),
			nil, []unify.Constraint{c})
		if err == nil || err.Error() != "no valid overloads" {
			t.Errorf("got %v, want no valid overloads", err)
		}
	})
	t.Run("nilActionAddsEquation", func(t *testing.T) {
		u := unify.New()
		a := u.VariableNamed("A")
		c := unify.Constraint{
			Arg: a,
			Candidates: []unify.Candidate{
				{Term: u.Atom("int")},
				{Term: unify.Apply("fn", u.Atom("bool"))},
			},
		}
		// A unifies with fn(B); the int candidate is eliminated,
		// and the remaining candidate's equation resolves B.
		b := u.VariableNamed("B")
		s, err := u.Unify(pairs(a, unify.Apply("fn", b)),
			nil, []unify.Constraint{c})
		if err != nil {
			t.Fatal(err)
		}
		want := "[fn(bool)/A, bool/B]"
		if got := s.ResolveAll().String(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
}
