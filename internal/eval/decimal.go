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

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// The Decimal structure. A "decimal" value is exact decimal floating
// point in the IEEE 754-2008 decimal128 format: 34 significant
// digits, radix 10, rounding half-even.

const (
	// DecPrecision is the number of significant digits, 34.
	DecPrecision = 34
	// decEMax is the largest adjusted exponent, 6144.
	decEMax = 6144
	// decMaxScale is the largest scale, 6176: the least significant
	// digit of a value is never smaller than 10^-6176.
	decMaxScale = 6176
	// decMaxParsedExponent bounds the exponent we read from a
	// string. Any exponent this large overflows or underflows, so
	// clamping it loses nothing and keeps the arithmetic small.
	decMaxParsedExponent = 100_000
	decBase              = 10
)

// Decimal is a value of the "decimal" type, in canonical form:
// the value is digits * 10^exp, negated if neg.
//
// Canonical form means one representation per value, so two decimals
// are numerically equal exactly when the structs are equal, and a
// decimal can be a map key. It requires that digits has no leading
// zero and no trailing zero, that it is at most DecPrecision long,
// and that zero is the single value with empty digits, exp 0 and neg
// false.
type Decimal struct {
	neg    bool
	digits string
	exp    int32
}

// DecZero is the decimal 0.
var DecZero = Decimal{}

// decMaxFinite is Decimal.maxFinite,
// 9.999999999999999999999999999999999E6144.
var decMaxFinite = Decimal{
	digits: strings.Repeat("9", DecPrecision),
	exp:    decEMax - DecPrecision + 1,
}

// decMinPos is Decimal.minPos, 1E~6176.
var decMinPos = Decimal{digits: "1", exp: -decMaxScale}

// decPattern is the syntax of a decimal: an optional sign, digits
// with an optional decimal point, and an optional exponent.
var decPattern = regexp.MustCompile(
	`^([~+-]?)([0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE]([~+-]?)([0-9]+))?`)

// String renders a decimal with "~" for a negative value or
// exponent.
func (d Decimal) String() string { return d.ToString('~') }

// ToString renders a decimal, using negation for a negative value or
// exponent. It uses plain notation if the adjusted exponent is in
// [~7, 34), scientific notation otherwise; for example "12.3",
// "1200", "1E100", "1E~8".
func (d Decimal) ToString(negation byte) string {
	if d.isZero() {
		return "0"
	}
	var b strings.Builder
	if d.neg {
		b.WriteByte(negation)
	}
	const minPlainExp = -7
	if adjusted := d.adjExp(); adjusted < minPlainExp ||
		adjusted >= DecPrecision {
		b.WriteByte(d.digits[0])
		if len(d.digits) > 1 {
			b.WriteByte('.')
			b.WriteString(d.digits[1:])
		}
		b.WriteByte('E')
		if adjusted < 0 {
			b.WriteByte(negation)
			adjusted = -adjusted
		}
		b.WriteString(strconv.Itoa(adjusted))
		return b.String()
	}
	switch k := int(-d.exp); {
	case k <= 0:
		b.WriteString(d.digits)
		b.WriteString(strings.Repeat("0", -k))
	case k < len(d.digits):
		b.WriteString(d.digits[:len(d.digits)-k])
		b.WriteByte('.')
		b.WriteString(d.digits[len(d.digits)-k:])
	default:
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", k-len(d.digits)))
		b.WriteString(d.digits)
	}
	return b.String()
}

// isZero reports whether a decimal is zero.
func (d Decimal) isZero() bool { return d.digits == "" }

// adjExp is the exponent the value has in scientific notation: 0 for
// [1, 10), 1 for [10, 100), -1 for [0.1, 1).
func (d Decimal) adjExp() int { return len(d.digits) - 1 + int(d.exp) }

// unscaled is the signed integer that, scaled by 10^exp, is the
// value.
func (d Decimal) unscaled() *big.Int {
	if d.isZero() {
		return big.NewInt(0)
	}
	i, _ := new(big.Int).SetString(d.digits, decBase)
	if d.neg {
		i.Neg(i)
	}
	return i
}

// scaledTo is the signed integer that, scaled by 10^exp, is the
// value; exp must be no greater than the decimal's own.
func (d Decimal) scaledTo(exp int32) *big.Int {
	i := d.unscaled()
	if n := int(d.exp - exp); n > 0 {
		i.Mul(i, decPow10(n))
	}
	return i
}

// decPow10 is 10^n.
func decPow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(decBase), big.NewInt(int64(n)), nil)
}

// decCanonical converts unscaled * 10^exp to canonical form,
// reporting false if the magnitude is too large to represent. It
// rounds half-even to DecPrecision significant digits, truncates
// toward zero a value too small to represent, and strips trailing
// zeros.
func decCanonical(unscaled *big.Int, exp int) (Decimal, bool) {
	if unscaled.Sign() == 0 {
		return DecZero, true
	}
	neg := unscaled.Sign() < 0
	digits := new(big.Int).Abs(unscaled).String()
	switch adjusted := len(digits) - 1 + exp; {
	case adjusted > decEMax:
		return DecZero, false
	case adjusted < -decMaxScale:
		// Too small for even the smallest subnormal.
		return DecZero, true
	}
	if scale := -exp; scale > decMaxScale {
		// Truncate toward zero to the smallest representable digit.
		drop := scale - decMaxScale
		if drop >= len(digits) {
			return DecZero, true
		}
		digits = digits[:len(digits)-drop]
		exp = -decMaxScale
	}
	if len(digits) > DecPrecision {
		drop := len(digits) - DecPrecision
		rounded, adj := roundSig(digits, DecPrecision, true)
		digits, exp = rounded, exp+drop+adj
	}
	digits, exp = decStrip(digits, exp)
	if digits == zeroDigits {
		return DecZero, true
	}
	// Rounding up may have raised the exponent, as 9.99...9E6144 to
	// 1E6145.
	if len(digits)-1+exp > decEMax {
		return DecZero, false
	}
	//nolint:gosec // the checks above bound exp to [-6176, 6144].
	return Decimal{neg: neg, digits: digits, exp: int32(exp)}, true
}

// decStrip removes trailing zeros from a digit string, raising the
// exponent to compensate.
func decStrip(digits string, exp int) (string, int) {
	for len(digits) > 1 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		exp++
	}
	return digits, exp
}

// decOverflow is the result of an operation whose value is too large.
func decOverflow() (Val, error) {
	return nil, &MorelError{Exn: ExnOverflow}
}

// decOf wraps the result of decCanonical as a built-in's result,
// raising Overflow if the value is too large.
func decOf(d Decimal, ok bool) (Val, error) {
	if !ok {
		return decOverflow()
	}
	return d, nil
}

// negate returns a decimal with the opposite sign. Zero negates to
// itself, because there is no negative zero.
func (d Decimal) negate() Decimal {
	if d.isZero() {
		return d
	}
	d.neg = !d.neg
	return d
}

// abs returns the magnitude of a decimal.
func (d Decimal) abs() Decimal {
	d.neg = false
	return d
}

// decAdd adds two decimals.
func decAdd(a, b Decimal) (Decimal, bool) {
	exp := min(a.exp, b.exp)
	sum := new(big.Int).Add(a.scaledTo(exp), b.scaledTo(exp))
	return decCanonical(sum, int(exp))
}

// decMul multiplies two decimals.
func decMul(a, b Decimal) (Decimal, bool) {
	product := new(big.Int).Mul(a.unscaled(), b.unscaled())
	return decCanonical(product, int(a.exp)+int(b.exp))
}

// decDiv divides two decimals, rounding half-even to DecPrecision
// significant digits. The second result is false on overflow; the
// error is Div if the divisor is zero.
func decDiv(a, b Decimal) (Decimal, bool, error) {
	if b.isZero() {
		return DecZero, true, &MorelError{Exn: ExnDiv}
	}
	if a.isZero() {
		return DecZero, true, nil
	}
	// Compute two digits more than the precision, so that rounding
	// has a digit to inspect and one to spare.
	const extra = 2
	scale := DecPrecision + extra - len(a.digits) + len(b.digits)
	num := new(big.Int).Mul(a.unscaled(), decPow10(scale))
	quo, rem := new(big.Int).QuoRem(num, b.unscaled(), new(big.Int))
	if rem.Sign() != 0 {
		// The quotient is inexact. A tie at the rounding place must
		// break upward rather than to even, and the only way a tie
		// can arise is a dropped tail of zeros, so it is enough to
		// make the last digit nonzero.
		if new(big.Int).Mod(quo, big.NewInt(decBase)).Sign() == 0 {
			if quo.Sign() < 0 {
				quo.Sub(quo, big.NewInt(1))
			} else {
				quo.Add(quo, big.NewInt(1))
			}
		}
	}
	d, ok := decCanonical(quo, int(a.exp)-int(b.exp)-scale)
	return d, ok, nil
}

// decRem is the remainder of a division truncated toward zero, so it
// has the sign of the dividend. The error is Div if the divisor is
// zero.
func decRem(a, b Decimal) (Decimal, bool, error) {
	if b.isZero() {
		return DecZero, true, &MorelError{Exn: ExnDiv}
	}
	exp := min(a.exp, b.exp)
	x, y := a.scaledTo(exp), b.scaledTo(exp)
	rem := new(big.Int).Rem(x, y)
	d, ok := decCanonical(rem, int(exp))
	return d, ok, nil
}

// decCmp compares two decimals, returning -1, 0 or 1. Canonical form
// makes this exact without any arithmetic: the sign decides, then the
// exponent in scientific notation, then the digits.
func decCmp(a, b Decimal) int {
	switch {
	case a.isZero() && b.isZero():
		return 0
	case a.isZero():
		return negIf(!b.neg)
	case b.isZero():
		return negIf(a.neg)
	case a.neg != b.neg:
		return negIf(a.neg)
	}
	c := decCmpMag(a, b)
	if a.neg {
		return -c
	}
	return c
}

// negIf returns -1 if b, otherwise 1. It keeps the comparison above
// free of repeated branches.
func negIf(b bool) int {
	if b {
		return -1
	}
	return 1
}

// decCmpMag compares the magnitudes of two non-zero decimals.
func decCmpMag(a, b Decimal) int {
	if c := a.adjExp() - b.adjExp(); c != 0 {
		return negIf(c < 0)
	}
	// Equal scientific exponents, so the digit strings decide, the
	// shorter padded with the zeros canonical form removed.
	x, y := a.digits, b.digits
	if n := len(x) - len(y); n < 0 {
		x += strings.Repeat("0", -n)
	} else if n > 0 {
		y += strings.Repeat("0", n)
	}
	return strings.Compare(x, y)
}

// decRoundMode is how a decimal is rounded to an integer.
type decRoundMode int

const (
	decTrunc decRoundMode = iota
	decFloor
	decCeil
	decHalfEven
)

// roundToInt rounds a decimal to an integral decimal.
func (d Decimal) roundToInt(mode decRoundMode) Decimal {
	if d.exp >= 0 {
		// Already integral.
		return d
	}
	pow := decPow10(int(-d.exp))
	mag, _ := new(big.Int).SetString(d.digits, decBase)
	quo, rem := new(big.Int).QuoRem(mag, pow, new(big.Int))
	if decRoundUp(mode, d.neg, quo, rem, pow) {
		quo.Add(quo, big.NewInt(1))
	}
	if d.neg {
		quo.Neg(quo)
	}
	// An integral value always fits, so the overflow result cannot
	// arise.
	result, _ := decCanonical(quo, 0)
	return result
}

// decRoundUp reports whether rounding a magnitude quo with remainder
// rem out of pow, of the given sign, increases the magnitude.
func decRoundUp(mode decRoundMode, neg bool, quo, rem, pow *big.Int) bool {
	if rem.Sign() == 0 {
		return false
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch mode {
	case decCeil:
		return !neg
	case decFloor:
		return neg
	case decTrunc:
		return false
	default:
		// Half-even: the remainder against half of the divisor.
		twice := new(big.Int).Lsh(rem, 1)
		switch twice.Cmp(pow) {
		case 1:
			return true
		case 0:
			return quo.Bit(0) == 1
		default:
			return false
		}
	}
}

// toInt converts an integral decimal to an int, raising Overflow if
// it does not fit.
func (d Decimal) toInt() (Val, error) {
	i := d.unscaled()
	if d.exp > 0 {
		i.Mul(i, decPow10(int(d.exp)))
	}
	if !i.IsInt64() {
		return decOverflow()
	}
	n := i.Int64()
	if n < math.MinInt32 || n > math.MaxInt32 {
		return decOverflow()
	}
	return int32(n), nil
}

// decParse converts a match of decPattern to an unscaled value and
// an exponent.
func decParse(m []string) (*big.Int, int) {
	mantissa := m[2]
	point := strings.IndexByte(mantissa, '.')
	frac := 0
	if point >= 0 {
		frac = len(mantissa) - point - 1
		mantissa = mantissa[:point] + mantissa[point+1:]
	}
	if mantissa == "" {
		mantissa = zeroDigits
	}
	unscaled, _ := new(big.Int).SetString(mantissa, decBase)
	if m[1] != "" && m[1] != "+" {
		unscaled.Neg(unscaled)
	}
	exp := -frac
	if m[4] != "" {
		e := decMaxParsedExponent
		const maxExponentDigits = 6
		if len(m[4]) <= maxExponentDigits {
			e, _ = strconv.Atoi(m[4])
			e = min(e, decMaxParsedExponent)
		}
		if m[3] != "" && m[3] != "+" {
			e = -e
		}
		exp += e
	}
	return unscaled, exp
}

// DecParseExact parses a string that is exactly a decimal and whose
// value is exactly representable, which is what the "decimal"
// function requires. The second result is false if the string is
// malformed, has more than DecPrecision significant digits, or is out
// of range.
func DecParseExact(s string) (Decimal, bool) {
	m := decPattern.FindStringSubmatch(s)
	if m == nil || m[0] != s {
		return DecZero, false
	}
	unscaled, exp := decParse(m)
	if unscaled.Sign() == 0 {
		return DecZero, true
	}
	digits, exp := decStrip(new(big.Int).Abs(unscaled).String(), exp)
	if len(digits) > DecPrecision ||
		len(digits)-1+exp > decEMax ||
		exp < -decMaxScale {
		return DecZero, false
	}
	//nolint:gosec // the tests above bound exp to [-6176, 6144].
	return Decimal{
		neg:    unscaled.Sign() < 0,
		digits: digits,
		exp:    int32(exp),
	}, true
}

// decParsePrefix parses a decimal from a prefix of a string, after
// skipping leading whitespace. The value is not rounded and may be
// too large to represent; pass it to decCanonical. The third result
// is false if there is no decimal.
func decParsePrefix(s string) (*big.Int, int, bool) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' ||
		s[i] == '\r' || s[i] == '\v' || s[i] == '\f') {
		i++
	}
	m := decPattern.FindStringSubmatch(s[i:])
	if m == nil {
		return nil, 0, false
	}
	unscaled, exp := decParse(m)
	return unscaled, exp, true
}

// asDecimal reads a decimal argument.
func asDecimal(v Val) Decimal {
	d, _ := v.(Decimal)
	return d
}

// asDecimalPair reads a pair of decimal arguments.
func asDecimalPair(arg Val) (Decimal, Decimal) {
	a, b := asPair(arg)
	return asDecimal(a), asDecimal(b)
}

// decimalFn is "decimal s", and "Decimal.decimal s": the value the
// string denotes exactly. A string that is not exactly a decimal
// raises Domain. A string literal is converted at compile time
// instead, and is a compile error.
func decimalFn(arg Val) (Val, error) {
	d, ok := DecParseExact(asString(arg))
	if !ok {
		return nil, &MorelError{Exn: ExnDomain}
	}
	return d, nil
}

// decAddFn is "Decimal.+ (x, y)".
func decAddFn(arg Val) (Val, error) {
	return decOf(decAdd(asDecimalPair(arg)))
}

// decSubFn is "Decimal.- (x, y)".
func decSubFn(arg Val) (Val, error) {
	a, b := asDecimalPair(arg)
	return decOf(decAdd(a, b.negate()))
}

// decMulFn is "Decimal.* (x, y)".
func decMulFn(arg Val) (Val, error) {
	return decOf(decMul(asDecimalPair(arg)))
}

// decDivFn is "Decimal./ (x, y)"; Div on division by zero.
func decDivFn(arg Val) (Val, error) {
	a, b := asDecimalPair(arg)
	d, ok, err := decDiv(a, b)
	if err != nil {
		return nil, err
	}
	return decOf(d, ok)
}

// decRemFn is "Decimal.rem (x, y)"; Div on division by zero.
func decRemFn(arg Val) (Val, error) {
	a, b := asDecimalPair(arg)
	d, ok, err := decRem(a, b)
	if err != nil {
		return nil, err
	}
	return decOf(d, ok)
}

// decNegateFn is "Decimal.~ x".
func decNegateFn(arg Val) (Val, error) {
	return asDecimal(arg).negate(), nil
}

// decAbsFn is "Decimal.abs x".
func decAbsFn(arg Val) (Val, error) {
	return asDecimal(arg).abs(), nil
}

// decMinFn is "Decimal.min (x, y)".
func decMinFn(arg Val) (Val, error) {
	a, b := asDecimalPair(arg)
	if decCmp(a, b) <= 0 {
		return a, nil
	}
	return b, nil
}

// decMaxFn is "Decimal.max (x, y)".
func decMaxFn(arg Val) (Val, error) {
	a, b := asDecimalPair(arg)
	if decCmp(a, b) >= 0 {
		return a, nil
	}
	return b, nil
}

// decSignFn is "Decimal.sign x": ~1, 0 or 1.
func decSignFn(arg Val) (Val, error) {
	d := asDecimal(arg)
	switch {
	case d.isZero():
		return int32(0), nil
	case d.neg:
		return int32(-1), nil
	default:
		return int32(1), nil
	}
}

// decCompareFn is "Decimal.compare (x, y)".
func decCompareFn(arg Val) (Val, error) {
	a, b := asDecimalPair(arg)
	return orderVal(decCmp(a, b)), nil
}

// decCompareOp adapts an ordering test to one of the comparison
// operators of the Decimal structure.
func decCompareOp(test func(c int) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asDecimalPair(arg)
		return test(decCmp(a, b)), nil
	}
}

// decRoundFn adapts a rounding mode to "Decimal.realFloor" and its
// three companions, which round to an integral decimal.
func decRoundFn(mode decRoundMode) Fn {
	return func(arg Val) (Val, error) {
		return asDecimal(arg).roundToInt(mode), nil
	}
}

// decRoundIntFn adapts a rounding mode to "Decimal.floor" and its
// three companions, which round to an int; Overflow if the result
// does not fit.
func decRoundIntFn(mode decRoundMode) Fn {
	return func(arg Val) (Val, error) {
		return asDecimal(arg).roundToInt(mode).toInt()
	}
}

// decFromIntFn is "Decimal.fromInt i".
func decFromIntFn(arg Val) (Val, error) {
	d, _ := decCanonical(big.NewInt(int64(asInt(arg))), 0)
	return d, nil
}

// decFromRealFn is "Decimal.fromReal r": the shortest decimal that
// converts back to r. An infinity raises Overflow and a NaN Domain,
// neither having a decimal value.
func decFromRealFn(arg Val) (Val, error) {
	f := float64(asReal(arg))
	if math.IsNaN(f) {
		return nil, &MorelError{Exn: ExnDomain}
	}
	if math.IsInf(f, 0) {
		return decOverflow()
	}
	d, ok := DecParseExact(strconv.FormatFloat(f, 'E', -1, 32))
	if !ok {
		return decOverflow()
	}
	return d, nil
}

// decToRealFn is "Decimal.toReal d": the nearest real, which is an
// infinity if the value is too large.
func decToRealFn(arg Val) (Val, error) {
	d := asDecimal(arg)
	if d.isZero() {
		return float32(0), nil
	}
	s := d.digits + "E" + strconv.Itoa(int(d.exp))
	if d.neg {
		s = "-" + s
	}
	// A value out of range parses as an infinity, with an error that
	// says so; that is the answer we want.
	f, _ := strconv.ParseFloat(s, 32)
	return float32(f), nil
}

// decToStringFn is "Decimal.toString d".
func decToStringFn(arg Val) (Val, error) {
	return asDecimal(arg).String(), nil
}

// decFromStringFn is "Decimal.fromString s": the decimal a prefix of
// s denotes, rounded to DecPrecision digits, or NONE if there is
// none. Overflow if the value is too large.
func decFromStringFn(arg Val) (Val, error) {
	unscaled, exp, ok := decParsePrefix(asString(arg))
	if !ok {
		return noneVal, nil
	}
	d, ok := decCanonical(unscaled, exp)
	if !ok {
		return decOverflow()
	}
	return someVal(d), nil
}

// decFmtFn is "Decimal.fmt spec": it validates the spec, as
// "Real.fmt" does, and returns the function that renders a decimal in
// that style. Ties round half-even, as decimal arithmetic does.
func decFmtFn(spec Val) (Val, error) {
	if fmtSpecBad(spec) {
		return nil, &MorelError{Exn: ExnSize}
	}
	return Fn(func(arg Val) (Val, error) {
		return decFmt(spec, asDecimal(arg)), nil
	}), nil
}

// decFmt renders a decimal in the given realfmt style.
func decFmt(spec Val, d Decimal) string {
	kind, n := parseFmtSpec(spec)
	digits, exp := d.digits, d.adjExp()
	if d.isZero() {
		digits, exp = zeroDigits, 0
	}
	var body string
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case exactKind:
		body = formatExact(digits, exp)
	case fixKind:
		body = formatFix(digits, exp, n, true)
	case genKind:
		body = formatGen(digits, exp, n, true)
	case sciKind:
		body = formatSci(digits, exp, n, true)
	}
	if d.neg {
		return "~" + body
	}
	return body
}

// FormatDecimal renders a decimal value for display.
func FormatDecimal(v Val) string {
	return asDecimal(v).String()
}

// DecimalToString renders a decimal value, using negation for a
// negative value or exponent.
func DecimalToString(v Val, negation byte) string {
	return asDecimal(v).ToString(negation)
}

// decAddD, decSubD, decMulD and decDivD are the decimal cases of the
// overloaded operators "+", "-", "*" and "/".
func decAddD(a, b Decimal) (Val, error) { return decOf(decAdd(a, b)) }

func decSubD(a, b Decimal) (Val, error) {
	return decOf(decAdd(a, b.negate()))
}

func decMulD(a, b Decimal) (Val, error) { return decOf(decMul(a, b)) }

func decDivD(a, b Decimal) (Val, error) {
	d, ok, err := decDiv(a, b)
	if err != nil {
		return nil, err
	}
	return decOf(d, ok)
}

// decSum adds a list of decimals exactly, then rounds once. Overflow
// if the total is too large.
func decSum(list []Val) (Val, error) {
	exp := int32(0)
	first := true
	for _, v := range list {
		d := asDecimal(v)
		if d.isZero() {
			continue
		}
		if first || d.exp < exp {
			exp, first = d.exp, false
		}
	}
	if first {
		// Every term is zero, so no exponent was chosen.
		return DecZero, nil
	}
	total := new(big.Int)
	for _, v := range list {
		if d := asDecimal(v); !d.isZero() {
			total.Add(total, d.scaledTo(exp))
		}
	}
	return decOf(decCanonical(total, int(exp)))
}
