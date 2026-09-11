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
	"math"
	"math/big"
)

// Exact rational arithmetic, for deducing bounds.
//
// Bound deduction divides -- by a coefficient, and again in the
// multiply propagator -- and a division in binary floating point
// is not exact. An inexact endpoint is not merely imprecise: if it
// rounds the wrong way the bound is tighter than the truth, and a
// row the query should return is never generated. Rounding
// outwards at every step would keep it sound, but only by making
// each bound weaker than the arithmetic had computed.
//
// So the interval engine works in exact rationals throughout, and
// a bound is rounded once, where it is written: to the tightest
// integer for an int variable, outwards for a real one. See
// boundConjunct. morel-java's BigRational and morel-rust's Rat do
// the same, so the three deduce the same bounds.
//
// A nil rational reads as zero, so that a partly-built term is
// harmless rather than a panic. Rationals are never modified in
// place: every helper here returns a new one, and a span may share
// an endpoint with another.

// zeroRat is nil read as zero.
func zeroRat(r *big.Rat) *big.Rat {
	if r == nil {
		return new(big.Rat)
	}
	return r
}

// ratOf is a whole number as a rational.
func ratOf(i int64) *big.Rat { return new(big.Rat).SetInt64(i) }

func ratAdd(a, b *big.Rat) *big.Rat {
	return new(big.Rat).Add(zeroRat(a), zeroRat(b))
}

func ratSub(a, b *big.Rat) *big.Rat {
	return new(big.Rat).Sub(zeroRat(a), zeroRat(b))
}

func ratMul(a, b *big.Rat) *big.Rat {
	return new(big.Rat).Mul(zeroRat(a), zeroRat(b))
}

// ratDiv is exact; the divisor must be non-zero.
func ratDiv(a, b *big.Rat) *big.Rat {
	return new(big.Rat).Quo(zeroRat(a), zeroRat(b))
}

func ratNeg(a *big.Rat) *big.Rat {
	return new(big.Rat).Neg(zeroRat(a))
}

func ratAbs(a *big.Rat) *big.Rat {
	return new(big.Rat).Abs(zeroRat(a))
}

// ratCmp orders two rationals, either of which may be nil.
func ratCmp(a, b *big.Rat) int {
	return zeroRat(a).Cmp(zeroRat(b))
}

// ratSign is -1, 0 or 1.
func ratSign(a *big.Rat) int { return zeroRat(a).Sign() }

func ratMax(a, b *big.Rat) *big.Rat {
	if ratCmp(a, b) >= 0 {
		return zeroRat(a)
	}
	return zeroRat(b)
}

// ratFloor is the greatest integer no greater than the rational.
// big.Rat keeps a positive denominator, and big.Int's Div rounds
// towards negative infinity for a positive divisor, so this is
// the floor rather than a truncation.
func ratFloor(r *big.Rat) *big.Int {
	r = zeroRat(r)
	return new(big.Int).Div(r.Num(), r.Denom())
}

// ratCeil is the least integer no less than the rational.
func ratCeil(r *big.Rat) *big.Int {
	return new(big.Int).Neg(ratFloor(ratNeg(r)))
}

// ratInt32 is a whole rational as an int32, and whether it fits.
// A bound that does not fit is dropped rather than wrapped: an
// out-of-range bound constrains an int variable no better than no
// bound at all, and a wrapped one would constrain it wrongly.
func ratInt32(r *big.Rat) (int32, bool) {
	if !zeroRat(r).IsInt() {
		return 0, false
	}
	return bigInt32(zeroRat(r).Num())
}

// bigInt32 narrows an integer to an int32, and reports whether it
// fits.
func bigInt32(i *big.Int) (int32, bool) {
	if !i.IsInt64() {
		return 0, false
	}
	n := i.Int64()
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, false
	}
	return int32(n), true
}

// ratFloat32 rounds a rational to the nearest float32, then
// outwards if that was not exact -- down for a lower bound, up for
// an upper one -- so that the bound written is never tighter than
// the one deduced.
func ratFloat32(r *big.Rat, lower bool) float32 {
	f, exact := zeroRat(r).Float32()
	if exact || math.IsInf(float64(f), 0) {
		return f
	}
	rounded := new(big.Rat).SetFloat64(float64(f))
	if rounded == nil {
		return f
	}
	switch {
	case lower && rounded.Cmp(zeroRat(r)) > 0:
		return math.Nextafter32(f, float32(math.Inf(-1)))
	case !lower && rounded.Cmp(zeroRat(r)) < 0:
		return math.Nextafter32(f, float32(math.Inf(1)))
	default:
		return f
	}
}
