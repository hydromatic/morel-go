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
	case listTyCon:
		elem, err := c.termType(s.Terms[0])
		if err != nil {
			return nil, err
		}
		return c.m.sys.List(elem), nil
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
	fields := make([]types.Field, len(s.Terms))
	for i, term := range s.Terms {
		t, err := c.termType(term)
		if err != nil {
			return nil, err
		}
		fields[i] = types.Field{Label: labels[i], Type: t}
	}
	return c.m.sys.Record(fields), nil
}
