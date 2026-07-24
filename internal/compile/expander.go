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
	"slices"
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// expander grounds one query: it accumulates the query's filter
// conjuncts, deduces a generator for each unbounded variable, and
// rebuilds the query with each infinite extent scan replaced by
// its variable's generator. A used variable with no finite
// generator is an error: the query cannot be evaluated.
type expander struct {
	sys         *types.System
	recFns      map[string]*core.Fn
	cache       *generatorCache
	constraints []core.Exp
	// scanPats are the variables bound by any of the query's own
	// scans; a generator dependency outside them is bound by an
	// outer scope and needs no scheduling.
	scanPats map[*core.IDPat]bool
	// extentPats are the variables of extent scans, keyed to the
	// span their errors report.
	extentPats map[*core.IDPat]token.Span
	// used are the variables any step references; an unused
	// extent variable's scan is simply dropped.
	used map[*core.IDPat]bool
}

// expandFrom grounds a query, returning it unchanged (the same
// pointer) if nothing needed rewriting.
func expandFrom(sys *types.System, recFns map[string]*core.Fn,
	from *core.From,
) (*core.From, error) {
	x := &expander{
		sys:        sys,
		recFns:     recFns,
		cache:      &generatorCache{m: map[*core.IDPat][]*generator{}},
		scanPats:   map[*core.IDPat]bool{},
		extentPats: map[*core.IDPat]token.Span{},
		used:       map[*core.IDPat]bool{},
	}
	from = applyFbbt(sys, from)
	from = rangePushdown(sys, from)
	x.deduce(from)
	x.markUsed(from)
	// A used variable whose best generator is still infinite is
	// not grounded: no predicate could be inverted for it.
	for _, step := range from.Steps {
		scan, ok := step.(*core.Scan)
		if !ok || extentOf(scan.Exp) == nil {
			continue
		}
		for _, pat := range core.PatIDs(scan.Pat) {
			if !x.used[pat] {
				continue
			}
			g := x.cache.best(pat)
			if g == nil || g.card == infinite {
				return nil, x.notGrounded(pat)
			}
		}
	}
	return x.rebuild(from)
}

// notGrounded is the error for a variable no generator grounds.
func (x *expander) notGrounded(pat *core.IDPat) error {
	return &Error{
		Span: x.extentPats[pat],
		Msg: fmt.Sprintf("pattern '%s' is not grounded",
			pat.Name),
	}
}

// deduce walks the steps accumulating constraints and deducing
// generators: an extent scan registers its extent, and each
// filter conjunct in order may improve an unbounded variable's
// generator, seeing every earlier conjunct as context.
func (x *expander) deduce(from *core.From) {
	for _, step := range from.Steps {
		switch s := step.(type) {
		case *core.Scan:
			for _, pat := range core.PatIDs(s.Pat) {
				x.scanPats[pat] = true
			}
			apply, isApply := s.Exp.(*core.Apply)
			if extentOf(s.Exp) != nil && isApply {
				for _, pat := range core.PatIDs(s.Pat) {
					x.extentPats[pat] = apply.Span
				}
				x.cache.add(extentGenerator(s))
			}
		case *core.Where:
			var conjuncts []core.Exp
			decomposeConjuncts(s.Exp, &conjuncts)
			for _, conjunct := range conjuncts {
				if isBoolLiteral(conjunct, true) {
					continue
				}
				x.constraints = append(x.constraints, conjunct)
				x.improveGenerators()
			}
		}
	}
}

// improveGenerators retries the variables whose best generator is
// still infinite against the constraints accumulated so far.
func (x *expander) improveGenerators() {
	for pat := range x.extentPats {
		g := x.cache.best(pat)
		if g == nil || g.card != infinite {
			continue
		}
		extentSet := map[*core.IDPat]bool{}
		for p := range x.extentPats {
			extentSet[p] = true
		}
		ctx := &genContext{
			sys:     x.sys,
			extents: extentSet,
			recFns:  x.recFns,
		}
		if g2 := maybeGenerator(ctx, pat,
			x.constraints); g2 != nil {
			x.cache.add(g2)
		}
	}
}

// markUsed records every variable a step references. A query that
// collects its row implicitly (no trailing yield, into, or group)
// also uses every scan variable, since the row is built from
// them; "exists" and "forall" collect nothing.
func (x *expander) markUsed(from *core.From) {
	for _, step := range from.Steps {
		for _, pat := range stepRefs(step) {
			if x.scanPats[pat] {
				x.used[pat] = true
			}
		}
	}
	if from.Kind == ast.ExistsOp || from.Kind == ast.ForallOp {
		return
	}
	if !implicitCollect(from.Steps) {
		return
	}
	for pat := range x.scanPats {
		x.used[pat] = true
	}
}

// implicitCollect reports whether the query's result rows are
// built from its scan variables rather than a final yield, into,
// group, or through.
func implicitCollect(steps []core.FromStep) bool {
	for _, step := range steps {
		switch s := step.(type) {
		case *core.Group, *core.Into, *core.Through:
			return false
		case *core.Yield:
			if s.Fields == nil {
				return false
			}
		}
	}
	return true
}

// stepRefs returns the variables a step's expressions use.
func stepRefs(step core.FromStep) []*core.IDPat {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	a.step(step)
	var pats []*core.IDPat
	for pat, info := range a.uses {
		if info.count > 0 && !info.declared {
			pats = append(pats, pat)
		}
	}
	return pats
}

// applyFbbt strengthens a query's filters before inversion: the
// unbounded variables (extent scans, and one-sided range-list
// scans, whose ranges inject the bounds they imply) feed
// interval propagation, and deduced constant bounds join the
// filters for the range extractor to consume. The injected
// implied bounds are stripped again — the scan ranges already
// enforce them.
func applyFbbt(sys *types.System, from *core.From) *core.From {
	var unbounded []*core.IDPat
	var implied []core.Exp
	for _, step := range from.Steps {
		scan, ok := step.(*core.Scan)
		if !ok {
			continue
		}
		if isInfiniteExtent(scan.Exp) {
			unbounded = append(unbounded,
				core.PatIDs(scan.Pat)...)
			continue
		}
		if pat, b := impliedRangeBound(sys, scan); pat != nil {
			unbounded = append(unbounded, pat)
			implied = append(implied, b)
		}
	}
	if len(unbounded) == 0 {
		return from
	}
	steps := append([]core.FromStep(nil), from.Steps...)
	injected := false
	changed := false
	for i, step := range steps {
		where, ok := step.(*core.Where)
		if !ok {
			continue
		}
		exp := where.Exp
		if !injected && len(implied) > 0 {
			exp = composeConjuncts(sys,
				append(slices.Clone(implied), exp))
			injected = true
		}
		exp = fbbtStrengthen(sys, unbounded, exp)
		exp = stripByIdentity(sys, exp, implied)
		if exp != where.Exp {
			steps[i] = &core.Where{Exp: exp}
			changed = true
		}
	}
	if !changed {
		return from
	}
	return &core.From{T: from.T, Steps: steps, Kind: from.Kind}
}

// impliedRangeBound is the comparison a one-sided range-list
// scan implies for its variable.
func impliedRangeBound(sys *types.System, scan *core.Scan,
) (*core.IDPat, core.Exp) {
	pat, ok := scan.Pat.(*core.IDPat)
	if !ok {
		return nil, nil
	}
	rl, ok := scan.Exp.(*core.RangeList)
	if !ok || len(rl.Items) != 1 {
		return nil, nil
	}
	item := rl.Items[0]
	var op string
	var v core.Exp
	// lint: sort until '^\t}' where '^\tcase '
	switch item.Kind {
	case ast.RangeAtLeast:
		op, v = opGe, item.Lo
	case ast.RangeAtMost:
		op, v = opLe, item.Hi
	case ast.RangeGreaterThan:
		op, v = opGt, item.Lo
	case ast.RangeLessThan:
		op, v = opLt, item.Hi
	default:
		return nil, nil
	}
	pairT := sys.Tuple(pat.T, pat.T)
	return pat, &core.Apply{
		T: sys.Bool,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(pairT, sys.Bool),
			Name: op,
		}},
		Arg: &core.Tuple{T: pairT, Args: []core.Exp{
			&core.ID{Pat: pat}, v,
		}},
	}
}

// stripByIdentity removes the injected conjuncts from a filter,
// by pointer.
func stripByIdentity(sys *types.System, exp core.Exp,
	injected []core.Exp,
) core.Exp {
	if len(injected) == 0 {
		return exp
	}
	var conjuncts []core.Exp
	decomposeConjuncts(exp, &conjuncts)
	var remaining []core.Exp
	stripped := false
	for _, c := range conjuncts {
		if slices.Contains(injected, c) {
			stripped = true
			continue
		}
		remaining = append(remaining, c)
	}
	if !stripped {
		return exp
	}
	return composeConjuncts(sys, remaining)
}

// rangePushdown tightens each scan over a one-sided range list
// using a literal bound from the filters: "from x in [1 ..] where
// x < 5" scans [1 ..^ 5], and the consumed conjunct disappears.
// The tightest literal wins; a crossing that empties the range is
// left alone.
func rangePushdown(sys *types.System, from *core.From,
) *core.From {
	steps := append([]core.FromStep(nil), from.Steps...)
	changed := false
	for i, step := range steps {
		scan, ok := step.(*core.Scan)
		if !ok {
			continue
		}
		pat, ok := scan.Pat.(*core.IDPat)
		if !ok {
			continue
		}
		rl, ok := scan.Exp.(*core.RangeList)
		if !ok || len(rl.Items) != 1 {
			continue
		}
		item := rl.Items[0]
		wantUpper := item.Kind == ast.RangeAtLeast ||
			item.Kind == ast.RangeGreaterThan
		wantLower := item.Kind == ast.RangeAtMost ||
			item.Kind == ast.RangeLessThan
		if !wantUpper && !wantLower {
			continue
		}
		item2, consumed := pushBound(sys, pat, item, wantUpper,
			steps[i+1:])
		if consumed == nil {
			continue
		}
		steps[i] = &core.Scan{
			Pat: scan.Pat,
			Exp: &core.RangeList{
				T:     rl.T,
				Items: []core.RangeItem{item2},
			},
		}
		removeConjunct(sys, steps[i+1:], consumed)
		changed = true
	}
	if !changed {
		return from
	}
	return &core.From{T: from.T, Steps: steps, Kind: from.Kind}
}

// pushBound finds the tightest literal bound on the variable in
// the following filter steps and folds it into a one-sided range
// item, returning the closed item and the conjunct consumed.
func pushBound(sys *types.System, pat *core.IDPat,
	item core.RangeItem, wantUpper bool, rest []core.FromStep,
) (core.RangeItem, core.Exp) {
	var best *bound
	var source core.Exp
	for _, step := range rest {
		where, ok := step.(*core.Where)
		if !ok {
			continue
		}
		var conjuncts []core.Exp
		decomposeConjuncts(where.Exp, &conjuncts)
		for _, c := range conjuncts {
			lo, hi := conjunctBounds(sys, c, pat)
			b := hi
			if !wantUpper {
				b = lo
			}
			if b == nil {
				continue
			}
			lit, ok := b.value.(*core.Literal)
			if !ok {
				continue
			}
			if best != nil && !tighter(lit, b, best, wantUpper) {
				continue
			}
			best, source = b, c
		}
	}
	if best == nil {
		return item, nil
	}
	if crossesEmpty(item, best, wantUpper) {
		return item, nil
	}
	if wantUpper {
		return core.RangeItem{
			Kind: closeUpper(item.Kind, best.strict),
			Lo:   item.Lo,
			Hi:   best.value,
		}, source
	}
	return core.RangeItem{
		Kind: closeLower(item.Kind, best.strict),
		Lo:   best.value,
		Hi:   item.Hi,
	}, source
}

// tighter reports whether a new literal bound is stricter than
// the best so far.
func tighter(lit *core.Literal, b, best *bound,
	wantUpper bool,
) bool {
	bestLit, ok := best.value.(*core.Literal)
	if !ok {
		return true
	}
	c := compareLiterals(lit, bestLit)
	if wantUpper {
		return c < 0 || (c == 0 && b.strict && !best.strict)
	}
	return c > 0 || (c == 0 && b.strict && !best.strict)
}

// crossesEmpty reports whether folding the bound into the item
// would produce an obviously empty range (both endpoints
// literal, in the wrong order).
func crossesEmpty(item core.RangeItem, b *bound,
	wantUpper bool,
) bool {
	fixed := item.Lo
	if !wantUpper {
		fixed = item.Hi
	}
	fixedLit, ok := fixed.(*core.Literal)
	if !ok {
		return false
	}
	lit, ok := b.value.(*core.Literal)
	if !ok {
		return false
	}
	if wantUpper {
		return compareLiterals(lit, fixedLit) < 0
	}
	return compareLiterals(lit, fixedLit) > 0
}

// compareLiterals orders two int or char literals (both are
// int32-valued).
func compareLiterals(a, b *core.Literal) int {
	av, aOK := a.Value.(int32)
	bv, bOK := b.Value.(int32)
	switch {
	case !aOK || !bOK:
		return 0
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}

// closeUpper is the bounded range kind after adding an upper
// bound to a lower-only item.
func closeUpper(kind ast.RangeKind, strict bool) ast.RangeKind {
	open := kind == ast.RangeGreaterThan
	switch {
	case open && strict:
		return ast.RangeOpen
	case open:
		return ast.RangeOpenClosed
	case strict:
		return ast.RangeClosedOpen
	default:
		return ast.RangeClosed
	}
}

// closeLower is the bounded range kind after adding a lower bound
// to an upper-only item.
func closeLower(kind ast.RangeKind, strict bool) ast.RangeKind {
	open := kind == ast.RangeLessThan
	switch {
	case open && strict:
		return ast.RangeOpen
	case open:
		return ast.RangeClosedOpen
	case strict:
		return ast.RangeOpenClosed
	default:
		return ast.RangeClosed
	}
}

// removeConjunct deletes a consumed conjunct from the filter step
// that carries it, dropping the step if nothing remains.
func removeConjunct(sys *types.System, steps []core.FromStep,
	consumed core.Exp,
) {
	for i, step := range steps {
		where, ok := step.(*core.Where)
		if !ok {
			continue
		}
		var conjuncts []core.Exp
		decomposeConjuncts(where.Exp, &conjuncts)
		var remaining []core.Exp
		found := false
		for _, c := range conjuncts {
			if c == consumed {
				found = true
				continue
			}
			remaining = append(remaining, c)
		}
		if !found {
			continue
		}
		if len(remaining) == 0 {
			steps[i] = &core.Where{Exp: boolLiteral(sys, true)}
		} else {
			steps[i] = &core.Where{
				Exp: composeConjuncts(sys, remaining),
			}
		}
		return
	}
}

// patState tracks scheduling of a variable's generator scan.
type patState int

const (
	unscheduled patState = iota
	inProgress
	done
)

// rebuild replaces each extent scan with its variables'
// generator scans, ordered so that a generator follows the scans
// binding its dependencies, and drops the filter conjuncts that
// the sealed generators subsume.
func (x *expander) rebuild(from *core.From) (*core.From, error) {
	r := &rebuilder{
		x:        x,
		state:    map[*core.IDPat]patState{},
		subsumed: map[core.Exp]bool{},
	}
	for _, step := range from.Steps {
		// A generator whose dependencies a step uses must be
		// scheduled before the step.
		for _, pat := range stepRefs(step) {
			r.addGeneratorScan(pat)
		}
		switch s := step.(type) {
		case *core.Scan:
			if extentOf(s.Exp) != nil {
				for _, pat := range core.PatIDs(s.Pat) {
					if x.used[pat] {
						r.addGeneratorScan(pat)
					} else {
						r.changed = true
					}
				}
				continue
			}
			for _, pat := range core.PatIDs(s.Pat) {
				r.state[pat] = done
			}
			r.steps = append(r.steps, s)
		case *core.Where:
			r.filterStep(s)
		default:
			r.steps = append(r.steps, step)
		}
	}
	err := x.checkAllGrounded(from, r.state)
	if err != nil {
		return nil, err
	}
	if !r.changed {
		return from, nil
	}
	return &core.From{T: from.T, Steps: r.steps, Kind: from.Kind},
		nil
}

// checkAllGrounded reports an error for a used variable never
// scheduled: it is caught in a dependency cycle — its generator
// waits on a variable whose generator waits on it.
func (x *expander) checkAllGrounded(from *core.From,
	state map[*core.IDPat]patState,
) error {
	for _, step := range from.Steps {
		scan, ok := step.(*core.Scan)
		if !ok || extentOf(scan.Exp) == nil {
			continue
		}
		for _, pat := range core.PatIDs(scan.Pat) {
			if x.used[pat] && state[pat] != done {
				return x.notGrounded(pat)
			}
		}
	}
	return nil
}

// rebuilder accumulates the rewritten steps of one query.
type rebuilder struct {
	x       *expander
	steps   []core.FromStep
	state   map[*core.IDPat]patState
	changed bool
	// subsumed holds the provenance conjuncts of the generators
	// whose scans this rebuild actually emitted; only those
	// conjuncts leave the filters. A cached generator that was
	// never scanned must not delete its conjunct (a variable may
	// be bound by a different generator, leaving this conjunct as
	// the only enforcement).
	subsumed map[core.Exp]bool
}

// addGeneratorScan schedules the generator scan that binds a
// variable, after the scans binding the generator's own
// dependencies. A dependency that is not yet bound defers the
// scan: a later step, or the grounding check, will retry it.
func (r *rebuilder) addGeneratorScan(pat *core.IDPat) {
	if r.state[pat] != unscheduled || !r.isExtentPat(pat) {
		return
	}
	g := r.x.cache.best(pat)
	if g == nil {
		return
	}
	r.state[pat] = inProgress
	for _, dep := range g.freePats {
		r.addGeneratorScan(dep)
	}
	for _, dep := range g.freePats {
		// A dependency bound by this query must already be
		// scheduled; one from an outer scope is always bound.
		if r.x.scanPats[dep] && r.state[dep] != done {
			delete(r.state, pat)
			return
		}
	}
	// Of the variables the generator's pattern binds, only the
	// unbound extent variables still need it; the rest are
	// already scanned, and become join conditions.
	expanded := core.PatIDs(g.pat)
	var required []*core.IDPat
	for _, p := range expanded {
		if r.state[p] == done {
			continue
		}
		if r.isExtentPat(p) || slices.Contains(g.freshPats, p) {
			required = append(required, p)
		}
	}
	switch {
	case len(required) < len(expanded):
		for _, p := range expanded {
			if r.x.scanPats[p] && r.state[p] != done &&
				!slices.Contains(required, p) {
				// A bound component not yet scheduled; retry
				// when a later step pulls the generator in.
				delete(r.state, pat)
				return
			}
		}
		r.steps = append(r.steps, r.projectedScan(g, required))
		r.subsume(g)
		r.changed = true
	default:
		exp := g.exp
		if !g.unique {
			// A non-unique generator may repeat values (several
			// branches can produce one); scan it distinct.
			exp = distinctScan(r.x.sys, g.pat, exp)
		}
		r.steps = append(r.steps, &core.Scan{Pat: g.pat, Exp: exp})
		r.subsume(g)
		if len(g.conds) > 0 {
			r.steps = append(r.steps, &core.Where{
				Exp: composeConjuncts(r.x.sys, g.conds),
			})
		}
		if !r.x.isExtentScanExp(g.exp) {
			// Re-emitting the variable's own extent scan is not
			// a change; substituting anything else is.
			r.changed = true
		}
	}
	for _, id := range required {
		r.state[id] = done
	}
}

// projectedScan builds the scan of a generator only some of whose
// variables are still unbound: a subquery that scans the
// generator with the bound variables renamed, equates each
// renamed variable with its binding, and yields the required
// ones. The subquery's variables are fresh; the outer scan binds
// the required variables themselves.
func (r *rebuilder) projectedScan(g *generator,
	required []*core.IDPat,
) core.FromStep {
	fresh := map[*core.IDPat]*core.IDPat{}
	scanPat := clonePat(g.pat, fresh)
	var joins []core.Exp
	for orig, renamed := range fresh {
		if slices.Contains(required, orig) {
			continue
		}
		renamed.Name += "'"
		joins = append(joins, eqExp(r.x.sys,
			&core.ID{Pat: renamed}, &core.ID{Pat: orig}))
	}
	for _, c := range g.conds {
		joins = append(joins, substituteFresh(c, fresh))
	}
	steps := []core.FromStep{&core.Scan{Pat: scanPat, Exp: g.exp}}
	if len(joins) > 0 {
		steps = append(steps,
			&core.Where{Exp: composeConjuncts(r.x.sys, joins)})
	}
	yieldExp, outerPat := rowOf(r.x.sys, required, fresh)
	steps = append(steps, &core.Yield{Exp: yieldExp})
	sub := &core.From{
		T:     r.x.sys.Named("bag", outerPat.Type()),
		Steps: steps,
		Kind:  ast.FromOp,
	}
	return &core.Scan{Pat: outerPat, Exp: sub}
}

// distinctScan wraps a collection in a deduplicating subquery,
// "from p in exp distinct", with a fresh pattern.
func distinctScan(sys *types.System, pat core.Pat,
	exp core.Exp,
) core.Exp {
	fresh := map[*core.IDPat]*core.IDPat{}
	subPat := clonePat(pat, fresh)
	return &core.From{
		T: sys.Named("bag", pat.Type()),
		Steps: []core.FromStep{
			&core.Scan{Pat: subPat, Exp: exp},
			&core.Distinct{},
		},
		Kind: ast.FromOp,
	}
}

// substituteFresh rewrites a condition onto a scan's fresh
// pattern copies.
func substituteFresh(e core.Exp,
	fresh map[*core.IDPat]*core.IDPat,
) core.Exp {
	binds := map[*core.IDPat]core.Exp{}
	for orig, copy := range fresh {
		binds[orig] = &core.ID{Pat: copy}
	}
	return substituteExp(e, binds)
}

// rowOf builds the yielded row of a projected scan and the outer
// pattern that rebinds it: the sole variable, or a record (a
// sorted tuple) of them. The yield reads the subquery's fresh
// copies; the outer pattern binds the originals.
func rowOf(sys *types.System, required []*core.IDPat,
	fresh map[*core.IDPat]*core.IDPat,
) (core.Exp, core.Pat) {
	if len(required) == 1 {
		p := required[0]
		return &core.ID{Pat: fresh[p]}, p
	}
	sorted := append([]*core.IDPat(nil), required...)
	slices.SortFunc(sorted, func(a, b *core.IDPat) int {
		return strings.Compare(a.Name, b.Name)
	})
	fields := make([]types.Field, len(sorted))
	args := make([]core.Exp, len(sorted))
	pats := make([]core.Pat, len(sorted))
	for i, p := range sorted {
		fields[i] = types.Field{Label: p.Name, Type: p.T}
		args[i] = &core.ID{Pat: fresh[p]}
		pats[i] = p
	}
	t := sys.Record(fields)
	return &core.Tuple{T: t, Args: args},
		&core.TuplePat{T: t, Args: pats}
}

// eqExp builds an equality between two expressions.
func eqExp(sys *types.System, a, b core.Exp) core.Exp {
	argType := sys.Tuple(a.Type(), b.Type())
	return &core.Apply{
		T: sys.Bool,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(argType, sys.Bool),
			Name: eqOpName,
		}},
		Arg: &core.Tuple{T: argType, Args: []core.Exp{a, b}},
	}
}

// isExtentPat reports whether the variable came from an extent
// scan.
func (r *rebuilder) isExtentPat(pat *core.IDPat) bool {
	_, ok := r.x.extentPats[pat]
	return ok
}

// isExtentScanExp reports whether the expression is one of the
// query's original extent-scan sources.
func (x *expander) isExtentScanExp(e core.Exp) bool {
	return extentOf(e) != nil
}

// subsume records that a sealed generator's scan was emitted, so
// the conjuncts it enforces leave the filters.
func (r *rebuilder) subsume(g *generator) {
	if !g.sealed {
		return
	}
	for conjunct := range g.provenance {
		r.subsumed[conjunct] = true
	}
}

// filterStep re-emits a filter without the conjuncts that this
// rebuild's emitted generator scans subsume; a filter with
// nothing left is dropped.
func (r *rebuilder) filterStep(s *core.Where) {
	subsumed := r.subsumed
	var conjuncts []core.Exp
	decomposeConjuncts(s.Exp, &conjuncts)
	var remaining []core.Exp
	for _, conjunct := range conjuncts {
		if isBoolLiteral(conjunct, true) || subsumed[conjunct] {
			r.changed = true
			continue
		}
		remaining = append(remaining, conjunct)
	}
	if len(remaining) == 0 {
		r.changed = true
		return
	}
	if len(remaining) == len(conjuncts) {
		r.steps = append(r.steps, s)
		return
	}
	r.steps = append(r.steps,
		&core.Where{Exp: composeConjuncts(r.x.sys, remaining)})
}
