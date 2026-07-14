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
	"fmt"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/parse"
)

// Parse converts Morel type syntax (e.g. "'a list -> int") to a
// type. Type variables are numbered by first occurrence,
// left to right. This bootstraps built-in signatures from text.
func (s *System) Parse(src string) (Type, error) {
	t, err := parse.TypeString("type", src)
	if err != nil {
		return nil, err
	}
	return s.FromAST(t, map[string]int{})
}

// FromAST converts a type AST to a type. tyVars gives the
// ordinals of the type variables already in scope; a variable
// not in the map is numbered by first occurrence.
func (s *System) FromAST(t ast.Type, tyVars map[string]int) (
	Type, error,
) {
	c := &converter{sys: s, vars: tyVars}
	return c.convert(t)
}

type converter struct {
	sys  *System
	vars map[string]int
}

func (c *converter) convert(t ast.Type) (Type, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *ast.FnType:
		return c.convert2(n.Param, n.Result, c.sys.Fn)
	case *ast.NamedType:
		return c.convertNamed(n)
	case *ast.RecordType:
		return c.convertRecord(n)
	case *ast.TupleType:
		args, err := c.convertList(n.Args)
		if err != nil {
			return nil, err
		}
		return c.sys.Tuple(args...), nil
	case *ast.TyVar:
		ord, ok := c.vars[n.Name]
		if !ok {
			ord = len(c.vars)
			c.vars[n.Name] = ord
		}
		return c.sys.Var(ord), nil
	default:
		return nil, fmt.Errorf("cannot convert type %s",
			t.Op())
	}
}

// convert2 converts two types and combines them.
func (c *converter) convert2(a, b ast.Type,
	combine func(Type, Type) Type,
) (Type, error) {
	ta, err := c.convert(a)
	if err != nil {
		return nil, err
	}
	tb, err := c.convert(b)
	if err != nil {
		return nil, err
	}
	return combine(ta, tb), nil
}

func (c *converter) convertList(args []ast.Type) ([]Type, error) {
	out := make([]Type, len(args))
	for i, a := range args {
		t, err := c.convert(a)
		if err != nil {
			return nil, err
		}
		out[i] = t
	}
	return out, nil
}

func (c *converter) convertNamed(n *ast.NamedType) (Type, error) {
	if n.Name == "list" && len(n.Args) == 1 {
		elem, err := c.convert(n.Args[0])
		if err != nil {
			return nil, err
		}
		return c.sys.List(elem), nil
	}
	if arity, ok := c.sys.DatatypeArity(n.Name); ok &&
		arity == len(n.Args) {
		args := make([]Type, len(n.Args))
		for i, arg := range n.Args {
			t, err := c.convert(arg)
			if err != nil {
				return nil, err
			}
			args[i] = t
		}
		return c.sys.Named(n.Name, args...), nil
	}
	if len(n.Args) == 0 {
		if t := c.sys.Lookup(n.Name); t != nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("unknown type %q", n.Name)
}

func (c *converter) convertRecord(n *ast.RecordType) (Type,
	error,
) {
	fields := make([]Field, len(n.Fields))
	for i, f := range n.Fields {
		t, err := c.convert(f.Type)
		if err != nil {
			return nil, err
		}
		fields[i] = Field{Label: f.Label, Type: t}
	}
	return c.sys.Record(fields), nil
}
