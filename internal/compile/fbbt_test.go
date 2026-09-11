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

//nolint:testpackage // white-box: fbbtStrengthen is unexported
package compile

import (
	"math/big"
	"testing"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// Tests for bound deduction. They test what FBBT deduces, not
// only what a query built on it returns, so that a deduction that
// is merely weaker than it should be -- which a query still
// answers correctly, only slower -- does not pass unnoticed.

// fbbtFixture is a type system and a few variables to write
// constraints over.
type fbbtFixture struct {
	sys        *types.System
	x, y, z, r *core.IDPat
}

func newFbbtFixture() *fbbtFixture {
	sys := types.NewSystem()
	return &fbbtFixture{
		sys: sys,
		x:   &core.IDPat{T: sys.Int, Name: "x"},
		y:   &core.IDPat{T: sys.Int, Name: "y"},
		z:   &core.IDPat{T: sys.Int, Name: "z"},
		r:   &core.IDPat{T: sys.Real, Name: "r"},
	}
}

func (f *fbbtFixture) id(pat *core.IDPat) core.Exp {
	return &core.ID{Pat: pat}
}

// i is an int literal.
func (f *fbbtFixture) i(n int32) core.Exp {
	return &core.Literal{
		T: f.sys.Int, Kind: ast.IntLiteralOp, Value: n,
	}
}

// re is a real literal.
func (f *fbbtFixture) re(v float32) core.Exp {
	return &core.Literal{
		T: f.sys.Real, Kind: ast.RealLiteralOp, Value: v,
	}
}

// call is an application of a binary operator to two operands.
func (f *fbbtFixture) call(op string, a, b core.Exp,
	t types.Type,
) core.Exp {
	pairT := f.sys.Tuple(a.Type(), b.Type())
	return &core.Apply{
		T: t,
		Fn: &core.ID{Pat: &core.IDPat{
			T: f.sys.Fn(pairT, t), Name: op,
		}},
		Arg: &core.Tuple{T: pairT, Args: []core.Exp{a, b}},
	}
}

func (f *fbbtFixture) cmp(op string, a, b core.Exp) core.Exp {
	return f.call(op, a, b, f.sys.Bool)
}

func (f *fbbtFixture) arith(op string, a, b core.Exp) core.Exp {
	return f.call(op, a, b, a.Type())
}

// abs is "abs e".
func (f *fbbtFixture) abs(e core.Exp) core.Exp {
	return &core.Apply{
		T: e.Type(),
		Fn: &core.ID{Pat: &core.IDPat{
			T: f.sys.Fn(e.Type(), e.Type()), Name: absName,
		}},
		Arg: e,
	}
}

// and conjoins the constraints, right-associated as the parser
// builds them.
func (f *fbbtFixture) and(exps ...core.Exp) core.Exp {
	return composeConjuncts(f.sys, exps)
}

// strengthen runs FBBT and renders the result, so that a test
// can say what was deduced rather than only that something was.
func (f *fbbtFixture) strengthen(where core.Exp,
	pats ...*core.IDPat,
) string {
	return f.text(fbbtStrengthen(f.sys, pats, where))
}

// text renders an expression the way a plan does.
func (f *fbbtFixture) text(e core.Exp) string {
	u := &unparser{sys: f.sys, seen: map[string][]*core.IDPat{}}
	u.exp(e, 0, 0)
	return u.sb.String()
}

func TestFbbt(t *testing.T) {
	f := newFbbtFixture()
	x, y, z, r := f.id(f.x), f.id(f.y), f.id(f.z), f.id(f.r)
	// times returns "n * e".
	times := func(n int32, e core.Exp) core.Exp {
		return f.arith(opTimes, f.i(n), e)
	}
	tests := []struct {
		name  string
		where core.Exp
		pats  []*core.IDPat
		want  string
	}{{
		// Bounds the extractor can already read are the baseline;
		// re-emitting them would be noise.
		name: "constant bounds deduce nothing",
		where: f.and(f.cmp(opGt, x, f.i(0)),
			f.cmp(opLt, x, f.i(10))),
		pats: []*core.IDPat{f.x},
		want: "x > 0 andalso x < 10",
	}, {
		// The issue's cyclic-bound example: each variable bounds
		// the other, and the constants reach both.
		name: "cyclic bound",
		where: f.and(f.cmp(opGt, x, f.i(0)), f.cmp(opLt, x, y),
			f.cmp(opLt, y, f.i(10))),
		pats: []*core.IDPat{f.x, f.y},
		want: "x < 10 andalso (y > 0 andalso (x > 0 andalso " +
			"(x < y andalso y < 10)))",
	}, {
		name:  "other variable untouched",
		where: f.cmp(opLt, y, f.i(5)),
		pats:  []*core.IDPat{f.x},
		want:  "y < 5",
	}, {
		// "abs x < 5" gives "~5 < x < 5", open at both ends
		// because "<" is strict.
		name:  "abs less than",
		where: f.cmp(opLt, f.abs(x), f.i(5)),
		pats:  []*core.IDPat{f.x},
		want:  "x > ~5 andalso (x < 5 andalso #abs Int x < 5)",
	}, {
		// A constraint with coefficients over two variables:
		// "3x + 5y = 30" with both non-negative.
		name: "coefficients",
		where: f.and(f.cmp(opGe, x, f.i(0)),
			f.cmp(opGe, y, f.i(0)),
			f.cmp(eqOpName,
				f.arith(opPlus, times(3, x), times(5, y)),
				f.i(30))),
		pats: []*core.IDPat{f.x, f.y},
		want: "x <= 10 andalso (y <= 6 andalso (x >= 0 andalso " +
			"(y >= 0 andalso 3 * x + 5 * y = 30)))",
	}, {
		// "3x <= 10" gives "x <= 3", not "x <= 10/3": an
		// endpoint that does not divide exactly snaps to the
		// integer inside it.
		name: "upper bound rounds down",
		where: f.and(f.cmp(opGe, x, f.i(0)),
			f.cmp(opLe, times(3, x), f.i(10))),
		pats: []*core.IDPat{f.x},
		want: "x <= 3 andalso (x >= 0 andalso 3 * x <= 10)",
	}, {
		name: "lower bound rounds up",
		where: f.and(f.cmp(opLe, x, f.i(100)),
			f.cmp(opGe, times(3, x), f.i(10))),
		pats: []*core.IDPat{f.x},
		want: "x >= 4 andalso (x <= 100 andalso 3 * x >= 10)",
	}, {
		// A negative coefficient bounds a variable from the
		// other side.
		name: "negative coefficient",
		where: f.and(f.cmp(opGe, y, f.i(0)),
			f.cmp(opLe, y, f.i(2)),
			f.cmp(opLe, f.arith(opMinus, x, y), f.i(5))),
		pats: []*core.IDPat{f.x, f.y},
		want: "x <= 7 andalso (y >= 0 andalso (y <= 2 andalso " +
			"x - y <= 5))",
	}, {
		// Constraints that contradict each other leave an empty
		// interval, which has no endpoints to emit. There is
		// nothing to deduce, and nothing to throw.
		name: "contradiction deduces nothing",
		where: f.and(f.cmp(opGt, x, f.i(5)),
			f.cmp(opLt, x, f.i(3)), f.cmp(opLt, y, x)),
		pats: []*core.IDPat{f.x, f.y},
		want: "x > 5 andalso (x < 3 andalso y < x)",
	}, {
		name: "contradiction via a sum",
		where: f.and(f.cmp(opGe, x, f.i(0)),
			f.cmp(opGe, y, f.i(0)),
			f.cmp(eqOpName, f.arith(opPlus, x, y), f.i(-1))),
		pats: []*core.IDPat{f.x, f.y},
		want: "x >= 0 andalso (y >= 0 andalso x + y = ~1)",
	}, {
		name:  "abs of an offset",
		where: f.cmp(opLt, f.abs(f.arith(opMinus, x, f.i(2))), f.i(5)),
		pats:  []*core.IDPat{f.x},
		want: "x > ~3 andalso (x < 7 andalso " +
			"#abs Int (x - 2) < 5)",
	}, {
		// A coefficient outside the absolute value works as one
		// inside.
		name: "coefficient outside abs",
		where: f.cmp(opLt,
			times(3, f.abs(f.arith(opMinus, x, f.i(2)))), f.i(9)),
		pats: []*core.IDPat{f.x},
		want: "x > ~1 andalso (x < 5 andalso " +
			"3 * #abs Int (x - 2) < 9)",
	}, {
		// A negative coefficient inside swaps the ends:
		// "abs (~3 * x) < 10" is "~10/3 < x < 10/3", and each end
		// snaps outwards to an integer.
		name:  "negative coefficient inside abs",
		where: f.cmp(opLt, f.abs(times(-3, x)), f.i(10)),
		pats:  []*core.IDPat{f.x},
		want: "x >= ~3 andalso (x <= 3 andalso " +
			"#abs Int (~3 * x) < 10)",
	}, {
		// The bound is exactly a ten-trillionth, which dividing at
		// twelve decimal places could only round to 1e-12, an
		// interval ten times too wide. Rationals divide exactly,
		// so the deduction is the true one; and the ends still
		// swap, the coefficient being negative.
		//
		// The literal that states it is a float32, which holds no
		// exact ten-trillionth, so each end rounds outwards to the
		// float32 beyond it. morel-java writes "1E-13" here
		// because its literal is a decimal; writing the nearest
		// float32 instead would put the endpoint just inside the
		// deduced interval and exclude a value that satisfies the
		// query.
		name: "large negative coefficient inside abs",
		where: f.cmp(opLt,
			f.abs(f.arith(opTimes, f.re(-1e13), r)), f.re(1.0)),
		pats: []*core.IDPat{f.r},
		want: "r > ~1.00000005e-13 andalso " +
			"(r < 1.00000005e-13 andalso " +
			"#abs Real (~1e+13 * r) < 1)",
	}, {
		// The argument of an absolute value must be linear in one
		// variable. This is not, so FBBT declines rather than
		// deducing something wrong.
		name:  "abs of a non-linear argument declines",
		where: f.cmp(opLt, f.abs(f.arith(opTimes, x, y)), f.i(5)),
		pats:  []*core.IDPat{f.x, f.y},
		want:  "#abs Int (x * y) < 5",
	}, {
		// A variable that a scan bound -- "z" here, which is not
		// one of the variables we are deducing for -- constrains
		// its neighbours and gets no bounds of its own.
		name: "a scan-bound variable constrains but is not bounded",
		where: f.and(f.cmp(opGe, z, f.i(1)),
			f.cmp(opLe, z, f.i(3)), f.cmp(opLt, x, z)),
		pats: []*core.IDPat{f.x},
		want: "x < 3 andalso (z >= 1 andalso (z <= 3 andalso " +
			"x < z))",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := f.strengthen(test.where, test.pats...)
			if got != test.want {
				t.Errorf("strengthen:\n got %s\nwant %s",
					got, test.want)
			}
		})
	}
}

// TestRat covers the rational arithmetic that bound deduction
// rests on: what it divides exactly, and how it rounds when a
// bound is finally written.
func TestRat(t *testing.T) {
	third := big.NewRat(1, 3)
	if got := ratMul(third, ratOf(3)); ratCmp(got, ratOf(1)) != 0 {
		t.Errorf("1/3 * 3 = %s, want 1", got.RatString())
	}
	if got := ratDiv(ratOf(10), ratOf(3)); got.RatString() != "10/3" {
		t.Errorf("10 / 3 = %s, want 10/3", got.RatString())
	}
	// A nil rational reads as zero, so a partly-built term is
	// harmless rather than a panic.
	if ratSign(nil) != 0 || ratCmp(nil, ratOf(0)) != 0 {
		t.Error("nil should read as zero")
	}
	floors := []struct {
		r           *big.Rat
		floor, ceil int64
	}{
		{big.NewRat(15, 2), 7, 8},
		{big.NewRat(-15, 2), -8, -7},
		{ratOf(4), 4, 4},
	}
	for _, c := range floors {
		if got := ratFloor(c.r).Int64(); got != c.floor {
			t.Errorf("floor %s = %d, want %d", c.r.RatString(),
				got, c.floor)
		}
		if got := ratCeil(c.r).Int64(); got != c.ceil {
			t.Errorf("ceil %s = %d, want %d", c.r.RatString(),
				got, c.ceil)
		}
	}
	// A third has no exact float32, so it rounds outwards: down
	// for a lower bound, up for an upper one, and the two differ.
	lo := ratFloat32(third, true)
	hi := ratFloat32(third, false)
	if !(ratCmp(new(big.Rat).SetFloat64(float64(lo)), third) < 0) {
		t.Errorf("lower %v should be below 1/3", lo)
	}
	if !(ratCmp(new(big.Rat).SetFloat64(float64(hi)), third) > 0) {
		t.Errorf("upper %v should be above 1/3", hi)
	}
	// A value a float32 holds exactly rounds to itself either way.
	half := big.NewRat(1, 2)
	if ratFloat32(half, true) != 0.5 || ratFloat32(half, false) != 0.5 {
		t.Error("1/2 should be exact")
	}
	// A bound too large for an int cannot be written.
	if _, ok := ratInt32(ratOf(1 << 40)); ok {
		t.Error("2^40 should not fit in an int")
	}
}
