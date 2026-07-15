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
		return nil, &MorelError{Exn: ExnEmpty}
	}
	return list[0], nil
}

// tlFn is "tl xs"; the tail of the empty list raises Empty.
func tlFn(arg Val) (Val, error) {
	list := asList(arg)
	if len(list) == 0 {
		return nil, &MorelError{Exn: ExnEmpty}
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

// Nth returns the built-in form of a field selector: a function
// that extracts element i of a record or tuple value.
func Nth(i int) Fn {
	return func(arg Val) (Val, error) {
		return asList(arg)[i], nil
	}
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
	case uint64:
		return cmpOrdered(a, asWord(b))
	default:
		panic(fmt.Sprintf("cannot compare %T", a))
	}
}

func cmpOrdered[T int32 | float32 | string | uint64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// lastFn is "List.last xs"; Empty on the empty list.
func lastFn(arg Val) (Val, error) {
	list := asList(arg)
	if len(list) == 0 {
		return nil, &MorelError{Exn: ExnEmpty}
	}
	return list[len(list)-1], nil
}

// getItemFn is "List.getItem xs": SOME (hd, tl), or NONE on the
// empty list.
func getItemFn(arg Val) (Val, error) {
	list := asList(arg)
	if len(list) == 0 {
		return noneVal, nil
	}
	return someVal([]Val{list[0], list[1:]}), nil
}

// nthFn is "List.nth (xs, i)"; Subscript if i is out of range.
func nthFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	list, i := asList(a), asInt(b)
	if i < 0 || int(i) >= len(list) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	return list[i], nil
}

// onlyFn is "List.only xs" (a Morel extension): the sole
// element; Empty if the list is empty, Size if it has more than
// one element.
func onlyFn(arg Val) (Val, error) {
	list := asList(arg)
	switch len(list) {
	case 0:
		return nil, &MorelError{Exn: ExnEmpty}
	case 1:
		return list[0], nil
	default:
		return nil, &MorelError{Exn: ExnSize}
	}
}

// takeFn is "List.take (xs, i)": the first i elements;
// Subscript if i < 0 or i > length xs.
func takeFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	list, i := asList(a), asInt(b)
	if i < 0 || int(i) > len(list) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	return list[:i], nil
}

// dropFn is "List.drop (xs, i)": all but the first i elements;
// Subscript if i < 0 or i > length xs.
func dropFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	list, i := asList(a), asInt(b)
	if i < 0 || int(i) > len(list) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	return list[i:], nil
}

// listConcatFn is "List.concat xss", the concatenation of a
// list of lists.
func listConcatFn(arg Val) (Val, error) {
	lists := asList(arg)
	n := 0
	for _, l := range lists {
		n += len(asList(l))
	}
	out := make([]Val, 0, n)
	for _, l := range lists {
		out = append(out, asList(l)...)
	}
	return out, nil
}

// revAppendFn is "List.revAppend (xs, ys)": rev xs @ ys.
func revAppendFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	list, tail := asList(a), asList(b)
	out := make([]Val, 0, len(list)+len(tail))
	for _, v := range slices.Backward(list) {
		out = append(out, v)
	}
	return append(out, tail...), nil
}

// appFn is "List.app f xs": f applied to each element for its
// effect; the result is unit.
func appFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		for _, v := range asList(arg) {
			_, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
		}
		return unitVal, nil
	}), nil
}

// mapPartialFn is "List.mapPartial f xs": the SOME results of f.
func mapPartialFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		out := []Val{}
		for _, v := range asList(arg) {
			r, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
			if inner, isSome := asOption(r); isSome {
				out = append(out, inner)
			}
		}
		return out, nil
	}), nil
}

// findFn is "List.find f xs": SOME of the first element
// satisfying f, or NONE.
func findFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		for _, v := range asList(arg) {
			r, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
			if asBool(r) {
				return someVal(v), nil
			}
		}
		return noneVal, nil
	}), nil
}

// filterFn is "List.filter f xs".
func filterFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		out := []Val{}
		for _, v := range asList(arg) {
			r, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
			if asBool(r) {
				out = append(out, v)
			}
		}
		return out, nil
	}), nil
}

// partitionFn is "List.partition f xs": the elements that
// satisfy f, and those that do not.
func partitionFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		yes, no := []Val{}, []Val{}
		for _, v := range asList(arg) {
			r, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
			if asBool(r) {
				yes = append(yes, v)
			} else {
				no = append(no, v)
			}
		}
		return []Val{yes, no}, nil
	}), nil
}

// fold implements foldl (left-to-right) and foldr: f is applied
// to (element, accumulator).
func fold(leftToRight bool) Fn {
	return Curry2(func(f, init Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			list := asList(arg)
			acc := init
			for i := range list {
				v := list[i]
				if !leftToRight {
					v = list[len(list)-1-i]
				}
				r, err := ApplyVal(f, []Val{v, acc})
				if err != nil {
					return nil, err
				}
				acc = r
			}
			return acc, nil
		}), nil
	})
}

// existsFn is "List.exists f xs".
func existsFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		for _, v := range asList(arg) {
			r, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
			if asBool(r) {
				return true, nil
			}
		}
		return false, nil
	}), nil
}

// allFn is "List.all f xs".
func allFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		for _, v := range asList(arg) {
			r, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
			if !asBool(r) {
				return false, nil
			}
		}
		return true, nil
	}), nil
}

// tabulateFn is "List.tabulate (n, f)": the list [f 0, ...,
// f (n-1)]; Size if n < 0.
func tabulateFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	n := asInt(a)
	if n < 0 {
		return nil, &MorelError{Exn: ExnSize}
	}
	out := make([]Val, n)
	for i := range out {
		r, err := ApplyVal(b, int32(i))
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

// listCollateFn is "List.collate f (xs, ys)": lexicographic
// comparison using f on elements.
func listCollateFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		a, b := asPair(arg)
		xs, ys := asList(a), asList(b)
		for i := 0; i < len(xs) && i < len(ys); i++ {
			v, err := ApplyVal(f, []Val{xs[i], ys[i]})
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
		//nolint:gosec // a list's length fits in an int.
		return orderVal(cmpOrdered(int32(len(xs)),
			int32(len(ys)))), nil
	}), nil
}

// exceptFn is "List.except [xs, ys, ...]" (a Morel extension):
// the elements of the first list that appear in none of the
// others.
func exceptFn(arg Val) (Val, error) {
	lists := asList(arg)
	if len(lists) == 0 {
		return []Val{}, nil
	}
	out := []Val{}
	for _, v := range asList(lists[0]) {
		found := false
		for _, other := range lists[1:] {
			for _, w := range asList(other) {
				if valsEqual(v, w) {
					found = true
					break
				}
			}
		}
		if !found {
			out = append(out, v)
		}
	}
	return out, nil
}

// intersectFn is "List.intersect [xs, ys, ...]" (a Morel
// extension): the elements of the first list that appear in all
// of the others.
func intersectFn(arg Val) (Val, error) {
	lists := asList(arg)
	if len(lists) == 0 {
		return []Val{}, nil
	}
	out := []Val{}
	for _, v := range asList(lists[0]) {
		inAll := true
		for _, other := range lists[1:] {
			found := false
			for _, w := range asList(other) {
				if valsEqual(v, w) {
					found = true
					break
				}
			}
			if !found {
				inAll = false
				break
			}
		}
		if inAll {
			out = append(out, v)
		}
	}
	return out, nil
}

// mapiFn is "List.mapi f xs": f applied to (index, element).
func mapiFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		list := asList(arg)
		out := make([]Val, len(list))
		for i, v := range list {
			r, err := ApplyVal(f, []Val{int32(i), v})
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}), nil
}
