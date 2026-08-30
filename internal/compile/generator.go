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
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// cardinality is how many values a generator produces per binding
// of its free variables.
type cardinality int

const (
	// single: exactly one value, e.g. "x = 5".
	single cardinality = iota

	// finite: finitely many values, e.g. a materialized extent.
	finite

	// infinite: unboundedly many; the generator does not ground
	// its pattern.
	infinite
)

// A generator is an efficient way to produce the values of a
// query variable that a predicate constrains: an inverse of a
// class of predicate. The grounding pass replaces each scan over
// an infinite extent with the best generator deduced for its
// pattern.
type generator struct {
	// exp is the collection expression producing the values.
	exp core.Exp

	// pat is the pattern the generator grounds.
	pat core.Pat

	// freePats are the variables exp reads: the generator can be
	// scheduled only after scans binding them.
	freePats []*core.IDPat

	card cardinality

	// unique means exp produces no duplicates; a non-unique
	// generator's scan must be wrapped in "distinct".
	unique bool

	// sealed means every value exp produces satisfies all the
	// conjuncts in provenance, so those conjuncts may be deleted
	// from the query's filters. An unsealed generator's
	// provenance is advisory only.
	sealed bool

	// provenance is the set of original filter conjuncts the
	// generator subsumes, identified by pointer.
	provenance map[core.Exp]bool

	// rangeExps are the range-constructor applications behind a
	// range generator, kept so a union of ranges can merge them.
	rangeExps []core.Exp

	// pointExp is the single value behind a point generator, kept
	// so a union can turn it into a POINT range.
	pointExp core.Exp

	// conds are filters the generator's scan must apply
	// immediately: equalities binding fresh pattern components to
	// the expressions they stand for; freshPats are those
	// components, bound by this generator's scan alone.
	conds     []core.Exp
	freshPats []*core.IDPat
}

// generatorCache accumulates the generators deduced for each
// pattern. Entries are only added, never removed, and a later
// entry for a pattern is always at least as good as an earlier
// one, so the best generator is the most recent.
type generatorCache struct {
	m map[*core.IDPat][]*generator
}

// add indexes a generator under every variable of its pattern.
func (c *generatorCache) add(g *generator) {
	for _, id := range core.PatIDs(g.pat) {
		c.m[id] = append(c.m[id], g)
	}
}

// best returns the most recently added generator for a pattern,
// or nil.
func (c *generatorCache) best(pat *core.IDPat) *generator {
	gens := c.m[pat]
	if len(gens) == 0 {
		return nil
	}
	return gens[len(gens)-1]
}

// extentGenerator wraps a scan over an extent: the values of the
// pattern's type. It is the first generator registered for every
// unbounded pattern; finite by itself when the type is finite.
func extentGenerator(scan *core.Scan) *generator {
	card := finite
	if isInfiniteExtent(scan.Exp) {
		card = infinite
	}
	return &generator{
		exp:    scan.Exp,
		pat:    scan.Pat,
		card:   card,
		unique: true,
		sealed: true,
	}
}

// genContext is the environment generator deduction runs in: the
// type system, the query's unbounded variables, and the bodies of
// the recursive functions in scope (which are not inlined, and
// invert as fixed-point iterations).
type genContext struct {
	sys     *types.System
	extents map[*core.IDPat]bool
	recFns  map[string]*core.Fn
}

// maybeGenerator deduces a generator for a variable from the
// constraints accumulated so far, trying predicate classes in
// priority order: the first membership conjunct mentioning the
// variable, else the first equality on it.
func maybeGenerator(ctx *genContext, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	sys := ctx.sys
	var elemMatch, pointMatch core.Exp
	for _, c := range constraints {
		if elemMatch == nil && matchesElem(c, pat) {
			elemMatch = c
		}
		if pointMatch == nil && pointValue(c, pat) != nil {
			pointMatch = c
		}
	}
	if elemMatch != nil {
		if g := collectionGenerator(sys, elemMatch); g != nil {
			return g
		}
	}
	if pointMatch != nil {
		return pointGenerator(sys, pat, pointMatch)
	}
	if g := maybeTupleCase(ctx, pat, constraints); g != nil {
		return g
	}
	if g := maybeRangeGenerator(sys, pat, constraints); g != nil {
		return g
	}
	if g := maybePrefix(sys, pat, constraints); g != nil {
		return g
	}
	if g := maybeFields(ctx, pat, constraints); g != nil {
		return g
	}
	if g := maybeExists(ctx, pat, constraints); g != nil {
		return g
	}
	if g := maybeRecFn(ctx, pat, constraints); g != nil {
		return g
	}
	if g := maybeConCase(sys, pat, constraints); g != nil {
		return g
	}
	if g := maybeCase(ctx, pat, constraints); g != nil {
		return g
	}
	return maybeUnion(ctx, pat, constraints)
}

// maybePrefix inverts "String.isPrefix p s": p ranges over the
// prefixes of s, tabulated by length.
func maybePrefix(sys *types.System, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	for _, c := range constraints {
		outer, ok := c.(*core.Apply)
		if !ok {
			continue
		}
		inner, ok := outer.Fn.(*core.Apply)
		if !ok {
			continue
		}
		if builtinName(inner.Fn) != "String.isPrefix" {
			continue
		}
		p, ok := inner.Arg.(*core.ID)
		if !ok || p.Pat != pat {
			continue
		}
		s := outer.Arg
		if slices.Contains(freePatsOf(s), pat) {
			continue
		}
		return prefixGenerator(sys, pat, s, c)
	}
	return nil
}

// BuiltinName names the function an expression denotes: a
// variable's name, or "Structure.member" for a member selection.
func BuiltinName(e core.Exp) string {
	return builtinName(e)
}

// builtinName names the function an expression denotes: a
// variable's name, or "Structure.member" for a member selection.
func builtinName(e core.Exp) string {
	switch e := e.(type) {
	case *core.Apply:
		sel, okSel := e.Fn.(*core.Selector)
		id, okID := e.Arg.(*core.ID)
		if okSel && okID {
			return id.Pat.Name + "." + sel.Name
		}
	case *core.ID:
		return e.Pat.Name
	}
	return ""
}

// prefixGenerator builds the prefixes of a string:
// List.tabulate (String.size s + 1, fn i => String.substring
// (s, 0, i)). The prefixes are distinct and in order, so the
// generator is a list, as an unbounded scan's source is.
func prefixGenerator(sys *types.System, pat *core.IDPat,
	s core.Exp, conjunct core.Exp,
) *generator {
	intT := sys.Int
	strT := sys.String
	size := &core.Apply{
		T: intT,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(strT, intT),
			Name: "String.size",
		}},
		Arg: s,
	}
	count := shiftExp(sys, size, opPlus, &core.Literal{
		T: intT, Kind: ast.IntLiteralOp, Value: int32(1),
	})
	i := &core.IDPat{T: intT, Name: "i"}
	tripleT := sys.Tuple(strT, intT, intT)
	substring := &core.Apply{
		T: strT,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(tripleT, strT),
			Name: "String.substring",
		}},
		Arg: &core.Tuple{T: tripleT, Args: []core.Exp{
			s,
			&core.Literal{
				T: intT, Kind: ast.IntLiteralOp,
				Value: int32(0),
			},
			&core.ID{Pat: i},
		}},
	}
	fnT, ok := sys.Fn(intT, strT).(*types.Fn)
	if !ok {
		return nil
	}
	listT := sys.List(strT)
	pairT := sys.Tuple(intT, fnT)
	return &generator{
		exp: &core.Apply{
			T: listT,
			Fn: &core.ID{Pat: &core.IDPat{
				T:    sys.Fn(pairT, listT),
				Name: "List.tabulate",
			}},
			Arg: &core.Tuple{T: pairT, Args: []core.Exp{
				count,
				&core.Fn{T: fnT, IDPat: i, Exp: substring},
			}},
		},
		pat:        pat,
		freePats:   freePatsOf(s),
		card:       finite,
		unique:     true,
		sealed:     true,
		provenance: map[core.Exp]bool{conjunct: true},
	}
}

// maybeExists inverts a quantified conjunct: "exists z where P"
// grounds an outer variable when P does, the quantified variables
// projecting away. The exists body's conjuncts join the search
// (with the conjunct itself removed), and the derived generator
// wraps the body's scans; being unsealed, the quantifier survives
// as a filter.
func maybeExists(ctx *genContext, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	for i, c := range constraints {
		from, ok := c.(*core.From)
		if !ok || from.Kind != ast.ExistsOp {
			continue
		}
		base := slices.Concat(constraints[:i], constraints[i+1:])
		g := invertExists(ctx, pat, from, base)
		if g != nil {
			return g
		}
	}
	return nil
}

// invertExists derives a generator for the outer variable from
// one exists conjunct, trying after each of the body's filters.
func invertExists(ctx *genContext, pat *core.IDPat,
	from *core.From, base []core.Exp,
) *generator {
	working := slices.Clone(base)
	var innerScans []*core.Scan
	for _, step := range from.Steps {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch s := step.(type) {
		case *core.Group, *core.Yield:
			// No constraints to add.
		case *core.Scan:
			innerScans = append(innerScans, s)
		case *core.Where:
			decomposeConjuncts(s.Exp, &working)
			g := maybeGenerator(ctx, pat, working)
			if g == nil {
				continue
			}
			return existsGenerator(ctx, pat, g, innerScans,
				working)
		default:
			return nil
		}
	}
	return nil
}

// existsGenerator wraps the generator derived inside an exists
// body. When it depends on a quantified variable's scan, the
// body's scans join in front of it; either way the body's other
// filters apply, the outer variables project out, and the scan
// deduplicates — several quantified bindings may witness one
// value.
func existsGenerator(ctx *genContext, pat *core.IDPat,
	g *generator, innerScans []*core.Scan, working []core.Exp,
) *generator {
	sys, extents := ctx.sys, ctx.extents
	covered := map[*core.IDPat]bool{}
	for _, id := range core.PatIDs(g.pat) {
		covered[id] = true
	}
	innerVars := map[*core.IDPat]bool{}
	for _, s := range innerScans {
		for _, id := range core.PatIDs(s.Pat) {
			innerVars[id] = true
		}
	}
	steps, dependent := existsScans(g, innerScans, covered)
	steps = append(steps, &core.Scan{Pat: g.pat, Exp: g.exp})
	if len(g.conds) > 0 {
		steps = append(steps,
			&core.Where{Exp: composeConjuncts(sys, g.conds)})
	}
	// The body's other filters apply inside — except one reading
	// another unbounded variable, which the surviving quantifier
	// filter enforces instead; one that reads a quantified
	// variable no scan here binds cannot apply at all.
	var filters []core.Exp
	for _, c := range working {
		if g.provenance[c] {
			continue
		}
		include := true
		for _, f := range freePatsOf(c) {
			if innerVars[f] && !covered[f] {
				return nil
			}
			if extents[f] && !covered[f] {
				include = false
			}
		}
		if include {
			filters = append(filters, c)
		}
	}
	if len(filters) > 0 {
		steps = append(steps,
			&core.Where{Exp: composeConjuncts(sys, filters)})
	}
	yieldPats := core.PatIDs(g.pat)
	if !dependent {
		yieldPats = []*core.IDPat{pat}
	}
	yieldExp, outerPat := rowOfOriginals(sys, yieldPats)
	steps = append(steps, &core.Yield{Exp: yieldExp})
	built := &core.From{
		T:     sys.Named("bag", outerPat.Type()),
		Steps: steps,
		Kind:  ast.FromOp,
	}
	fresh := map[*core.IDPat]*core.IDPat{}
	exp := distinctScan(sys, outerPat, cloneExp(built, fresh))
	return &generator{
		exp:      exp,
		pat:      outerPat,
		freePats: freePatsOf(exp),
		card:     finite,
		unique:   true,
	}
}

// existsScans emits the quantified variables' own scans when the
// derived generator depends on them, reporting whether it does.
func existsScans(g *generator, innerScans []*core.Scan,
	covered map[*core.IDPat]bool,
) ([]core.FromStep, bool) {
	dependent := false
	for _, dep := range g.freePats {
		if !covered[dep] {
			for _, s := range innerScans {
				if slices.Contains(core.PatIDs(s.Pat), dep) {
					dependent = true
				}
			}
		}
	}
	if !dependent {
		return nil, false
	}
	var steps []core.FromStep
	for _, s := range innerScans {
		all := true
		for _, id := range core.PatIDs(s.Pat) {
			if !covered[id] {
				all = false
			}
		}
		if !all {
			steps = append(steps, s)
			for _, id := range core.PatIDs(s.Pat) {
				covered[id] = true
			}
		}
	}
	return steps, true
}

// rowOfOriginals builds a yielded row over the variables
// themselves and the pattern that rebinds them: the sole
// variable, or a record (a sorted tuple) of them.
func rowOfOriginals(sys *types.System, pats []*core.IDPat,
) (core.Exp, core.Pat) {
	if len(pats) == 1 {
		return &core.ID{Pat: pats[0]}, pats[0]
	}
	sorted := append([]*core.IDPat(nil), pats...)
	slices.SortFunc(sorted, func(a, b *core.IDPat) int {
		return strings.Compare(a.Name, b.Name)
	})
	fields := make([]types.Field, len(sorted))
	args := make([]core.Exp, len(sorted))
	patArgs := make([]core.Pat, len(sorted))
	for i, p := range sorted {
		fields[i] = types.Field{Label: p.Name, Type: p.T}
		args[i] = &core.ID{Pat: p}
		patArgs[i] = p
	}
	t := sys.Record(fields)
	return &core.Tuple{T: t, Args: args},
		&core.TuplePat{T: t, Args: patArgs}
}

// bound is one side of a range a conjunct implies: its value, its
// openness, and the conjunct it came from.
type bound struct {
	value  core.Exp
	strict bool
	source core.Exp
}

// maybeRangeGenerator inverts a pair of bound conjuncts — a lower
// like "x > 3" and an upper like "x < 10" — into a generator that
// enumerates the range between them. Both sides are required: a
// one-sided bound generates nothing. Constant bounds are
// preferred, since a variable bound makes the generator depend on
// the variable's scan.
func maybeRangeGenerator(sys *types.System, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	if pat.T != sys.Int {
		return nil
	}
	lo := findBound(sys, pat, constraints, true, true)
	if lo == nil {
		lo = findBound(sys, pat, constraints, true, false)
	}
	hi := findBound(sys, pat, constraints, false, true)
	if hi == nil {
		hi = findBound(sys, pat, constraints, false, false)
	}
	if lo == nil || hi == nil {
		return nil
	}
	return rangeGenerator(sys, pat, lo, hi)
}

// findBound returns the first bound of the given side a conjunct
// implies for the variable, optionally requiring a constant.
func findBound(sys *types.System, pat *core.IDPat,
	constraints []core.Exp, lower, constOnly bool,
) *bound {
	for _, c := range constraints {
		lo, hi := conjunctBounds(sys, c, pat)
		b := hi
		if lower {
			b = lo
		}
		if b == nil {
			continue
		}
		if _, isConst := b.value.(*core.Literal); constOnly &&
			!isConst {
			continue
		}
		return b
	}
	return nil
}

// conjunctBounds returns the lower and upper bounds a conjunct
// implies for the variable: a comparison with the variable on
// either side, a comparison against the variable plus or minus a
// literal offset (the bound shifts by the offset), or membership
// in a one-sided range list.
func conjunctBounds(sys *types.System, c core.Exp,
	pat *core.IDPat,
) (*bound, *bound) {
	for _, form := range [...]struct {
		op     string
		strict bool
		// less: the operator orders its left side below its right.
		less bool
	}{
		{op: opLt, strict: true, less: true},
		{op: opLe, less: true},
		{op: opGt, strict: true},
		{op: opGe},
	} {
		a, b := binaryCall(c, form.op)
		if a == nil {
			continue
		}
		// In "a < b", a bounds the variable from below when the
		// right side is the variable (possibly offset by a
		// literal), and b bounds it from above when the left side
		// is exactly the variable; "a > b" mirrors. The offset
		// form is recognized on the right side only.
		var fromRight, fromLeft *bound
		if v, ok := offsetRef(sys, b, pat); ok {
			fromRight = &bound{
				value: v(a), strict: form.strict, source: c,
			}
		} else if id, ok := a.(*core.ID); ok && id.Pat == pat {
			fromLeft = &bound{
				value: b, strict: form.strict, source: c,
			}
		}
		if form.less {
			return fromRight, fromLeft
		}
		return fromLeft, fromRight
	}
	return oneSidedElemBounds(c, pat)
}

// offsetRef matches the variable, or the variable plus or minus a
// literal offset; it returns a function that shifts a bound
// expression back by the offset.
func offsetRef(sys *types.System, e core.Exp, pat *core.IDPat,
) (func(core.Exp) core.Exp, bool) {
	if id, ok := e.(*core.ID); ok && id.Pat == pat {
		return func(b core.Exp) core.Exp { return b }, true
	}
	for _, op := range [...]string{opPlus, opMinus} {
		a, b := binaryCall(e, op)
		if a == nil {
			continue
		}
		id, aIsID := a.(*core.ID)
		lit, bIsLit := b.(*core.Literal)
		if op == opPlus && !aIsID {
			// "k + x" also matches; "k - x" does not.
			if id2, ok := b.(*core.ID); ok {
				if lit2, ok := a.(*core.Literal); ok {
					id, lit = id2, lit2
					aIsID, bIsLit = true, true
				}
			}
		}
		if !aIsID || !bIsLit || id.Pat != pat {
			return nil, false
		}
		return func(bnd core.Exp) core.Exp {
			// The bound on "x + k" is shifted: subtract for
			// "+", add back for "-".
			shift := opMinus
			if op == opMinus {
				shift = opPlus
			}
			return shiftExp(sys, bnd, shift, lit)
		}, true
	}
	return nil, false
}

// shiftExp applies an arithmetic operator to a bound and a
// literal offset.
func shiftExp(sys *types.System, e core.Exp, op string,
	lit *core.Literal,
) core.Exp {
	t := e.Type()
	pairT := sys.Tuple(t, t)
	return &core.Apply{
		T: t,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(pairT, t),
			Name: op,
		}},
		Arg: &core.Tuple{T: pairT, Args: []core.Exp{e, lit}},
	}
}

// oneSidedElemBounds converts membership in a one-sided range
// list ("x elem [3 ..]") to the bound it implies.
func oneSidedElemBounds(c core.Exp, pat *core.IDPat,
) (*bound, *bound) {
	lhs, coll := binaryCall(c, elemName)
	id, ok := lhs.(*core.ID)
	if !ok || id.Pat != pat {
		return nil, nil
	}
	rl, ok := coll.(*core.RangeList)
	if !ok || len(rl.Items) != 1 {
		return nil, nil
	}
	item := rl.Items[0]
	// lint: sort until '^\t}' where '^\tcase '
	switch item.Kind {
	case ast.RangeAtLeast:
		return &bound{value: item.Lo, source: c}, nil
	case ast.RangeAtMost:
		return nil, &bound{value: item.Hi, source: c}
	case ast.RangeGreaterThan:
		return &bound{value: item.Lo, strict: true, source: c}, nil
	case ast.RangeLessThan:
		return nil, &bound{value: item.Hi, strict: true, source: c}
	default:
		return nil, nil
	}
}

// rangeCtorName is the range constructor for the bounds'
// openness.
func rangeCtorName(loStrict, hiStrict bool) string {
	switch {
	case loStrict && hiStrict:
		return "OPEN"
	case loStrict:
		return "OPEN_CLOSED"
	case hiStrict:
		return "CLOSED_OPEN"
	default:
		return "CLOSED"
	}
}

// rangeGenerator builds the generator enumerating the values
// between two bounds: Bag.fromList (Range.flatten [CTOR (lo,
// hi)]), with the constructor encoding each bound's openness.
// Both source conjuncts are subsumed; other bound conjuncts on
// the variable remain as filters.
func rangeGenerator(sys *types.System, pat *core.IDPat,
	lo, hi *bound,
) *generator {
	t := pat.T
	ctorName := rangeCtorName(lo.strict, hi.strict)
	tc, ok := sys.LookupTyCon(ctorName)
	if !ok {
		return nil
	}
	rangeT := sys.Named("range", t)
	pairT := sys.Tuple(t, t)
	ctorApply := &core.Apply{
		T: rangeT,
		Fn: &core.Con{
			T:        sys.Fn(pairT, rangeT),
			Datatype: "range",
			Name:     ctorName,
			Ordinal:  tc.Ordinal,
			HasArg:   true,
		},
		Arg: &core.Tuple{
			T: pairT, Args: []core.Exp{lo.value, hi.value},
		},
	}
	ctors := []core.Exp{ctorApply}
	return &generator{
		exp: rangeScanExp(sys, t, ctors),
		pat: pat,
		freePats: append(freePatsOf(lo.value),
			freePatsOf(hi.value)...),
		card:   finite,
		unique: true,
		sealed: true,
		provenance: map[core.Exp]bool{
			lo.source: true,
			hi.source: true,
		},
		rangeExps: ctors,
	}
}

// rangeScanExp builds the collection enumerating a list of
// ranges: Range.flatten [ctors]. The result is a list, not a
// bag: an unbounded scan is ordered and distinct, so its source
// needs no bag conversion.
func rangeScanExp(sys *types.System, t types.Type,
	ctors []core.Exp,
) core.Exp {
	rangeT := sys.Named("range", t)
	listT := sys.List(t)
	return &core.Apply{
		T: listT,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(sys.List(rangeT), listT),
			Name: rangeFlattenName,
		}},
		Arg: &core.List{T: sys.List(rangeT), Args: ctors},
	}
}

// rangeSetScanExp builds the collection enumerating ranges that
// may overlap: Range.toList (Range.discreteSetOf [ctors]), whose
// set semantics deduplicate and sort.
func rangeSetScanExp(sys *types.System, t types.Type,
	ctors []core.Exp,
) core.Exp {
	rangeT := sys.Named("range", t)
	setT := sys.Named("discrete_set", t)
	set := &core.Apply{
		T: setT,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(sys.List(rangeT), setT),
			Name: "Range.discreteSetOf",
		}},
		Arg: &core.List{T: sys.List(rangeT), Args: ctors},
	}
	listT := sys.List(t)
	return &core.Apply{
		T: listT,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(setT, listT),
			Name: "Range.toList",
		}},
		Arg: set,
	}
}

// maybeUnion inverts a disjunction: each branch of the first
// "orelse" conjunct is inverted with only its own conjuncts in
// scope, and the branch generators combine. If any branch fails
// to ground the variable, the disjunction cannot be inverted.
func maybeUnion(ctx *genContext, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	sys := ctx.sys
	for _, c := range constraints {
		var branches []core.Exp
		decomposeDisjuncts(c, &branches)
		if len(branches) == 1 {
			continue
		}
		gens := make([]*generator, 0, len(branches))
		ok := true
		for _, branch := range branches {
			var bc []core.Exp
			decomposeConjuncts(branch, &bc)
			g := maybeGenerator(ctx, pat, bc)
			if g == nil {
				ok = false
				break
			}
			gens = append(gens, g)
		}
		if ok {
			return generateUnion(sys, pat, gens, c)
		}
	}
	return nil
}

// generateUnion combines the generators of a disjunction's
// branches. When every branch is a range or a point, the ranges
// merge: provably disjoint ranges concatenate into one
// Range.flatten, and possibly-overlapping ones go through
// Range.discreteSetOf, whose set semantics deduplicate — either
// way a sealed generator that subsumes the whole disjunction.
// Otherwise the branch collections concatenate; the result may
// hold duplicates (a value can satisfy several branches), so it
// is not unique, and, being unsealed, the disjunction remains a
// filter.
func generateUnion(sys *types.System, pat *core.IDPat,
	gens []*generator, orelse core.Exp,
) *generator {
	if ctors := mergedRangeCtors(sys, pat.T, gens); ctors != nil {
		exp := rangeScanExp(sys, pat.T, ctors)
		if !rangesDisjoint(ctors) {
			exp = rangeSetScanExp(sys, pat.T, ctors)
		}
		return &generator{
			exp:        exp,
			pat:        pat,
			freePats:   unionFreePats(gens),
			card:       finite,
			unique:     true,
			sealed:     true,
			provenance: map[core.Exp]bool{orelse: true},
			rangeExps:  ctors,
		}
	}
	exps := make([]core.Exp, len(gens))
	for i, g := range gens {
		exps[i] = g.exp
	}
	bagT := sys.Named("bag", pat.T)
	listT := sys.List(bagT)
	return &generator{
		exp: &core.Apply{
			T: bagT,
			Fn: &core.ID{Pat: &core.IDPat{
				T:    sys.Fn(listT, bagT),
				Name: "Bag.concat",
			}},
			Arg: &core.List{T: listT, Args: exps},
		},
		pat:      pat,
		freePats: unionFreePats(gens),
		card:     finite,
	}
}

// unionFreePats is the union of the branch generators'
// dependencies.
func unionFreePats(gens []*generator) []*core.IDPat {
	var pats []*core.IDPat
	for _, g := range gens {
		for _, p := range g.freePats {
			if !slices.Contains(pats, p) {
				pats = append(pats, p)
			}
		}
	}
	return pats
}

// mergedRangeCtors collects the range constructors behind the
// branch generators, or nil if any branch is neither a range nor
// a point.
func mergedRangeCtors(sys *types.System, t types.Type,
	gens []*generator,
) []core.Exp {
	var ctors []core.Exp
	for _, g := range gens {
		switch {
		case g.rangeExps != nil:
			ctors = append(ctors, g.rangeExps...)
		case g.pointExp != nil:
			p := pointCtor(sys, t, g.pointExp)
			if p == nil {
				return nil
			}
			ctors = append(ctors, p)
		default:
			return nil
		}
	}
	return ctors
}

// pointCtor builds "POINT v", the single-value range.
func pointCtor(sys *types.System, t types.Type,
	v core.Exp,
) core.Exp {
	tc, ok := sys.LookupTyCon("POINT")
	if !ok {
		return nil
	}
	rangeT := sys.Named("range", t)
	return &core.Apply{
		T: rangeT,
		Fn: &core.Con{
			T:        sys.Fn(t, rangeT),
			Datatype: "range",
			Name:     "POINT",
			Ordinal:  tc.Ordinal,
			HasArg:   true,
		},
		Arg: v,
	}
}

// endpoints describes one range constructor for the disjointness
// test: literal bounds and their openness.
type endpoints struct {
	lo, hi         float64
	loOpen, hiOpen bool
}

// rangesDisjoint reports whether the ranges provably do not
// overlap: every endpoint a numeric literal, and, in order of
// lower endpoint, each range strictly below the next. Touching
// closed endpoints count as overlapping.
func rangesDisjoint(ctors []core.Exp) bool {
	eps := make([]endpoints, len(ctors))
	for i, c := range ctors {
		ep, ok := ctorEndpoints(c)
		if !ok {
			return false
		}
		eps[i] = ep
	}
	slices.SortFunc(eps, func(a, b endpoints) int {
		switch {
		case a.lo < b.lo:
			return -1
		case a.lo > b.lo:
			return 1
		case a.loOpen != b.loOpen:
			if b.loOpen {
				return -1
			}
			return 1
		default:
			return 0
		}
	})
	for i := 0; i+1 < len(eps); i++ {
		a, b := eps[i], eps[i+1]
		if a.hi > b.lo {
			return false
		}
		if a.hi == b.lo && !a.hiOpen && !b.loOpen {
			return false
		}
	}
	return true
}

// ctorEndpoints extracts a constructor's literal endpoints.
func ctorEndpoints(c core.Exp) (endpoints, bool) {
	apply, ok := c.(*core.Apply)
	if !ok {
		return endpoints{}, false
	}
	con, ok := apply.Fn.(*core.Con)
	if !ok {
		return endpoints{}, false
	}
	if con.Name == "POINT" {
		v, isNum := literalNumber(apply.Arg)
		if !isNum {
			return endpoints{}, false
		}
		return endpoints{lo: v, hi: v}, true
	}
	tuple, ok := apply.Arg.(*core.Tuple)
	if !ok || len(tuple.Args) != 2 {
		return endpoints{}, false
	}
	lo, ok := literalNumber(tuple.Args[0])
	if !ok {
		return endpoints{}, false
	}
	hi, ok := literalNumber(tuple.Args[1])
	if !ok {
		return endpoints{}, false
	}
	ep := endpoints{lo: lo, hi: hi}
	// lint: sort until '^\t}' where '^\tcase '
	switch con.Name {
	case "CLOSED":
	case "CLOSED_OPEN":
		ep.hiOpen = true
	case "OPEN":
		ep.loOpen, ep.hiOpen = true, true
	case "OPEN_CLOSED":
		ep.loOpen = true
	default:
		return endpoints{}, false
	}
	return ep, true
}

// literalNumber extracts an int or real literal's value.
func literalNumber(e core.Exp) (float64, bool) {
	lit, ok := e.(*core.Literal)
	if !ok {
		return 0, false
	}
	switch v := lit.Value.(type) {
	case int32:
		return float64(v), true
	case float32:
		return float64(v), true
	default:
		return 0, false
	}
}

// binaryCall decodes an application of a named top-level operator
// to a pair, returning the two operands.
func binaryCall(e core.Exp, name string) (core.Exp, core.Exp) {
	apply, ok := e.(*core.Apply)
	if !ok {
		return nil, nil
	}
	fn, ok := apply.Fn.(*core.ID)
	if !ok || fn.Pat.Name != name {
		return nil, nil
	}
	tuple, ok := apply.Arg.(*core.Tuple)
	if !ok || len(tuple.Args) != 2 {
		return nil, nil
	}
	return tuple.Args[0], tuple.Args[1]
}

// pointValue returns the expression an equality conjunct pins the
// variable to, or nil. The constrained side must be exactly the
// variable.
func pointValue(conjunct core.Exp, pat *core.IDPat) core.Exp {
	a, b := binaryCall(conjunct, eqOpName)
	if id, ok := a.(*core.ID); ok && id.Pat == pat {
		return b
	}
	if id, ok := b.(*core.ID); ok && id.Pat == pat {
		return a
	}
	return nil
}

// pointGenerator inverts an equality conjunct "x = e" or "e = x"
// into a generator producing the single value of e; e's free
// variables become the generator's dependencies.
func pointGenerator(sys *types.System, pat *core.IDPat,
	conjunct core.Exp,
) *generator {
	point := pointValue(conjunct, pat)
	return &generator{
		exp: &core.List{
			T:    sys.Named("bag", pat.T),
			Args: []core.Exp{point},
		},
		pat:        pat,
		freePats:   freePatsOf(point),
		card:       single,
		unique:     true,
		sealed:     true,
		provenance: map[core.Exp]bool{conjunct: true},
		pointExp:   point,
	}
}

// elemName is the top-level binding of the membership operator.
const elemName = opElem

// matchesElem reports whether the conjunct is a membership test
// whose element side mentions the variable.
func matchesElem(conjunct core.Exp, pat *core.IDPat) bool {
	lhs, coll := binaryCall(conjunct, elemName)
	return lhs != nil && containsRef(lhs, pat) &&
		finiteCollection(coll)
}

// finiteCollection reports whether a collection expression is
// finitely enumerable: any collection except a range list with an
// unbounded item. Membership in a one-sided range list is a bound
// (oneSidedElemBounds), not a scan.
func finiteCollection(coll core.Exp) bool {
	rl, ok := coll.(*core.RangeList)
	if !ok {
		return true
	}
	for _, item := range rl.Items {
		switch item.Kind {
		case ast.RangeAll, ast.RangeAtLeast, ast.RangeAtMost,
			ast.RangeGreaterThan, ast.RangeLessThan:
			return false
		default:
			// Point and the bounded intervals enumerate.
		}
	}
	return true
}

// containsRef reports whether the expression mentions the
// variable: it is the variable itself, or a tuple with the
// variable somewhere within.
func containsRef(e core.Exp, pat *core.IDPat) bool {
	switch e := e.(type) {
	case *core.ID:
		return e.Pat == pat
	case *core.Tuple:
		for _, arg := range e.Args {
			if containsRef(arg, pat) {
				return true
			}
		}
	}
	return false
}

// collectionGenerator inverts a membership conjunct "x elem coll"
// into a generator scanning coll. The element side may be a tuple
// of variables and literals: the scan's pattern then grounds
// every variable at once, its literals filtering the rows. The
// collection's free variables become dependencies. The collection
// is scanned as-is — duplicates are deliberately kept.
func collectionGenerator(sys *types.System,
	conjunct core.Exp,
) *generator {
	lhs, coll := binaryCall(conjunct, elemName)
	var conds []core.Exp
	pat, ok := patForExp(sys, lhs, &conds)
	if !ok {
		return nil
	}
	freePats := freePatsOf(coll)
	var freshPats []*core.IDPat
	for _, c := range conds {
		if a, _ := binaryCall(c, eqOpName); a != nil {
			if id, isID := a.(*core.ID); isID {
				freshPats = append(freshPats, id.Pat)
			}
		}
		for _, f := range freePatsOf(c) {
			if !slices.Contains(freshPats, f) {
				freePats = append(freePats, f)
			}
		}
	}
	return &generator{
		exp:  bagToList(sys, coll),
		pat:  pat,
		card: finite,
		// The collection is whatever the predicate names, and may
		// repeat a value; the scan that grounds the variable takes
		// its distinct values.
		unique:     false,
		freePats:   freePats,
		sealed:     true,
		provenance: map[core.Exp]bool{conjunct: true},
		conds:      conds,
		freshPats:  freshPats,
	}
}

// bagToList reads a bag as a list, "Bag.toList coll". An
// unbounded scan is ordered and distinct, so its source is a
// list; where the predicate names a bag -- a foreign relation
// such as "scott.depts" -- the bag is read as one, as
// morel-java's plan shows. A collection that is already a list
// is returned unchanged.
func bagToList(sys *types.System, coll core.Exp) core.Exp {
	named, ok := coll.Type().(*types.Named)
	if !ok || named.Name != "bag" || len(named.Args) != 1 {
		return coll
	}
	listT := sys.List(named.Args[0])
	return &core.Apply{
		T: listT,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(coll.Type(), listT),
			Name: "Bag.toList",
		}},
		Arg: coll,
	}
}

// patForExp converts a membership conjunct's element side to the
// pattern its scan binds: a variable to its pattern, a tuple to a
// tuple of converted components, a literal to a literal pattern
// (a filter), and any other expression to a fresh variable with
// an equality condition binding it.
func patForExp(sys *types.System, e core.Exp,
	conds *[]core.Exp,
) (core.Pat, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.ID:
		return e.Pat, true
	case *core.Literal:
		return &core.LiteralPat{
			T: e.T, Kind: e.Kind, Value: e.Value,
		}, true
	case *core.Tuple:
		args := make([]core.Pat, len(e.Args))
		for i, arg := range e.Args {
			p, ok := patForExp(sys, arg, conds)
			if !ok {
				return nil, false
			}
			args[i] = p
		}
		return &core.TuplePat{T: e.T, Args: args}, true
	default:
		fresh := &core.IDPat{T: e.Type(), Name: "c"}
		*conds = append(*conds,
			eqExp(sys, &core.ID{Pat: fresh}, e))
		return fresh, true
	}
}

// freePatsOf returns the variables used but not declared in an
// expression.
func freePatsOf(e core.Exp) []*core.IDPat {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	a.exp(e)
	var pats []*core.IDPat
	for pat, info := range a.uses {
		if info.count > 0 && !info.declared {
			pats = append(pats, pat)
		}
	}
	return pats
}
