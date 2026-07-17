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

// The ListPair structure operates on two lists in parallel. The
// plain operations stop at the shorter list; the "Eq" variants
// raise UnequalLengths when the lists differ in length (except
// allEq, which simply returns false).

// listPairLen returns the two lists and the length to iterate, or
// an UnequalLengths error when eq is set and the lengths differ.
func listPairLen(arg Val, eq bool) ([]Val, []Val, int, error) {
	a, b := asPair(arg)
	l1, l2 := asList(a), asList(b)
	if eq && len(l1) != len(l2) {
		return nil, nil, 0, &MorelError{Exn: ExnUnequal}
	}
	return l1, l2, min(len(l1), len(l2)), nil
}

// listPairZip is "ListPair.zip (l1, l2)" and, with eq, "zipEq": the
// list of corresponding pairs.
func listPairZip(eq bool) Fn {
	return func(arg Val) (Val, error) {
		l1, l2, n, err := listPairLen(arg, eq)
		if err != nil {
			return nil, err
		}
		out := make([]Val, n)
		for i := range n {
			out[i] = []Val{l1[i], l2[i]}
		}
		return out, nil
	}
}

// listPairUnzipFn is "ListPair.unzip l": the pair of lists from a
// list of pairs.
func listPairUnzipFn(arg Val) (Val, error) {
	pairs := asList(arg)
	l1 := make([]Val, len(pairs))
	l2 := make([]Val, len(pairs))
	for i, p := range pairs {
		x, y := asPair(p)
		l1[i], l2[i] = x, y
	}
	return []Val{l1, l2}, nil
}

// listPairMap is "ListPair.map f (l1, l2)" and, with eq, "mapEq".
func listPairMap(eq bool) Fn {
	return func(f Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			l1, l2, n, err := listPairLen(arg, eq)
			if err != nil {
				return nil, err
			}
			out := make([]Val, n)
			for i := range n {
				r, err := ApplyVal(f, []Val{l1[i], l2[i]})
				if err != nil {
					return nil, err
				}
				out[i] = r
			}
			return out, nil
		}), nil
	}
}

// listPairApp is "ListPair.app f (l1, l2)" and, with eq, "appEq".
func listPairApp(eq bool) Fn {
	return func(f Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			l1, l2, n, err := listPairLen(arg, eq)
			if err != nil {
				return nil, err
			}
			for i := range n {
				_, err := ApplyVal(f, []Val{l1[i], l2[i]})
				if err != nil {
					return nil, err
				}
			}
			return unitVal, nil
		}), nil
	}
}

// listPairFold is "ListPair.foldl/foldr f init (l1, l2)", left to
// right or right to left, and with eq the "Eq" variants.
func listPairFold(leftToRight, eq bool) Fn {
	return Curry2(func(f, init Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			l1, l2, n, err := listPairLen(arg, eq)
			if err != nil {
				return nil, err
			}
			acc := init
			for k := range n {
				i := k
				if !leftToRight {
					i = n - 1 - k
				}
				r, err := ApplyVal(f, []Val{l1[i], l2[i], acc})
				if err != nil {
					return nil, err
				}
				acc = r
			}
			return acc, nil
		}), nil
	})
}

// listPairTest is "ListPair.all f (l1, l2)" (want true) or "exists"
// (want false), stopping at the shorter list.
func listPairTest(want bool) Fn {
	return func(f Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			l1, l2, n, _ := listPairLen(arg, false)
			for i := range n {
				r, err := ApplyVal(f, []Val{l1[i], l2[i]})
				if err != nil {
					return nil, err
				}
				if asBool(r) != want {
					return !want, nil
				}
			}
			return want, nil
		}), nil
	}
}

// listPairAllEqFn is "ListPair.allEq f (l1, l2)": false if the
// lists differ in length, else whether f holds for every pair.
func listPairAllEqFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		a, b := asPair(arg)
		l1, l2 := asList(a), asList(b)
		if len(l1) != len(l2) {
			return false, nil
		}
		for i := range l1 {
			r, err := ApplyVal(f, []Val{l1[i], l2[i]})
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
