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
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The Time structure. A "time" value is a duration or instant
// measured in nanoseconds, held as an int64.

const (
	nsPerSecond = 1_000_000_000
	nsPerMilli  = 1_000_000
	nsPerMicro  = 1_000
	// toStringDigits is the fractional precision of Time.toString.
	toStringDigits = 3
)

// ZeroTime is the value Time.zeroTime.
var ZeroTime Val = int64(0)

// asTime views a runtime value as a time in nanoseconds.
func asTime(v Val) int64 {
	t, ok := v.(int64)
	if !ok {
		panic(fmt.Sprintf("expected time, got %T", v))
	}
	return t
}

// FormatTime renders a time value as its integer count of
// nanoseconds, with an ordinary minus sign (as the reference
// implementation prints it).
func FormatTime(v Val) string {
	return strconv.FormatInt(asTime(v), 10)
}

// timeFromUnit builds a "from<Unit>" function that scales an int
// number of units to nanoseconds.
func timeFromUnit(scale int64) Fn {
	return func(arg Val) (Val, error) {
		return int64(asInt(arg)) * scale, nil
	}
}

// timeToUnit builds a "to<Unit>" function that scales nanoseconds
// down to an int number of units.
func timeToUnit(scale int64) Fn {
	return func(arg Val) (Val, error) {
		//nolint:gosec // the corpus stays within int range
		return int32(asTime(arg) / scale), nil
	}
}

// timeFromRealFn is "Time.fromReal r": seconds as a real to
// nanoseconds. An infinite or NaN argument raises Time.
func timeFromRealFn(arg Val) (Val, error) {
	r := float64(asReal(arg))
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return nil, &MorelError{Exn: ExnTime}
	}
	return int64(r * nsPerSecond), nil
}

// timeToRealFn is "Time.toReal t": nanoseconds to seconds as a real.
func timeToRealFn(arg Val) (Val, error) {
	return float32(float64(asTime(arg)) / nsPerSecond), nil
}

// timeArith builds a "+"/"-" function on a pair of times.
func timeArith(f func(a, b int64) int64) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		return f(asTime(a), asTime(b)), nil
	}
}

func cmpTime(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// timeCompareFn is "Time.compare (t1, t2)".
func timeCompareFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return orderVal(cmpTime(asTime(a), asTime(b))), nil
}

// timeCmp builds a comparison predicate on a pair of times.
func timeCmp(want func(c int) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		return want(cmpTime(asTime(a), asTime(b))), nil
	}
}

// formatTimeSeconds renders a time as seconds with the given number
// of fractional digits, using "~" for the minus sign.
func formatTimeSeconds(ns int64, digits int) string {
	sec := float64(ns) / nsPerSecond
	s := strconv.FormatFloat(sec, 'f', digits, 64)
	return strings.Replace(s, "-", "~", 1)
}

// timeFmtFn is "Time.fmt n t": t in seconds, n fractional digits.
func timeFmtFn(n Val) (Val, error) {
	digits := int(asInt(n))
	return Fn(func(t Val) (Val, error) {
		return formatTimeSeconds(asTime(t), digits), nil
	}), nil
}

// timeToStringFn is "Time.toString t": seconds with three digits.
func timeToStringFn(arg Val) (Val, error) {
	return formatTimeSeconds(asTime(arg), toStringDigits), nil
}

// timeFromStringFn is "Time.fromString s": parses seconds as a real,
// returning SOME time or NONE.
func timeFromStringFn(arg Val) (Val, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(asString(arg)), 64)
	if err != nil {
		return noneVal, nil //nolint:nilerr // an unparsable string is NONE
	}
	return someVal(int64(f * nsPerSecond)), nil
}
