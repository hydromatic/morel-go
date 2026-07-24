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
	"slices"

	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// InlineEnv is the cross-statement context for inlining: the
// expressions that defined earlier top-level bindings, and the
// set of names that are resolvable at evaluation time (used to
// check that an inlined function body remains self-contained).
type InlineEnv struct {
	// Exps maps a top-level binding's name to the Core
	// expression that defined it. Only single-variable,
	// non-recursive bindings appear; a binding whose expression
	// must not be duplicated is simply absent.
	Exps map[string]core.Exp

	// Known reports whether a name is bound at evaluation time.
	Known func(name string) bool
}

// Inline optimizes a declaration by inlining bindings into their
// use sites: a binding used once, or bound to a variable or
// literal, is substituted and its declaration dropped; an unused
// binding is dropped outright; an application of a function
// expression is beta-reduced to a let; a case on a constant
// reduces to its matching branch. Each pass re-analyzes the
// declaration, and passes repeat (up to passCount) until a pass
// changes nothing: beta-reduction introduces lets that only the
// next pass can eliminate.
//
// When passCount is zero, one limited pass runs instead:
// cross-statement inlining, beta-reduction, and singleton-case
// substitution stay on, but bindings are not eliminated and
// constant cases are not folded.
func Inline(decl core.Decl, env *InlineEnv, passCount int,
) core.Decl {
	if passCount <= 0 {
		return newPass(decl, env, true).rewriteDecl(decl)
	}
	for range passCount {
		decl2 := newPass(decl, env, false).rewriteDecl(decl)
		if decl2 == decl {
			break
		}
		decl = decl2
	}
	return decl
}

// newPass prepares one inlining pass over the declaration.
func newPass(decl core.Decl, env *InlineEnv, limited bool,
) *inliner {
	inl := &inliner{
		analysis: analyze(decl),
		env:      env,
		limited:  limited,
		subst:    map[*core.IDPat]core.Exp{},
		minted:   map[*core.IDPat]bool{},
	}
	inl.exp = inl.visit
	return inl
}

// inliner performs one inlining pass.
type inliner struct {
	rewriter

	analysis analysis
	env      *InlineEnv
	limited  bool
	subst    map[*core.IDPat]core.Exp
	// minted holds the patterns of expressions this pass copied
	// in. They are declared in the tree but absent from the
	// analysis, which predates them; without this, a copied
	// function's parameter could be mistaken for a global of the
	// same name.
	minted map[*core.IDPat]bool
}

// visit intercepts the expressions the pass rewrites beyond the
// structural walk: variable uses (substitution), applications
// (beta-reduction), singleton cases (substitution), and lets
// (elimination of dead or inlinable bindings).
func (inl *inliner) visit(e core.Exp) (core.Exp, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.Apply:
		fn := inl.rewriteExp(e.Fn)
		arg := inl.rewriteExp(e.Arg)
		// Beta-reduction: applying a function expression binds
		// its parameter in a let; a later pass inlines the let.
		if fnExp, ok := fn.(*core.Fn); ok {
			return &core.Let{
				Decl: &core.NonRecValDecl{
					Pat:  fnExp.IDPat,
					Exp:  arg,
					Span: e.Span,
				},
				Exp: fnExp.Exp,
			}, true
		}
		if fn == e.Fn && arg == e.Arg {
			return e, true
		}
		return &core.Apply{T: e.T, Fn: fn, Arg: arg, Span: e.Span}, true
	case *core.Case:
		return inl.visitCase(e), true
	case *core.ID:
		if exp, ok := inl.subst[e.Pat]; ok {
			return inl.rewriteExp(exp), true
		}
		if exp := inl.crossUnit(e); exp != nil {
			return exp, true
		}
		return e, true
	case *core.Let:
		decl, ok := e.Decl.(*core.NonRecValDecl)
		if !ok || inl.limited {
			return nil, false
		}
		pat, ok := decl.Pat.(*core.IDPat)
		if !ok {
			return nil, false
		}
		switch inl.analysis[pat] {
		case dead:
			return inl.rewriteExp(e.Exp), true
		case atomic, onceSafe:
			inl.subst[pat] = decl.Exp
			return inl.rewriteExp(e.Exp), true
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

// visitCase rewrites a case: a singleton case on an atomic
// scrutinee substitutes its binding; a multi-branch case on a
// constant reduces to its matching branch.
func (inl *inliner) visitCase(e *core.Case) core.Exp {
	scrut := inl.rewriteExp(e.Exp)
	if len(e.Matches) == 1 {
		if binds, ok := caseSubstitution(e.Matches[0].Pat,
			scrut); ok {
			maps.Copy(inl.subst, binds)
			return inl.rewriteExp(e.Matches[0].Exp)
		}
	}
	if len(e.Matches) > 1 && !inl.limited {
		if folded, ok := foldConstantCase(scrut, e); ok {
			return inl.rewriteExp(folded)
		}
	}
	matches := make([]core.Match, len(e.Matches))
	changed := scrut != e.Exp
	for i, m := range e.Matches {
		matches[i] = m
		matches[i].Exp = inl.rewriteExp(m.Exp)
		if matches[i].Exp != m.Exp {
			changed = true
		}
	}
	if !changed {
		return e
	}
	return &core.Case{
		T: e.T, Exp: scrut, Matches: matches, Span: e.Span,
	}
}

// caseSubstitution matches a singleton case's pattern against an
// atomic scrutinee — a variable or literal, or a tuple of them
// against a tuple pattern — returning the variable bindings the
// case performs. Such a case is a pure binding, substitutable
// without duplicating work.
func caseSubstitution(pat core.Pat, scrut core.Exp,
) (map[*core.IDPat]core.Exp, bool) {
	binds := map[*core.IDPat]core.Exp{}
	if !caseBinds(pat, scrut, binds) {
		return nil, false
	}
	return binds, true
}

// caseBinds accumulates the bindings of a pattern over an atomic
// scrutinee.
func caseBinds(pat core.Pat, scrut core.Exp,
	binds map[*core.IDPat]core.Exp,
) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *core.IDPat:
		if !isAtom(scrut) {
			return false
		}
		binds[p] = scrut
		return true
	case *core.TuplePat:
		tuple, ok := scrut.(*core.Tuple)
		if !ok || len(tuple.Args) != len(p.Args) {
			return false
		}
		for i, sub := range p.Args {
			if !caseBinds(sub, tuple.Args[i], binds) {
				return false
			}
		}
		return true
	case *core.WildcardPat:
		return isAtom(scrut)
	default:
		return false
	}
}

// crossUnit inlines a use of a top-level binding from an earlier
// statement, or returns nil. The binding's expression substitutes
// when it is atomic, or when it is a self-contained function:
// monomorphic, free of nested recursive declarations, and every
// free variable resolvable at evaluation time. A function is
// copied with fresh patterns, since the same expression may
// substitute at several sites and patterns are identified by
// pointer.
func (inl *inliner) crossUnit(id *core.ID) core.Exp {
	if _, declared := inl.analysis[id.Pat]; declared {
		return nil
	}
	if inl.minted[id.Pat] || inl.env == nil {
		return nil
	}
	exp, ok := inl.env.Exps[id.Pat.Name]
	if !ok {
		return nil
	}
	if isAtom(exp) {
		return inl.rewriteExp(exp)
	}
	fn, ok := exp.(*core.Fn)
	if !ok {
		return nil
	}
	if hasTypeVar(fn.T) || containsRecDecl(fn) ||
		!inl.freeVarsKnown(fn) {
		return nil
	}
	fresh := map[*core.IDPat]*core.IDPat{}
	clone := cloneExp(fn, fresh)
	for _, pat := range fresh {
		inl.minted[pat] = true
	}
	return inl.rewriteExp(clone)
}

// freeVarsKnown reports whether every variable that is free in the
// expression is resolvable at evaluation time, so the expression
// can move into another statement.
func (inl *inliner) freeVarsKnown(e core.Exp) bool {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	a.exp(e)
	for pat, info := range a.uses {
		if info.count > 0 && !info.declared &&
			!inl.env.Known(pat.Name) {
			return false
		}
	}
	return true
}

// hasTypeVar reports whether a type contains a type variable; an
// expression with such a type is polymorphic and cannot be moved
// without specializing it.
func hasTypeVar(t types.Type) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Collection:
		return hasTypeVar(t.Elem)
	case *types.Fn:
		return hasTypeVar(t.Param) || hasTypeVar(t.Result)
	case *types.List:
		return hasTypeVar(t.Elem)
	case *types.Named:
		return slices.ContainsFunc(t.Args, hasTypeVar)
	case *types.Record:
		return slices.ContainsFunc(t.Fields,
			func(f types.Field) bool { return hasTypeVar(f.Type) })
	case *types.Tuple:
		return slices.ContainsFunc(t.Args, hasTypeVar)
	case *types.Var:
		return true
	}
	return false
}

// FreeNames returns the names of the variables that occur free in
// the expression.
func FreeNames(e core.Exp) []string {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	a.exp(e)
	var names []string
	for pat, info := range a.uses {
		if info.count > 0 && !info.declared {
			names = append(names, pat.Name)
		}
	}
	return names
}

// containsRecDecl reports whether the expression contains a
// recursive declaration, which cannot be moved into another
// statement.
func containsRecDecl(e core.Exp) bool {
	found := false
	r := &rewriter{}
	r.exp = func(e core.Exp) (core.Exp, bool) {
		if let, ok := e.(*core.Let); ok {
			if _, rec := let.Decl.(*core.RecValDecl); rec {
				found = true
			}
		}
		return nil, false
	}
	r.rewriteExp(e)
	return found
}
