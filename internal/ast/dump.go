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
	case *Apply:
		b.WriteString("(apply ")
		dump(b, n.Fn)
		b.WriteString(" ")
		dump(b, n.Arg)
		b.WriteString(")")
	case *ID:
		b.WriteString("(id " + n.Name + ")")
	case *InfixCall:
		b.WriteString("(" + n.Kind.String() + " ")
		dump(b, n.A0)
		b.WriteString(" ")
		dump(b, n.A1)
		b.WriteString(")")
	case *ListExp:
		sexp(b, "list", n.Args)
	case *Literal:
		dumpLiteral(b, n)
	case *PrefixCall:
		b.WriteString("(" + n.Kind.String() + " ")
		dump(b, n.A)
		b.WriteString(")")
	case *Record:
		b.WriteString("(record")
		for _, f := range n.Fields {
			b.WriteString(" (" + f.Label + " ")
			dump(b, f.Exp)
			b.WriteString(")")
		}
		b.WriteString(")")
	case *RecordSelector:
		b.WriteString("(record_selector #" + n.Name + ")")
	case *Tuple:
		sexp(b, "tuple", n.Args)
	default:
		panic(fmt.Sprintf("dump: unknown node %T", node))
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
