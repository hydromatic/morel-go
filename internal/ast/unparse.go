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
	case *ConPat:
		b.WriteString(n.Name + " ")
		unparseConArg(b, n.Arg)
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
	case *ExpressionType:
		b.WriteString("typeof ")
		unparseExpr(b, n.Exp, applyPrec)
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

// UnparseDatatypeDecl renders a datatype declaration as source
// text; the shell echoes a datatype declaration this way.
func UnparseDatatypeDecl(d *DatatypeDecl) string {
	return unparseDatatype(d)
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

// unparseConArg parenthesizes a constructor argument that is not
// atomic.
func unparseConArg(b *strings.Builder, p Pat) {
	switch p.(type) {
	case *AnnotatedPat, *AsPat, *ConPat, *ConsPat:
		b.WriteString("(")
		unparsePat(b, p)
		b.WriteString(")")
	default:
		unparsePat(b, p)
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

// Operator rendering for the unparser: text with padding, and
// precedence. Cons and at are
// right-associative.
type opInfo struct {
	text  string
	prec  int
	right bool
}

var unparseOps = map[Op]opInfo{
	// lint: sort until '^}'
	AndalsoOp: {" andalso ", 2, false},
	AtOp:      {" @ ", 5, true},
	CaretOp:   {" ^ ", 6, false},
	ComposeOp: {" o ", 3, false},
	ConsOp:    {" :: ", 5, true},
	DivOp:     {" div ", 7, false},
	DivideOp:  {" / ", 7, false},
	ElemOp:    {" elem ", 4, false},
	EqOp:      {" = ", 4, false},
	GeOp:      {" >= ", 4, false},
	GtOp:      {" > ", 4, false},
	ImpliesOp: {" implies ", 0, false},
	LeOp:      {" <= ", 4, false},
	LtOp:      {" < ", 4, false},
	MinusOp:   {" - ", 6, false},
	ModOp:     {" mod ", 7, false},
	NeOp:      {" <> ", 4, false},
	NotElemOp: {" notelem ", 4, false},
	OrelseOp:  {" orelse ", 1, false},
	OverOp:    {" over ", 8, false},
	PlusOp:    {" + ", 6, false},
	TimesOp:   {" * ", 7, false},
}

// applyPrec is the precedence of function application; "over"
// sits between the multiplicative level and application.
const applyPrec = 9

// UnparseExpr renders an expression as source text,
// parenthesizing by operator precedence.
func UnparseExpr(e Expr) string {
	var b strings.Builder
	unparseExpr(&b, e, 0)
	return b.String()
}

func unparseExpr(b *strings.Builder, e Expr, prec int) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := e.(type) {
	case *Apply:
		unparseParen(b, prec, applyPrec, func() {
			unparseExpr(b, n.Fn, applyPrec)
			b.WriteString(" ")
			unparseExpr(b, n.Arg, applyPrec+1)
		})
	case *From:
		unparseParen(b, prec, 1, func() { unparseFrom(b, n) })
	case *ID:
		b.WriteString(n.Name)
	case *InfixCall:
		op := unparseOps[n.Kind]
		left, r := op.prec, op.prec+1
		if op.right {
			left, r = op.prec+1, op.prec
		}
		unparseParen(b, prec, op.prec, func() {
			unparseExpr(b, n.A0, left)
			b.WriteString(op.text)
			unparseExpr(b, n.A1, r)
		})
	case *ListExp:
		unparseExprList(b, "[", n.Args, "]")
	case *Literal:
		b.WriteString(n.Value)
	case *PrefixCall:
		unparseParen(b, prec, applyPrec, func() {
			b.WriteString("~ ")
			unparseExpr(b, n.A, applyPrec)
		})
	case *Record:
		b.WriteString("{")
		for i, f := range n.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			if f.Label != "" {
				b.WriteString(f.Label + " = ")
			}
			unparseExpr(b, f.Exp, 0)
		}
		b.WriteString("}")
	case *RecordSelector:
		b.WriteString("#" + n.Name)
	case *Tuple:
		unparseExprList(b, "(", n.Args, ")")
	default:
		panic("unparse: unknown expression")
	}
}

// unparseParen renders body, parenthesized when the operator
// binds less tightly than the context requires.
func unparseParen(b *strings.Builder, prec, opPrec int,
	body func(),
) {
	if prec > opPrec {
		b.WriteString("(")
		body()
		b.WriteString(")")
		return
	}
	body()
}

func unparseExprList(b *strings.Builder, open string,
	args []Expr, closer string,
) {
	b.WriteString(open)
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		unparseExpr(b, a, 0)
	}
	b.WriteString(closer)
}

// unparseFrom renders a query expression, including its keyword
// (from, exists, or forall). Every scan after the first renders
// with a comma, so a join unparses as ", pat in exp on cond".
func unparseFrom(b *strings.Builder, f *From) {
	b.WriteString(f.Kind.String())
	first := true
	for _, step := range f.Steps {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch n := step.(type) {
		case *ComputeStep:
			b.WriteString(" compute ")
			unparseBinder(b, n.Binder)
			unparseExpr(b, n.Exp, 0)
		case *DistinctStep:
			b.WriteString(" distinct")
		case *GroupStep:
			b.WriteString(" group ")
			unparseBinder(b, n.Binder)
			unparseExpr(b, n.Exp, 0)
		case *IntoStep:
			b.WriteString(" into ")
			unparseExpr(b, n.Exp, 0)
		case *OrderStep:
			b.WriteString(" order ")
			unparseExpr(b, n.Exp, 0)
		case *RequireStep:
			b.WriteString(" require ")
			unparseExpr(b, n.Exp, 0)
		case *Scan:
			if first {
				b.WriteString(" ")
				first = false
			} else {
				b.WriteString(", ")
			}
			unparseScan(b, n)
		case *SetOpStep:
			b.WriteString(" " + n.Kind.String() + " ")
			if n.Distinct {
				b.WriteString("distinct ")
			}
			for i, e := range n.Exps {
				if i > 0 {
					b.WriteString(", ")
				}
				unparseExpr(b, e, 0)
			}
		case *SkipStep:
			b.WriteString(" skip ")
			unparseExpr(b, n.Exp, 0)
		case *TakeStep:
			b.WriteString(" take ")
			unparseExpr(b, n.Exp, 0)
		case *ThroughStep:
			b.WriteString(" through ")
			unparsePat(b, n.Pat)
			b.WriteString(" in ")
			unparseExpr(b, n.Exp, 0)
		case *UnorderStep:
			b.WriteString(" unorder")
		case *WhereStep:
			b.WriteString(" where ")
			unparseExpr(b, n.Exp, 0)
		case *YieldStep:
			b.WriteString(" yield ")
			unparseExpr(b, n.Exp, 0)
		}
	}
}

func unparseBinder(b *strings.Builder, binder string) {
	if binder != "" {
		b.WriteString(binder + " = ")
	}
}

func unparseScan(b *strings.Builder, n *Scan) {
	unparsePat(b, n.Pat)
	// lint: sort until '^\t}' where '^\tcase '
	switch n.Kind {
	case ScanEq:
		b.WriteString(" = ")
		unparseExpr(b, n.Exp, 0)
	case ScanIn:
		b.WriteString(" in ")
		unparseExpr(b, n.Exp, 0)
	case ScanUnbounded:
	default:
	}
	if n.On != nil {
		b.WriteString(" on ")
		unparseExpr(b, n.On, 0)
	}
}
