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
	"fmt"
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/internal/unify"
)

// TypeMap gives the deduced type of every node of a declaration.
type TypeMap struct {
	sys      *types.System
	nodeTerm map[ast.Node]unify.Term
	subst    *unify.Substitution
	// residuals are the named overload constraints left unresolved by
	// unification (their argument type never became concrete). They
	// become the predicates of a qualified type on the bound pattern.
	residuals []unify.Constraint
	// bindings gives the type of every name in the top-level
	// environment, by name. The core resolver reads it to recognize a
	// use of a qualified-typed binding declared in an earlier statement
	// (dictionary passing, hydromatic/morel#426). It may be nil.
	bindings map[string]*Binding
	// desugared maps a record with modifiers to the nested "let"s
	// that the type resolver replaced it with. The core resolver
	// converts those, never the record.
	desugared map[*ast.Record]ast.Expr
	// methodCalls maps a postfix method call — one whose receiver
	// only inference typed — to the structure call it desugars to.
	// The core resolver converts that, never the call.
	methodCalls map[*ast.Apply]ast.Expr
}

// TypeOf returns the type deduced for a node. Unification
// variables that remain unresolved become type variables ('a,
// 'b, ...), numbered by first occurrence within the node's type.
func (m *TypeMap) TypeOf(node ast.Node) (types.Type, error) {
	term, ok := m.nodeTerm[node]
	if !ok {
		return nil, fmt.Errorf("no type for node %s", node.Op())
	}
	c := &termToTypeConverter{
		m:    m,
		vars: map[*unify.Var]int{},
	}
	return c.termType(m.subst.Resolve(term))
}

// Qualify returns the type of a bound pattern qualified by the
// overload predicates deduced for this declaration, or nil if there
// are none that constrain the pattern's type variables. The
// predicates and the base type are numbered by one shared pass, so
// their type variables line up (e.g. a predicate "second : 'a ->
// 'b" over a base "'a -> 'b * 'c"). The candidate instance types of
// each predicate are numbered independently, since they belong to a
// separate variable scope that is refreshed at each use.
func (m *TypeMap) Qualify(node ast.Node) (types.Type, error) {
	if len(m.residuals) == 0 {
		return nil, nil //nolint:nilnil // no qualified type, no error
	}
	term, ok := m.nodeTerm[node]
	if !ok {
		return nil, fmt.Errorf("no type for node %s", node.Op())
	}
	c := &termToTypeConverter{m: m, vars: map[*unify.Var]int{}}
	base, err := c.termType(m.subst.Resolve(term))
	if err != nil {
		return nil, err
	}
	// The variables that occur in the base type; a predicate is
	// included only if it shares at least one variable with the base.
	baseVars := map[*unify.Var]bool{}
	collectVars(m.subst.Resolve(term), baseVars)
	var predicates []types.Predicate
	for _, con := range m.residuals {
		argTerm := m.subst.Resolve(con.Arg)
		resultTerm := m.subst.Resolve(con.Result)
		predVars := map[*unify.Var]bool{}
		collectVars(argTerm, predVars)
		collectVars(resultTerm, predVars)
		if disjointVars(predVars, baseVars) {
			continue
		}
		argType, err := c.termType(argTerm)
		if err != nil {
			return nil, err
		}
		resultType, err := c.termType(resultTerm)
		if err != nil {
			return nil, err
		}
		candidates := make([]types.Type, 0, len(con.ArgResults))
		for _, ar := range con.ArgResults {
			// A separate converter per candidate: its variables are a
			// scope of their own, refreshed at each instantiation.
			cc := &termToTypeConverter{m: m, vars: map[*unify.Var]int{}}
			cArg, err := cc.termType(m.subst.Resolve(ar.Left))
			if err != nil {
				return nil, err
			}
			cResult, err := cc.termType(m.subst.Resolve(ar.Right))
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, m.sys.Fn(cArg, cResult))
		}
		predicates = append(predicates, types.Predicate{
			Name:       con.Name,
			Type:       m.sys.Fn(argType, resultType),
			Candidates: candidates,
		})
	}
	if len(predicates) == 0 {
		return nil, nil //nolint:nilnil // no qualified type, no error
	}
	return m.sys.Qualified(predicates, base), nil
}

// collectVars adds every unification variable in a term to set.
func collectVars(t unify.Term, set map[*unify.Var]bool) {
	switch t := t.(type) {
	case *unify.Var:
		set[t] = true
	case *unify.Sequence:
		for _, child := range t.Terms {
			collectVars(child, set)
		}
	}
}

// disjointVars reports whether two variable sets share no variable.
func disjointVars(a, b map[*unify.Var]bool) bool {
	for v := range a {
		if b[v] {
			return false
		}
	}
	return true
}

// termToTypeConverter converts resolved unification terms to
// types; vars numbers the unresolved variables seen so far.
type termToTypeConverter struct {
	m    *TypeMap
	vars map[*unify.Var]int
}

func (c *termToTypeConverter) termType(t unify.Term) (types.Type,
	error,
) {
	switch t := t.(type) {
	case *unify.Var:
		ordinal, ok := c.vars[t]
		if !ok {
			ordinal = len(c.vars)
			c.vars[t] = ordinal
		}
		return c.m.sys.Var(ordinal), nil
	case *unify.Sequence:
		return c.sequenceType(t)
	}
	return nil, fmt.Errorf("cannot convert term %s", t)
}

func (c *termToTypeConverter) sequenceType(s *unify.Sequence) (
	types.Type, error,
) {
	if strings.HasPrefix(s.Op, recordTyCon+":") {
		return c.recordType(s)
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch s.Op {
	case collectionTyCon:
		elem, err := c.termType(s.Terms[0])
		if err != nil {
			return nil, err
		}
		// A collection is a list only when its orderedness is the
		// concrete "ordered" atom; a free orderedness variable, like
		// the "unordered" atom, reads back as a bag.
		if isOrderedAtom(s.Terms[1]) {
			return c.m.sys.List(elem), nil
		}
		return c.m.sys.Named(bagTyCon, elem), nil
	case fnTyCon:
		param, err := c.termType(s.Terms[0])
		if err != nil {
			return nil, err
		}
		result, err := c.termType(s.Terms[1])
		if err != nil {
			return nil, err
		}
		return c.m.sys.Fn(param, result), nil
	case tupleTyCon:
		args := make([]types.Type, len(s.Terms))
		for i, term := range s.Terms {
			arg, err := c.termType(term)
			if err != nil {
				return nil, err
			}
			args[i] = arg
		}
		return c.m.sys.Tuple(args...), nil
	default:
		if len(s.Terms) == 0 {
			if t := c.m.sys.Lookup(s.Op); t != nil {
				return t, nil
			}
		}
		if arity, ok := c.m.sys.DatatypeArity(s.Op); ok &&
			arity == len(s.Terms) {
			args := make([]types.Type, len(s.Terms))
			for i, term := range s.Terms {
				arg, err := c.termType(term)
				if err != nil {
					return nil, err
				}
				args[i] = arg
			}
			return c.m.sys.Named(s.Op, args...), nil
		}
		return nil, fmt.Errorf("cannot convert term %s", s)
	}
}

func (c *termToTypeConverter) recordType(s *unify.Sequence) (
	types.Type, error,
) {
	labels := splitQuoted(s.Op)[1:]
	if len(labels) != len(s.Terms) {
		return nil, fmt.Errorf("cannot convert term %s", s)
	}
	// The progressive mark is not a field; it says that the fields
	// are not all known yet. Strip it, and record what it said.
	progressive := false
	if n := len(labels) - 1; n >= 0 && labels[n] == progressiveLabel {
		progressive = true
		labels = labels[:n]
	}
	fields := make([]types.Field, len(labels))
	for i := range labels {
		t, err := c.termType(s.Terms[i])
		if err != nil {
			return nil, err
		}
		fields[i] = types.Field{Label: labels[i], Type: t}
	}
	if progressive {
		return c.m.sys.ProgressiveRecord(fields), nil
	}
	return c.m.sys.Record(fields), nil
}
