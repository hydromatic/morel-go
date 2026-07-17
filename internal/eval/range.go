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

package eval

import "slices"

// The Range structure. A "range" value is a Con of the "range"
// datatype, one of ten interval shapes; a "continuous_set" or
// "discrete_set" wraps a normalized list of non-overlapping ranges
// in a CONTINUOUS_SET or DISCRETE_SET constructor.

const (
	rangeDatatype         = "range"
	continuousSetDatatype = "continuous_set"
	discreteSetDatatype   = "discrete_set"
	continuousSetCon      = "CONTINUOUS_SET"
	discreteSetCon        = "DISCRETE_SET"
)

// The range constructor names, in declaration order (which fixes
// their ordinals).
const (
	rConAll         = "ALL"
	rConAtLeast     = "AT_LEAST"
	rConAtMost      = "AT_MOST"
	rConClosed      = "CLOSED"
	rConClosedOpen  = "CLOSED_OPEN"
	rConGreaterThan = "GREATER_THAN"
	rConLessThan    = "LESS_THAN"
	rConOpen        = "OPEN"
	rConOpenClosed  = "OPEN_CLOSED"
	rConPoint       = "POINT"
)

var rangeConNames = []string{
	rConAll, rConAtLeast, rConAtMost, rConClosed, rConClosedOpen,
	rConGreaterThan, rConLessThan, rConOpen, rConOpenClosed, rConPoint,
}

// rangeCon builds a range value, taking the ordinal from the
// constructor's position in the declaration order.
func rangeCon(name string, arg Val) Con {
	return Con{
		Datatype: rangeDatatype,
		Name:     name,
		Arg:      arg,
		Ordinal:  slices.Index(rangeConNames, name),
	}
}

func continuousSetVal(ranges []Val) Con {
	return Con{
		Datatype: continuousSetDatatype,
		Name:     continuousSetCon,
		Arg:      ranges,
	}
}

func discreteSetVal(ranges []Val) Con {
	return Con{
		Datatype: discreteSetDatatype,
		Name:     discreteSetCon,
		Arg:      ranges,
	}
}

// setRangeList returns the list of ranges wrapped in a set value.
func setRangeList(v Val) []Val {
	con, _ := v.(Con)
	return asList(con.Arg)
}

// bound is one end of an interval. If inf, it is negative or
// positive infinity (an unbounded end); otherwise val is the
// endpoint and closed says whether it is included.
type bound struct {
	val    Val
	closed bool
	inf    bool
}

// interval is a range in bound form: lo.inf means unbounded below,
// hi.inf means unbounded above.
type interval struct {
	lo, hi bound
}

var infBound = bound{inf: true}

func closedBound(v Val) bound { return bound{val: v, closed: true} }
func openBound(v Val) bound   { return bound{val: v} }

// rangeToInterval converts a range value to bound form.
func rangeToInterval(r Val) interval {
	con, _ := r.(Con)
	// lint: sort until '^	}' where '^	case '
	switch con.Name {
	case rConAll:
		return interval{infBound, infBound}
	case rConAtLeast:
		return interval{closedBound(con.Arg), infBound}
	case rConAtMost:
		return interval{infBound, closedBound(con.Arg)}
	case rConClosed:
		lo, hi := asPair(con.Arg)
		return interval{closedBound(lo), closedBound(hi)}
	case rConClosedOpen:
		lo, hi := asPair(con.Arg)
		return interval{closedBound(lo), openBound(hi)}
	case rConGreaterThan:
		return interval{openBound(con.Arg), infBound}
	case rConLessThan:
		return interval{infBound, openBound(con.Arg)}
	case rConOpen:
		lo, hi := asPair(con.Arg)
		return interval{openBound(lo), openBound(hi)}
	case rConOpenClosed:
		lo, hi := asPair(con.Arg)
		return interval{openBound(lo), closedBound(hi)}
	case rConPoint:
		return interval{closedBound(con.Arg), closedBound(con.Arg)}
	default:
		panic("unknown range constructor: " + con.Name)
	}
}

// intervalToRange converts a bound-form interval back to the
// tightest range constructor.
func intervalToRange(iv interval) Val {
	switch {
	case iv.lo.inf && iv.hi.inf:
		return rangeCon(rConAll, nil)
	case iv.lo.inf:
		if iv.hi.closed {
			return rangeCon(rConAtMost, iv.hi.val)
		}
		return rangeCon(rConLessThan, iv.hi.val)
	case iv.hi.inf:
		if iv.lo.closed {
			return rangeCon(rConAtLeast, iv.lo.val)
		}
		return rangeCon(rConGreaterThan, iv.lo.val)
	}
	pair := []Val{iv.lo.val, iv.hi.val}
	switch {
	case iv.lo.closed && iv.hi.closed:
		if valsEqual(iv.lo.val, iv.hi.val) {
			return rangeCon(rConPoint, iv.lo.val)
		}
		return rangeCon(rConClosed, pair)
	case iv.lo.closed:
		return rangeCon(rConClosedOpen, pair)
	case iv.hi.closed:
		return rangeCon(rConOpenClosed, pair)
	default:
		return rangeCon(rConOpen, pair)
	}
}

func b2i(x bool) int {
	if x {
		return 1
	}
	return 0
}

// cmpBool orders two booleans with false before true.
func cmpBool(a, b bool) int { return b2i(a) - b2i(b) }

// cmpLower orders two lower bounds: unbounded is least, and for
// equal values a closed (inclusive) bound comes before an open one.
func cmpLower(a, b bound) int {
	if a.inf || b.inf {
		return cmpBool(b.inf, a.inf)
	}
	if c := compareVals(a.val, b.val); c != 0 {
		return c
	}
	return cmpBool(!a.closed, !b.closed)
}

// cmpUpper orders two upper bounds: unbounded is greatest, and for
// equal values a closed (inclusive) bound comes after an open one.
func cmpUpper(a, b bound) int {
	if a.inf || b.inf {
		return cmpBool(a.inf, b.inf)
	}
	if c := compareVals(a.val, b.val); c != 0 {
		return c
	}
	return cmpBool(a.closed, b.closed)
}

// succVal and predVal step to the next or previous discrete value.
// They succeed only for discrete types (int and char, both int32).
func succVal(v Val) (Val, bool) {
	if n, ok := v.(int32); ok {
		return n + 1, true
	}
	return nil, false
}

func predVal(v Val) (Val, bool) {
	if n, ok := v.(int32); ok {
		return n - 1, true
	}
	return nil, false
}

// firstVal is the least value included by a finite lower bound.
func firstVal(lo bound) Val {
	if lo.closed {
		return lo.val
	}
	v, _ := succVal(lo.val)
	return v
}

// lastVal is the greatest value included by a finite upper bound.
func lastVal(hi bound) Val {
	if hi.closed {
		return hi.val
	}
	v, _ := predVal(hi.val)
	return v
}

// emptyInterval reports an interval that contains no values, such
// as CLOSED (5, 3) or OPEN (3, 3).
func emptyInterval(iv interval) bool {
	if iv.lo.inf || iv.hi.inf {
		return false
	}
	c := compareVals(iv.lo.val, iv.hi.val)
	if c > 0 {
		return true
	}
	if c == 0 {
		return !iv.lo.closed || !iv.hi.closed
	}
	return false
}

// connected reports whether interval b, which sorts at or after a,
// overlaps or touches a, so the two should merge. When discrete is
// set, intervals with no discrete value between them also merge.
func connected(a, b interval, discrete bool) bool {
	if a.hi.inf || b.lo.inf {
		return true
	}
	c := compareVals(a.hi.val, b.lo.val)
	if c > 0 {
		return true
	}
	if c == 0 {
		return a.hi.closed || b.lo.closed
	}
	if !discrete {
		return false
	}
	last, first := lastVal(a.hi), firstVal(b.lo)
	if last == nil || first == nil {
		return false
	}
	s, ok := succVal(last)
	return ok && compareVals(s, first) >= 0
}

// normalizeRanges sorts and merges a list of ranges into a minimal
// list of disjoint ranges. Empty ranges are dropped.
func normalizeRanges(ranges []Val, discrete bool) []Val {
	ivs := make([]interval, 0, len(ranges))
	for _, r := range ranges {
		if iv := rangeToInterval(r); !emptyInterval(iv) {
			ivs = append(ivs, iv)
		}
	}
	slices.SortStableFunc(ivs, func(a, b interval) int {
		if c := cmpLower(a.lo, b.lo); c != 0 {
			return c
		}
		return cmpUpper(a.hi, b.hi)
	})
	out := make([]interval, 0, len(ivs))
	for _, iv := range ivs {
		if n := len(out); n > 0 && connected(out[n-1], iv, discrete) {
			if cmpUpper(iv.hi, out[n-1].hi) > 0 {
				out[n-1].hi = iv.hi
			}
			continue
		}
		out = append(out, iv)
	}
	result := make([]Val, len(out))
	for i, iv := range out {
		result[i] = intervalToRange(iv)
	}
	return result
}

// flipBound turns a bound into the complement bound at the same
// value: an included endpoint becomes excluded, and vice versa.
func flipBound(b bound) bound {
	return bound{val: b.val, closed: !b.closed}
}

// complementIntervals returns the complement of a normalized list
// of disjoint intervals, on the whole ordered line.
func complementIntervals(ivs []interval) []interval {
	out := make([]interval, 0, len(ivs)+1)
	prev := infBound
	for i, iv := range ivs {
		lo := prev
		if i > 0 {
			lo = flipBound(prev)
		}
		if !iv.lo.inf {
			out = append(out, interval{lo, flipBound(iv.lo)})
		}
		prev = iv.hi
	}
	switch {
	case len(ivs) == 0:
		out = append(out, interval{infBound, infBound})
	case !prev.inf:
		out = append(out, interval{flipBound(prev), infBound})
	}
	return out
}

// intervalContains reports whether x lies in the interval.
func intervalContains(iv interval, x Val) bool {
	if !iv.lo.inf {
		c := compareVals(x, iv.lo.val)
		if c < 0 || (c == 0 && !iv.lo.closed) {
			return false
		}
	}
	if !iv.hi.inf {
		c := compareVals(x, iv.hi.val)
		if c > 0 || (c == 0 && !iv.hi.closed) {
			return false
		}
	}
	return true
}

// enumerateInterval lists the discrete values in a bounded
// interval, in ascending order.
func enumerateInterval(iv interval) ([]Val, error) {
	if iv.lo.inf || iv.hi.inf {
		return nil, &MorelError{Exn: ExnDomain}
	}
	lo, ok1 := firstVal(iv.lo).(int32)
	hi, ok2 := lastVal(iv.hi).(int32)
	if !ok1 || !ok2 {
		return nil, &MorelError{Exn: ExnDomain}
	}
	out := make([]Val, 0, max(0, int(hi)-int(lo)+1))
	for i := lo; i <= hi; i++ {
		out = append(out, i)
	}
	return out, nil
}

// enumerateRanges concatenates the enumerated values of each range,
// in input order.
func enumerateRanges(ranges []Val) (Val, error) {
	out := []Val{}
	for _, r := range ranges {
		vs, err := enumerateInterval(rangeToInterval(r))
		if err != nil {
			return nil, err
		}
		out = append(out, vs...)
	}
	return out, nil
}

// rangeContainsFn is "Range.contains r x": whether x lies in r.
var rangeContainsFn = Curry2(func(r, x Val) (Val, error) {
	return intervalContains(rangeToInterval(r), x), nil
})

// rangeContinuousSetOfFn is "Range.continuousSetOf": normalize a
// list of ranges into a continuous set, merging overlapping ranges.
func rangeContinuousSetOfFn(arg Val) (Val, error) {
	return continuousSetVal(normalizeRanges(asList(arg), false)), nil
}

// rangeDiscreteSetOfFn is "Range.discreteSetOf": as continuousSetOf,
// but also merging adjacent ranges.
func rangeDiscreteSetOfFn(arg Val) (Val, error) {
	return discreteSetVal(normalizeRanges(asList(arg), true)), nil
}

// rangeFlattenFn is "Range.flatten": concatenate the values of each
// range, in input order, without merging.
func rangeFlattenFn(arg Val) (Val, error) {
	return enumerateRanges(asList(arg))
}

// rangeRangesFn is "Range.ranges cs": the list of ranges in a set.
func rangeRangesFn(arg Val) (Val, error) {
	return setRangeList(arg), nil
}

// rangeToListFn is "Range.toList ds": the values of a discrete set,
// in ascending order.
func rangeToListFn(arg Val) (Val, error) {
	return enumerateRanges(setRangeList(arg))
}

// rangeToBagFn is "Range.toBag ds": the values of a discrete set as
// a bag.
func rangeToBagFn(arg Val) (Val, error) {
	return enumerateRanges(setRangeList(arg))
}

// rangeComplementFn is "Range.complement cs": the continuous set of
// everything not in cs.
func rangeComplementFn(arg Val) (Val, error) {
	ranges := setRangeList(arg)
	ivs := make([]interval, len(ranges))
	for i, r := range ranges {
		ivs[i] = rangeToInterval(r)
	}
	comp := complementIntervals(ivs)
	out := make([]Val, len(comp))
	for i, iv := range comp {
		out[i] = intervalToRange(iv)
	}
	return continuousSetVal(out), nil
}
