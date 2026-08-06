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

package compile

import "github.com/hydromatic/morel-go/internal/unify"

// typeEnv gives the term for each name in scope during type
// deduction.
type typeEnv interface {
	// get returns the term bound to a name, or false if the name
	// is not in scope.
	get(r *typeResolver, name string) (unify.Term, bool)
	// overloads returns the instance variables of an overloaded
	// name (an "over" name with its "val inst" instances), or nil
	// if the name is not overloaded.
	overloads(name string) []*unify.Var
}

// emptyTypeEnv is the environment with no names in scope.
type emptyTypeEnv struct{}

func (emptyTypeEnv) get(*typeResolver, string) (unify.Term,
	bool,
) {
	return nil, false
}

func (emptyTypeEnv) overloads(string) []*unify.Var { return nil }

// bindTypeEnv binds one name in front of a parent environment.
type bindTypeEnv struct {
	parent typeEnv
	name   string
	term   unify.Term
}

func (e *bindTypeEnv) get(r *typeResolver, name string) (
	unify.Term, bool,
) {
	if name == e.name {
		return e.term, true
	}
	return e.parent.get(r, name)
}

func (e *bindTypeEnv) overloads(name string) []*unify.Var {
	return e.parent.overloads(name)
}

// overTypeEnv marks a name as overloaded (an "over" declaration).
// It is the boundary at which collecting instances stops.
type overTypeEnv struct {
	parent typeEnv
	name   string
}

func (e *overTypeEnv) get(r *typeResolver, name string) (
	unify.Term, bool,
) {
	return e.parent.get(r, name)
}

func (e *overTypeEnv) overloads(name string) []*unify.Var {
	if name == e.name {
		return []*unify.Var{}
	}
	return e.parent.overloads(name)
}

// instTypeEnv adds one instance variable of an overloaded name (a
// "val inst" declaration).
type instTypeEnv struct {
	parent typeEnv
	name   string
	v      *unify.Var
}

func (e *instTypeEnv) get(r *typeResolver, name string) (
	unify.Term, bool,
) {
	return e.parent.get(r, name)
}

func (e *instTypeEnv) overloads(name string) []*unify.Var {
	if name == e.name {
		return append([]*unify.Var{e.v}, e.parent.overloads(name)...)
	}
	return e.parent.overloads(name)
}

func bind(env typeEnv, name string, term unify.Term) typeEnv {
	return &bindTypeEnv{parent: env, name: name, term: term}
}

// schemeTypeEnv binds a generalized value from a local "let". Each
// lookup copies the binding's (resolved) type term, substituting a
// fresh unification variable for each generalizable variable, so
// that the value can be used at several types in the "let" body
// (Hindley-Milner let-polymorphism). Variables not in genVars are
// shared with the enclosing environment and are copied unchanged.
type schemeTypeEnv struct {
	parent   typeEnv
	name     string
	resolved unify.Term
	genVars  []*unify.Var
	// predicates carries the overload constraints the binding was left
	// with (its value uses overloaded names at an abstract type). Each
	// use re-instantiates them, so a let-generalized qualified binding
	// behaves like a top-level one (hydromatic/morel#426). Empty for an
	// ordinary generalized binding.
	predicates []schemePredicate
}

// schemePredicate is one overload predicate of a generalized qualified
// binding, in resolved-term form so schemeTypeEnv.get can re-create it
// at each use, exactly as typeTerm re-creates a top-level *types.Qualified.
// arg and result share the binding's generalizable variables (refreshed
// per use by the scheme's fresh map); each candidate's (arg, result)
// pair is an independent variable scope, refreshed wholesale per use.
type schemePredicate struct {
	name       string
	arg        unify.Term
	result     unify.Term
	candidates []schemeCandidate
}

// schemeCandidate is one instance alternative of a schemePredicate: the
// (argument, result) type-term pair of a candidate overload instance.
type schemeCandidate struct {
	arg    unify.Term
	result unify.Term
}

func (e *schemeTypeEnv) get(r *typeResolver, name string) (
	unify.Term, bool,
) {
	if name == e.name {
		fresh := make(map[*unify.Var]unify.Term, len(e.genVars))
		for _, g := range e.genVars {
			fresh[g] = r.u.Variable()
		}
		// Re-instantiate the binding's overload predicates against this
		// use's fresh variables, so the predicate is resolved or
		// re-deferred against the now-concrete argument at this use site
		// (as a top-level qualified type is instantiated in typeTerm).
		for _, p := range e.predicates {
			r.instantiateSchemePredicate(p, fresh)
		}
		return freshTerm(e.resolved, fresh), true
	}
	return e.parent.get(r, name)
}

func (e *schemeTypeEnv) overloads(name string) []*unify.Var {
	return e.parent.overloads(name)
}

// freshTerm copies a term, replacing each variable that appears in
// m with its mapped term. It returns a structurally new term so
// that each use of a generalized binding gets its own variables.
func freshTerm(t unify.Term, m map[*unify.Var]unify.Term) unify.Term {
	switch t := t.(type) {
	case *unify.Var:
		if nt, ok := m[t]; ok {
			return nt
		}
		return t
	case *unify.Sequence:
		if len(t.Terms) == 0 {
			return t
		}
		terms := make([]unify.Term, len(t.Terms))
		for i, ct := range t.Terms {
			terms[i] = freshTerm(ct, m)
		}
		return unify.Apply(t.Op, terms...)
	default:
		return t
	}
}

// bindingTypeEnv resolves names from initial Bindings. Each
// lookup instantiates the binding's type with fresh unification
// variables, so a polymorphic value can be used at different
// types in one declaration.
type bindingTypeEnv struct {
	parent   typeEnv
	bindings map[string]*Binding
}

func (e *bindingTypeEnv) get(r *typeResolver, name string) (
	unify.Term, bool,
) {
	if b, ok := e.bindings[name]; ok {
		return r.typeTerm(b.Type, map[int]*unify.Var{}), true
	}
	return e.parent.get(r, name)
}

func (e *bindingTypeEnv) overloads(name string) []*unify.Var {
	return e.parent.overloads(name)
}
