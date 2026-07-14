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
	"slices"
	"strings"
)

// The list and string built-ins, and the equality and comparison
// operators.

func asList(v Val) []Val {
	vals, ok := v.([]Val)
	if !ok {
		panic(fmt.Sprintf("expected list, got %T", v))
	}
	return vals
}

func asPair(v Val) (Val, Val) {
	vals := asList(v)
	const pair = 2
	if len(vals) != pair {
		panic(fmt.Sprintf("expected pair, got %d values",
			len(vals)))
	}
	return vals[0], vals[1]
}

// consFn is "x :: xs".
func consFn(arg Val) (Val, error) {
	head, tail := asPair(arg)
	list := asList(tail)
	out := make([]Val, 0, len(list)+1)
	out = append(out, head)
	return append(out, list...), nil
}

// atFn is "xs @ ys", list concatenation.
func atFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return slices.Concat(asList(a), asList(b)), nil
}

// caretFn is "s ^ t", string concatenation.
func caretFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return asString(a) + asString(b), nil
}

// hdFn is "hd xs"; the head of the empty list raises Empty.
func hdFn(arg Val) (Val, error) {
	list := asList(arg)
	if len(list) == 0 {
		return nil, &MorelError{Exn: "Empty"}
	}
	return list[0], nil
}

// tlFn is "tl xs"; the tail of the empty list raises Empty.
func tlFn(arg Val) (Val, error) {
	list := asList(arg)
	if len(list) == 0 {
		return nil, &MorelError{Exn: "Empty"}
	}
	return list[1:], nil
}

// lengthFn is "length xs".
func lengthFn(arg Val) (Val, error) {
	//nolint:gosec // a list's length fits in an int.
	return int32(len(asList(arg))), nil
}

// nullFn is "null xs".
func nullFn(arg Val) (Val, error) {
	return len(asList(arg)) == 0, nil
}

// revFn is "rev xs".
func revFn(arg Val) (Val, error) {
	list := asList(arg)
	out := make([]Val, len(list))
	for i, v := range list {
		out[len(list)-1-i] = v
	}
	return out, nil
}

// mapFn is "map f xs".
func mapFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		list := asList(arg)
		out := make([]Val, len(list))
		for i, v := range list {
			r, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}), nil
}

// explodeFn is "explode s", the characters of a string.
func explodeFn(arg Val) (Val, error) {
	s := asString(arg)
	out := make([]Val, 0, len(s))
	for _, r := range s {
		out = append(out, r)
	}
	return out, nil
}

// implodeFn is "implode cs", the string of a character list.
func implodeFn(arg Val) (Val, error) {
	list := asList(arg)
	b := make([]rune, len(list))
	for i, v := range list {
		b[i] = asChar(v)
	}
	return string(b), nil
}

// concatFn is "concat ss", the concatenation of a string list.
func concatFn(arg Val) (Val, error) {
	var b strings.Builder
	for _, v := range asList(arg) {
		b.WriteString(asString(v))
	}
	return b.String(), nil
}

// equalFn is "op =" (or its negation, "op <>"): structural
// equality. Scalars compare directly; lists, tuples, and records
// compare element-wise; constructor values compare by datatype,
// ordinal, and argument.
func equalFn(negate bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		return valsEqual(a, b) != negate, nil
	}
}

func valsEqual(a, b Val) bool {
	switch a := a.(type) {
	case []Val:
		b2, ok := b.([]Val)
		if !ok || len(a) != len(b2) {
			return false
		}
		for i, v := range a {
			if !valsEqual(v, b2[i]) {
				return false
			}
		}
		return true
	case Con:
		b2, ok := b.(Con)
		if !ok || a.Datatype != b2.Datatype ||
			a.Ordinal != b2.Ordinal {
			return false
		}
		if a.Arg == nil || b2.Arg == nil {
			return a.Arg == nil && b2.Arg == nil
		}
		return valsEqual(a.Arg, b2.Arg)
	default:
		return a == b
	}
}

// compareFn adapts an ordering test to the comparison operators
// "<", "<=", ">", and ">=", which are overloaded over int, real,
// char, and string.
func compareFn(test func(c int) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		// An ordering comparison with a NaN real is false, as IEEE
		// requires; cmpOrdered would otherwise report NaN as equal.
		if isNaNReal(a) || isNaNReal(b) {
			return false, nil
		}
		return test(compareVals(a, b)), nil
	}
}

// isNaNReal reports whether v is a NaN real.
func isNaNReal(v Val) bool {
	f, ok := v.(float32)
	return ok && math.IsNaN(float64(f))
}

func compareVals(a, b Val) int {
	// lint: sort until '^	}' where '^	case '
	switch a := a.(type) {
	case float32:
		f, ok := b.(float32)
		if !ok {
			panic(fmt.Sprintf("expected real, got %T", b))
		}
		return cmpOrdered(a, f)
	case int32:
		return cmpOrdered(a, asInt(b))
	case string:
		return cmpOrdered(a, asString(b))
	default:
		panic(fmt.Sprintf("cannot compare %T", a))
	}
}

func cmpOrdered[T int32 | float32 | string](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
