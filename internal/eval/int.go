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

// The Int structure.

// orderDatatype names the datatype of comparison results.
const orderDatatype = "order"

// Ordinals of the order constructors, in declaration order.
const (
	lessOrdinal = iota
	equalOrdinal
	greaterOrdinal
)

// orderVal returns a value of the order datatype: LESS, EQUAL,
// or GREATER for a comparison result of -1, 0, or 1.
func orderVal(c int) Val {
	switch {
	case c < 0:
		return Con{
			Datatype: orderDatatype,
			Name:     "LESS",
			Ordinal:  lessOrdinal,
		}
	case c > 0:
		return Con{
			Datatype: orderDatatype,
			Name:     "GREATER",
			Ordinal:  greaterOrdinal,
		}
	default:
		return Con{
			Datatype: orderDatatype,
			Name:     "EQUAL",
			Ordinal:  equalOrdinal,
		}
	}
}

// identityFn implements the conversions that are no-ops at this
// precision, such as Int.toLarge.
func identityFn(arg Val) (Val, error) {
	return arg, nil
}

// intCompareFn is "Int.compare (a, b)".
func intCompareFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return orderVal(cmpOrdered(asInt(a), asInt(b))), nil
}

// intQuotFn is "Int.quot (a, b)": division truncating toward
// zero (unlike "div", which floors).
func intQuotFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	if asInt(b) == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	return checkIntRange(int64(asInt(a)) / int64(asInt(b)))
}

// intRemFn is "Int.rem (a, b)": the remainder of quot, with the
// dividend's sign.
func intRemFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	if asInt(b) == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	return asInt(a) % asInt(b), nil
}

// intMinFn is "Int.min (a, b)".
func intMinFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return min(asInt(a), asInt(b)), nil
}

// intMaxFn is "Int.max (a, b)".
func intMaxFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return max(asInt(a), asInt(b)), nil
}

// intSignFn is "Int.sign i": ~1, 0, or 1.
func intSignFn(arg Val) (Val, error) {
	i := asInt(arg)
	switch {
	case i < 0:
		return int32(-1), nil
	case i > 0:
		return int32(1), nil
	default:
		return int32(0), nil
	}
}

// intSameSignFn is "Int.sameSign (a, b)".
func intSameSignFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	sa, _ := intSignFn(a)
	sb, _ := intSignFn(b)
	return sa == sb, nil
}

// intToStringFn is "Int.toString i", with "~" for minus.
func intToStringFn(arg Val) (Val, error) {
	return FormatInt(asInt(arg)), nil
}

// FormatInt renders an int with Morel's negation sign: ~3.
func FormatInt(i int32) string {
	return strings.ReplaceAll(strconv.FormatInt(int64(i), 10),
		"-", "~")
}

// intFmtFn is "Int.fmt radix i": i rendered in the radix's base,
// with upper-case hex digits and "~" for a negative sign.
func intFmtFn(radix Val) (Val, error) {
	base := radixBase(radix)
	return Fn(func(arg Val) (Val, error) {
		s := strconv.FormatInt(int64(asInt(arg)), base)
		return strings.ReplaceAll(strings.ToUpper(s), "-", "~"), nil
	}), nil
}

// The numeric bases of the StringCvt.radix constructors.
const (
	binRadix = 2
	octRadix = 8
	decRadix = 10
	hexRadix = 16
)

// radixBase maps a StringCvt.radix constructor (BIN, OCT, DEC, HEX)
// to its numeric base.
func radixBase(radix Val) int {
	con, ok := radix.(Con)
	if !ok {
		panic("Int.fmt: radix is not a constructor")
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch con.Name {
	case "BIN":
		return binRadix
	case "HEX":
		return hexRadix
	case "OCT":
		return octRadix
	default:
		return decRadix
	}
}

// intFromStringFn is "Int.fromString s": parses the longest
// prefix of s that looks like an integer, returning NONE if
// there is none.
func intFromStringFn(arg Val) (Val, error) {
	s := asString(arg)
	i := 0
	neg := false
	if i < len(s) && (s[i] == '~' || s[i] == '-' || s[i] == '+') {
		neg = s[i] == '~' || s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return noneVal, nil
	}
	v, err := strconv.ParseInt(s[start:i], 10, 64)
	if err != nil {
		return noneVal, nil //nolint:nilerr // NONE, not an error
	}
	if neg {
		v = -v
	}
	if v < math.MinInt32 || v > math.MaxInt32 {
		return noneVal, nil
	}
	return someVal(int32(v)), nil
}
