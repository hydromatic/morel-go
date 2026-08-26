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

//nolint:testpackage // white-box: a tail call's sentinel is unexported
package compile

import "testing"

// TestCharValue checks the value of a character constant from its
// unquoted text. A "\ddd" escape decodes to one byte, which above
// 127 is not a character of UTF-8 on its own, so reading the text
// as UTF-8 would give the replacement character.
func TestCharValue(t *testing.T) {
	tests := []struct {
		text string
		want int32
		ok   bool
	}{
		{"a", 'a', true},
		{"\x00", 0, true},
		{"\x7f", 127, true},
		{"\x80", 128, true},
		{"\xfd", 253, true},
		{"\xff", 255, true},
		// A character written as itself is UTF-8, and is a
		// character only if a byte can hold it.
		{"é", 233, true},
		{"€", 0, false},
		{"", 0, false},
		{"ab", 0, false},
	}
	for _, test := range tests {
		got, ok := charValue(test.text)
		if ok != test.ok || got != test.want {
			t.Errorf("charValue(%q) = %d, %v; want %d, %v",
				test.text, got, ok, test.want, test.ok)
		}
	}
}
