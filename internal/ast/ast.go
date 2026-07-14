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
