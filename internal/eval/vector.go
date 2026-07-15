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

// The Vector built-ins. A vector shares the list representation
// ([]Val), so its element-wise operations (app, map, foldl,
// foldr, find, exists, all, collate, tabulate, concat, length)
// reuse the List code; only the indexed operations and the
// index-checked sub/update are specific to it.

// vectorMaxLen is the largest supported vector length,
// 2^24 - 1.
const vectorMaxLen = int32(1<<24 - 1)

// vectorFromListFn is "Vector.fromList l": a vector holds the
// same elements as the list, in the same representation.
func vectorFromListFn(arg Val) (Val, error) {
	return arg, nil
}

// vectorSubFn is "Vector.sub (vec, i)"; Subscript if i is out of
// range.
func vectorSubFn(arg Val) (Val, error) {
	v, i := asPair(arg)
	vec := asList(v)
	n := asInt(i)
	if n < 0 || int(n) >= len(vec) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	return vec[n], nil
}

// vectorUpdateFn is "Vector.update (vec, i, x)": a copy of vec
// with element i replaced by x; Subscript if i is out of range.
func vectorUpdateFn(arg Val) (Val, error) {
	args := asList(arg)
	vec := asList(args[0])
	n := asInt(args[1])
	if n < 0 || int(n) >= len(vec) {
		return nil, &MorelError{Exn: ExnSubscript}
	}
	out := make([]Val, len(vec))
	copy(out, vec)
	out[n] = args[2]
	return out, nil
}

// vectorAppiFn is "Vector.appi f vec": f applied to each (index,
// element) pair for its effect.
func vectorAppiFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		for i, v := range asList(arg) {
			_, err := ApplyVal(f, []Val{int32(i), v})
			if err != nil {
				return nil, err
			}
		}
		return unitVal, nil
	}), nil
}

// vectorMapiFn is "Vector.mapi f vec": the vector of f applied to
// each (index, element) pair.
func vectorMapiFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		vec := asList(arg)
		out := make([]Val, len(vec))
		for i, v := range vec {
			r, err := ApplyVal(f, []Val{int32(i), v})
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}), nil
}

// vectorFoldi folds f over the (index, element, accumulator)
// triples of a vector, left to right (foldli) or right to left
// (foldri).
func vectorFoldi(leftToRight bool) Fn {
	return Curry2(func(f, init Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			vec := asList(arg)
			acc := init
			for k := range vec {
				i := k
				if !leftToRight {
					i = len(vec) - 1 - k
				}
				r, err := ApplyVal(f,
					[]Val{int32(i), vec[i], acc})
				if err != nil {
					return nil, err
				}
				acc = r
			}
			return acc, nil
		}), nil
	})
}

// vectorFindiFn is "Vector.findi f vec": SOME (i, x) of the first
// (index, element) pair satisfying f, or NONE.
func vectorFindiFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		for i, v := range asList(arg) {
			r, err := ApplyVal(f, []Val{int32(i), v})
			if err != nil {
				return nil, err
			}
			if asBool(r) {
				return someVal([]Val{int32(i), v}), nil
			}
		}
		return noneVal, nil
	}), nil
}
