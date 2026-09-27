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

	"github.com/hydromatic/morel-go/internal/core"
)

// The list and string built-ins, and the equality and comparison
// operators.

func asList(v Val) []Val {
	if r, ok := v.(Relation); ok {
		return r.Rows
	}
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
//
// A char is one of 256 values and a string is indexed by byte, so
// the characters are the string's bytes. Reading them as runes
// would decode UTF-8, and a byte above 127 that is not part of a
// sequence -- "\128", which a string may hold -- would read as the
// replacement character.
func explodeFn(arg Val) (Val, error) {
	s := asString(arg)
	out := make([]Val, 0, len(s))
	for i := range len(s) {
		out = append(out, rune(s[i]))
	}
	return out, nil
}

// implodeFn is "implode cs", the string of a character list.
func implodeFn(arg Val) (Val, error) {
	list := asList(arg)
	// Each character is one byte, as explode reads them; a char is
	// one of 256 values, so the conversion cannot lose anything.
	b := make([]byte, len(list))
	for i, v := range list {
		b[i] = byte(asChar(v) & maxCharVal)
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
		// A directory's fields are its entries, so selecting one
		// reads that entry rather than indexing a record's values.
		if f, isFile := arg.(*File); isFile {
			return f.Field(i), nil
		}
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
	// lint: sort until '^\t}' where '^\tcase '
	switch a := a.(type) {
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
	case Variant:
		// Variants are equal when their (interned) types are the
		// same type and their payloads are equal.
		b2, ok := b.(Variant)
		return ok && a.Type == b2.Type &&
			valsEqual(a.Value, b2.Value)
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
	default:
		return a == b
	}
}

// compareFn adapts an ordering test to the comparison operators
// "<", "<=", ">", and ">=". They apply to any type that has an
// order, which is every type but a function.
func compareFn(test func(c int) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		// A comparison that reaches a NaN is unordered, as IEEE 754
		// requires, and then the operator is false whichever way it
		// points -- even inside a tuple, record, list or option.
		c, ordered := partialCompareVals(a, b)
		if !ordered {
			return false, nil
		}
		return test(c), nil
	}
}

// compareVals is the total order: every value has a place in it, so
// it can sort. "order", "min" and "max" use it, as does the range
// machinery. Reals are ordered with ~0.0 before 0.0 and NaN after
// every other value.
func compareVals(a, b Val) int {
	c, _ := compareVals1(a, b, false)
	return c
}

// partialCompareVals is the order the comparison operators use: the
// total order, except that reals follow IEEE 754, where ~0.0 equals
// 0.0 and any comparison involving NaN is unordered. The second
// result is false when a NaN decided the outcome; a NaN that an
// earlier part has already settled does not count, so
// "(1.0, 0.0 / 0.0) < (2.0, 3.0)" is ordered, and true.
func partialCompareVals(a, b Val) (int, bool) {
	return compareVals1(a, b, true)
}

// compareVals1 compares two values, following IEEE 754 for reals if
// `partial`. The second result is false if the comparison is
// unordered, which only IEEE 754 real comparison can make it.
func compareVals1(a, b Val, partial bool) (int, bool) {
	// lint: sort until '^	}' where '^	case '
	switch a := a.(type) {
	case Con:
		bc, ok := b.(Con)
		if !ok {
			panic(fmt.Sprintf("expected datatype, got %T", b))
		}
		return compareCons(a, bc, partial)
	case Decimal:
		// Canonical form makes the comparison exact, and a decimal
		// has no NaN, so it is never unordered.
		return decCmp(a, asDecimal(b)), true
	case []Val:
		bs, _ := b.([]Val)
		return compareSlices(a, bs, partial)
	case bool:
		bb, _ := b.(bool)
		return cmpBool(a, bb), true
	case core.Unit:
		// All units are equal.
		return 0, true
	case float32:
		f, ok := b.(float32)
		if !ok {
			panic(fmt.Sprintf("expected real, got %T", b))
		}
		if partial {
			return cmpRealPartial(a, f)
		}
		return cmpReal(a, f), true
	case int32:
		return cmpOrdered(a, asInt(b)), true
	case nil:
		// A cleared slot outside the current row; any consistent
		// order will do.
		if b == nil {
			return 0, true
		}
		return -1, true
	case string:
		return cmpOrdered(a, asString(b)), true
	case uint64:
		return cmpOrdered(a, asWord(b)), true
	default:
		panic(fmt.Sprintf("cannot compare %T", a))
	}
}

// compareCons compares two values of a datatype. A "descending"
// value (DESC x) reverses the order of what it wraps, so
// "order (DESC e)" sorts by e descending. Any other datatype
// compares by constructor, then by argument; a constant constructor
// has no argument.
func compareCons(a, b Con, partial bool) (int, bool) {
	if a.Datatype == descendingDatatype {
		c, ordered := compareVals1(a.Arg, b.Arg, partial)
		return -c, ordered
	}
	if a.Ordinal != b.Ordinal {
		if a.Ordinal < b.Ordinal {
			return -1, true
		}
		return 1, true
	}
	if a.Arg == nil {
		return 0, true
	}
	return compareVals1(a.Arg, b.Arg, partial)
}

// compareSlices compares two tuples, or two records whose fields
// are in canonical order, lexicographically. A sequence that is a
// prefix of the other comes first.
func compareSlices(a, b []Val, partial bool) (int, bool) {
	for i := range a {
		if i >= len(b) {
			return 1, true
		}
		c, ordered := compareVals1(a[i], b[i], partial)
		if !ordered {
			return 0, false
		}
		if c != 0 {
			return c, true
		}
	}
	if len(b) > len(a) {
		return -1, true
	}
	return 0, true
}

// cmpReal is the total order on reals: NaN comes after every other
// value and equals itself, and ~0.0 comes before 0.0. IEEE 754
// leaves the first pair unordered and calls the second equal, which
// would leave "order" to the accident of input order.
func cmpReal(a, b float32) int {
	aNaN, bNaN := isNaN(a), isNaN(b)
	switch {
	case aNaN && bNaN:
		return 0
	case aNaN:
		return 1
	case bNaN:
		return -1
	case a < b:
		return -1
	case a > b:
		return 1
	}
	// Equal under IEEE 754, so only the sign of a zero is left to
	// separate them.
	aNeg, bNeg := math.Signbit(float64(a)), math.Signbit(float64(b))
	switch {
	case aNeg == bNeg:
		return 0
	case aNeg:
		return -1
	default:
		return 1
	}
}

// cmpRealPartial compares two reals as IEEE 754 does: ~0.0 equals
// 0.0, and a comparison involving NaN is unordered.
func cmpRealPartial(a, b float32) (int, bool) {
	if isNaN(a) || isNaN(b) {
		return 0, false
	}
	return cmpOrdered(a, b), true
}

// isNaN reports whether a real is NaN.
func isNaN(f float32) bool { return math.IsNaN(float64(f)) }

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

// exceptFn is "List.except [xs, ys, ...]" (a Morel extension): the
// multiset difference of the first list and the others, so an
// element is dropped once per occurrence in the others, not
// entirely.
func exceptFn(arg Val) (Val, error) {
	return setOpFn(arg, exceptOp)
}

// intersectFn is "List.intersect [xs, ys, ...]" (a Morel
// extension): the multiset meet of the lists, keeping each element
// as many times as it appears in every list.
func intersectFn(arg Val) (Val, error) {
	return setOpFn(arg, intersectOp)
}

// setOpFn applies a multiset set-operation (the same one the
// query set-op steps use) to "List.op [xs, ys, ...]".
func setOpFn(arg Val,
	op func(left []Val, args [][]Val, distinct bool) []Val,
) (Val, error) {
	lists := asList(arg)
	if len(lists) == 0 {
		return []Val{}, nil
	}
	first := asList(lists[0])
	if len(lists) == 1 {
		// A single list is its own intersection and difference.
		return first, nil
	}
	args := make([][]Val, len(lists)-1)
	for i, other := range lists[1:] {
		args[i] = asList(other)
	}
	out := op(first, args, false)
	if out == nil {
		out = []Val{}
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
