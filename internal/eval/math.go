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

import "math"

// The Math structure. Most members reuse the real1/real2 helpers;
// only pow needs its own special-casing.

// mathPowFn is "Math.pow (x, y)". It follows C's pow except that a
// base of magnitude one raised to a non-finite exponent is NaN, not
// one, as the Standard ML Basis requires.
func mathPowFn(arg Val) (Val, error) {
	x, y := asRealPair(arg)
	if math.Abs(x) == 1 && (math.IsInf(y, 0) || math.IsNaN(y)) {
		return float32(math.NaN()), nil
	}
	return float32(math.Pow(x, y)), nil
}
