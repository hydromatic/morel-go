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
)

// use classifies how a binding is used, which decides whether the
// inliner may substitute its expression at the use sites.
type use int

const (
	// multiUnsafe: may occur many times; inlining could duplicate
	// work. Also the class of every top-level binding, which must
	// survive for later statements.
	multiUnsafe use = iota

	// dead: never used; the binding can be discarded.
	dead

	// atomic: bound to a variable or literal; inlining any number
	// of occurrences duplicates neither code nor work.
	atomic

	// onceSafe: occurs exactly once; inlining is unconditionally
	// safe.
	onceSafe

	// multiSafe: occurs at most once on each of several case
	// branches; inlining duplicates code but not work. Recognized
	// but not yet acted on.
	multiSafe
)

// useInfo accumulates the analysis of one binding.
type useInfo struct {
	declared bool // declaration site seen (else the use is global)
	top      bool // a top-level binding: never removed
	atomic   bool // bound expression is a variable or literal
	parallel bool // used on more than one branch of a case
	count    int  // number of uses (max per branch across cases)
}

// fix converts the accumulated counts to a use class.
func (u *useInfo) fix() use {
	switch {
	case u.top:
		return multiUnsafe
	case u.count == 0:
		return dead
	case u.atomic:
		return atomic
	case u.count == 1:
		if u.parallel {
			return multiSafe
		}
		return onceSafe
	default:
		return multiUnsafe
	}
}

// analysis is the result of analyzing a declaration: the use
// class of every binding declared within it. A pattern absent
// from the map was declared outside the declaration (a global);
// looking one up yields the zero value, multiUnsafe, so an
// unknown binding is never inlined or discarded.
type analysis map[*core.IDPat]use

// analyzer walks a Core tree counting declarations and uses.
type analyzer struct {
	uses map[*core.IDPat]*useInfo
}

// analyze computes the use class of every binding declared in the
// declaration. The declaration's own pattern is marked so that it
// is never classified dead: it must survive for later statements.
func analyze(decl core.Decl) analysis {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	switch d := decl.(type) {
	case *core.NonRecValDecl:
		for _, id := range core.PatIDs(d.Pat) {
			a.info(id).top = true
		}
	case *core.RecValDecl:
		for _, b := range d.Binds {
			for _, id := range core.PatIDs(b.Pat) {
				a.info(id).top = true
			}
		}
	}
	a.decl(decl)
	result := make(analysis, len(a.uses))
	for pat, info := range a.uses {
		if info.declared {
			result[pat] = info.fix()
		}
	}
	return result
}

// info returns the accumulator for a pattern, creating it at the
// pattern's declaration site.
func (a *analyzer) info(pat *core.IDPat) *useInfo {
	info := a.uses[pat]
	if info == nil {
		info = &useInfo{}
		a.uses[pat] = info
	}
	return info
}

// declare records the declaration site of a variable.
func (a *analyzer) declare(pat *core.IDPat) {
	a.info(pat).declared = true
}

// declarePat records the declaration of every variable in a
// pattern, so an unused one is classified dead rather than global.
func (a *analyzer) declarePat(pat core.Pat) {
	for _, id := range core.PatIDs(pat) {
		a.declare(id)
	}
}

// decl analyzes a declaration's expressions.
func (a *analyzer) decl(d core.Decl) {
	switch d := d.(type) {
	case *core.NonRecValDecl:
		a.declarePat(d.Pat)
		a.exp(d.Exp)
		if id, ok := d.Pat.(*core.IDPat); ok && isAtom(d.Exp) {
			a.info(id).atomic = true
		}
	case *core.RecValDecl:
		for _, b := range d.Binds {
			a.declarePat(b.Pat)
		}
		for _, b := range d.Binds {
			a.exp(b.Exp)
		}
	}
}

// isAtom reports whether an expression is a variable or literal,
// which is safe to substitute any number of times.
func isAtom(e core.Exp) bool {
	switch e.(type) {
	case *core.ID, *core.Literal:
		return true
	default:
		return false
	}
}

// referencesAny reports whether the expression uses any of the
// patterns.
func referencesAny(e core.Exp, pats []*core.IDPat) bool {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	a.exp(e)
	for _, p := range pats {
		if info := a.uses[p]; info != nil && info.count > 0 {
			return true
		}
	}
	return false
}

// exp analyzes an expression, counting each variable use.
func (a *analyzer) exp(e core.Exp) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.Apply:
		a.exp(e.Fn)
		a.exp(e.Arg)
	case *core.Case:
		a.exp(e.Exp)
		if len(e.Matches) == 1 {
			a.declarePat(e.Matches[0].Pat)
			a.exp(e.Matches[0].Exp)
			return
		}
		// Only one branch of a multi-branch case runs, so a
		// binding's uses across branches cost only the worst
		// branch: add the maximum per-branch count. A binding
		// that occurs on several branches is marked parallel —
		// inlining it there duplicates code, though not work.
		worst := map[*core.IDPat]int{}
		branches := map[*core.IDPat]int{}
		for _, m := range e.Matches {
			a.declarePat(m.Pat)
			branch := &analyzer{uses: map[*core.IDPat]*useInfo{}}
			branch.exp(m.Exp)
			for pat, info := range branch.uses {
				if info.declared {
					a.declare(pat)
				}
				if info.count == 0 {
					a.info(pat)
					continue
				}
				worst[pat] = max(worst[pat], info.count)
				branches[pat]++
			}
		}
		for pat, count := range worst {
			info := a.info(pat)
			info.count += count
			if branches[pat] > 1 {
				info.parallel = true
			}
		}
	case *core.Fn:
		a.declare(e.IDPat)
		a.exp(e.Exp)
	case *core.From:
		for _, s := range e.Steps {
			a.step(s)
		}
	case *core.ID:
		a.info(e.Pat).count++
	case *core.Let:
		a.decl(e.Decl)
		a.exp(e.Exp)
	case *core.List:
		a.exps(e.Args)
	case *core.RangeList:
		for _, item := range e.Items {
			a.exp(item.Lo)
			if item.Hi != nil {
				a.exp(item.Hi)
			}
		}
	case *core.Tuple:
		a.exps(e.Args)
	}
}

// exps analyzes each expression of a slice.
func (a *analyzer) exps(exps []core.Exp) {
	for _, e := range exps {
		a.exp(e)
	}
}

// step analyzes the expressions and bindings of a query step.
func (a *analyzer) step(s core.FromStep) {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := s.(type) {
	case *core.Group:
		for _, k := range s.Keys {
			a.exp(k.Exp)
			a.declare(k.Pat)
		}
		for _, agg := range s.Aggs {
			a.exp(agg.Fn)
			if agg.Arg != nil {
				a.exp(agg.Arg)
			}
			a.declare(agg.Pat)
		}
	case *core.Into:
		a.exp(s.Fn)
	case *core.Order:
		a.exp(s.Exp)
	case *core.Scan:
		a.exp(s.Exp)
		a.declarePat(s.Pat)
	case *core.SetOp:
		a.exps(s.Args)
	case *core.Skip:
		a.exp(s.Exp)
	case *core.Take:
		a.exp(s.Exp)
	case *core.Through:
		a.exp(s.Fn)
		a.declarePat(s.Pat)
	case *core.Where:
		a.exp(s.Exp)
	case *core.Yield:
		if s.Exp != nil {
			a.exp(s.Exp)
		}
		for _, f := range s.Fields {
			a.exp(f.Exp)
			a.declare(f.Pat)
		}
	}
}
