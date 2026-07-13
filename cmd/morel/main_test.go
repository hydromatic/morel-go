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

package main

import "testing"

func TestAdd(t *testing.T) {
	cases := []struct {
		a, b, want int
	}{
		{3, 4, 7},
		{-1, 1, 0},
		{0, 0, 0},
		{-5, -5, -10},
	}
	for _, c := range cases {
		if got := add(c.a, c.b); got != c.want {
			t.Errorf("add(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
