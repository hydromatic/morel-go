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
	"strconv"
	"strings"
)

// The Real structure, and the real1/real2 helpers that the Math
// structure's registry entries reuse (Math has no code of its
// own). Everything computes in float64 and rounds once to
// float32, so intermediate rounding never compounds.

func asReal(v Val) float32 {
	f, ok := v.(float32)
	if !ok {
		panic("expected real")
	}
	return f
}

func asRealPair(arg Val) (float64, float64) {
	a, b := asPair(arg)
	return float64(asReal(a)), float64(asReal(b))
}

// real1 adapts a float64 function of one argument.
func real1(f func(a float64) float64) Fn {
	return func(arg Val) (Val, error) {
		return float32(f(float64(asReal(arg)))), nil
	}
}

// real2 adapts a float64 function of a pair.
func real2(f func(a, b float64) float64) Fn {
	return func(arg Val) (Val, error) {
		a, b := asRealPair(arg)
		return float32(f(a, b)), nil
	}
}

// realPredicate adapts a float64 predicate.
func realPredicate(f func(a float64) bool) Fn {
	return func(arg Val) (Val, error) {
		return f(float64(asReal(arg))), nil
	}
}

// realPairPredicate adapts a float64 predicate of a pair.
func realPairPredicate(f func(a, b float64) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asRealPair(arg)
		return f(a, b), nil
	}
}

// realCompareFn is "Real.compare (a, b)"; it raises Unordered if
// either argument is NaN.
func realCompareFn(arg Val) (Val, error) {
	a, b := asRealPair(arg)
	if math.IsNaN(a) || math.IsNaN(b) {
		return nil, &MorelError{Exn: "Unordered"}
	}
	switch {
	case a < b:
		return orderVal(-1), nil
	case a > b:
		return orderVal(1), nil
	default:
		return orderVal(0), nil
	}
}

// realSignFn is "Real.sign r": ~1, 0, or 1; NaN raises Domain.
func realSignFn(arg Val) (Val, error) {
	r := float64(asReal(arg))
	switch {
	case math.IsNaN(r):
		return nil, &MorelError{Exn: ExnDomain}
	case r < 0:
		return int32(-1), nil
	case r > 0:
		return int32(1), nil
	default:
		return int32(0), nil
	}
}

// realMinFn is "Real.min (a, b)"; if one argument is NaN, the
// other.
func realMinFn(arg Val) (Val, error) {
	a, b := asRealPair(arg)
	switch {
	case math.IsNaN(a):
		return float32(b), nil
	case math.IsNaN(b):
		return float32(a), nil
	default:
		return float32(math.Min(a, b)), nil
	}
}

// realMaxFn is "Real.max (a, b)"; if one argument is NaN, the
// other.
func realMaxFn(arg Val) (Val, error) {
	a, b := asRealPair(arg)
	switch {
	case math.IsNaN(a):
		return float32(b), nil
	case math.IsNaN(b):
		return float32(a), nil
	default:
		return float32(math.Max(a, b)), nil
	}
}

// realCheckFloatFn is "Real.checkFloat r": r, unless it is an
// infinity (Overflow) or NaN (Div).
func realCheckFloatFn(arg Val) (Val, error) {
	r := float64(asReal(arg))
	switch {
	case math.IsInf(r, 0):
		return nil, &MorelError{Exn: ExnOverflow}
	case math.IsNaN(r):
		return nil, &MorelError{Exn: ExnDiv}
	default:
		return asReal(arg), nil
	}
}

// realToIntFn adapts a float64 rounding function to a real->int
// built-in that raises Overflow when the result does not fit.
func realToIntFn(round func(float64) float64) Fn {
	return func(arg Val) (Val, error) {
		r := round(float64(asReal(arg)))
		switch {
		case math.IsNaN(r):
			return nil, &MorelError{Exn: ExnDomain}
		case r < math.MinInt32 || r > math.MaxInt32:
			return nil, &MorelError{Exn: ExnOverflow}
		default:
			return int32(r), nil
		}
	}
}

// realIsNegative reports whether a real's raw sign bit is set,
// including for a NaN; this defines Real.signBit, sameSign, and
// copySign. IEEE 754 leaves the sign of a NaN produced by
// arithmetic unspecified, so callers must not rely on it.
func realIsNegative(f float64) bool {
	return math.Signbit(f)
}

// realCopySign is "Real.copySign (a, b)": the magnitude of a with
// the sign of b.
func realCopySign(a, b float64) float64 {
	if realIsNegative(b) {
		return -math.Abs(a)
	}
	return math.Abs(a)
}

// realSplitFn is "Real.split r": its fractional and whole parts,
// as the record {frac, whole}. The whole part of a zero is positive.
func realSplitFn(arg Val) (Val, error) {
	r := float64(asReal(arg))
	var frac, whole float64
	switch {
	case math.IsNaN(r):
		frac, whole = r, r
	case r == 0:
		frac, whole = r, 0
	case math.IsInf(r, 0):
		frac, whole = math.Copysign(0, r), r
	default:
		whole = math.Trunc(r)
		frac = r - whole
	}
	return []Val{float32(frac), float32(whole)}, nil
}

// realTruncFn is "Real.realTrunc r": r rounded toward zero, as a
// real. An infinity has no whole part, so it yields NaN.
func realTruncFn(arg Val) (Val, error) {
	r := float64(asReal(arg))
	if math.IsInf(r, 0) {
		return float32(math.NaN()), nil
	}
	return float32(math.Trunc(r)), nil
}

// realModFn is "Real.realMod r", the fractional part of r.
func realModFn(arg Val) (Val, error) {
	v, err := realSplitFn(arg)
	if err != nil {
		return nil, err
	}
	return asList(v)[0], nil
}

// realToManExpFn is "Real.toManExp r": mantissa and exponent, as
// the record {exp, man}, with the mantissa in [0.5, 1).
func realToManExpFn(arg Val) (Val, error) {
	r := float64(asReal(arg))
	// Exponents for the IEEE special cases, as SML reports them.
	const specialExp, zeroExp = 129, -126
	var man float64
	var exp int
	switch {
	case math.IsNaN(r) || math.IsInf(r, 0):
		man, exp = r, specialExp
	case r == 0:
		man, exp = r, zeroExp
	default:
		man, exp = math.Frexp(r)
		if exp < zeroExp {
			// A subnormal's exponent is clamped to the minimum
			// normal exponent; its mantissa drops below 0.5.
			man = math.Ldexp(man, exp-zeroExp)
			exp = zeroExp
		}
	}
	return []Val{int32(exp), float32(man)}, nil
}

// realFromManExpFn is "Real.fromManExp {exp, man}".
func realFromManExpFn(arg Val) (Val, error) {
	vals := asList(arg)
	exp := asInt(vals[0])
	man := float64(asReal(vals[1]))
	return float32(math.Ldexp(man, int(exp))), nil
}

// isSpaceByte reports whether b is a whitespace character that
// Real.fromString skips before the number.
func isSpaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

// realFromStringFn is "Real.fromString s": after leading
// whitespace, parses the longest prefix of s that looks like a
// real, returning NONE if there is none.
func realFromStringFn(arg Val) (Val, error) {
	s := asString(arg)
	i := 0
	for i < len(s) && isSpaceByte(s[i]) {
		i++
	}
	start := i
	accept := func(test func(byte) bool) bool {
		if i < len(s) && test(s[i]) {
			i++
			return true
		}
		return false
	}
	digit := func(c byte) bool { return c >= '0' && c <= '9' }
	sign := func(c byte) bool {
		return c == '~' || c == '-' || c == '+'
	}
	accept(sign)
	digits := 0
	for accept(digit) {
		digits++
	}
	dot := i
	if accept(func(c byte) bool { return c == '.' }) {
		fracDigits := 0
		for accept(digit) {
			fracDigits++
		}
		if fracDigits == 0 {
			i = dot
		} else {
			digits += fracDigits
		}
	}
	if digits == 0 {
		return noneVal, nil
	}
	e := i
	if accept(func(c byte) bool { return c == 'e' || c == 'E' }) {
		accept(sign)
		expDigits := 0
		for accept(digit) {
			expDigits++
		}
		if expDigits == 0 {
			i = e
		}
	}
	text := strings.ReplaceAll(s[start:i], "~", "-")
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return noneVal, nil //nolint:nilerr // NONE, not an error
	}
	return someVal(float32(f)), nil
}

// minNormal is the smallest positive normal float32.
const minNormal = 1.1754943508222875e-38

// realFromIntFn is "Real.fromInt i" (and top-level "real").
func realFromIntFn(arg Val) (Val, error) {
	return float32(asInt(arg)), nil
}

// realToStringFn is "Real.toString r".
func realToStringFn(arg Val) (Val, error) {
	return FormatReal(asReal(arg)), nil
}

// FormatReal renders a real as Standard ML's Real.toString
// does: the shortest
// decimal digits that round-trip the float32; plain decimal
// notation for magnitudes in [1e-3, 1e7) and scientific notation
// otherwise; a trailing ".0" dropped (1.0 prints as "1", 1.0e10
// as "1E10"); and "~" for minus, in exponents too.
func FormatReal(f float32) string {
	f64 := float64(f)
	switch {
	case math.IsNaN(f64):
		return "nan"
	case math.IsInf(f64, 1):
		return "inf"
	case math.IsInf(f64, -1):
		return "~inf"
	}
	s := strconv.FormatFloat(f64, 'E', -1, 32)
	mantissa, expText, _ := strings.Cut(s, "E")
	exp, err := strconv.Atoi(expText)
	if err != nil {
		return s
	}
	neg := strings.HasPrefix(mantissa, "-")
	digits := strings.ReplaceAll(
		strings.TrimPrefix(mantissa, "-"), ".", "")
	var b strings.Builder
	if neg {
		b.WriteString("~")
	}
	const loExp, hiExp = -3, 7
	if exp >= loExp && exp < hiExp {
		writeDecimal(&b, digits, exp)
	} else {
		writeScientific(&b, digits, exp)
	}
	// Real.minPos: SML reports 1.4E~45, though "1E~45" denotes
	// the same float.
	result := b.String()
	if strings.HasSuffix(result, "1E~45") {
		result = strings.Replace(result, "1E~45", "1.4E~45", 1)
	}
	return result
}

// writeDecimal renders digits with the decimal point after the
// digit at position exp: digits "15" with exp 0 is "1.5", digits
// "1" with exp -3 is "0.001", digits "1" with exp 2 is "100".
func writeDecimal(b *strings.Builder, digits string, exp int) {
	if exp < 0 {
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -exp-1))
		b.WriteString(digits)
		return
	}
	if len(digits) <= exp+1 {
		b.WriteString(digits)
		b.WriteString(strings.Repeat("0", exp+1-len(digits)))
		return
	}
	b.WriteString(digits[:exp+1])
	b.WriteString(".")
	b.WriteString(digits[exp+1:])
}

// writeScientific renders "d.dddEx", dropping a ".0" mantissa
// ("1E10" rather than "1.0E10") and using "~" for a negative
// exponent.
func writeScientific(b *strings.Builder, digits string, exp int) {
	b.WriteString(digits[:1])
	if len(digits) > 1 {
		b.WriteString(".")
		b.WriteString(digits[1:])
	}
	b.WriteString("E")
	if exp < 0 {
		b.WriteString("~")
		exp = -exp
	}
	b.WriteString(strconv.Itoa(exp))
}

// fmtKind is the style of a StringCvt.realfmt: scientific, fixed,
// general, or exact.
type fmtKind int

const (
	sciKind fmtKind = iota
	fixKind
	genKind
	exactKind
)

// realFmtFn is "Real.fmt spec": it validates the spec — a negative
// precision for SCI or FIX, or a precision below 1 for GEN, raises
// Size — and returns the function that renders a real in that style.
func realFmtFn(spec Val) (Val, error) {
	if fmtSpecBad(spec) {
		return nil, &MorelError{Exn: ExnSize}
	}
	return Fn(func(arg Val) (Val, error) {
		return realFmt(spec, asReal(arg)), nil
	}), nil
}

// fmtSpecBad reports whether a realfmt spec's precision is out of
// range for its kind.
func fmtSpecBad(spec Val) bool {
	con, _ := spec.(Con)
	switch con.Name {
	case "FIX", "SCI":
		if n, ok := optInt(con.Arg); ok && n < 0 {
			return true
		}
	case "GEN":
		if n, ok := optInt(con.Arg); ok && n < 1 {
			return true
		}
	}
	return false
}

// optInt returns the payload of an "int option" value: (n, true)
// for SOME n, (0, false) for NONE.
func optInt(opt Val) (int, bool) {
	con, ok := opt.(Con)
	if !ok || con.Name != "SOME" {
		return 0, false
	}
	i, _ := con.Arg.(int32)
	return int(i), true
}

// parseFmtSpec returns a realfmt spec's kind and precision, applying
// the per-kind default when the precision is NONE: 6 for SCI and
// FIX, 12 for GEN.
func parseFmtSpec(spec Val) (fmtKind, int) {
	con, _ := spec.(Con)
	var kind fmtKind
	def := 0
	// lint: sort until '^\t}' where '^\tcase '
	switch con.Name {
	case "EXACT":
		return exactKind, 0
	case "FIX":
		kind, def = fixKind, 6
	case "GEN":
		kind, def = genKind, 12
	case "SCI":
		kind, def = sciKind, 6
	}
	if n, ok := optInt(con.Arg); ok {
		return kind, n
	}
	return kind, def
}

// realFmt renders a real in the given realfmt style. Special values
// are "nan", "inf", and "~inf"; a negative sign is "~".
func realFmt(spec Val, r float32) string {
	kind, n := parseFmtSpec(spec)
	f := float64(r)
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 0) {
		if math.Signbit(f) {
			return "~inf"
		}
		return "inf"
	}
	neg := math.Signbit(f)
	abs := float32(math.Abs(f))
	var body string
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case exactKind:
		body = formatExact(abs)
	case fixKind:
		body = formatFix(abs, n)
	case genKind:
		body = formatGen(abs, n)
	case sciKind:
		body = formatSci(abs, n)
	}
	if neg {
		return "~" + body
	}
	return body
}

// formatSci renders abs in scientific notation "D.dddE<exp>" with n
// digits after the decimal point.
func formatSci(abs float32, n int) string {
	if abs == 0 {
		if n == 0 {
			return "0E0"
		}
		return "0." + strings.Repeat("0", n) + "E0"
	}
	digits, exp := canonicalDigits(abs)
	rounded, expAdj := roundHalfDown(digits, n+1)
	exp += expAdj
	if n == 0 {
		return rounded[:1] + "E" + smlExp(exp)
	}
	return rounded[:1] + "." + rounded[1:] + "E" + smlExp(exp)
}

// formatFix renders abs in fixed-point notation with n digits after
// the decimal point.
func formatFix(abs float32, n int) string {
	if abs == 0 {
		if n == 0 {
			return "0"
		}
		return "0." + strings.Repeat("0", n)
	}
	digits, exp := canonicalDigits(abs)
	// Keep digits down to the 10^-n place: the first exp+n+1 of
	// them. A non-positive count means the number is below that
	// place, so it rounds to zero unless a digit exactly at
	// 10^-(n+1) carries up to 10^-n.
	totalSig := exp + n + 1
	if totalSig <= 0 {
		up := totalSig == 0 && (digits[0] > '5' ||
			(digits[0] == '5' && strings.Trim(digits[1:], "0") != ""))
		if up {
			if n == 0 {
				return "1"
			}
			return "0." + strings.Repeat("0", n-1) + "1"
		}
		if n == 0 {
			return "0"
		}
		return "0." + strings.Repeat("0", n)
	}
	rounded, expAdj := roundHalfDown(digits, totalSig)
	exp += expAdj
	return placeDecimal(rounded, exp, n, false)
}

// formatGen renders abs with at most n significant digits, using
// fixed notation when the exponent is in [-3, n) and scientific
// otherwise, with trailing zeros stripped.
func formatGen(abs float32, n int) string {
	if abs == 0 {
		return "0"
	}
	digits, exp := canonicalDigits(abs)
	rounded, expAdj := roundHalfDown(digits, n)
	exp += expAdj
	stripped := strings.TrimRight(rounded, "0")
	if stripped == "" {
		stripped = "0"
	}
	if exp <= -3 || exp >= n {
		if len(stripped) == 1 {
			return stripped + "E" + smlExp(exp)
		}
		return stripped[:1] + "." + stripped[1:] + "E" + smlExp(exp)
	}
	return placeDecimal(stripped, exp, 0, true)
}

// formatExact renders abs as "0.<digits>E<exp>", the exact decimal
// value with trailing zeros stripped.
func formatExact(abs float32) string {
	if abs == 0 {
		return "0.0"
	}
	digits, exp := canonicalDigits(abs)
	stripped := strings.TrimRight(digits, "0")
	if stripped == "" {
		stripped = "0"
	}
	exp++
	if exp == 0 {
		return "0." + stripped
	}
	return "0." + stripped + "E" + smlExp(exp)
}

// smlExp renders an exponent with "~" for a negative sign.
func smlExp(exp int) string {
	if exp < 0 {
		return "~" + strconv.Itoa(-exp)
	}
	return strconv.Itoa(exp)
}

// canonicalDigits returns abs's significant digits (no leading
// zeros, no point) and the scientific exponent e such that the
// number is digits[0].digits[1..] * 10^e. It uses the shortest
// round-tripping decimal for the float32.
func canonicalDigits(abs float32) (string, int) {
	if abs == math.SmallestNonzeroFloat32 {
		// SML reports 2^-149 as 1.4E~45, though the shortest decimal
		// that round-trips to it is 1E~45.
		return "14", -45
	}
	s := strconv.FormatFloat(float64(abs), 'e', -1, 32)
	mantissa, expStr, found := strings.Cut(s, "e")
	if !found {
		expStr = "0"
	}
	expBase, _ := strconv.Atoi(expStr)
	intPart, fracPart, _ := strings.Cut(mantissa, ".")
	digits := intPart + fracPart
	if digits == "" {
		digits = "0"
	}
	return digits, expBase + len(intPart) - 1
}

// roundHalfDown rounds a digit string to target significant digits,
// ties toward zero. It returns the rounded digits and an exponent
// adjustment of 1 when the rounding carried past the leading digit
// ("999" -> "1000").
func roundHalfDown(digits string, target int) (string, int) {
	if target <= 0 {
		return "0", 0
	}
	if len(digits) <= target {
		return digits + strings.Repeat("0", target-len(digits)), 0
	}
	kept := digits[:target]
	dropped := digits[target:]
	roundUp := dropped[0] > '5' ||
		(dropped[0] == '5' && strings.Trim(dropped[1:], "0") != "")
	if !roundUp {
		return kept, 0
	}
	const decimalBase = 10
	b := []byte(kept)
	carry := byte(1)
	for i := len(b) - 1; i >= 0 && carry != 0; i-- {
		if v := b[i] - '0' + carry; v >= decimalBase {
			b[i] = '0'
		} else {
			b[i] = '0' + v
			carry = 0
		}
	}
	if carry == 1 {
		return "1" + strings.Repeat("0", target-1), 1
	}
	return string(b), 0
}

// placeDecimal writes digits with a decimal point so the value is
// digits[0].digits[1..] * 10^exp, keeping at least minFrac
// fractional digits. When strip is true, trailing fractional zeros
// (and a trailing point) are removed.
func placeDecimal(digits string, exp, minFrac int, strip bool) string {
	intLen := exp + 1
	var body string
	switch {
	case intLen <= 0:
		body = "0." + strings.Repeat("0", -intLen) + digits
	case intLen >= len(digits):
		out := digits + strings.Repeat("0", intLen-len(digits))
		if minFrac > 0 {
			out += "." + strings.Repeat("0", minFrac)
		}
		if strip {
			return trimDecimal(out)
		}
		return out
	default:
		body = digits[:intLen] + "." + digits[intLen:]
	}
	if dot := strings.IndexByte(body, '.'); dot >= 0 {
		if frac := len(body) - dot - 1; frac < minFrac {
			body += strings.Repeat("0", minFrac-frac)
		}
	} else if minFrac > 0 {
		body += "." + strings.Repeat("0", minFrac)
	}
	if strip {
		return trimDecimal(body)
	}
	return body
}

// trimDecimal removes trailing zeros after a decimal point, then a
// trailing point.
func trimDecimal(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	t := strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if t == "" {
		return "0"
	}
	return t
}
