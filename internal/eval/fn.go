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

// The Fn structure keeps three members native — id (identityFn),
// o (composeFn), and the recursive repeat — while its other
// combinators are derived in Morel (lib/fn.sml).

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
