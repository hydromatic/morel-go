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

import (
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// cannotConvertValInst is the error when a "val inst" declaration
// cannot be lowered to core (no overload registry, or a form the
// milestone does not yet handle).
const cannotConvertValInst = "cannot convert to core: val inst"

// OverloadEnv carries the overloaded names ("over name") and their
// registered instances ("val inst name = e") across statements, so
// that a use of an overloaded name resolves against every instance
// declared so far. The type resolver reads it to know which names
// are overloaded and what their instance types are; the core
// resolver reads it to select and reference the winning instance's
// hidden binding.
type OverloadEnv struct {
	// parent is the enclosing overload scope, or nil at the session
	// level. A child (created for a "let" by NewChildOverloadEnv)
	// sees the parent's overloaded names and instances but records
	// its own "over"/"val inst" declarations locally, so they do not
	// leak to the parent when the let is done.
	parent *OverloadEnv
	// instances maps an overloaded name to its instances, in
	// declaration order. A name present with a nil slice is declared
	// "over" but has no instances yet. A name present in this scope
	// shadows the same name in the parent.
	instances map[string][]OverloadInstance
}

// OverloadInstance is one registered instance of an overloaded
// name: the generated hidden binding its implementation was lowered
// to (both its pattern, so a use inside the same "let" resolves to
// the binding's slot, and its name, so a use in a later statement
// resolves to the session value), and the instance's resolved
// (function) type.
type OverloadInstance struct {
	HiddenName string
	HiddenPat  *core.IDPat
	Type       types.Type
}

// NewOverloadEnv returns an empty session-level overload registry.
func NewOverloadEnv() *OverloadEnv {
	return &OverloadEnv{instances: map[string][]OverloadInstance{}}
}

// NewChildOverloadEnv returns a local overload registry nested in
// parent: it resolves overloaded names against the parent, but
// "over"/"val inst" declarations recorded in it (inside a "let")
// stay local and are discarded with it — they never leak to the
// parent (session) scope.
func NewChildOverloadEnv(parent *OverloadEnv) *OverloadEnv {
	return &OverloadEnv{
		parent:    parent,
		instances: map[string][]OverloadInstance{},
	}
}

// Declare records that a name is overloaded ("over name") in this
// scope. It is idempotent and never discards instances a name
// already has; in a child scope it shadows the parent's name with a
// fresh (instance-less) overload.
func (o *OverloadEnv) Declare(name string) {
	if _, ok := o.instances[name]; !ok {
		o.instances[name] = nil
	}
}

// IsOverloaded reports whether a name has been declared overloaded
// or has at least one instance, in this scope or an enclosing one.
func (o *OverloadEnv) IsOverloaded(name string) bool {
	if o == nil {
		return false
	}
	if _, ok := o.instances[name]; ok {
		return true
	}
	return o.parent.IsOverloaded(name)
}

// Instances returns the instances registered for an overloaded
// name, in declaration order. A name declared in this scope shadows
// the parent's; otherwise the parent's instances are returned.
func (o *OverloadEnv) Instances(name string) []OverloadInstance {
	if o == nil {
		return nil
	}
	if insts, ok := o.instances[name]; ok {
		return insts
	}
	return o.parent.Instances(name)
}

// Add registers a new instance of an overloaded name in this scope
// and returns the hidden binding its implementation lowers to. The
// binding's name is unique within the scope for that overloaded
// name (e.g. "first$0", "first$1"); a use inside the same "let"
// references the returned pattern, so it resolves to the binding's
// slot rather than a session value.
func (o *OverloadEnv) Add(name string, t types.Type) *core.IDPat {
	hidden := name + "$" + itoa(len(o.instances[name]))
	pat := &core.IDPat{T: t, Name: hidden}
	o.instances[name] = append(o.instances[name],
		OverloadInstance{HiddenName: hidden, HiddenPat: pat, Type: t})
	return pat
}

// itoa renders a small non-negative int without importing strconv
// at every call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// specializes reports whether the concrete type c matches the
// pattern type p, treating each type variable in p as a wildcard:
// c specializes p if some substitution of p's type variables yields
// c. It is how an overloaded use selects the instance whose
// parameter type accepts the argument type (mirroring morel-java's
// Type.specializes / FnType.canCallArgOf).
func specializes(c, p types.Type) bool {
	if _, ok := p.(*types.Var); ok {
		return true
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch c := c.(type) {
	case *types.Collection:
		p2, ok := p.(*types.Collection)
		return ok && specializes(c.Elem, p2.Elem)
	case *types.Fn:
		p2, ok := p.(*types.Fn)
		return ok && specializes(c.Param, p2.Param) &&
			specializes(c.Result, p2.Result)
	case *types.List:
		p2, ok := p.(*types.List)
		return ok && specializes(c.Elem, p2.Elem)
	case *types.Named:
		p2, ok := p.(*types.Named)
		return ok && c.Name == p2.Name && specializesAll(c.Args, p2.Args)
	case *types.Primitive:
		p2, ok := p.(*types.Primitive)
		return ok && c.String() == p2.String()
	case *types.Record:
		p2, ok := p.(*types.Record)
		return ok && specializesFields(c.Fields, p2.Fields)
	case *types.Tuple:
		p2, ok := p.(*types.Tuple)
		return ok && specializesAll(c.Args, p2.Args)
	}
	return false
}

// specializesAll reports whether each concrete type specializes the
// pattern type at the same position (and the lists are the same
// length).
func specializesAll(cs, ps []types.Type) bool {
	if len(cs) != len(ps) {
		return false
	}
	for i := range cs {
		if !specializes(cs[i], ps[i]) {
			return false
		}
	}
	return true
}

// specializesFields is specializesAll for record fields, also
// requiring the labels to match position by position.
func specializesFields(cs, ps []types.Field) bool {
	if len(cs) != len(ps) {
		return false
	}
	for i := range cs {
		if cs[i].Label != ps[i].Label ||
			!specializes(cs[i].Type, ps[i].Type) {
			return false
		}
	}
	return true
}
