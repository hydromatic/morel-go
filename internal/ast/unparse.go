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
	"strconv"
	"strings"
)

// literalText renders a literal as source: an int canonicalized
// (leading zeros dropped), a string or char quoted.
func literalText(l *Literal) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch l.Kind {
	case CharLiteralOp:
		return `#"` + l.Value + `"`
	case IntLiteralOp:
		neg := strings.HasPrefix(l.Value, "~")
		n, err := strconv.ParseUint(strings.TrimPrefix(l.Value, "~"),
			10, 64)
		if err != nil {
			return l.Value
		}
		s := strconv.FormatUint(n, 10)
		if neg {
			return "~" + s
		}
		return s
	case StringLiteralOp:
		r := strings.ReplaceAll(l.Value, `\`, `\\`)
		return `"` + strings.ReplaceAll(r, `"`, `\"`) + `"`
	default:
		return l.Value
	}
}

// UnparsePat renders a pattern as source text: an implicit record
// field renders expanded ("{a = a}"), and a nested cons or layered
// pattern is parenthesized where the grammar requires.
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

// UnparseSignatureDecl renders a signature declaration as the
// shell echoes it: one line per binding, "signature NAME = sig
// spec ... end" with the specifications space-joined in source
// order.
func UnparseSignatureDecl(d *SignatureDecl) string {
	lines := make([]string, 0, len(d.Binds))
	for _, bind := range d.Binds {
		var b strings.Builder
		b.WriteString("signature " + bind.Name + " = sig")
		for _, spec := range bind.Specs {
			b.WriteString(" ")
			unparseSigSpec(&b, spec)
		}
		b.WriteString(" end")
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

// unparseSigSpec renders one signature specification.
func unparseSigSpec(b *strings.Builder, spec SigSpec) {
	// lint: sort until '^\t}' where '^\tcase '
	switch spec.Kind {
	case DatatypeDeclOp:
		b.WriteString("datatype ")
		writeTyVars(b, spec.Bind.TyVars)
		b.WriteString(spec.Bind.Name + " = ")
		for j, c := range spec.Bind.Cons {
			if j > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(c.Name)
			if c.Of != nil {
				b.WriteString(" of ")
				unparseType(b, c.Of, ", ")
			}
		}
	case TypeDeclOp:
		b.WriteString("type ")
		writeTyVars(b, spec.TyVars)
		b.WriteString(spec.Name)
		if spec.Type != nil {
			b.WriteString(" = ")
			unparseType(b, spec.Type, ", ")
		}
	case ValDeclOp:
		b.WriteString("val " + spec.Name + " : ")
		unparseType(b, spec.Type, ", ")
	default: // exception
		b.WriteString("exception " + spec.Name)
		if spec.Type != nil {
			b.WriteString(" of ")
			unparseType(b, spec.Type, ", ")
		}
	}
}

// writeTyVars renders a type-parameter list and a trailing space:
// nothing, "'a ", or "('k, 'v) ".
func writeTyVars(b *strings.Builder, tyVars []string) {
	switch len(tyVars) {
	case 0:
	case 1:
		b.WriteString(tyVars[0] + " ")
	default:
		b.WriteString("(" + strings.Join(tyVars, ", ") + ") ")
	}
}

// UnparseType renders a type expression as source text: a
// function type parenthesizes a function-type parameter, and a
// tuple type parenthesizes function- and tuple-type elements.
func UnparseType(t Type) string {
	var b strings.Builder
	unparseType(&b, t, ", ")
	return b.String()
}

// comma is the separator between the arguments of a multi-argument
// named type: ", " in the general (parse-tree) rendering, but ","
// in the shell's datatype echo.
func unparseType(b *strings.Builder, t Type, comma string) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *ExpressionType:
		b.WriteString("typeof ")
		unparseExpr(b, n.Exp, applyPrec)
	case *FnType:
		unparseTypeArg(b, n.Param, false, comma)
		b.WriteString(" -> ")
		unparseType(b, n.Result, comma)
	case *NamedType:
		unparseNamedType(b, n, comma)
	case *RecordType:
		b.WriteString("{")
		for i, f := range n.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(f.Label + ":")
			unparseType(b, f.Type, comma)
		}
		b.WriteString("}")
	case *TupleType:
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(" * ")
			}
			unparseTypeArg(b, a, true, comma)
		}
	case *TyVar:
		b.WriteString(n.Name)
	}
}

// unparseTypeArg parenthesizes an operand where the grammar
// requires: a function type always, a tuple type inside another
// tuple type.
func unparseTypeArg(b *strings.Builder, t Type, inTuple bool,
	comma string,
) {
	_, isFn := t.(*FnType)
	_, isTuple := t.(*TupleType)
	if isFn || (inTuple && isTuple) {
		b.WriteString("(")
		unparseType(b, t, comma)
		b.WriteString(")")
		return
	}
	unparseType(b, t, comma)
}

func unparseNamedType(b *strings.Builder, n *NamedType, comma string) {
	switch len(n.Args) {
	case 0:
	case 1:
		unparseTypeArg(b, n.Args[0], true, comma)
		b.WriteString(" ")
	default:
		b.WriteString("(")
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(comma)
			}
			unparseType(b, a, comma)
		}
		b.WriteString(") ")
	}
	b.WriteString(n.Name)
}

// UnparseDatatypeDecl renders a datatype declaration as the shell
// echoes it: each bind's type variables are normalized to 'a, 'b,
// ... in head order (so "datatype 'x tree" echoes as "datatype 'a
// tree"), the same renaming applies throughout the constructor
// argument types, and multi-argument type applications use ","
// without a space.
func UnparseDatatypeDecl(d *DatatypeDecl, width int) string {
	var b strings.Builder
	for i, bind := range d.Binds {
		if i > 0 {
			b.WriteString("\n")
		}
		var head strings.Builder
		head.WriteString("datatype ")
		rename := canonicalTyVars(bind.TyVars)
		names := make([]string, len(bind.TyVars))
		for j, tv := range bind.TyVars {
			names[j] = rename[tv]
		}
		unparseDatatypeTyVars(&head, names)
		head.WriteString(bind.Name)
		cons := make([]string, len(bind.Cons))
		for j, c := range bind.Cons {
			var cb strings.Builder
			cb.WriteString(c.Name)
			if c.Of != nil {
				cb.WriteString(" of ")
				unparseType(&cb, renameTyVars(c.Of, rename), ",")
			}
			cons[j] = cb.String()
		}
		b.WriteString(layoutDatatype(head.String(), cons, width))
	}
	return b.String()
}

// layoutDatatype lays a datatype bind out on one line, or, where
// that would be wider than the line, over several: the name alone,
// then one constructor to a line, indented and led by "=" and "|".
//
//	datatype personnel_id
//	  = EMPLOYEE of int
//	  | CONTRACTOR of {agency:string, ssid:string}
func layoutDatatype(head string, cons []string, width int) string {
	oneLine := head + " = " + strings.Join(cons, " | ")
	if width <= 0 || len(oneLine) <= width {
		return oneLine
	}
	return head + "\n  = " + strings.Join(cons, "\n  | ")
}

// UnparseTypeDecl renders a type-alias declaration as the shell
// echoes it: type variables normalized to 'a, 'b, ... and
// multi-argument type applications with "," and no space, like
// UnparseDatatypeDecl.
func UnparseTypeDecl(d *TypeDecl) string {
	var b strings.Builder
	for i, bind := range d.Binds {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("type ")
		rename := canonicalTyVars(bind.TyVars)
		names := make([]string, len(bind.TyVars))
		for j, tv := range bind.TyVars {
			names[j] = rename[tv]
		}
		unparseDatatypeTyVars(&b, names)
		b.WriteString(bind.Name + " = ")
		unparseType(&b, renameTyVars(bind.Type, rename), ",")
	}
	return b.String()
}

// unparseDatatypeTyVars renders a datatype's type-variable head with
// "," and no space, as the shell echo does.
func unparseDatatypeTyVars(b *strings.Builder, tyVars []string) {
	switch len(tyVars) {
	case 0:
	case 1:
		b.WriteString(tyVars[0] + " ")
	default:
		b.WriteString("(" + strings.Join(tyVars, ",") + ") ")
	}
}

// unparseDatatype renders a datatype declaration for the parse-tree
// dump, keeping the source's type-variable names and the general
// ", " separator.
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

// canonicalTyVars maps type variables to 'a, 'b, ... in order.
func canonicalTyVars(tyVars []string) map[string]string {
	m := make(map[string]string, len(tyVars))
	for i, tv := range tyVars {
		m[tv] = tyVarName(i)
	}
	return m
}

// tyVarName is the i-th canonical type-variable name: 'a, 'b, ...,
// 'z, 'ba, 'bb, ... -- base-26 with 'a' as 0, matching TypeVar.name
// (so it does not run past 'z into non-letter bytes).
func tyVarName(i int) string {
	var b []byte
	for {
		b = append(b, byte('a'+i%26))
		i /= 26
		if i == 0 {
			break
		}
	}
	for l, r := 0, len(b)-1; l < r; l, r = l+1, r-1 {
		b[l], b[r] = b[r], b[l]
	}
	return "'" + string(b)
}

// renameTyVars returns a copy of t with each type variable renamed
// per m; a variable absent from m is left unchanged.
func renameTyVars(t Type, m map[string]string) Type {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *FnType:
		return &FnType{
			typeBase: n.typeBase,
			Param:    renameTyVars(n.Param, m),
			Result:   renameTyVars(n.Result, m),
		}
	case *NamedType:
		args := make([]Type, len(n.Args))
		for i, a := range n.Args {
			args[i] = renameTyVars(a, m)
		}
		return &NamedType{typeBase: n.typeBase, Name: n.Name, Args: args}
	case *RecordType:
		fields := make([]TypeField, len(n.Fields))
		for i, f := range n.Fields {
			fields[i] = TypeField{Label: f.Label, Type: renameTyVars(f.Type, m)}
		}
		return &RecordType{typeBase: n.typeBase, Fields: fields}
	case *TupleType:
		args := make([]Type, len(n.Args))
		for i, a := range n.Args {
			args[i] = renameTyVars(a, m)
		}
		return &TupleType{typeBase: n.typeBase, Args: args}
	case *TyVar:
		if name, ok := m[n.Name]; ok {
			return &TyVar{typeBase: n.typeBase, Name: name}
		}
		return n
	default:
		return t
	}
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
// precedence. Cons and at are right-associative.
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
	case *Elements:
		b.WriteString("elements")
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
		b.WriteString(literalText(n))
	case *PrefixCall:
		unparseParen(b, prec, applyPrec, func() {
			b.WriteString("~ ")
			unparseExpr(b, n.A, applyPrec)
		})
	case *Record:
		b.WriteString("{")
		if n.Base != nil {
			unparseExpr(b, n.Base, 0)
		}
		unparseFields(b, n.Fields)
		for _, m := range n.Modifiers {
			unparseModifier(b, m)
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

// unparseFields renders "a = e1, b = e2, ..."; a field with no
// label renders as its expression alone.
func unparseFields(b *strings.Builder, fields []Field) {
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		if f.Label != "" {
			b.WriteString(f.Label + " = ")
		}
		unparseExpr(b, f.Exp, 0)
	}
}

// unparseModifier renders a record modifier, including the space
// that separates it from what precedes it.
func unparseModifier(b *strings.Builder, m Modifier) {
	b.WriteString(" " + m.Verbs() + " ")
	// lint: sort until '^	}' where '^	case '
	switch m := m.(type) {
	case *AllModifier:
		unparseExpr(b, m.Exp, 0)
	case *AssignModifier:
		unparseFields(b, m.Fields)
	case *RemoveModifier:
		for i, label := range m.Labels {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(label.Name)
		}
	case *RenameModifier:
		for i, pair := range m.Pairs {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pair.To.Name + " = " + pair.From.Name)
		}
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
			unparseBinder(b, n.Binder)
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
