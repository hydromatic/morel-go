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

// The Either structure. An "('a, 'b) either" value is a Con of the
// "either" datatype: INL holds a left value, INR a right value.

const eitherDatatype = "either"

// leftVal and rightVal build the INL and INR constructor values.
func leftVal(a Val) Val {
	return Con{Datatype: eitherDatatype, Name: "INL", Arg: a}
}

func rightVal(a Val) Val {
	return Con{Datatype: eitherDatatype, Name: "INR", Arg: a, Ordinal: 1}
}

// asEither reports whether an either value is a left (INL), and
// returns its payload.
func asEither(v Val) (bool, Val) {
	con, _ := v.(Con)
	return con.Name == "INL", con.Arg
}

// eitherIsLeftFn is "Either.isLeft e"; eitherIsRightFn its dual.
func eitherIsLeftFn(arg Val) (Val, error) {
	left, _ := asEither(arg)
	return left, nil
}

func eitherIsRightFn(arg Val) (Val, error) {
	left, _ := asEither(arg)
	return !left, nil
}

// eitherAsLeftFn is "Either.asLeft e": SOME the left value, or NONE.
func eitherAsLeftFn(arg Val) (Val, error) {
	if left, a := asEither(arg); left {
		return someVal(a), nil
	}
	return noneVal, nil
}

func eitherAsRightFn(arg Val) (Val, error) {
	if left, a := asEither(arg); !left {
		return someVal(a), nil
	}
	return noneVal, nil
}

// eitherProjFn is "Either.proj e": the value, whichever side it is
// (the two sides share a type).
func eitherProjFn(arg Val) (Val, error) {
	_, a := asEither(arg)
	return a, nil
}

// eitherMapFn is "Either.map (f, g) e": the value with f applied to
// a left, g to a right.
func eitherMapFn(fg Val) (Val, error) {
	f, g := asPair(fg)
	return Fn(func(arg Val) (Val, error) {
		left, a := asEither(arg)
		if left {
			r, err := ApplyVal(f, a)
			if err != nil {
				return nil, err
			}
			return leftVal(r), nil
		}
		r, err := ApplyVal(g, a)
		if err != nil {
			return nil, err
		}
		return rightVal(r), nil
	}), nil
}

// eitherMapSide is "Either.mapLeft f e" (left) or "mapRight" (right):
// the value with f applied to the matching side, else unchanged.
func eitherMapSide(onLeft bool) Fn {
	return func(f Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			left, a := asEither(arg)
			if left != onLeft {
				return arg, nil
			}
			r, err := ApplyVal(f, a)
			if err != nil {
				return nil, err
			}
			if onLeft {
				return leftVal(r), nil
			}
			return rightVal(r), nil
		}), nil
	}
}

// eitherAppFn is "Either.app (f, g) e": f or g applied for effect.
func eitherAppFn(fg Val) (Val, error) {
	f, g := asPair(fg)
	return Fn(func(arg Val) (Val, error) {
		left, a := asEither(arg)
		h := g
		if left {
			h = f
		}
		_, err := ApplyVal(h, a)
		if err != nil {
			return nil, err
		}
		return unitVal, nil
	}), nil
}

// eitherAppSide is "Either.appLeft f e" or "appRight": f applied for
// effect only when the value is the matching side.
func eitherAppSide(onLeft bool) Fn {
	return func(f Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			left, a := asEither(arg)
			if left == onLeft {
				_, err := ApplyVal(f, a)
				if err != nil {
					return nil, err
				}
			}
			return unitVal, nil
		}), nil
	}
}

// eitherFoldFn is "Either.fold (f, g) init e": f (left, init) or
// g (right, init).
func eitherFoldFn(fg Val) (Val, error) {
	f, g := asPair(fg)
	return Curry2(func(init, e Val) (Val, error) {
		left, a := asEither(e)
		h := g
		if left {
			h = f
		}
		return ApplyVal(h, []Val{a, init})
	}), nil
}

// eitherPartitionFn is "Either.partition es": the left payloads and
// the right payloads, as a pair of lists in order.
func eitherPartitionFn(arg Val) (Val, error) {
	left, right := []Val{}, []Val{}
	for _, e := range asList(arg) {
		if isLeft, a := asEither(e); isLeft {
			left = append(left, a)
		} else {
			right = append(right, a)
		}
	}
	return []Val{left, right}, nil
}
