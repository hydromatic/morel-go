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
	"strings"
	"unicode"
)

// stringCvtPadLeftFn is "StringCvt.padLeft c i s": s padded on the
// left with copies of c to a width of at least i, or s unchanged if
// it is already that wide.
func stringCvtPadLeftFn(c, i, s Val) (Val, error) {
	str := asString(s)
	n := int(asInt(i)) - len(str)
	if n <= 0 {
		return str, nil
	}
	return strings.Repeat(string(asChar(c)), n) + str, nil
}

// stringCvtPadRightFn is "StringCvt.padRight c i s": s padded on the
// right with copies of c to a width of at least i, or s unchanged if
// it is already that wide.
func stringCvtPadRightFn(c, i, s Val) (Val, error) {
	str := asString(s)
	n := int(asInt(i)) - len(str)
	if n <= 0 {
		return str, nil
	}
	return str + strings.Repeat(string(asChar(c)), n), nil
}

// splitl reads from src the longest prefix of characters that
// satisfy p, and returns it with the stream that follows.
//
// It is shared by "StringCvt.splitl", "takel" and "dropl", which
// return the prefix and the rest, the prefix, and the rest
// respectively, and by "skipWS", whose predicate is whitespace.
func splitl(p, rdr, src Val) (string, Val, error) {
	var b strings.Builder
	s := src
	for {
		option, err := ApplyVal(rdr, s)
		if err != nil {
			return "", nil, err
		}
		pair, some := asOption(option)
		if !some {
			break // NONE: end of stream
		}
		first, next := asPair(pair)
		c := asChar(first)
		keep, err := ApplyVal(p, c)
		if err != nil {
			return "", nil, err
		}
		if !asBool(keep) {
			break
		}
		b.WriteRune(c)
		s = next
	}
	return b.String(), s, nil
}

// stringCvtSplitlFn is "StringCvt.splitl p rdr src": the longest
// prefix of src whose characters satisfy p, and the rest of src.
func stringCvtSplitlFn(p, rdr, src Val) (Val, error) {
	prefix, rest, err := splitl(p, rdr, src)
	if err != nil {
		return nil, err
	}
	return []Val{prefix, rest}, nil
}

// stringCvtTakelFn is "StringCvt.takel p rdr src": the first
// component of "splitl p rdr src".
func stringCvtTakelFn(p, rdr, src Val) (Val, error) {
	prefix, _, err := splitl(p, rdr, src)
	if err != nil {
		return nil, err
	}
	return prefix, nil
}

// stringCvtDroplFn is "StringCvt.dropl p rdr src": the second
// component of "splitl p rdr src".
func stringCvtDroplFn(p, rdr, src Val) (Val, error) {
	_, rest, err := splitl(p, rdr, src)
	if err != nil {
		return nil, err
	}
	return rest, nil
}

// stringCvtSkipWSFn is "StringCvt.skipWS rdr src": src with any
// leading whitespace dropped.
func stringCvtSkipWSFn(rdr, src Val) (Val, error) {
	_, rest, err := splitl(Fn(isSpaceFn), rdr, src)
	if err != nil {
		return nil, err
	}
	return rest, nil
}

// isSpaceFn reports whether a character is whitespace; it is the
// predicate that "skipWS" drops on.
func isSpaceFn(v Val) (Val, error) {
	return unicode.IsSpace(asChar(v)), nil
}

// stringCvtScanStringFn is "StringCvt.scanString f s": the value
// that the scanner f reads from a prefix of s, or NONE.
//
// The scanner is given a reader over the characters of s. The
// stream is a position in s, but the scanner's type does not say
// so, and the reader is the only thing that can make sense of it.
func stringCvtScanStringFn(f, s Val) (Val, error) {
	runes := []rune(asString(s))
	reader := Fn(func(stream Val) (Val, error) {
		i := int(asInt(stream))
		if i >= len(runes) {
			return noneVal, nil
		}
		//nolint:gosec // a string's length fits in an int32.
		return someVal([]Val{runes[i], int32(i + 1)}), nil
	})
	scan, err := ApplyVal(f, reader)
	if err != nil {
		return nil, err
	}
	option, err := ApplyVal(scan, int32(0))
	if err != nil {
		return nil, err
	}
	pair, some := asOption(option)
	if !some {
		return noneVal, nil
	}
	value, _ := asPair(pair)
	return someVal(value), nil
}
