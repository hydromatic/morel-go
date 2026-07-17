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

// The Word structure. A word is an unsigned 64-bit value, held as a
// uint64; its arithmetic wraps modulo 2^64 and its division and
// comparison are unsigned.

// wordSize is "Word.wordSize".
const wordSize = int32(64)

// FormatWord renders a word in Morel's hexadecimal form, "0wx" then
// upper-case hex digits with no leading zeros: 0wx7F.
func FormatWord(w uint64) string {
	return "0wx" + wordHex(w)
}

// wordHex is a word's upper-case hex digits, as Word.toString and
// Word.fmt HEX render it.
func wordHex(w uint64) string {
	return strings.ToUpper(strconv.FormatUint(w, 16))
}

func asWord(v Val) uint64 {
	w, ok := v.(uint64)
	if !ok {
		panic("expected word")
	}
	return w
}

func asWordPair(arg Val) (uint64, uint64) {
	a, b := asPair(arg)
	return asWord(a), asWord(b)
}

// word2 adapts a uint64 function of a pair.
func word2(f func(a, b uint64) uint64) Fn {
	return func(arg Val) (Val, error) {
		a, b := asWordPair(arg)
		return f(a, b), nil
	}
}

// wordCmp adapts a uint64 predicate of a pair.
func wordCmp(test func(a, b uint64) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asWordPair(arg)
		return test(a, b), nil
	}
}

// wordFromIntFn is "Word.fromInt i": the int as a word, sign-extended
// into 64 bits.
func wordFromIntFn(arg Val) (Val, error) {
	//nolint:gosec // reinterpret the sign-extended bits as a word.
	return uint64(int64(asInt(arg))), nil
}

// wordToIntFn is "Word.toInt w": the word's unsigned value as an int,
// raising Overflow if it exceeds Int.maxInt.
func wordToIntFn(arg Val) (Val, error) {
	w := asWord(arg)
	if w > math.MaxInt32 {
		return nil, &MorelError{Exn: ExnOverflow}
	}
	return int32(w), nil
}

// wordToIntXFn is "Word.toIntX w": the word's two's-complement value
// as an int, raising Overflow if it is out of int range.
func wordToIntXFn(arg Val) (Val, error) {
	//nolint:gosec // reinterpret the word's bits as a signed value.
	i := int64(asWord(arg))
	if i < math.MinInt32 || i > math.MaxInt32 {
		return nil, &MorelError{Exn: ExnOverflow}
	}
	return int32(i), nil
}

// wordNotbFn is "Word.notb w", the bitwise complement.
func wordNotbFn(arg Val) (Val, error) {
	return ^asWord(arg), nil
}

// wordNegFn is "Word.~ w", the two's-complement negation.
func wordNegFn(arg Val) (Val, error) {
	return -asWord(arg), nil
}

// wordAshrFn is "Word.~>> (w, n)", the arithmetic (sign-filling)
// right shift.
func wordAshrFn(arg Val) (Val, error) {
	w, n := asWordPair(arg)
	//nolint:gosec // arithmetic shift via the signed reinterpretation.
	return uint64(int64(w) >> n), nil
}

// The word arithmetic operations, shared by the Word.* members and
// the overloaded operators (+, -, *, div, mod).

func addWord(a, b uint64) (Val, error) { return a + b, nil }
func subWord(a, b uint64) (Val, error) { return a - b, nil }
func mulWord(a, b uint64) (Val, error) { return a * b, nil }

func divWord(a, b uint64) (Val, error) {
	if b == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	return a / b, nil
}

func modWord(a, b uint64) (Val, error) {
	if b == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	return a % b, nil
}

// wordDivFn is "Word.div (a, b)", unsigned division; Div on zero.
func wordDivFn(arg Val) (Val, error) {
	a, b := asWordPair(arg)
	return divWord(a, b)
}

// wordModFn is "Word.mod (a, b)", the unsigned remainder; Div on
// zero.
func wordModFn(arg Val) (Val, error) {
	a, b := asWordPair(arg)
	return modWord(a, b)
}

// wordCompareFn is "Word.compare (a, b)", an unsigned comparison.
func wordCompareFn(arg Val) (Val, error) {
	a, b := asWordPair(arg)
	switch {
	case a < b:
		return orderVal(-1), nil
	case a > b:
		return orderVal(1), nil
	default:
		return orderVal(0), nil
	}
}

// wordFmtFn is "Word.fmt radix w": the word in the radix's base with
// upper-case digits and no sign or "0wx" prefix.
func wordFmtFn(radix Val) (Val, error) {
	base := radixBase(radix)
	return Fn(func(arg Val) (Val, error) {
		return strings.ToUpper(
			strconv.FormatUint(asWord(arg), base)), nil
	}), nil
}

// wordToStringFn is "Word.toString w", equivalent to Word.fmt HEX.
func wordToStringFn(arg Val) (Val, error) {
	return wordHex(asWord(arg)), nil
}

// wordFromStringFn is "Word.fromString s": scans, after leading
// whitespace, the longest prefix of hexadecimal digits as a word,
// returning NONE if there is none.
func wordFromStringFn(arg Val) (Val, error) {
	s := asString(arg)
	i := 0
	for i < len(s) && isSpaceByte(s[i]) {
		i++
	}
	// An optional "0wx"/"0wX" or "0x"/"0X" prefix, stripped only when
	// a hex digit follows; otherwise the leading "0" is the value
	// (so "0xG" scans as 0w0).
	switch {
	case i+3 < len(s) && s[i] == '0' &&
		(s[i+1] == 'w' || s[i+1] == 'W') &&
		(s[i+2] == 'x' || s[i+2] == 'X') && isHexByte(s[i+3]):
		i += 3
	case i+2 < len(s) && s[i] == '0' &&
		(s[i+1] == 'x' || s[i+1] == 'X') && isHexByte(s[i+2]):
		i += 2
	}
	start := i
	for i < len(s) && isHexByte(s[i]) {
		i++
	}
	if i == start {
		return noneVal, nil
	}
	u, err := strconv.ParseUint(s[start:i], 16, 64)
	if err != nil {
		return noneVal, nil //nolint:nilerr // NONE, not an error
	}
	return someVal(u), nil
}

// isHexByte reports whether b is a hexadecimal digit.
func isHexByte(b byte) bool {
	return b >= '0' && b <= '9' ||
		b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
