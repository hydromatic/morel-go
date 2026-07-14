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

import (
	"fmt"
	"strings"
)

// Dump returns a parenthesized S-expression rendering of node,
// the Sys.parseTree output format: each non-leaf node
// is "(kind child1 child2 ...)" where kind is the node's Op name;
// leaves render atomically. The output makes tree structure
// assertable from .smli scripts; it is not re-parsable.
func Dump(node Node) string {
	var b strings.Builder
	dump(&b, node)
	return b.String()
}

func dump(b *strings.Builder, node Node) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := node.(type) {
	case *AnnotatedExp:
		b.WriteString("(annotatedExp ")
		dump(b, n.Exp)
		b.WriteString(" ")
		dump(b, n.Type)
		b.WriteString(")")
	case *Apply:
		b.WriteString("(apply ")
		dump(b, n.Fn)
		b.WriteString(" ")
		dump(b, n.Arg)
		b.WriteString(")")
	case *Case:
		b.WriteString("(case ")
		dump(b, n.Exp)
		dumpMatches(b, "", n.Matches)
	case *DatatypeDecl:
		b.WriteString("(datatype_decl " + unparseDatatype(n) +
			")")
	case *Fn:
		dumpMatches(b, "(fn", n.Matches)
	case *FnType:
		b.WriteString("(fnType ")
		dump(b, n.Param)
		b.WriteString(" ")
		dump(b, n.Result)
		b.WriteString(")")
	case *From:
		b.WriteString("(" + n.Kind.String() + " ")
		unparseFrom(b, n)
		b.WriteString(")")
	case *FunBind:
		dumpList(b, "(funBind", n.Matches)
	case *FunDecl:
		dumpList(b, "(fun", n.Binds)
	case *FunMatch:
		dumpFunMatch(b, n)
	case *ID:
		b.WriteString("(id " + n.Name + ")")
	case *If:
		b.WriteString("(if ")
		dump(b, n.Cond)
		b.WriteString(" ")
		dump(b, n.IfTrue)
		b.WriteString(" ")
		dump(b, n.IfFalse)
		b.WriteString(")")
	case *InfixCall:
		b.WriteString("(" + n.Kind.String() + " ")
		dump(b, n.A0)
		b.WriteString(" ")
		dump(b, n.A1)
		b.WriteString(")")
	case *Let:
		dumpLet(b, n)
	case *ListExp:
		sexp(b, "list", n.Args)
	case *Literal:
		dumpLiteral(b, n)
	case *Match:
		b.WriteString("(match ")
		dumpPat(b, n.Pat)
		b.WriteString(" ")
		dump(b, n.Exp)
		b.WriteString(")")
	case *NamedType:
		dumpNamedType(b, n)
	case *PrefixCall:
		b.WriteString("(" + n.Kind.String() + " ")
		dump(b, n.A)
		b.WriteString(")")
	case *Record:
		dumpRecord(b, n)
	case *RecordSelector:
		b.WriteString("(record_selector #" + n.Name + ")")
	case *RecordType:
		b.WriteString("(record_type " + UnparseType(n) + ")")
	case *Tuple:
		sexp(b, "tuple", n.Args)
	case *TupleType:
		dumpList(b, "(tupleType", n.Args)
	case *TyVar:
		b.WriteString("(tyVar " + n.Name + ")")
	case *TypeDecl:
		b.WriteString("(type_decl " + unparseTypeDecl(n) + ")")
	case *ValBind:
		b.WriteString("(valBind ")
		dumpPat(b, n.Pat)
		b.WriteString(" ")
		dump(b, n.Exp)
		b.WriteString(")")
	case *ValDecl:
		dumpValDecl(b, n)
	default:
		panic(fmt.Sprintf("dump: unknown node %T", node))
	}
}

func dumpFunMatch(b *strings.Builder, n *FunMatch) {
	b.WriteString("(funMatch " + n.Name)
	for _, pat := range n.Pats {
		b.WriteString(" ")
		dumpPat(b, pat)
	}
	if n.ReturnType != nil {
		b.WriteString(" ")
		dump(b, n.ReturnType)
	}
	b.WriteString(" ")
	dump(b, n.Exp)
	b.WriteString(")")
}

func dumpLet(b *strings.Builder, n *Let) {
	b.WriteString("(let")
	for _, d := range n.Decls {
		b.WriteString(" ")
		dump(b, d)
	}
	b.WriteString(" ")
	dump(b, n.Exp)
	b.WriteString(")")
}

func dumpNamedType(b *strings.Builder, n *NamedType) {
	b.WriteString("(named " + n.Name)
	for _, a := range n.Args {
		b.WriteString(" ")
		dump(b, a)
	}
	b.WriteString(")")
}

func dumpRecord(b *strings.Builder, n *Record) {
	b.WriteString("(record")
	for _, f := range n.Fields {
		b.WriteString(" (" + f.Label + " ")
		dump(b, f.Exp)
		b.WriteString(")")
	}
	b.WriteString(")")
}

func dumpValDecl(b *strings.Builder, n *ValDecl) {
	b.WriteString("(val")
	if n.Rec {
		b.WriteString(" rec")
	}
	for _, vb := range n.Binds {
		b.WriteString(" ")
		dump(b, vb)
	}
	b.WriteString(")")
}

// dumpList renders "open child1 child2 ...)".
func dumpList[T Node](b *strings.Builder, open string,
	children []T,
) {
	b.WriteString(open)
	for _, c := range children {
		b.WriteString(" ")
		dump(b, c)
	}
	b.WriteString(")")
}

func dumpMatches(b *strings.Builder, open string,
	matches []*Match,
) {
	b.WriteString(open)
	for _, m := range matches {
		b.WriteString(" ")
		dump(b, m)
	}
	b.WriteString(")")
}

// dumpPat renders a pattern. Simple patterns have their own
// forms; compound patterns render as the pattern's op name plus
// its unparsed source.
func dumpPat(b *strings.Builder, p Pat) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := p.(type) {
	case *AnnotatedPat:
		b.WriteString("(annotatedPat ")
		dumpPat(b, n.Pat)
		b.WriteString(" ")
		dump(b, n.Type)
		b.WriteString(")")
	case *IDPat:
		b.WriteString("(idPat " + n.Name + ")")
	case *LiteralPat:
		b.WriteString("(" + n.Kind.String() + " " + n.Value + ")")
	case *TuplePat:
		b.WriteString("(tuplePat")
		for _, a := range n.Args {
			b.WriteString(" ")
			dumpPat(b, a)
		}
		b.WriteString(")")
	case *WildcardPat:
		b.WriteString("wildcard")
	default:
		b.WriteString("(" + p.Op().String() + " " +
			UnparsePat(p) + ")")
	}
}

// sexp renders "(kind c1 c2 ...)".
func sexp(b *strings.Builder, kind string, children []Expr) {
	b.WriteString("(" + kind)
	for _, c := range children {
		b.WriteString(" ")
		dump(b, c)
	}
	b.WriteString(")")
}

func dumpLiteral(b *strings.Builder, l *Literal) {
	b.WriteString("(" + l.Kind.String() + " ")
	switch l.Kind {
	case StringLiteralOp:
		b.WriteString(`"` + l.Value + `"`)
	case CharLiteralOp:
		b.WriteString(`#"` + l.Value + `"`)
	default:
		b.WriteString(l.Value)
	}
	b.WriteString(")")
}
