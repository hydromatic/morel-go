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
	"maps"
	"strconv"

	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// Ground rewrites every query containing an unbounded variable —
// a scan over an infinite extent — by inverting the predicates
// that constrain the variable into a finite generator of its
// values. A used variable that no predicate grounds is an error.
// Queries inside recursive function bodies are left alone: they
// are the function's logic, and the queries that call the
// function handle them. The declaration is returned unchanged
// (the same pointer) when nothing needed rewriting.
func Ground(decl core.Decl, sys *types.System,
	recFns map[string]*core.Fn,
) (core.Decl, error) {
	if _, rec := decl.(*core.RecValDecl); rec {
		return decl, nil
	}
	g := &grounder{sys: sys, recFns: recFns}
	if g.recFns == nil {
		g.recFns = map[string]*core.Fn{}
	}
	g.exp = g.visit
	decl2 := g.rewriteDecl(decl)
	if g.err != nil {
		return nil, g.err
	}
	return decl2, nil
}

// Replan re-runs the optimization pipeline over a statement's
// resolved declaration up to a numbered pass, for Sys.planEx. A
// phase that is not an integer returns the declaration as
// resolved; "0" the limited inlining pass; "2" onward the state
// after each changing inlining pass, then after each changing
// grounding pass; anything past the last pass — "-1"
// conventionally — the final form.
func Replan(decl core.Decl, env *InlineEnv, sys *types.System,
	recFns map[string]*core.Fn, passCount int, phase string,
) core.Decl {
	target, err := strconv.Atoi(phase)
	if err != nil {
		// Not a number -- "~1", as the corpus writes it -- so the
		// declaration is returned as it was resolved, before any
		// pass. Its built-in references are still identifiers.
		return decl
	}
	if target == 0 || passCount <= 0 {
		return resolveBuiltins(
			newPass(decl, env, true).rewriteDecl(decl))
	}
	cur := decl
	for i := range passCount {
		next := newPass(cur, env, false).rewriteDecl(cur)
		if next == cur {
			break
		}
		cur = next
		if i+2 == target {
			return resolveBuiltins(cur)
		}
	}
	for i := range passCount {
		if !ContainsUnbounded(cur) {
			break
		}
		next, gerr := Ground(cur, sys, recFns)
		if gerr != nil || next == cur {
			break
		}
		cur = next
		if i+2 == target {
			return resolveBuiltins(cur)
		}
	}
	return resolveBuiltins(cur)
}

// grounder walks a declaration expanding each query, outermost
// first; an expanded query's subqueries are then visited within
// the rewritten form.
type grounder struct {
	rewriter

	sys    *types.System
	recFns map[string]*core.Fn
	err    error
}

// visit intercepts queries (to expand them) and recursive
// declarations (to leave their bodies alone).
func (g *grounder) visit(e core.Exp) (core.Exp, bool) {
	if g.err != nil {
		return e, true
	}
	switch e := e.(type) {
	case *core.From:
		from, err := expandFrom(g.sys, g.recFns, e)
		if err != nil {
			g.err = err
			return e, true
		}
		if from == e {
			return nil, false
		}
		// Continue into the rewritten query's step expressions
		// for nested queries.
		e2 := g.rewriteExpOnly(from)
		return e2, true
	case *core.Let:
		var binds []*core.NonRecValDecl
		skipDecl := false
		switch d := e.Decl.(type) {
		case *core.NonRecValDecl:
			binds = []*core.NonRecValDecl{d}
		case *core.RecValDecl:
			// The recursive bindings' bodies are the functions'
			// own logic, not expanded; the let's body is.
			binds = d.Binds
			skipDecl = true
		}
		saved := g.recFns
		extended := false
		for _, b := range binds {
			pat, okPat := b.Pat.(*core.IDPat)
			fn, okFn := b.Exp.(*core.Fn)
			if !okPat || !okFn {
				continue
			}
			if !extended {
				g.recFns = maps.Clone(saved)
				if g.recFns == nil {
					g.recFns = map[string]*core.Fn{}
				}
				extended = true
			}
			g.recFns[pat.Name] = fn
		}
		if !extended && !skipDecl {
			return nil, false
		}
		decl := e.Decl
		if !skipDecl {
			decl = g.rewriteDecl(e.Decl)
		}
		body := g.rewriteExp(e.Exp)
		g.recFns = saved
		if decl == e.Decl && body == e.Exp {
			return e, true
		}
		return &core.Let{Decl: decl, Exp: body}, true
	default:
		return nil, false
	}
}

// rewriteExpOnly rewrites an expression's children without
// re-intercepting the expression itself.
func (g *grounder) rewriteExpOnly(from *core.From) core.Exp {
	steps := make([]core.FromStep, len(from.Steps))
	changed := false
	for i, s := range from.Steps {
		steps[i] = g.rewriteStep(s)
		if steps[i] != s {
			changed = true
		}
	}
	if !changed {
		return from
	}
	return &core.From{T: from.T, Steps: steps, Kind: from.Kind}
}
