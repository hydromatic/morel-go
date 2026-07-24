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
	"slices"

	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/token"
)

// matchResult is the outcome of testing a pattern against a
// constant expression: it matches, it cannot match, or matching
// cannot be decided at compile time.
type matchResult int

const (
	matchNo matchResult = iota
	matchYes
	matchUndecided
)

// foldConstantCase reduces a case whose scrutinee is a constant to
// the first branch that matches it: the branch's body, wrapped in
// a let for each variable its pattern binds. It returns false when
// the scrutinee is not a constant, or when no branch decidably
// matches.
func foldConstantCase(scrut core.Exp, e *core.Case,
) (core.Exp, bool) {
	if !constExp(scrut) {
		return nil, false
	}
	for _, m := range e.Matches {
		var binds []*core.NonRecValDecl
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch patMatch(m.Pat, scrut, &binds, e.Span) {
		case matchNo:
			continue
		case matchUndecided:
			return nil, false
		case matchYes:
			result := m.Exp
			for _, bind := range slices.Backward(binds) {
				result = &core.Let{Decl: bind, Exp: result}
			}
			return result, true
		}
	}
	return nil, false
}

// constExp reports whether an expression is a compile-time
// constant: a literal, a constructor (possibly applied to a
// constant), or a tuple or list of constants.
func constExp(e core.Exp) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.Apply:
		con, ok := e.Fn.(*core.Con)
		return ok && con.HasArg && constExp(e.Arg)
	case *core.Con:
		return !e.HasArg
	case *core.List:
		return constExps(e.Args)
	case *core.Literal:
		return true
	case *core.Tuple:
		return constExps(e.Args)
	default:
		return false
	}
}

// constExps reports whether every expression is a constant.
func constExps(exps []core.Exp) bool {
	for _, e := range exps {
		if !constExp(e) {
			return false
		}
	}
	return true
}

// patMatch tests a pattern against a constant expression,
// accumulating a let declaration for each variable the pattern
// binds to the matched sub-expression.
func patMatch(p core.Pat, e core.Exp,
	binds *[]*core.NonRecValDecl, span token.Span,
) matchResult {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := p.(type) {
	case *core.AsPat:
		*binds = append(*binds, &core.NonRecValDecl{
			Pat: p.Pat, Exp: e, Span: span,
		})
		return patMatch(p.Body, e, binds, span)
	case *core.Con0Pat:
		return matchCon0(p, e)
	case *core.ConPat:
		return matchCon(p, e, binds, span)
	case *core.ConsPat:
		return matchCons(p, e, binds, span)
	case *core.IDPat:
		*binds = append(*binds, &core.NonRecValDecl{
			Pat: p, Exp: e, Span: span,
		})
		return matchYes
	case *core.ListPat:
		list, ok := e.(*core.List)
		if !ok {
			return matchUndecided
		}
		if len(p.Args) != len(list.Args) {
			return matchNo
		}
		return patMatchAll(p.Args, list.Args, binds, span)
	case *core.LiteralPat:
		lit, ok := e.(*core.Literal)
		if !ok {
			return matchUndecided
		}
		return matchName(lit.Value == p.Value)
	case *core.TuplePat:
		tuple, ok := e.(*core.Tuple)
		if !ok {
			return matchUndecided
		}
		if len(p.Args) != len(tuple.Args) {
			return matchUndecided
		}
		return patMatchAll(p.Args, tuple.Args, binds, span)
	case *core.WildcardPat:
		return matchYes
	default:
		return matchUndecided
	}
}

// matchCon0 tests an argless-constructor pattern against a
// constant expression.
func matchCon0(p *core.Con0Pat, e core.Exp) matchResult {
	if con, ok := e.(*core.Con); ok {
		return matchName(p.Name == con.Name)
	}
	if isConApply(e) {
		return matchNo
	}
	return matchUndecided
}

// matchCon tests a constructor-with-argument pattern against a
// constant expression.
func matchCon(p *core.ConPat, e core.Exp,
	binds *[]*core.NonRecValDecl, span token.Span,
) matchResult {
	if apply, ok := e.(*core.Apply); ok {
		if con, ok := apply.Fn.(*core.Con); ok {
			if p.Name != con.Name {
				return matchNo
			}
			return patMatch(p.Arg, apply.Arg, binds, span)
		}
	}
	if _, ok := e.(*core.Con); ok {
		return matchNo
	}
	return matchUndecided
}

// matchCons tests a cons pattern against a constant list.
func matchCons(p *core.ConsPat, e core.Exp,
	binds *[]*core.NonRecValDecl, span token.Span,
) matchResult {
	list, ok := e.(*core.List)
	if !ok {
		return matchUndecided
	}
	if len(list.Args) == 0 {
		return matchNo
	}
	head := patMatch(p.Head, list.Args[0], binds, span)
	if head != matchYes {
		return head
	}
	tail := &core.List{T: list.T, Args: list.Args[1:]}
	return patMatch(p.Tail, tail, binds, span)
}

// patMatchAll pairwise-tests patterns against constant
// expressions; all must match.
func patMatchAll(pats []core.Pat, exps []core.Exp,
	binds *[]*core.NonRecValDecl, span token.Span,
) matchResult {
	for i, p := range pats {
		res := patMatch(p, exps[i], binds, span)
		if res != matchYes {
			return res
		}
	}
	return matchYes
}

// matchName converts a name or value comparison to a result.
func matchName(equal bool) matchResult {
	if equal {
		return matchYes
	}
	return matchNo
}

// isConApply reports whether an expression applies a constructor
// to an argument.
func isConApply(e core.Exp) bool {
	apply, ok := e.(*core.Apply)
	if !ok {
		return false
	}
	_, ok = apply.Fn.(*core.Con)
	return ok
}
