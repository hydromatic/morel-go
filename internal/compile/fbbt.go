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
	"math/big"
	"slices"
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// Feasibility-based bound tightening: propagate the filters'
// implications over interval domains until nothing tightens, and
// prepend any deduced constant bound as a new conjunct. The range
// extractor prefers constant bounds and consumes them into scans;
// the original conjuncts all survive as filters.

// fbbtRounds caps the propagation fixpoint.
const fbbtRounds = 8

// span is a contiguous interval with optionally open or absent
// endpoints; empty is the infeasible interval.
type span struct {
	lo, hi         *big.Rat
	hasLo, hasHi   bool
	loOpen, hiOpen bool
	empty          bool
}

// tighten intersects another span in, reporting change.
func (s *span) tighten(o span) bool {
	if s.empty {
		return false
	}
	if o.empty {
		s.empty = true
		return true
	}
	changed := false
	if o.hasLo && (!s.hasLo || ratCmp(o.lo, s.lo) > 0 ||
		(ratCmp(o.lo, s.lo) == 0 && o.loOpen && !s.loOpen)) {
		s.lo, s.hasLo, s.loOpen = o.lo, true, o.loOpen
		changed = true
	}
	if o.hasHi && (!s.hasHi || ratCmp(o.hi, s.hi) < 0 ||
		(ratCmp(o.hi, s.hi) == 0 && o.hiOpen && !s.hiOpen)) {
		s.hi, s.hasHi, s.hiOpen = o.hi, true, o.hiOpen
		changed = true
	}
	if s.hasLo && s.hasHi &&
		(ratCmp(s.lo, s.hi) > 0 ||
			(ratCmp(s.lo, s.hi) == 0 &&
				(s.loOpen || s.hiOpen))) {
		s.empty = true
	}
	return changed
}

// atLeast, greaterThan, atMost, lessThan, exactly build the
// half-bounded and singleton spans.
func atLeast(v *big.Rat) span { return span{lo: v, hasLo: true} }

func moreThan(v *big.Rat) span {
	return span{lo: v, hasLo: true, loOpen: true}
}
func atMost(v *big.Rat) span { return span{hi: v, hasHi: true} }
func lessThan(v *big.Rat) span {
	return span{hi: v, hasHi: true, hiOpen: true}
}

func exactly(v *big.Rat) span {
	return span{lo: v, hi: v, hasLo: true, hasHi: true}
}

// fbbtState is the interval per variable, and the input snapshot
// that decides what counts as newly deduced.
type fbbtState struct {
	sys       *types.System
	intervals map[*core.IDPat]*span
	inputs    map[*core.IDPat]span
	// deduce is the variables we are deducing bounds for. Others
	// are tracked but never emitted.
	deduce map[*core.IDPat]bool
}

// knows reports whether FBBT tracks a variable's interval. Every
// numeric variable is tracked, not only the ones whose bounds we
// are deducing: a variable that a scan bounds, such as "z" in
// "from z in [1, 2, 3], x where x < z", tells us about its
// neighbours even though it needs no bounds of its own.
func (st *fbbtState) knows(pat *core.IDPat) bool {
	return pat != nil &&
		(pat.T == st.sys.Int || pat.T == st.sys.Real)
}

// interval is a variable's current interval, unbounded until
// something tightens it.
func (st *fbbtState) interval(pat *core.IDPat) *span {
	s := st.intervals[pat]
	if s == nil {
		s = &span{}
		st.intervals[pat] = s
	}
	return s
}

func (st *fbbtState) tighten(pat *core.IDPat, o span) bool {
	if !st.knows(pat) {
		return false
	}
	return st.interval(pat).tighten(o)
}

// strengthen deduces constant bounds for the unbounded variables
// from a filter, returning the filter with the new bounds
// prepended — or unchanged when nothing new is deduced.
func fbbtStrengthen(sys *types.System,
	unbounded []*core.IDPat, where core.Exp,
) core.Exp {
	st := &fbbtState{
		sys:       sys,
		intervals: map[*core.IDPat]*span{},
		inputs:    map[*core.IDPat]span{},
		deduce:    map[*core.IDPat]bool{},
	}
	for _, pat := range unbounded {
		if st.knows(pat) {
			st.deduce[pat] = true
			st.interval(pat)
		}
	}
	if len(st.deduce) == 0 {
		return where
	}
	var conjuncts []core.Exp
	decomposeConjuncts(where, &conjuncts)
	// The baseline is what the extractor can already use: a bare
	// variable compared with a constant. A bound that needs
	// arithmetic to see -- "x" from "x + 1 = 3" -- is a deduction,
	// and has to be emitted even though the same propagator could
	// have found it; counting it as already present is what kept
	// "from x where x + 1 = 3" from being grounded.
	for _, c := range conjuncts {
		st.baselineBound(c)
	}
	for pat, s := range st.intervals {
		st.inputs[pat] = *s
	}
	for range fbbtRounds {
		changed := false
		for _, c := range conjuncts {
			if st.propagateLinear(c) {
				changed = true
			}
			if st.propagateAbs(c) {
				changed = true
			}
			if st.propagateMultiply(c) {
				changed = true
			}
			if st.propagateSum(c) {
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	extra := st.deducedBounds()
	if len(extra) == 0 {
		return where
	}
	return composeConjuncts(sys, append(extra, where))
}

// deducedBounds materializes the strictly-tightened bounds as
// conjuncts, variables in name order, each lower before upper.
func (st *fbbtState) deducedBounds() []core.Exp {
	pats := make([]*core.IDPat, 0, len(st.intervals))
	for pat := range st.intervals {
		pats = append(pats, pat)
	}
	slices.SortFunc(pats, func(a, b *core.IDPat) int {
		return strings.Compare(a.Name, b.Name)
	})
	var out []core.Exp
	for _, pat := range pats {
		if !st.deduce[pat] {
			// A variable that a scan bounds. It needs no bounds of its
			// own.
			continue
		}
		s := st.intervals[pat]
		if s.empty {
			continue
		}
		in := st.inputs[pat]
		if s.hasLo && (!in.hasLo || ratCmp(s.lo, in.lo) > 0 ||
			(ratCmp(s.lo, in.lo) == 0 && s.loOpen && !in.loOpen)) {
			if c := boundConjunct(st.sys, pat, s.lo, s.loOpen,
				true); c != nil {
				out = append(out, c)
			}
		}
		if s.hasHi && (!in.hasHi || ratCmp(s.hi, in.hi) < 0 ||
			(ratCmp(s.hi, in.hi) == 0 && s.hiOpen && !in.hiOpen)) {
			if c := boundConjunct(st.sys, pat, s.hi, s.hiOpen,
				false); c != nil {
				out = append(out, c)
			}
		}
	}
	return out
}

// boundConjunct builds "x >= v" and friends. The arithmetic that
// deduced the bound was exact, so this is the only place it is
// rounded. It returns nil where the bound cannot be written: an
// integer endpoint outside the range of an int.
func boundConjunct(sys *types.System, pat *core.IDPat, v *big.Rat,
	strict, lower bool,
) core.Exp {
	var lit *core.Literal
	if pat.T == sys.Int {
		n, snapped, fits := intBound(v, lower)
		if !fits {
			return nil
		}
		strict = strict && !snapped
		lit = &core.Literal{
			T: sys.Int, Kind: ast.IntLiteralOp, Value: n,
		}
	} else {
		lit = &core.Literal{
			T: sys.Real, Kind: ast.RealLiteralOp,
			Value: ratFloat32(v, lower),
		}
	}
	op := opGe
	switch {
	case lower && strict:
		op = opGt
	case !lower && strict:
		op = opLt
	case !lower:
		op = opLe
	}
	pairT := sys.Tuple(pat.T, pat.T)
	return &core.Apply{
		T: sys.Bool,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(pairT, sys.Bool),
			Name: op,
		}},
		Arg: &core.Tuple{T: pairT, Args: []core.Exp{
			&core.ID{Pat: pat}, lit,
		}},
	}
}

// intBound snaps a bound on an int variable to the tightest
// integer endpoint, since a propagator can produce a fractional
// one (30/4 = 15/2, say): x > 15/2 becomes x >= 8, and x < 15/2
// becomes x <= 7. It reports whether it snapped -- a snapped
// endpoint is inclusive -- and whether the result is an int at
// all.
func intBound(v *big.Rat, lower bool) (int32, bool, bool) {
	if n, fits := ratInt32(v); fits {
		return n, false, true
	}
	if zeroRat(v).IsInt() {
		// Whole, but too large for an int.
		return 0, false, false
	}
	i := ratFloor(v)
	if lower {
		i = ratCeil(v)
	}
	n, fits := bigInt32(i)
	return n, true, fits
}

// linTerm is a variable plus a constant offset, or a constant.
type linTerm struct {
	pat    *core.IDPat
	offset *big.Rat
	ok     bool
}

// linearTermOf decomposes "x", "x + 3", "3 + x", "x - 2", "5".
func linearTermOf(e core.Exp) linTerm {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.Apply:
		for _, op := range [...]string{opPlus, opMinus} {
			a, b := binaryCall(e, op)
			if a == nil {
				continue
			}
			ta, tb := linearTermOf(a), linearTermOf(b)
			if !ta.ok || !tb.ok {
				return linTerm{}
			}
			if ta.pat != nil && tb.pat != nil {
				return linTerm{}
			}
			if op == opMinus && tb.pat != nil {
				// "const - var" needs a negative coefficient.
				return linTerm{}
			}
			offset := tb.offset
			if op == opMinus {
				offset = ratNeg(offset)
			}
			pat := ta.pat
			if pat == nil {
				pat = tb.pat
			}
			return linTerm{
				pat:    pat,
				offset: ratAdd(ta.offset, offset),
				ok:     true,
			}
		}
	case *core.ID:
		return linTerm{pat: e.Pat, offset: new(big.Rat), ok: true}
	case *core.Literal:
		if v, isNum := literalRat(e); isNum {
			return linTerm{offset: v, ok: true}
		}
	}
	return linTerm{}
}

// A linear form is a combination of *atoms* with coefficients, plus
// a constant. An atom is a variable, or an "abs" term -- a quantity
// the arithmetic cannot see into, but whose value lies in a known
// interval, which is exactly what a variable is to FBBT.
//
// It is what lets a constraint with coefficients be read, such as
// "3 * t + 5 * f = 30", which the earlier form -- one variable and
// an offset -- could not.
type linAtom struct {
	key string
	exp core.Exp
	pat *core.IDPat // the variable, or the one inside an "abs"
	// inCoef and off are the coefficient and the constant inside
	// an "abs": 2 and ~1 in "abs (2 * x - 1)".
	inCoef *big.Rat
	off    *big.Rat
	isAbs  bool
	coef   *big.Rat
}

type linearForm struct {
	atoms []linAtom
	konst *big.Rat
	ok    bool
}

// constForm is a form with no atoms.
func constForm(v *big.Rat) linearForm {
	return linearForm{konst: v, ok: true}
}

// combine returns this form plus scale times that.
func (f linearForm) combine(g linearForm,
	scale *big.Rat,
) linearForm {
	if !f.ok || !g.ok {
		return linearForm{}
	}
	out := linearForm{
		konst: ratAdd(f.konst, ratMul(g.konst, scale)),
		ok:    true,
	}
	out.atoms = append(out.atoms, f.atoms...)
	for _, a := range g.atoms {
		a.coef = ratMul(a.coef, scale)
		out.atoms = addAtom(out.atoms, a)
	}
	// A variable whose coefficients cancel, as "x" does in
	// "x + y - x", drops out of the form.
	kept := out.atoms[:0]
	for _, a := range out.atoms {
		if ratSign(a.coef) != 0 {
			kept = append(kept, a)
		}
	}
	out.atoms = kept
	return out
}

// addAtom merges one atom into a list of them.
func addAtom(atoms []linAtom, a linAtom) []linAtom {
	for i := range atoms {
		if atoms[i].key == a.key {
			atoms[i].coef = ratAdd(atoms[i].coef, a.coef)
			return atoms
		}
	}
	return append(atoms, a)
}

// times scales every coefficient and the constant.
func (f linearForm) times(scale *big.Rat) linearForm {
	if !f.ok {
		return linearForm{}
	}
	if ratSign(scale) == 0 {
		return constForm(new(big.Rat))
	}
	out := linearForm{konst: ratMul(f.konst, scale), ok: true}
	for _, a := range f.atoms {
		a.coef = ratMul(a.coef, scale)
		out.atoms = append(out.atoms, a)
	}
	return out
}

// linearFormOf decomposes an expression into a linear combination
// of atoms, or a form that is not ok if it is not linear.
func linearFormOf(e core.Exp) linearForm {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.Apply:
		if a, b := binaryCall(e, opPlus); a != nil {
			return linearFormOf(a).combine(linearFormOf(b), ratOf(1))
		}
		if a, b := binaryCall(e, opMinus); a != nil {
			return linearFormOf(a).combine(linearFormOf(b), ratOf(-1))
		}
		if a, b := binaryCall(e, opTimes); a != nil {
			fa, fb := linearFormOf(a), linearFormOf(b)
			// One side must be constant: a product of two variables
			// is not linear.
			if fa.ok && len(fa.atoms) == 0 {
				return fb.times(fa.konst)
			}
			if fb.ok && len(fb.atoms) == 0 {
				return fa.times(fb.konst)
			}
			return linearForm{}
		}
		if fn, isID := e.Fn.(*core.ID); isID &&
			fn.Pat.Name == opNegate {
			return linearFormOf(e.Arg).times(ratOf(-1))
		}
		if atom, isAtom := absAtom(e); isAtom {
			return linearForm{atoms: []linAtom{atom}, ok: true}
		}
	case *core.ID:
		return linearForm{ok: true, atoms: []linAtom{{
			key:  fmt.Sprintf("v%p", e.Pat),
			exp:  e,
			pat:  e.Pat,
			coef: ratOf(1),
		}}}
	case *core.Literal:
		if v, isNum := literalRat(e); isNum {
			return constForm(v)
		}
	}
	return linearForm{}
}

// absAtom reads "abs e", whose value lies in [0, inf). Bounding it
// means bounding what is inside, which is the one thing that is
// special about it: "abs e <= b" gives "~b <= e <= b". Only an
// "abs" of a single variable is read, because that is the variable
// a bound is put on.
func absAtom(e *core.Apply) (linAtom, bool) {
	name := builtinName(e.Fn)
	if name != absName && name != "Int.abs" && name != "Real.abs" {
		return linAtom{}, false
	}
	inner := linearFormOf(e.Arg)
	if !inner.ok || len(inner.atoms) != 1 ||
		inner.atoms[0].pat == nil || inner.atoms[0].isAbs {
		return linAtom{}, false
	}
	in := inner.atoms[0]
	return linAtom{
		key: fmt.Sprintf("abs(%s*v%p+%s)", in.coef.RatString(),
			in.pat, zeroRat(inner.konst).RatString()),
		exp:    e,
		pat:    in.pat,
		inCoef: in.coef,
		off:    inner.konst,
		isAbs:  true,
		coef:   ratOf(1),
	}, true
}

// propagateSum bounds each atom of a comparison in turn, by
// substituting the extreme values its siblings' intervals allow.
// Iterated to a fixed point, as FBBT already does, this propagates
// bounds around a chain such as "1 <= a andalso a <= b andalso
// b <= c".
func (st *fbbtState) propagateSum(c core.Exp) bool {
	x, y, op := comparisonOf(c)
	if x == nil {
		return false
	}
	lhs, rhs := linearFormOf(x), linearFormOf(y)
	if !lhs.ok || !rhs.ok {
		return false
	}
	// Rewrite "lhs OP rhs" as "sum OP 0".
	sum := lhs.combine(rhs, ratOf(-1))
	if len(sum.atoms) == 0 {
		return false
	}
	changed := false
	for _, a := range sum.atoms {
		if st.tightenAtom(sum, a, op) {
			changed = true
		}
	}
	return changed
}

// tightenAtom bounds one atom of "sum OP 0", given the intervals of
// the others.
func (st *fbbtState) tightenAtom(sum linearForm, a linAtom,
	op string,
) bool {
	if !st.knows(a.pat) {
		return false
	}
	// The rest of the sum lies in [restMin, restMax]; either is
	// absent if a sibling is unbounded on that side.
	restMin, restMax := zeroRat(sum.konst), zeroRat(sum.konst)
	haveMin, haveMax := true, true
	for _, b := range sum.atoms {
		if b.key == a.key {
			continue
		}
		s, known := st.atomSpan(b)
		if !known {
			return false
		}
		// A positive coefficient takes its minimum at the atom's
		// lower endpoint, a negative one at its upper endpoint.
		lo, hi := s.lo, s.hi
		hasLo, hasHi := s.hasLo, s.hasHi
		if ratSign(b.coef) < 0 {
			lo, hi = hi, lo
			hasLo, hasHi = hasHi, hasLo
		}
		if haveMin && hasLo {
			restMin = ratAdd(restMin, ratMul(b.coef, lo))
		} else {
			haveMin = false
		}
		if haveMax && hasHi {
			restMax = ratAdd(restMax, ratMul(b.coef, hi))
		} else {
			haveMax = false
		}
	}
	// "coefficient * atom OP -rest". An upper bound on the atom
	// needs the largest that "-rest" can be, so the smallest rest;
	// and the other way about.
	// lint: sort until '^\t}' where '^\tcase '
	switch op {
	case eqOpName:
		lower := st.boundAtom(a, restMin, haveMin, false, opLe)
		upper := st.boundAtom(a, restMax, haveMax, true, opGe)
		return lower || upper
	case opGt, opGe:
		return st.boundAtom(a, restMax, haveMax, true, op)
	case opLt, opLe:
		return st.boundAtom(a, restMin, haveMin, false, op)
	default:
		return false
	}
}

// atomSpan is the interval an atom's value lies in: a variable's
// own, or, for an "abs" term, what its variable's interval implies.
func (st *fbbtState) atomSpan(a linAtom) (span, bool) {
	if !st.knows(a.pat) {
		return span{}, false
	}
	s := st.interval(a.pat)
	if s.empty {
		// An empty interval has no endpoints to substitute, and
		// anything deduced from one would be a bound that no value
		// satisfies.
		return span{}, false
	}
	if !a.isAbs {
		return *s, true
	}
	// "abs e" is at least zero, and at most the larger of what its
	// variable's endpoints give.
	out := span{lo: new(big.Rat), hasLo: true}
	if s.hasLo && s.hasHi {
		out.hi = ratMax(
			ratAbs(ratAdd(ratMul(a.inCoef, s.lo), a.off)),
			ratAbs(ratAdd(ratMul(a.inCoef, s.hi), a.off)))
		out.hasHi = true
	}
	return out, true
}

// boundAtom puts a bound on an atom, or on the variable inside an
// "abs" term.
func (st *fbbtState) boundAtom(a linAtom, rest *big.Rat,
	have bool, lower bool, op string,
) bool {
	if !have || ratSign(a.coef) == 0 {
		return false
	}
	// Dividing by a negative coefficient turns an upper bound into
	// a lower bound, and the other way about.
	flip := ratSign(a.coef) < 0
	resultLower := flip != lower
	value := ratDiv(ratNeg(rest), a.coef)
	strict := op == opLt || op == opGt
	if a.isAbs {
		// "abs e <= b" gives "~b <= e <= b"; a lower bound on a
		// non-negative quantity says nothing about e.
		if resultLower {
			return false
		}
		// "abs (c * x + k) <= b" gives
		// "(~b - k) / c <= x <= (b - k) / c", the two swapping
		// when c is negative.
		if ratSign(a.inCoef) == 0 {
			return false
		}
		lo := ratDiv(ratSub(ratNeg(value), a.off), a.inCoef)
		hi := ratDiv(ratSub(value, a.off), a.inCoef)
		if ratSign(a.inCoef) < 0 {
			lo, hi = hi, lo
		}
		s := span{
			lo: lo, hasLo: true, loOpen: strict,
			hi: hi, hasHi: true, hiOpen: strict,
		}
		return st.tighten(a.pat, s)
	}
	var s span
	switch {
	case resultLower && strict:
		s = moreThan(value)
	case resultLower:
		s = atLeast(value)
	case strict:
		s = lessThan(value)
	default:
		s = atMost(value)
	}
	return st.tighten(a.pat, s)
}

// comparison decodes a comparison conjunct: operands and the
// operator, normalized so the operator reads left-to-right.
func comparisonOf(c core.Exp) (core.Exp, core.Exp, string) {
	for _, name := range [...]string{
		opLt, opLe, opGt, opGe, eqOpName,
	} {
		if x, y := binaryCall(c, name); x != nil {
			return x, y, name
		}
	}
	return nil, nil, ""
}

// reverseOp mirrors a comparison.
func reverseOp(op string) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch op {
	case opGe:
		return opLe
	case opGt:
		return opLt
	case opLe:
		return opGe
	case opLt:
		return opGt
	default:
		return op
	}
}

// spanFromOp is the interval a comparison against a constant
// implies.
func spanFromOp(op string, v *big.Rat) (span, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch op {
	case eqOpName:
		return exactly(v), true
	case opGe:
		return atLeast(v), true
	case opGt:
		return moreThan(v), true
	case opLe:
		return atMost(v), true
	case opLt:
		return lessThan(v), true
	default:
		return span{}, false
	}
}

// propagateLinearConstant tightens by "x + k OP c" forms only —
// the pass that captures the input baseline.
func (st *fbbtState) propagateLinearConstant(c core.Exp) bool {
	a, b, op := comparisonOf(c)
	if a == nil {
		return false
	}
	ta, tb := linearTermOf(a), linearTermOf(b)
	if !ta.ok || !tb.ok {
		return false
	}
	if ta.pat != nil && tb.pat == nil && st.knows(ta.pat) {
		if s, ok := spanFromOp(op,
			ratSub(tb.offset, ta.offset)); ok {
			return st.tighten(ta.pat, s)
		}
	}
	if tb.pat != nil && ta.pat == nil && st.knows(tb.pat) {
		if s, ok := spanFromOp(reverseOp(op),
			ratSub(ta.offset, tb.offset)); ok {
			return st.tighten(tb.pat, s)
		}
	}
	return false
}

// baselineBound tightens from a comparison of a bare variable
// against a constant, which is the shape the range extractor reads
// for itself.
func (st *fbbtState) baselineBound(c core.Exp) bool {
	a, b, op := comparisonOf(c)
	if a == nil {
		return false
	}
	ta, tb := linearTermOf(a), linearTermOf(b)
	if !ta.ok || !tb.ok {
		return false
	}
	if ta.pat != nil && ratSign(ta.offset) == 0 && tb.pat == nil &&
		st.knows(ta.pat) {
		if s, ok := spanFromOp(op, tb.offset); ok {
			return st.tighten(ta.pat, s)
		}
	}
	if tb.pat != nil && ratSign(tb.offset) == 0 && ta.pat == nil &&
		st.knows(tb.pat) {
		if s, ok := spanFromOp(reverseOp(op), ta.offset); ok {
			return st.tighten(tb.pat, s)
		}
	}
	return false
}

// propagateLinear handles constant and two-variable comparisons.
func (st *fbbtState) propagateLinear(c core.Exp) bool {
	if st.propagateLinearConstant(c) {
		return true
	}
	a, b, op := comparisonOf(c)
	if a == nil {
		return false
	}
	ta, tb := linearTermOf(a), linearTermOf(b)
	if !ta.ok || !tb.ok || ta.pat == nil || tb.pat == nil ||
		!st.knows(ta.pat) || !st.knows(tb.pat) ||
		ta.pat == tb.pat {
		return false
	}
	delta := ratSub(tb.offset, ta.offset)
	changed := false
	if s, ok := spanFromOther(op, *st.interval(tb.pat),
		delta); ok {
		changed = st.tighten(ta.pat, s) || changed
	}
	if s, ok := spanFromOther(reverseOp(op),
		*st.interval(ta.pat), ratNeg(delta)); ok {
		changed = st.tighten(tb.pat, s) || changed
	}
	return changed
}

// spanFromOther bounds a variable by the other side's interval
// shifted by the offset difference.
func spanFromOther(op string, other span, delta *big.Rat,
) (span, bool) {
	if other.empty {
		return span{}, false
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch op {
	case eqOpName:
		if !other.hasLo && !other.hasHi {
			return span{}, false
		}
		s := other
		if s.hasLo {
			s.lo = ratAdd(s.lo, delta)
		}
		if s.hasHi {
			s.hi = ratAdd(s.hi, delta)
		}
		return s, true
	case opGe:
		if !other.hasLo {
			return span{}, false
		}
		if other.loOpen {
			return moreThan(ratAdd(other.lo, delta)), true
		}
		return atLeast(ratAdd(other.lo, delta)), true
	case opGt:
		if !other.hasLo {
			return span{}, false
		}
		return moreThan(ratAdd(other.lo, delta)), true
	case opLe:
		if !other.hasHi {
			return span{}, false
		}
		if other.hiOpen {
			return lessThan(ratAdd(other.hi, delta)), true
		}
		return atMost(ratAdd(other.hi, delta)), true
	case opLt:
		if !other.hasHi {
			return span{}, false
		}
		return lessThan(ratAdd(other.hi, delta)), true
	default:
		return span{}, false
	}
}

// propagateAbs handles "abs x OP c" for the connected intervals:
// upper bounds and equality with zero.
func (st *fbbtState) propagateAbs(c core.Exp) bool {
	a, b, op := comparisonOf(c)
	if a == nil {
		return false
	}
	pat, v, ok := absAndLiteral(a, b)
	if !ok {
		pat, v, ok = absAndLiteral(b, a)
		op = reverseOp(op)
	}
	if !ok || !st.knows(pat) {
		return false
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch op {
	case eqOpName:
		if ratSign(v) == 0 {
			return st.tighten(pat, exactly(new(big.Rat)))
		}
	case opLe:
		if ratSign(v) < 0 {
			return st.tighten(pat, span{empty: true})
		}
		return st.tighten(pat, span{
			lo: ratNeg(v), hi: v, hasLo: true, hasHi: true,
		})
	case opLt:
		if ratSign(v) <= 0 {
			return st.tighten(pat, span{empty: true})
		}
		return st.tighten(pat, span{
			lo: ratNeg(v), hi: v, hasLo: true, hasHi: true,
			loOpen: true, hiOpen: true,
		})
	}
	return false
}

// absAndLiteral matches "abs x" against a numeric literal.
func absAndLiteral(absSide, litSide core.Exp,
) (*core.IDPat, *big.Rat, bool) {
	apply, ok := absSide.(*core.Apply)
	if !ok {
		return nil, nil, false
	}
	name := builtinName(apply.Fn)
	if name != absName && name != "Int.abs" &&
		name != "Real.abs" {
		return nil, nil, false
	}
	id, ok := apply.Arg.(*core.ID)
	if !ok {
		return nil, nil, false
	}
	lit, ok := litSide.(*core.Literal)
	if !ok {
		return nil, nil, false
	}
	v, ok := literalRat(lit)
	return id.Pat, v, ok
}

// propagateMultiply handles "(a) * (b) OP c" inequalities in the
// positive quadrant: one factor's bound divides through to the
// other.
func (st *fbbtState) propagateMultiply(c core.Exp) bool {
	a, b, op := comparisonOf(c)
	if a == nil || op == eqOpName {
		return false
	}
	prod, lit := a, b
	times1, times2 := timesFactors(prod)
	if times1.pat == nil {
		prod, lit = b, a
		op = reverseOp(op)
		times1, times2 = timesFactors(prod)
	}
	if times1.pat == nil || times2.pat == nil ||
		!st.knows(times1.pat) || !st.knows(times2.pat) {
		return false
	}
	litExp, ok := lit.(*core.Literal)
	if !ok {
		return false
	}
	cv, ok := literalRat(litExp)
	if !ok {
		return false
	}
	changed := st.divideThrough(op, times1, times2, cv)
	changed = st.divideThrough(op, times2, times1, cv) || changed
	return changed
}

// timesFactors decomposes a product's factors as linear terms
// with variables.
func timesFactors(e core.Exp) (linTerm, linTerm) {
	a, b := binaryCall(e, opTimes)
	if a == nil {
		return linTerm{}, linTerm{}
	}
	ta, tb := linearTermOf(a), linearTermOf(b)
	if !ta.ok || !tb.ok || ta.pat == nil || tb.pat == nil {
		return linTerm{}, linTerm{}
	}
	return ta, tb
}

// divideThrough tightens one factor by dividing the constant by
// the other factor's positive bound.
func (st *fbbtState) divideThrough(op string, self,
	other linTerm, cv *big.Rat,
) bool {
	o := *st.interval(other.pat)
	if o.empty {
		return false
	}
	switch op {
	case opLe, opLt:
		if !o.hasLo {
			return false
		}
		lo := ratAdd(o.lo, other.offset)
		if ratSign(lo) <= 0 {
			return false
		}
		return st.tighten(self.pat,
			lessThan(ratSub(ratDiv(cv, lo), self.offset)))
	case opGe, opGt:
		if !o.hasHi {
			return false
		}
		hi := ratAdd(o.hi, other.offset)
		if ratSign(hi) <= 0 {
			return false
		}
		return st.tighten(self.pat,
			moreThan(ratSub(ratDiv(cv, hi), self.offset)))
	default:
		return false
	}
}
