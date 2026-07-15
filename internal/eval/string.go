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
	"strings"
)

// The String structure. Strings are byte-indexed, and indexing
// errors raise Subscript, as in the basis.

// stringMaxSize is "String.maxSize".
const stringMaxSize = int32(math.MaxInt32)

// stringSubFn is "String.sub (s, i)", the i-th character.
func stringSubFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	s, i := asString(a), asInt(b)
	if i < 0 || int(i) >= len(s) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	return rune(s[i]), nil
}

// stringExtractFn is "String.extract (s, i, jOpt)": the
// substring from i to the end, or of size j if jOpt is SOME j.
func stringExtractFn(arg Val) (Val, error) {
	vals := asList(arg)
	s, i := asString(vals[0]), asInt(vals[1])
	if i < 0 || int(i) > len(s) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	if jVal, isSome := asOption(vals[2]); isSome {
		j := asInt(jVal)
		if j < 0 || int(i)+int(j) > len(s) {
			return nil, &MorelError{Exn: ExnSubscript}
		}
		return s[i : i+j], nil
	}
	return s[i:], nil
}

// stringSubstringFn is "String.substring (s, i, j)", equivalent
// to "String.extract (s, i, SOME j)".
func stringSubstringFn(arg Val) (Val, error) {
	vals := asList(arg)
	s, i, j := asString(vals[0]), asInt(vals[1]), asInt(vals[2])
	if i < 0 || j < 0 || int(i)+int(j) > len(s) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	return s[i : i+j], nil
}

// stringConcatWithFn is "String.concatWith sep l".
func stringConcatWithFn(sep Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		list := asList(arg)
		parts := make([]string, len(list))
		for i, v := range list {
			parts[i] = asString(v)
		}
		return strings.Join(parts, asString(sep)), nil
	}), nil
}

// stringMapFn is "String.map f s": f applied to each character.
func stringMapFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		s := asString(arg)
		var b strings.Builder
		for i := range len(s) {
			v, err := ApplyVal(f, rune(s[i]))
			if err != nil {
				return nil, err
			}
			//nolint:gosec // a char is at most 255.
			b.WriteByte(byte(asChar(v)))
		}
		return b.String(), nil
	}), nil
}

// stringTranslateFn is "String.translate f s": the
// concatenation of f applied to each character.
func stringTranslateFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		s := asString(arg)
		var b strings.Builder
		for i := range len(s) {
			v, err := ApplyVal(f, rune(s[i]))
			if err != nil {
				return nil, err
			}
			b.WriteString(asString(v))
		}
		return b.String(), nil
	}), nil
}

// stringSplit implements tokens (which drops empty substrings)
// and fields (which keeps them): f is the delimiter predicate.
func stringSplit(keepEmpty bool) Fn {
	return Curry2(func(f, arg Val) (Val, error) {
		s := asString(arg)
		out := []Val{}
		start := 0
		for i := 0; i <= len(s); i++ {
			isDelim := false
			if i < len(s) {
				v, err := ApplyVal(f, rune(s[i]))
				if err != nil {
					return nil, err
				}
				isDelim = asBool(v)
			}
			if i == len(s) || isDelim {
				if keepEmpty || i > start {
					out = append(out, s[start:i])
				}
				start = i + 1
			}
		}
		return out, nil
	})
}

// stringIsPrefixFn is "String.isPrefix s1 s2"; the empty string
// is a prefix of any string.
func stringIsPrefixFn(s1 Val) (Val, error) {
	return Fn(func(s2 Val) (Val, error) {
		return strings.HasPrefix(asString(s2), asString(s1)), nil
	}), nil
}

// stringIsSubstringFn is "String.isSubstring s1 s2".
func stringIsSubstringFn(s1 Val) (Val, error) {
	return Fn(func(s2 Val) (Val, error) {
		return strings.Contains(asString(s2), asString(s1)), nil
	}), nil
}

// stringIsSuffixFn is "String.isSuffix s1 s2".
func stringIsSuffixFn(s1 Val) (Val, error) {
	return Fn(func(s2 Val) (Val, error) {
		return strings.HasSuffix(asString(s2), asString(s1)), nil
	}), nil
}

// stringCompareFn is "String.compare (s, t)".
func stringCompareFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return orderVal(cmpOrdered(asString(a), asString(b))), nil
}

// stringCollateFn is "String.collate f (s, t)": lexicographic
// comparison using f on characters.
func stringCollateFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		a, b := asPair(arg)
		s, t := asString(a), asString(b)
		for i := 0; i < len(s) && i < len(t); i++ {
			v, err := ApplyVal(f, []Val{rune(s[i]), rune(t[i])})
			if err != nil {
				return nil, err
			}
			con, ok := v.(Con)
			if !ok {
				panic(fmt.Sprintf("expected order, got %T", v))
			}
			if con.Ordinal != equalOrdinal {
				return con, nil
			}
		}
		//nolint:gosec // a string's length fits in an int.
		return orderVal(cmpOrdered(int32(len(s)),
			int32(len(t)))), nil
	}), nil
}

// stringOp adapts a binary string predicate to a built-in.
func stringOp(f func(a, b string) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		return f(asString(a), asString(b)), nil
	}
}
