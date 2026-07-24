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

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// maybeCase inverts a boolean multi-arm case conjunct: each arm
// becomes a disjunct — a literal arm an equality on the
// scrutinee, a variable arm its body with the scrutinee
// substituted, a constructor arm a single-arm case the
// constructor machinery inverts — and the disjunction is
// inverted in the conjunct's place. A false arm contributes no
// disjunct; instead later variable and literal arms exclude the
// values of every earlier literal arm. The case conjunct itself
// always survives as a filter, which is what enforces arm order
// and exclusions exactly.
func maybeCase(sys *types.System, pat *core.IDPat,
	constraints []core.Exp, extents map[*core.IDPat]bool,
) *generator {
	for i, c := range constraints {
		cs, ok := c.(*core.Case)
		if !ok || cs.T != sys.Bool || len(cs.Matches) < 2 {
			continue
		}
		if _, _, _, conn := asBoolCase(c); conn {
			// A connective's encoding, not a user case.
			continue
		}
		branches := caseBranches(sys, cs)
		if len(branches) == 0 {
			continue
		}
		orelse := composeDisjuncts(sys, branches)
		constraints2 := slices.Concat(constraints[:i],
			[]core.Exp{orelse}, constraints[i+1:])
		if g := maybeGenerator(sys, pat, constraints2,
			extents); g != nil {
			return g
		}
	}
	return nil
}

// caseBranches converts a case's arms to disjuncts.
func caseBranches(sys *types.System, cs *core.Case) []core.Exp {
	scrut := cs.Exp
	var branches []core.Exp
	var excludes []core.Exp
	for _, m := range cs.Matches {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch p := m.Pat.(type) {
		case *core.Con0Pat, *core.ConPat:
			if isBoolLiteral(m.Exp, false) {
				continue
			}
			branches = append(branches, &core.Case{
				T:       sys.Bool,
				Exp:     scrut,
				Matches: []core.Match{m},
				Span:    cs.Span,
			})
		case *core.IDPat:
			if isBoolLiteral(m.Exp, false) {
				continue
			}
			branch := substituteExp(m.Exp,
				map[*core.IDPat]core.Exp{p: scrut})
			branches = append(branches,
				withExclusions(sys, branch, scrut, excludes))
		case *core.LiteralPat:
			lit := &core.Literal{
				T: p.T, Kind: p.Kind, Value: p.Value,
			}
			if !isBoolLiteral(m.Exp, false) {
				branch := eqExp(sys, scrut, lit)
				if !isBoolLiteral(m.Exp, true) {
					branch = boolCase(sys, branch, m.Exp,
						boolLiteral(sys, false))
				}
				branches = append(branches,
					withExclusions(sys, branch, scrut,
						excludes))
			}
			excludes = append(excludes, lit)
		case *core.WildcardPat:
			// Contributes nothing; only sound when its body is
			// false, but dropped regardless, as the reference
			// implementation does.
			continue
		default:
			return nil
		}
	}
	return branches
}

// withExclusions conjoins "not (scrutinee = v)" for each earlier
// literal arm's value.
func withExclusions(sys *types.System, branch, scrut core.Exp,
	excludes []core.Exp,
) core.Exp {
	for _, v := range excludes {
		notEq := &core.Apply{
			T: sys.Bool,
			Fn: &core.ID{Pat: &core.IDPat{
				T:    sys.Fn(sys.Bool, sys.Bool),
				Name: "not",
			}},
			Arg: eqExp(sys, scrut, v),
		}
		branch = boolCase(sys, branch, notEq,
			boolLiteral(sys, false))
	}
	return branch
}

// composeDisjuncts rebuilds a disjunction from its branches.
func composeDisjuncts(sys *types.System,
	branches []core.Exp,
) core.Exp {
	result := branches[len(branches)-1]
	for _, branch := range slices.Backward(
		branches[:len(branches)-1]) {
		result = boolCase(sys, branch,
			boolLiteral(sys, true), result)
	}
	return result
}

// substituteExp replaces variables by expressions.
func substituteExp(e core.Exp,
	binds map[*core.IDPat]core.Exp,
) core.Exp {
	r := &rewriter{}
	r.exp = func(x core.Exp) (core.Exp, bool) {
		if id, ok := x.(*core.ID); ok {
			if v, ok := binds[id.Pat]; ok {
				return v, true
			}
		}
		return nil, false
	}
	return r.rewriteExp(e)
}

// maybeConCase inverts a single-arm boolean case whose pattern is
// a constructor and whose scrutinee is the variable: the variable
// ranges over the constructor applied to every inversion of the
// arm's body.
func maybeConCase(sys *types.System, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	for _, c := range constraints {
		cs, ok := c.(*core.Case)
		if !ok || cs.T != sys.Bool || len(cs.Matches) != 1 {
			continue
		}
		scrut, ok := cs.Exp.(*core.ID)
		if !ok || scrut.Pat != pat {
			continue
		}
		if g := conCaseGenerator(sys, pat,
			cs.Matches[0]); g != nil {
			return g
		}
	}
	return nil
}

// conCaseGenerator builds the generator of one constructor arm.
func conCaseGenerator(sys *types.System, pat *core.IDPat,
	m core.Match,
) *generator {
	named, ok := pat.T.(*types.Named)
	if !ok {
		return nil
	}
	switch p := m.Pat.(type) {
	case *core.Con0Pat:
		if !isBoolLiteral(m.Exp, true) {
			return nil
		}
		con := &core.Con{
			T:        pat.T,
			Datatype: named.Name,
			Name:     p.Name,
			Ordinal:  p.Ordinal,
		}
		return &generator{
			exp: &core.List{
				T:    sys.Named("bag", pat.T),
				Args: []core.Exp{con},
			},
			pat:      pat,
			card:     single,
			unique:   true,
			sealed:   true,
			pointExp: con,
		}
	case *core.ConPat:
		return conArmGenerator(sys, pat, named, p, m.Exp)
	default:
		return nil
	}
}

// conArmGenerator inverts "CON innerPat => body": the inner
// variables range over their extents filtered by the body, and
// the constructor maps over the rows. The nested extents are
// grounded when this generator's query is itself expanded.
func conArmGenerator(sys *types.System, pat *core.IDPat,
	named *types.Named, p *core.ConPat, body core.Exp,
) *generator {
	inner, ok := conPatExp(p.Arg)
	if !ok {
		return nil
	}
	conApply := &core.Apply{
		T: pat.T,
		Fn: &core.Con{
			T:        sys.Fn(p.Arg.Type(), pat.T),
			Datatype: named.Name,
			Name:     p.Name,
			Ordinal:  p.Ordinal,
			HasArg:   true,
		},
		Arg: inner,
	}
	var steps []core.FromStep
	for _, id := range core.PatIDs(p.Arg) {
		steps = append(steps, &core.Scan{
			Pat: id,
			Exp: extentScanExp(sys, id.T, token.Span{}),
		})
	}
	if len(steps) == 0 {
		// A wholly literal argument: the arm is a single value.
		if !isBoolLiteral(body, true) {
			return nil
		}
		return &generator{
			exp: &core.List{
				T:    sys.Named("bag", pat.T),
				Args: []core.Exp{conApply},
			},
			pat:      pat,
			card:     single,
			unique:   true,
			sealed:   true,
			pointExp: conApply,
		}
	}
	if !isBoolLiteral(body, true) {
		steps = append(steps, &core.Where{Exp: body})
	}
	steps = append(steps, &core.Yield{Exp: conApply})
	built := &core.From{
		T:     sys.Named("bag", pat.T),
		Steps: steps,
		Kind:  ast.FromOp,
	}
	fresh := map[*core.IDPat]*core.IDPat{}
	return &generator{
		exp:      cloneExp(built, fresh),
		pat:      pat,
		freePats: freePatsOf(built),
		card:     finite,
		unique:   true,
		sealed:   true,
	}
}

// conPatExp rebuilds a constructor argument pattern as the
// expression over its variables.
func conPatExp(p core.Pat) (core.Exp, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := p.(type) {
	case *core.IDPat:
		return &core.ID{Pat: p}, true
	case *core.LiteralPat:
		return &core.Literal{
			T: p.T, Kind: p.Kind, Value: p.Value,
		}, true
	case *core.TuplePat:
		args := make([]core.Exp, len(p.Args))
		for i, sub := range p.Args {
			e, ok := conPatExp(sub)
			if !ok {
				return nil, false
			}
			args[i] = e
		}
		return &core.Tuple{T: p.T, Args: args}, true
	default:
		return nil, false
	}
}

// extentScanExp builds a call of the internal extent builtin on
// the whole extent of a type.
func extentScanExp(sys *types.System, t types.Type,
	span token.Span,
) core.Exp {
	bagT := sys.Named("bag", t)
	return &core.Apply{
		T: bagT,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(sys.Unit, bagT),
			Name: ExtentName,
		}},
		Arg: &core.Literal{
			Kind:  ast.UnitLiteralOp,
			T:     sys.Unit,
			Value: eval.NewRangeExtent(sys, t, nil),
		},
		Span: span,
	}
}
