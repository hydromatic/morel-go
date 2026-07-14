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

package unify

import (
	"sort"
	"strings"
)

// Substitution maps variables to terms; it is the result of a
// successful unification. The raw result may map a variable to a
// term that itself contains substituted variables; ResolveAll
// expands such terms fully.
type Substitution struct {
	Map map[*Var]Term
}

// Resolve expands a term by applying the substitution repeatedly
// until it no longer changes.
func (s *Substitution) Resolve(t Term) Term {
	current := t
	for {
		next := current.apply(s.Map)
		if next.eq(current) {
			return current
		}
		current = next
	}
}

// ResolveAll returns a substitution with every term fully
// expanded, or the receiver if the substitution has cycles.
func (s *Substitution) ResolveAll() *Substitution {
	if s.hasCycles() {
		return s
	}
	m := make(map[*Var]Term, len(s.Map))
	for v, t := range s.Map {
		m[v] = s.Resolve(t)
	}
	return &Substitution{Map: m}
}

// String returns the substitution as "[t1/V1, t2/V2]", with
// variables in order.
func (s *Substitution) String() string {
	vars := sortedVars(s.Map)
	var b strings.Builder
	b.WriteString("[")
	for i, v := range vars {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s.Map[v].String())
		b.WriteString("/")
		b.WriteString(v.Name)
	}
	b.WriteString("]")
	return b.String()
}

func (s *Substitution) hasCycles() bool {
	active := map[*Var]bool{}
	for _, t := range s.Map {
		if cyclic(t, s.Map, active) {
			return true
		}
	}
	return false
}

// cyclic returns whether expanding a term leads back to a
// variable that is being expanded.
func cyclic(t Term, m map[*Var]Term, active map[*Var]bool) bool {
	switch t := t.(type) {
	case *Var:
		u, ok := m[t]
		if !ok {
			return false
		}
		if active[t] {
			return true
		}
		active[t] = true
		c := cyclic(u, m, active)
		delete(active, t)
		return c
	case *Sequence:
		for _, child := range t.Terms {
			if cyclic(child, m, active) {
				return true
			}
		}
	}
	return false
}

func sortedVars(m map[*Var]Term) []*Var {
	vars := make([]*Var, 0, len(m))
	for v := range m {
		vars = append(vars, v)
	}
	sort.Slice(vars, func(i, j int) bool {
		return varLess(vars[i], vars[j])
	})
	return vars
}
