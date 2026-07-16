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

import "strings"

// stringCvtPadLeftFn is "StringCvt.padLeft c i s": s padded on the
// left with copies of c to a width of at least i, or s unchanged if
// it is already that wide.
func stringCvtPadLeftFn(c, i, s Val) (Val, error) {
	str := asString(s)
	n := int(asInt(i)) - len(str)
	if n <= 0 {
		return str, nil
	}
	return strings.Repeat(string(asChar(c)), n) + str, nil
}

// stringCvtPadRightFn is "StringCvt.padRight c i s": s padded on the
// right with copies of c to a width of at least i, or s unchanged if
// it is already that wide.
func stringCvtPadRightFn(c, i, s Val) (Val, error) {
	str := asString(s)
	n := int(asInt(i)) - len(str)
	if n <= 0 {
		return str, nil
	}
	return str + strings.Repeat(string(asChar(c)), n), nil
}
