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

// Package ast defines the abstract syntax tree that the parser
// produces: the user-facing representation, with source spans,
// before type resolution converts it to Core.
package ast

import "github.com/hydromatic/morel-go/internal/token"

// Node is an AST node.
type Node interface {
	Op() Op
	Span() token.Span
	node()
}

// Expr is an expression node.
type Expr interface {
	Node
	expr()
}

// Pat is a pattern node.
type Pat interface {
	Node
	pat()
}

// Decl is a declaration node.
type Decl interface {
	Node
	decl()
}

// base carries the source span common to all nodes.
type base struct {
	span token.Span
}

func (b base) Span() token.Span { return b.span }
func (base) node()              {}

type exprBase struct{ base }

func (exprBase) expr() {}

// Literal is a constant expression. Kind is one of the literal
// ops; Value holds the text of the constant, without the quotes
// of a string or char literal.
type Literal struct {
	exprBase

	Value string
	Kind  Op
}

// NewLiteral returns a literal of the given kind.
func NewLiteral(span token.Span, kind Op, value string) *Literal {
	return &Literal{
		exprBase: exprBase{base{span}},
		Value:    value,
		Kind:     kind,
	}
}

// Op implements Node.
func (l *Literal) Op() Op { return l.Kind }

// ID is a reference to a name.
type ID struct {
	exprBase

	Name string
}

// NewID returns an identifier reference.
func NewID(span token.Span, name string) *ID {
	return &ID{exprBase: exprBase{base{span}}, Name: name}
}

// Op implements Node.
func (*ID) Op() Op { return IDOp }

// Apply is the application of a function to an argument.
type Apply struct {
	exprBase

	Fn  Expr
	Arg Expr
}

// NewApply returns a function application.
func NewApply(span token.Span, fn, arg Expr) *Apply {
	return &Apply{
		exprBase: exprBase{base{span}},
		Fn:       fn,
		Arg:      arg,
	}
}

// Op implements Node.
func (*Apply) Op() Op { return ApplyOp }

// RecordSelector is a field-selection function, "#label"; the
// expression "e.f" parses as the application of "#f" to "e".
type RecordSelector struct {
	exprBase

	Name string
}

// NewRecordSelector returns a record selector.
func NewRecordSelector(span token.Span,
	name string,
) *RecordSelector {
	return &RecordSelector{
		exprBase: exprBase{base{span}},
		Name:     name,
	}
}

// Op implements Node.
func (*RecordSelector) Op() Op { return RecordSelectorOp }

// Tuple is a tuple expression, "(e1, e2, ...)".
type Tuple struct {
	exprBase

	Args []Expr
}

// NewTuple returns a tuple expression.
func NewTuple(span token.Span, args []Expr) *Tuple {
	return &Tuple{exprBase: exprBase{base{span}}, Args: args}
}

// Op implements Node.
func (*Tuple) Op() Op { return TupleOp }

// ListExp is a list expression, "[e1, e2, ...]".
type ListExp struct {
	exprBase

	Args []Expr
}

// NewListExp returns a list expression.
func NewListExp(span token.Span, args []Expr) *ListExp {
	return &ListExp{exprBase: exprBase{base{span}}, Args: args}
}

// Op implements Node.
func (*ListExp) Op() Op { return ListOp }

// Field is one field of a record expression. Label is empty for
// an implicit label (e.g. "{x}"), which is filled in during
// resolution.
type Field struct {
	Label string
	Exp   Expr
}

// Record is a record expression, "{a = e1, b = e2, ...}", with
// fields in source order.
type Record struct {
	exprBase

	Fields []Field
}

// NewRecord returns a record expression.
func NewRecord(span token.Span, fields []Field) *Record {
	return &Record{exprBase: exprBase{base{span}}, Fields: fields}
}

// Op implements Node.
func (*Record) Op() Op { return RecordOp }

// InfixCall is the application of an infix operator.
type InfixCall struct {
	exprBase

	A0   Expr
	A1   Expr
	Kind Op
}

// NewInfixCall returns an infix operator application.
func NewInfixCall(span token.Span, kind Op, a0,
	a1 Expr,
) *InfixCall {
	return &InfixCall{
		exprBase: exprBase{base{span}},
		A0:       a0,
		A1:       a1,
		Kind:     kind,
	}
}

// Op implements Node.
func (c *InfixCall) Op() Op { return c.Kind }

// PrefixCall is the application of a prefix operator.
type PrefixCall struct {
	exprBase

	A    Expr
	Kind Op
}

// NewPrefixCall returns a prefix operator application.
func NewPrefixCall(span token.Span, kind Op, a Expr) *PrefixCall {
	return &PrefixCall{
		exprBase: exprBase{base{span}},
		A:        a,
		Kind:     kind,
	}
}

// Op implements Node.
func (c *PrefixCall) Op() Op { return c.Kind }

// If is a conditional expression.
type If struct {
	exprBase

	Cond    Expr
	IfTrue  Expr
	IfFalse Expr
}

// NewIf returns a conditional expression.
func NewIf(span token.Span, cond, ifTrue, ifFalse Expr) *If {
	return &If{
		exprBase: exprBase{base{span}},
		Cond:     cond,
		IfTrue:   ifTrue,
		IfFalse:  ifFalse,
	}
}

// Op implements Node.
func (*If) Op() Op { return IfOp }

// Match is one rule of a fn or case: a pattern and its result.
type Match struct {
	base

	Pat Pat
	Exp Expr
}

// NewMatch returns a match rule.
func NewMatch(span token.Span, pat Pat, exp Expr) *Match {
	return &Match{base: base{span}, Pat: pat, Exp: exp}
}

// Op implements Node.
func (*Match) Op() Op { return MatchOp }

// Fn is a function expression, "fn match | match ...".
type Fn struct {
	exprBase

	Matches []*Match
}

// NewFn returns a function expression.
func NewFn(span token.Span, matches []*Match) *Fn {
	return &Fn{exprBase: exprBase{base{span}}, Matches: matches}
}

// Op implements Node.
func (*Fn) Op() Op { return FnOp }

// Case is a case expression, "case e of match | match ...".
type Case struct {
	exprBase

	Exp     Expr
	Matches []*Match
}

// NewCase returns a case expression.
func NewCase(span token.Span, exp Expr, matches []*Match) *Case {
	return &Case{
		exprBase: exprBase{base{span}},
		Exp:      exp,
		Matches:  matches,
	}
}

// Op implements Node.
func (*Case) Op() Op { return CaseOp }
