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

// The Fn structure: function combinators. (Fn.id reuses identityFn
// and Fn.o reuses composeFn.)

// fnConstFn is "Fn.const x": the function that ignores its argument
// and returns x.
func fnConstFn(x Val) (Val, error) {
	return Fn(func(Val) (Val, error) {
		return x, nil
	}), nil
}

// fnApplyFn is "Fn.apply (f, x)": f x.
func fnApplyFn(arg Val) (Val, error) {
	f, x := asPair(arg)
	return ApplyVal(f, x)
}

// fnCurryFn is "Fn.curry f": the curried form, fn a => fn b =>
// f (a, b).
func fnCurryFn(f Val) (Val, error) {
	return Curry2(func(a, b Val) (Val, error) {
		return ApplyVal(f, []Val{a, b})
	}), nil
}

// fnUncurryFn is "Fn.uncurry f (a, b)": f a b.
func fnUncurryFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		a, b := asPair(arg)
		g, err := ApplyVal(f, a)
		if err != nil {
			return nil, err
		}
		return ApplyVal(g, b)
	}), nil
}

// fnFlipFn is "Fn.flip f (b, a)": f (a, b).
func fnFlipFn(f Val) (Val, error) {
	return Fn(func(arg Val) (Val, error) {
		b, a := asPair(arg)
		return ApplyVal(f, []Val{a, b})
	}), nil
}

// fnRepeatFn is "Fn.repeat n f": the function that applies f n
// times (the identity when n is zero). A negative n raises Domain,
// immediately, before the function argument arrives.
func fnRepeatFn(n Val) (Val, error) {
	count := int(asInt(n))
	if count < 0 {
		return nil, &MorelError{Exn: ExnDomain}
	}
	return Fn(func(f Val) (Val, error) {
		return Fn(func(arg Val) (Val, error) {
			x := arg
			for range count {
				r, err := ApplyVal(f, x)
				if err != nil {
					return nil, err
				}
				x = r
			}
			return x, nil
		}), nil
	}), nil
}

// fnEqual is "Fn.equal x y" (negate false) or "Fn.notEqual" (true),
// the curried equality test.
func fnEqual(negate bool) Fn {
	return Curry2(func(a, b Val) (Val, error) {
		return valsEqual(a, b) != negate, nil
	})
}
