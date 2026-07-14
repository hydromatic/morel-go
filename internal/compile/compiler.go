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
	"github.com/hydromatic/morel-go/internal/eval"
)

// Compiled is a statement ready to run: the pattern it binds,
// the code of its expression, and the number of variable slots
// its frame needs.
type Compiled struct {
	Pat   *core.IDPat
	Code  eval.Code
	Slots int
}

// Statement compiles a declaration "val name = exp".
// Values gives the runtime values of free names: built-ins and
// the results of earlier statements.
func Statement(decl core.Decl,
	values map[string]eval.Val,
) (*Compiled, error) {
	valDecl, ok := decl.(*core.NonRecValDecl)
	if !ok {
		return nil, &Error{
			Msg: "cannot compile " + decl.Op().String(),
		}
	}
	c := &compiler{
		values: values,
		slots:  map[*core.IDPat]int{},
	}
	code, err := c.compileExp(valDecl.Exp)
	if err != nil {
		return nil, err
	}
	return &Compiled{
		Pat:   valDecl.Pat,
		Code:  code,
		Slots: c.nSlots,
	}, nil
}

// compiler converts Core to Code, assigning each bound variable
// a frame slot.
type compiler struct {
	values map[string]eval.Val
	slots  map[*core.IDPat]int
	nSlots int
}

func (c *compiler) compileExp(exp core.Exp) (eval.Code, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *core.Apply:
		fn, err := c.compileExp(e.Fn)
		if err != nil {
			return nil, err
		}
		arg, err := c.compileExp(e.Arg)
		if err != nil {
			return nil, err
		}
		return eval.Apply(fn, arg), nil
	case *core.Case:
		return c.compileCase(e)
	case *core.ID:
		if slot, ok := c.slots[e.Pat]; ok {
			return eval.GetSlot(slot, e.Pat.Name), nil
		}
		if v, ok := c.values[e.Pat.Name]; ok {
			return eval.Constant(v), nil
		}
		return nil, &Error{Msg: "not found: " + e.Pat.Name}
	case *core.Let:
		return c.compileLet(e)
	case *core.Literal:
		return eval.Constant(e.Value), nil
	default:
		return nil, &Error{
			Msg: "cannot compile " + exp.Op().String(),
		}
	}
}

func (c *compiler) compileCase(caseExp *core.Case) (eval.Code,
	error,
) {
	scrutinee, err := c.compileExp(caseExp.Exp)
	if err != nil {
		return nil, err
	}
	clauses := make([]eval.MatchClause, len(caseExp.Matches))
	for i, m := range caseExp.Matches {
		pat, err := c.compilePat(m.Pat)
		if err != nil {
			return nil, err
		}
		body, err := c.compileExp(m.Exp)
		if err != nil {
			return nil, err
		}
		clauses[i] = eval.MatchClause{Pat: pat, Body: body}
	}
	return eval.Case(scrutinee, clauses), nil
}

// compilePat compiles a pattern, allocating a slot for each name
// it binds.
func (c *compiler) compilePat(pat core.Pat) (eval.Pat, error) {
	for _, id := range core.PatIDs(pat) {
		c.slots[id] = c.nSlots
		c.nSlots++
	}
	return c.patCode(pat)
}

func (c *compiler) patCode(pat core.Pat) (eval.Pat, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *core.IDPat:
		return eval.SlotPat{Slot: c.slots[p]}, nil
	case *core.LiteralPat:
		return eval.LiteralPat{V: p.Value}, nil
	case *core.WildcardPat:
		return eval.WildcardPat{}, nil
	default:
		return nil, &Error{
			Msg: "cannot compile pattern " + pat.Op().String(),
		}
	}
}

func (c *compiler) compileLet(let *core.Let) (eval.Code, error) {
	valDecl, ok := let.Decl.(*core.NonRecValDecl)
	if !ok {
		return nil, &Error{
			Msg: "cannot compile " + let.Decl.Op().String(),
		}
	}
	init, err := c.compileExp(valDecl.Exp)
	if err != nil {
		return nil, err
	}
	slot := c.nSlots
	c.nSlots++
	c.slots[valDecl.Pat] = slot
	body, err := c.compileExp(let.Exp)
	if err != nil {
		return nil, err
	}
	return eval.Let(slot, init, body), nil
}
