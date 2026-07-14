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

package ast

import "strings"

// UnparsePat renders a pattern as source text: an implicit
// record field renders expanded
// ("{a = a}"), and a nested cons or layered pattern is
// parenthesized where the grammar requires.
func UnparsePat(p Pat) string {
	var b strings.Builder
	unparsePat(&b, p)
	return b.String()
}

func unparsePat(b *strings.Builder, p Pat) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := p.(type) {
	case *AsPat:
		b.WriteString(n.Name + " as ")
		unparsePat(b, n.Pat)
	case *ConsPat:
		unparseConsSide(b, n.A0)
		b.WriteString(" :: ")
		unparsePat(b, n.A1)
	case *IDPat:
		b.WriteString(n.Name)
	case *ListPat:
		unparsePatList(b, "[", n.Args, "]")
	case *LiteralPat:
		b.WriteString(n.Value)
	case *RecordPat:
		unparseRecordPat(b, n)
	case *TuplePat:
		unparsePatList(b, "(", n.Args, ")")
	case *WildcardPat:
		b.WriteString("_")
	}
}

// unparseConsSide parenthesizes the left side of "::" when it is
// itself a cons or layered pattern.
func unparseConsSide(b *strings.Builder, p Pat) {
	switch p.(type) {
	case *ConsPat, *AsPat:
		b.WriteString("(")
		unparsePat(b, p)
		b.WriteString(")")
	default:
		unparsePat(b, p)
	}
}

func unparsePatList(b *strings.Builder, open string, args []Pat,
	closer string,
) {
	b.WriteString(open)
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		unparsePat(b, a)
	}
	b.WriteString(closer)
}

func unparseRecordPat(b *strings.Builder, n *RecordPat) {
	b.WriteString("{")
	for i, f := range n.Fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.Label + " = ")
		unparsePat(b, f.Pat)
	}
	if n.Ellipsis {
		if len(n.Fields) > 0 {
			b.WriteString(", ")
		}
		b.WriteString("...")
	}
	b.WriteString("}")
}
