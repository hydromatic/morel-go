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
	"strconv"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// maybeRecFn inverts a call of a recursive function. A body of
// the shape "base orelse (exists ... where f(...) andalso
// step...)" is a transitive closure: the base's inversion seeds a
// fixed-point iteration that repeatedly extends new rows through
// the base relation. Any other recursive body contributes its
// non-recursive branches to the search. Either way the call
// survives as a filter over the generated candidates — the
// closure may over-approximate (its steps are not wired
// per-predicate), and a stripped body under-approximates, so the
// filter, evaluated against the real function, makes the rows
// exact.
func maybeRecFn(ctx *genContext, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	for _, c := range constraints {
		apply, ok := c.(*core.Apply)
		if !ok {
			continue
		}
		name := builtinName(apply.Fn)
		fn := ctx.recFns[name]
		if fn == nil {
			continue
		}
		if !containsCallOf(fn.Exp, name) {
			if g := tryInlineCall(ctx, pat, apply, fn,
				constraints); g != nil {
				return g
			}
			continue
		}
		if g := tryClosure(ctx, pat, apply, fn, name); g != nil {
			return g
		}
		if g := tryStripped(ctx, pat, apply, fn,
			name); g != nil {
			return g
		}
	}
	return nil
}

// recAnalysis is a recursive body recognized as a transitive
// closure: formals, the base disjunct, and the recursive
// disjunct's parts.
type recAnalysis struct {
	formals []*core.IDPat
	base    core.Exp
	// quantified is the recursive disjunct (an exists query).
	quantified core.Exp
	recCall    *core.Apply
}

// unwrapFn splits a function into its formal parameters and body,
// looking through the single-arm case a tuple parameter compiles
// to.
func unwrapFn(fn *core.Fn) ([]*core.IDPat, core.Exp) {
	if cs, ok := fn.Exp.(*core.Case); ok &&
		len(cs.Matches) == 1 {
		if id, ok := cs.Exp.(*core.ID); ok &&
			id.Pat == fn.IDPat {
			return core.PatIDs(cs.Matches[0].Pat),
				cs.Matches[0].Exp
		}
	}
	return []*core.IDPat{fn.IDPat}, fn.Exp
}

// analyzeClosure recognizes "base orelse (exists ... where
// f(...) andalso steps)" in a (cloned) recursive function.
func analyzeClosure(fn *core.Fn, name string) *recAnalysis {
	formals, body := unwrapFn(fn)
	var disjuncts []core.Exp
	decomposeDisjuncts(body, &disjuncts)
	// Exactly a base disjunct and a recursive disjunct.
	if len(disjuncts) != len([...]string{"base", "recursive"}) {
		return nil
	}
	base, recCase := disjuncts[0], disjuncts[1]
	if containsCallOf(base, name) {
		return nil
	}
	from, ok := recCase.(*core.From)
	if !ok || from.Kind != ast.ExistsOp {
		return nil
	}
	var recCall *core.Apply
	others := 0
	for _, step := range from.Steps {
		where, ok := step.(*core.Where)
		if !ok {
			continue
		}
		var conjuncts []core.Exp
		decomposeConjuncts(where.Exp, &conjuncts)
		for _, cj := range conjuncts {
			apply, ok := cj.(*core.Apply)
			if ok && builtinName(apply.Fn) == name {
				if recCall != nil {
					return nil
				}
				recCall = apply
				continue
			}
			others++
		}
	}
	if recCall == nil || others == 0 {
		return nil
	}
	return &recAnalysis{
		formals:    formals,
		base:       base,
		quantified: recCase,
		recCall:    recCall,
	}
}

// containsCallOf reports whether the expression applies a
// function of the given name.
func containsCallOf(e core.Exp, name string) bool {
	found := false
	r := &rewriter{}
	r.exp = func(x core.Exp) (core.Exp, bool) {
		if apply, ok := x.(*core.Apply); ok &&
			builtinName(apply.Fn) == name {
			found = true
		}
		return nil, false
	}
	r.rewriteExp(e)
	return found
}

// tryClosure inverts a transitive-closure call. The call must
// apply the function to the query variable itself, or to a tuple
// of variables and literals; the recursive call must carry a
// formal through (else the base-plus-filter fallback serves); and
// the base must invert to a collection covering every formal.
func tryClosure(ctx *genContext, pat *core.IDPat,
	apply *core.Apply, fn *core.Fn, name string,
) *generator {
	fresh := map[*core.IDPat]*core.IDPat{}
	clone, ok := cloneExp(fn, fresh).(*core.Fn)
	if !ok {
		return nil
	}
	rec := analyzeClosure(clone, name)
	if rec == nil {
		return nil
	}
	goalT := ctx.sys.Tuple(formalTypes(rec.formals)...)
	if len(rec.formals) == 1 {
		goalT = rec.formals[0].T
	}
	baseGen := invertBase(ctx, rec, goalT)
	if baseGen == nil {
		return nil
	}
	carries := false
	for _, f := range rec.formals {
		if containsRefID(rec.recCall.Arg, f) {
			carries = true
		}
	}
	var iterate core.Exp
	if carries {
		iterate = buildIterate(ctx.sys, goalT, baseGen.exp)
	} else {
		iterate = buildBidirectional(ctx, rec, goalT,
			baseGen.exp)
	}
	return closureGenerator(ctx, pat, apply, iterate, goalT)
}

// containsRefID reports whether the expression mentions the
// variable, looking through tuples and selections.
func containsRefID(e core.Exp, pat *core.IDPat) bool {
	found := false
	r := &rewriter{}
	r.exp = func(x core.Exp) (core.Exp, bool) {
		if id, ok := x.(*core.ID); ok && id.Pat == pat {
			found = true
		}
		return nil, false
	}
	r.rewriteExp(e)
	return found
}

// formalTypes lists the formals' types.
func formalTypes(formals []*core.IDPat) []types.Type {
	ts := make([]types.Type, len(formals))
	for i, f := range formals {
		ts[i] = f.T
	}
	return ts
}

// invertBase derives the collection behind the closure's base
// case: the formals become selections of a synthetic row
// variable, which the field machinery grounds as a whole.
func invertBase(ctx *genContext, rec *recAnalysis,
	goalT types.Type,
) *generator {
	row := &core.IDPat{T: goalT, Name: "row"}
	binds := map[*core.IDPat]core.Exp{}
	if len(rec.formals) == 1 {
		binds[rec.formals[0]] = &core.ID{Pat: row}
	} else {
		for i, f := range rec.formals {
			binds[f] = fieldOf(ctx.sys, row, i)
		}
	}
	base := substituteExp(rec.base, binds)
	var bc []core.Exp
	decomposeConjuncts(base, &bc)
	inner := &genContext{
		sys:     ctx.sys,
		extents: map[*core.IDPat]bool{row: true},
		recFns:  ctx.recFns,
	}
	g := maybeGenerator(inner, row, bc)
	if g == nil {
		return nil
	}
	if g.sealed && g.provenance[base] {
		return g
	}
	// The generator may over-approximate (omitted filters); the
	// base predicate re-applies over its rows, so the seed is
	// exact.
	g.exp = &core.From{
		T: ctx.sys.Named("bag", row.T),
		Steps: []core.FromStep{
			&core.Scan{Pat: row, Exp: g.exp},
			&core.Where{Exp: base},
		},
		Kind: ast.FromOp,
	}
	g.pat = row
	return g
}

// buildBidirectional emits the fixed point of a closure whose
// recursive call rebinds every formal through step predicates
// (cousins: the recursion ascends to the parents). Each new row
// binds the recursive call's arguments; each formal joins back
// through its step predicate's collection.
func buildBidirectional(ctx *genContext, rec *recAnalysis,
	goalT types.Type, seedExp core.Exp,
) core.Exp {
	sys := ctx.sys
	recArgs, ok := callArgIDs(rec.recCall.Arg)
	if !ok {
		return nil
	}
	collT := sys.Named("bag", goalT)
	prev := &core.IDPat{T: goalT, Name: "prev"}
	prevField := map[*core.IDPat]core.Exp{}
	for i, a := range recArgs {
		prevField[a] = fieldOf(sys, prev, i)
	}
	allPats := &core.IDPat{T: collT, Name: "allPaths"}
	newPats := &core.IDPat{T: collT, Name: "newPaths"}
	steps := []core.FromStep{
		&core.Scan{Pat: prev, Exp: &core.ID{Pat: newPats}},
	}
	var yieldArgs []core.Exp
	for _, f := range rec.formals {
		if e, ok := prevField[f]; ok {
			yieldArgs = append(yieldArgs, e)
			continue
		}
		scan := formalJoin(ctx, f, rec, prevField)
		if scan == nil {
			return nil
		}
		steps = append(steps, scan...)
		yieldArgs = append(yieldArgs, &core.ID{Pat: f})
	}
	steps = append(steps, &core.Yield{Exp: &core.Tuple{
		T: goalT, Args: yieldArgs,
	}})
	stepFrom := &core.From{T: collT, Steps: steps, Kind: ast.FromOp}
	return iterateApply(sys, collT, seedExp, stepFrom, allPats,
		newPats)
}

// callArgIDs lists the variables of a recursive call's argument
// tuple.
func callArgIDs(arg core.Exp) ([]*core.IDPat, bool) {
	tuple, ok := arg.(*core.Tuple)
	if !ok {
		if id, ok := arg.(*core.ID); ok {
			return []*core.IDPat{id.Pat}, true
		}
		return nil, false
	}
	ids := make([]*core.IDPat, len(tuple.Args))
	for i, a := range tuple.Args {
		id, ok := a.(*core.ID)
		if !ok {
			return nil, false
		}
		ids[i] = id.Pat
	}
	return ids, true
}

// formalJoin joins a rebound formal back through its step
// predicate: a membership whose element side holds the formal,
// its other components equated to the new row's fields.
func formalJoin(ctx *genContext, f *core.IDPat,
	rec *recAnalysis, prevField map[*core.IDPat]core.Exp,
) []core.FromStep {
	sys := ctx.sys
	from, ok := rec.quantified.(*core.From)
	if !ok {
		return nil
	}
	for _, step := range from.Steps {
		where, ok := step.(*core.Where)
		if !ok {
			continue
		}
		var conjuncts []core.Exp
		decomposeConjuncts(where.Exp, &conjuncts)
		for _, c := range conjuncts {
			c = inlinePredicate(ctx, c)
			lhs, coll := binaryCall(c, elemName)
			if lhs == nil || !containsRefID(lhs, f) {
				continue
			}
			tuple, ok := lhs.(*core.Tuple)
			if !ok {
				continue
			}
			if steps := joinScan(sys, f, tuple, coll,
				prevField); steps != nil {
				return steps
			}
		}
	}
	return nil
}

// joinScan scans a step predicate's collection, binding the
// formal's component and equating the others to the new row's
// fields.
func joinScan(sys *types.System, f *core.IDPat,
	tuple *core.Tuple, coll core.Exp,
	prevField map[*core.IDPat]core.Exp,
) []core.FromStep {
	pats := make([]core.Pat, len(tuple.Args))
	var eqs []core.Exp
	for i, comp := range tuple.Args {
		if id, isID := comp.(*core.ID); isID && id.Pat == f {
			pats[i] = f
			continue
		}
		cf := &core.IDPat{T: comp.Type(), Name: f.Name + "$f"}
		pats[i] = cf
		eqs = append(eqs, eqExp(sys, &core.ID{Pat: cf},
			substituteExpMap(comp, prevField)))
	}
	steps := []core.FromStep{&core.Scan{
		Pat: &core.TuplePat{T: tuple.T, Args: pats},
		Exp: coll,
	}}
	if len(eqs) > 0 {
		steps = append(steps, &core.Where{
			Exp: composeConjuncts(sys, eqs),
		})
	}
	return steps
}

// substituteExpMap substitutes variables, failing if any mapped
// variable would remain unmapped inside the expression.
func substituteExpMap(e core.Exp,
	binds map[*core.IDPat]core.Exp,
) core.Exp {
	return substituteExp(e, binds)
}

// inlinePredicate substitutes a non-recursive function's body
// into a call of it, so a step predicate reduces to the
// membership it wraps.
func inlinePredicate(ctx *genContext, c core.Exp) core.Exp {
	apply, ok := c.(*core.Apply)
	if !ok {
		return c
	}
	name := builtinName(apply.Fn)
	fn := ctx.recFns[name]
	if fn == nil || containsCallOf(fn.Exp, name) {
		return c
	}
	fresh := map[*core.IDPat]*core.IDPat{}
	clone, okClone := cloneExp(fn, fresh).(*core.Fn)
	if !okClone {
		return c
	}
	formals, body := unwrapFn(clone)
	binds, okBinds := formalBindings(ctx.sys, formals, apply.Arg)
	if !okBinds {
		return c
	}
	return substituteExp(body, binds)
}

// buildIterate emits the fixed-point iteration of a closure:
//
//	Relational.iterate seed
//	  (fn v => case v of (allPaths, newPaths) =>
//	     from step in base, prev in newPaths
//	     where field(step, 0) = field(prev, 1)
//	     yield (field(prev, 0), field(step, 1)))
//
// The seed is the base collection, converted to pairs when its
// element type differs; the step always right-extends new rows
// through the base relation.
func buildIterate(sys *types.System, goalT types.Type,
	baseExp core.Exp,
) core.Exp {
	collT := sys.Named("bag", goalT)
	seed := convertSeed(sys, goalT, baseExp)
	stepPat := &core.IDPat{
		T: collectionElem(baseExp.Type()), Name: "step",
	}
	prevPat := &core.IDPat{T: goalT, Name: "prev"}
	allPats := &core.IDPat{T: collT, Name: "allPaths"}
	newPats := &core.IDPat{T: collT, Name: "newPaths"}
	stepFrom := &core.From{
		T: collT,
		Steps: []core.FromStep{
			&core.Scan{Pat: stepPat, Exp: baseExp},
			&core.Scan{Pat: prevPat, Exp: &core.ID{Pat: newPats}},
			&core.Where{Exp: eqExp(sys,
				fieldOf(sys, stepPat, 0),
				fieldOf(sys, prevPat, 1))},
			&core.Yield{Exp: &core.Tuple{
				T: goalT,
				Args: []core.Exp{
					fieldOf(sys, prevPat, 0),
					fieldOf(sys, stepPat, 1),
				},
			}},
		},
		Kind: ast.FromOp,
	}
	return iterateApply(sys, collT, seed, stepFrom, allPats,
		newPats)
}

// iterateApply wraps a step body in the iteration function and
// applies Relational.iterate to the seed.
func iterateApply(sys *types.System, collT types.Type,
	seed core.Exp, stepFrom core.Exp,
	allPats, newPats *core.IDPat,
) core.Exp {
	pairT := sys.Tuple(collT, collT)
	v := &core.IDPat{T: pairT, Name: "v"}
	fnT, ok := sys.Fn(pairT, collT).(*types.Fn)
	if !ok {
		return nil
	}
	stepFn := &core.Fn{
		T:     fnT,
		IDPat: v,
		Exp: &core.Case{
			T:   collT,
			Exp: &core.ID{Pat: v},
			Matches: []core.Match{{
				Pat: &core.TuplePat{
					T:    pairT,
					Args: []core.Pat{allPats, newPats},
				},
				Exp: stepFrom,
			}},
		},
	}
	iter := &core.ID{Pat: &core.IDPat{
		T: sys.Fn(collT,
			sys.Fn(fnT, collT)),
		Name: "Relational.iterate",
	}}
	return &core.Apply{
		T: collT,
		Fn: &core.Apply{
			T:   sys.Fn(fnT, collT),
			Fn:  iter,
			Arg: seed,
		},
		Arg: stepFn,
	}
}

// convertSeed adapts the base collection to the goal element
// type, projecting each element's fields into a tuple when they
// differ (a record relation seeding a pair closure).
func convertSeed(sys *types.System, goalT types.Type,
	baseExp core.Exp,
) core.Exp {
	elemT := collectionElem(baseExp.Type())
	if elemT == goalT {
		return baseExp
	}
	e := &core.IDPat{T: elemT, Name: "e"}
	goalTuple, ok := goalT.(*types.Tuple)
	if !ok {
		return baseExp
	}
	args := make([]core.Exp, len(goalTuple.Args))
	for i := range goalTuple.Args {
		args[i] = fieldOf(sys, e, i)
	}
	return &core.From{
		T: sys.Named("bag", goalT),
		Steps: []core.FromStep{
			&core.Scan{Pat: e, Exp: baseExp},
			&core.Yield{Exp: &core.Tuple{T: goalT, Args: args}},
		},
		Kind: ast.FromOp,
	}
}

// fieldOf selects a field of a record- or tuple-typed variable.
func fieldOf(sys *types.System, pat *core.IDPat, i int,
) core.Exp {
	var label string
	var fieldT types.Type
	switch t := pat.T.(type) {
	case *types.Record:
		label = t.Fields[i].Label
		fieldT = t.Fields[i].Type
	case *types.Tuple:
		label = tupleLabel(i)
		fieldT = t.Args[i]
	default:
		return &core.ID{Pat: pat}
	}
	return &core.Apply{
		T: fieldT,
		Fn: &core.Selector{
			T:     sys.Fn(pat.T, fieldT),
			Name:  label,
			Index: i,
		},
		Arg: &core.ID{Pat: pat},
	}
}

// tupleLabel is a tuple field's numeric label.
func tupleLabel(i int) string {
	return strconv.Itoa(i + 1)
}

// closureGenerator wraps a closure's iterate expression for the
// call's argument shape: the query variable itself; a tuple of
// variables and literals (literals filter through the scan
// pattern); or a tuple repeating a variable, which scans fresh
// copies and equates them. The call conjunct is subsumed — it
// must not survive as a filter, since evaluating the recursive
// function on a candidate beyond its base case would reach the
// unbounded query inside its body.
func closureGenerator(ctx *genContext, pat *core.IDPat,
	apply *core.Apply, iterate core.Exp, goalT types.Type,
) *generator {
	if iterate == nil {
		return nil
	}
	sys := ctx.sys
	base := &generator{
		exp:        iterate,
		freePats:   freePatsOf(iterate),
		card:       finite,
		unique:     true,
		sealed:     true,
		provenance: map[core.Exp]bool{apply: true},
	}
	if id, ok := apply.Arg.(*core.ID); ok {
		if id.Pat != pat {
			return nil
		}
		base.pat = pat
		return base
	}
	tuple, ok := apply.Arg.(*core.Tuple)
	if !ok {
		return nil
	}
	if hasDuplicateID(tuple) {
		return duplicatesWrapper(sys, pat, tuple, iterate, goalT,
			apply)
	}
	var conds []core.Exp
	wp, ok := patForExp(sys, tuple, &conds)
	if !ok || len(conds) > 0 {
		return nil
	}
	base.pat = wp
	return base
}

// hasDuplicateID reports whether a tuple repeats a variable.
func hasDuplicateID(tuple *core.Tuple) bool {
	seen := map[*core.IDPat]bool{}
	for _, arg := range tuple.Args {
		if id, ok := arg.(*core.ID); ok {
			if seen[id.Pat] {
				return true
			}
			seen[id.Pat] = true
		}
	}
	return false
}

// duplicatesWrapper handles a call like "f (v, v)": the closure
// scans fresh copies, the repeated positions equate, and the
// variable's copy is yielded.
func duplicatesWrapper(sys *types.System, pat *core.IDPat,
	tuple *core.Tuple, iterate core.Exp, goalT types.Type,
	tupleApply core.Exp,
) *generator {
	goalTuple, ok := goalT.(*types.Tuple)
	if !ok || len(goalTuple.Args) != len(tuple.Args) {
		return nil
	}
	copies := make([]core.Pat, len(tuple.Args))
	byVar := map[*core.IDPat][]*core.IDPat{}
	for i, arg := range tuple.Args {
		id, ok := arg.(*core.ID)
		if !ok {
			return nil
		}
		c := &core.IDPat{T: goalTuple.Args[i], Name: "tc"}
		copies[i] = c
		byVar[id.Pat] = append(byVar[id.Pat], c)
	}
	var eqs []core.Exp
	for _, group := range byVar {
		for i := 1; i < len(group); i++ {
			eqs = append(eqs, eqExp(sys,
				&core.ID{Pat: group[0]},
				&core.ID{Pat: group[i]}))
		}
	}
	first, ok2 := byVar[pat]
	if !ok2 {
		return nil
	}
	scanPat := &core.TuplePat{T: goalT, Args: copies}
	steps := []core.FromStep{
		&core.Scan{Pat: scanPat, Exp: iterate},
	}
	if len(eqs) > 0 {
		steps = append(steps,
			&core.Where{Exp: composeConjuncts(sys, eqs)})
	}
	steps = append(steps,
		&core.Yield{Exp: &core.ID{Pat: first[0]}})
	return &generator{
		exp: &core.From{
			T:     sys.Named("bag", pat.T),
			Steps: steps,
			Kind:  ast.FromOp,
		},
		pat:      pat,
		freePats: freePatsOf(iterate),
		card:     finite,
		unique:   false,
		sealed:   true,
		provenance: map[core.Exp]bool{
			tupleApply: true,
		},
	}
}

// tryInlineCall inverts a call of a non-recursive function by
// substituting its body into the constraints in the call's place.
func tryInlineCall(ctx *genContext, pat *core.IDPat,
	apply *core.Apply, fn *core.Fn, constraints []core.Exp,
) *generator {
	fresh := map[*core.IDPat]*core.IDPat{}
	clone, ok := cloneExp(fn, fresh).(*core.Fn)
	if !ok {
		return nil
	}
	formals, body := unwrapFn(clone)
	binds, ok := formalBindings(ctx.sys, formals, apply.Arg)
	if !ok {
		return nil
	}
	body = substituteExp(body, binds)
	var cs2 []core.Exp
	for _, c := range constraints {
		if c == apply {
			decomposeConjuncts(body, &cs2)
			continue
		}
		cs2 = append(cs2, c)
	}
	return maybeGenerator(ctx, pat, cs2)
}

// tryStripped inverts a recursive call by its non-recursive
// branches alone: the body's disjuncts that avoid the function
// seed the candidates, and the surviving call filters them to the
// full fixed point.
func tryStripped(ctx *genContext, pat *core.IDPat,
	apply *core.Apply, fn *core.Fn, name string,
) *generator {
	fresh := map[*core.IDPat]*core.IDPat{}
	clone, ok := cloneExp(fn, fresh).(*core.Fn)
	if !ok {
		return nil
	}
	formals, body := unwrapFn(clone)
	binds, ok := formalBindings(ctx.sys, formals, apply.Arg)
	if !ok {
		return nil
	}
	body = substituteExp(body, binds)
	var disjuncts []core.Exp
	decomposeDisjuncts(body, &disjuncts)
	var safe []core.Exp
	for _, d := range disjuncts {
		if !containsCallOf(d, name) {
			safe = append(safe, d)
		}
	}
	if len(safe) == 0 || len(safe) == len(disjuncts) {
		return nil
	}
	var bc []core.Exp
	decomposeConjuncts(composeDisjuncts(ctx.sys, safe), &bc)
	return maybeGenerator(ctx, pat, bc)
}

// formalBindings maps a function's formals to the call's
// arguments: component-wise for a tuple call, by selection for a
// whole-tuple argument.
func formalBindings(sys *types.System, formals []*core.IDPat,
	arg core.Exp,
) (map[*core.IDPat]core.Exp, bool) {
	binds := map[*core.IDPat]core.Exp{}
	if len(formals) == 1 {
		binds[formals[0]] = arg
		return binds, true
	}
	if tuple, ok := arg.(*core.Tuple); ok &&
		len(tuple.Args) == len(formals) {
		for i, f := range formals {
			binds[f] = tuple.Args[i]
		}
		return binds, true
	}
	if id, ok := arg.(*core.ID); ok {
		for i, f := range formals {
			binds[f] = fieldOf(sys, id.Pat, i)
		}
		return binds, true
	}
	return nil, false
}
