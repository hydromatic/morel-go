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
	case *AnnotatedPat:
		unparsePat(b, n.Pat)
		b.WriteString(" : " + UnparseType(n.Type))
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

// UnparseType renders a type expression as source text: a
// function type parenthesizes a function-type parameter, and a
// tuple type parenthesizes function- and tuple-type elements.
func UnparseType(t Type) string {
	var b strings.Builder
	unparseType(&b, t)
	return b.String()
}

func unparseType(b *strings.Builder, t Type) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *FnType:
		unparseTypeArg(b, n.Param, false)
		b.WriteString(" -> ")
		unparseType(b, n.Result)
	case *NamedType:
		unparseNamedType(b, n)
	case *RecordType:
		b.WriteString("{")
		for i, f := range n.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(f.Label + ": ")
			unparseType(b, f.Type)
		}
		b.WriteString("}")
	case *TupleType:
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(" * ")
			}
			unparseTypeArg(b, a, true)
		}
	case *TyVar:
		b.WriteString(n.Name)
	}
}

// unparseTypeArg parenthesizes an operand where the grammar
// requires: a function type always, a tuple type inside another
// tuple type.
func unparseTypeArg(b *strings.Builder, t Type, inTuple bool) {
	_, isFn := t.(*FnType)
	_, isTuple := t.(*TupleType)
	if isFn || (inTuple && isTuple) {
		b.WriteString("(")
		unparseType(b, t)
		b.WriteString(")")
		return
	}
	unparseType(b, t)
}

func unparseNamedType(b *strings.Builder, n *NamedType) {
	switch len(n.Args) {
	case 0:
	case 1:
		unparseTypeArg(b, n.Args[0], true)
		b.WriteString(" ")
	default:
		b.WriteString("(")
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			unparseType(b, a)
		}
		b.WriteString(") ")
	}
	b.WriteString(n.Name)
}

// unparseDatatype renders a datatype declaration, including its
// keyword.
func unparseDatatype(d *DatatypeDecl) string {
	var b strings.Builder
	b.WriteString("datatype ")
	for i, bind := range d.Binds {
		if i > 0 {
			b.WriteString(" and ")
		}
		unparseTyVars(&b, bind.TyVars)
		b.WriteString(bind.Name + " = ")
		for j, c := range bind.Cons {
			if j > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(c.Name)
			if c.Of != nil {
				b.WriteString(" of " + UnparseType(c.Of))
			}
		}
	}
	return b.String()
}

// unparseTypeDecl renders a type-alias declaration, including its
// keyword.
func unparseTypeDecl(d *TypeDecl) string {
	var b strings.Builder
	b.WriteString("type ")
	for i, bind := range d.Binds {
		if i > 0 {
			b.WriteString(" and ")
		}
		unparseTyVars(&b, bind.TyVars)
		b.WriteString(bind.Name + " = " + UnparseType(bind.Type))
	}
	return b.String()
}

func unparseTyVars(b *strings.Builder, tyVars []string) {
	switch len(tyVars) {
	case 0:
	case 1:
		b.WriteString(tyVars[0] + " ")
	default:
		b.WriteString("(" + strings.Join(tyVars, ", ") + ") ")
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
