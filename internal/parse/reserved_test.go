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

//nolint:testpackage // white-box: reads the unexported reservedWords
package parse

import (
	"testing"

	"github.com/hydromatic/morel-go/internal/token"
)

// notReserved are lexer keyword tokens that are not morel reserved
// words: they are internal, never written as a source
// identifier, so reservedWords omits them.
var notReserved = map[string]bool{
	"type_string": true, // the ":t" type-query marker
	"yieldAll":    true, // the implicit "yield" of all fields
}

// TestReservedWordsMatchKeywords keeps reservedWords in sync with
// the lexer's keyword set, so the list cannot silently fall out of
// date: every keyword is a reserved word (bar the internal tokens
// above), and every reserved word is a keyword. Adding a keyword
// to the lexer fails this test until reservedWords is updated
// to match.
func TestReservedWordsMatchKeywords(t *testing.T) {
	for k := token.And; k <= token.Over; k++ {
		kw := k.String()
		if notReserved[kw] {
			continue
		}
		if !reservedWords[kw] {
			t.Errorf("keyword %q is missing from reservedWords",
				kw)
		}
	}
	for w := range reservedWords {
		if token.Lookup(w) == token.Ident {
			t.Errorf("reservedWords has %q, which is not a "+
				"lexer keyword", w)
		}
	}
}
