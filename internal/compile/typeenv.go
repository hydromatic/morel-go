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
}

// emptyTypeEnv is the environment with no names in scope.
type emptyTypeEnv struct{}

func (emptyTypeEnv) get(*typeResolver, string) (unify.Term,
	bool,
) {
	return nil, false
}

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

func bind(env typeEnv, name string, term unify.Term) typeEnv {
	return &bindTypeEnv{parent: env, name: name, term: term}
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
