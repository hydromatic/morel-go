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

func (b *base) Span() token.Span        { return b.span }
func (b *base) setSpan(span token.Span) { b.span = span }
func (*base) node()                     {}

// spanSetter is a node whose span can be widened; every node is
// one, through base.
type spanSetter interface {
	setSpan(span token.Span)
}

// SetSpan widens a node's span to that of the parentheses that
// group it. Grouping parentheses are transparent -- "(e)" parses
// to the node of e -- so without this the node's span would stop
// short of them, and an error in, say, an application whose
// argument is parenthesized would be reported a column short. Only
// the parser, which owns a node until it returns it, calls this.
func SetSpan(n Node, span token.Span) {
	if s, ok := n.(spanSetter); ok {
		s.setSpan(span)
	}
}

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
// Keyword is true for "current" and "ordinal" written as
// keywords. Quoted, they are ordinary identifiers -- a field of
// that name, say -- and the keyword meaning does not apply.
type ID struct {
	exprBase

	Name    string
	Keyword bool
}

// NewID returns an identifier reference.
func NewID(span token.Span, name string) *ID {
	return &ID{exprBase: exprBase{base{span}}, Name: name}
}

// NewKeywordID returns a reference to "current" or "ordinal"
// written as a keyword.
func NewKeywordID(span token.Span, name string) *ID {
	return &ID{
		exprBase: exprBase{base{span}},
		Name:     name,
		Keyword:  true,
	}
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
// Safe marks the "?." spelling, which projects the field through
// the receiver's functor layers (option, list, bag, vector).
type RecordSelector struct {
	exprBase

	Name string
	Safe bool
}

// NewSafeRecordSelector returns a "?." selector.
func NewSafeRecordSelector(span token.Span,
	name string,
) *RecordSelector {
	return &RecordSelector{
		exprBase: exprBase{base{span}},
		Name:     name,
		Safe:     true,
	}
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

// RangeKind is the shape of a range-list item.
type RangeKind int

// The range-list item shapes: a single point, the four bounded
// intervals, and the five unbounded forms (which raise Size when a
// list is enumerated from them).
const (
	RangePoint RangeKind = iota
	RangeClosed
	RangeClosedOpen
	RangeOpenClosed
	RangeOpen
	RangeAll
	RangeAtLeast
	RangeAtMost
	RangeLessThan
	RangeGreaterThan
)

// RangeItem is one item of a range list: a point (Lo only), a
// bounded interval (Lo and Hi), or an unbounded form (a missing
// bound is nil).
type RangeItem struct {
	Kind RangeKind
	Lo   Expr
	Hi   Expr
}

// RangeList is a list built from range items, "[1 .. 5, 10]". A
// list all of whose items are points is an ordinary ListExp; a
// RangeList has at least one non-point item.
type RangeList struct {
	exprBase

	Items []RangeItem
}

// NewRangeList returns a range-list expression.
func NewRangeList(span token.Span, items []RangeItem) *RangeList {
	return &RangeList{exprBase: exprBase{base{span}}, Items: items}
}

// Op implements Node.
func (*RangeList) Op() Op { return RangeListOp }

// Field is one field of a record expression. Label is empty for
// an implicit label (e.g. "{x}"), which is filled in during
// resolution.
type Field struct {
	Label string
	// LabelSpan is the source range of the label, if the field
	// was written "label = exp"; error reports anchor to it.
	LabelSpan token.Span
	Exp       Expr
}

// Record is a record expression, "{a = e1, b = e2, ...}", with
// fields in source order. With is the source expression of a
// record update, "{e with a = e1}", or nil; it does not appear
// in the parse-tree dump.
type Record struct {
	exprBase

	With   Expr
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

// TypeStringExp is "type_string e": the inferred type of e,
// rendered as a string.
type TypeStringExp struct {
	exprBase

	Exp Expr
}

// NewTypeStringExp returns a type_string expression.
func NewTypeStringExp(span token.Span, exp Expr) *TypeStringExp {
	return &TypeStringExp{exprBase: exprBase{base{span}}, Exp: exp}
}

// Op implements Node.
func (*TypeStringExp) Op() Op { return TypeStringOp }

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

// Elements is the "elements" keyword: inside a compute clause,
// the current group's rows as a collection.
type Elements struct {
	exprBase
}

// NewElements returns an elements expression.
func NewElements(span token.Span) *Elements {
	return &Elements{exprBase: exprBase{base{span}}}
}

// Op implements Node.
func (*Elements) Op() Op { return ElementsOp }

// Raise is "raise e": e evaluates to an exn value, which is
// raised. The expression never returns, so its own type is free.
type Raise struct {
	exprBase

	E Expr
}

// NewRaise returns a raise expression.
func NewRaise(span token.Span, e Expr) *Raise {
	return &Raise{exprBase: exprBase{base{span}}, E: e}
}

// Op implements Node.
func (*Raise) Op() Op { return RaiseOp }

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
