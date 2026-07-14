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

import "github.com/hydromatic/morel-go/internal/token"

type declBase struct{ base }

func (declBase) decl() {}

// ValBind is one binding of a val declaration, "pat = exp".
type ValBind struct {
	base

	Pat Pat
	Exp Expr
}

// NewValBind returns a val binding.
func NewValBind(span token.Span, pat Pat, exp Expr) *ValBind {
	return &ValBind{base: base{span}, Pat: pat, Exp: exp}
}

// Op implements Node.
func (*ValBind) Op() Op { return ValBindOp }

// ValDecl is "val [rec] bind and bind ...".
type ValDecl struct {
	declBase

	Binds []*ValBind
	Rec   bool
}

// NewValDecl returns a val declaration.
func NewValDecl(span token.Span, rec bool,
	binds []*ValBind,
) *ValDecl {
	return &ValDecl{
		declBase: declBase{base{span}},
		Binds:    binds,
		Rec:      rec,
	}
}

// Op implements Node.
func (*ValDecl) Op() Op { return ValDeclOp }

// FunMatch is one clause of a function binding:
// "name pat... = exp".
type FunMatch struct {
	base

	Name       string
	Pats       []Pat
	ReturnType Type
	Exp        Expr
}

// NewFunMatch returns a function clause.
func NewFunMatch(span token.Span, name string, pats []Pat,
	returnType Type, exp Expr,
) *FunMatch {
	return &FunMatch{
		base:       base{span},
		Name:       name,
		Pats:       pats,
		ReturnType: returnType,
		Exp:        exp,
	}
}

// Op implements Node.
func (*FunMatch) Op() Op { return FunMatchOp }

// FunBind is the clauses of one function,
// "clause | clause ...".
type FunBind struct {
	base

	Matches []*FunMatch
}

// NewFunBind returns a function binding.
func NewFunBind(span token.Span, matches []*FunMatch) *FunBind {
	return &FunBind{base: base{span}, Matches: matches}
}

// Op implements Node.
func (*FunBind) Op() Op { return FunBindOp }

// FunDecl is "fun bind and bind ...".
type FunDecl struct {
	declBase

	Binds []*FunBind
}

// NewFunDecl returns a fun declaration.
func NewFunDecl(span token.Span, binds []*FunBind) *FunDecl {
	return &FunDecl{declBase: declBase{base{span}}, Binds: binds}
}

// Op implements Node.
func (*FunDecl) Op() Op { return FunDeclOp }

// Let is "let decl ... in exp end".
type Let struct {
	exprBase

	Decls []Decl
	Exp   Expr
}

// NewLet returns a let expression.
func NewLet(span token.Span, decls []Decl, exp Expr) *Let {
	return &Let{
		exprBase: exprBase{base{span}},
		Decls:    decls,
		Exp:      exp,
	}
}

// Op implements Node.
func (*Let) Op() Op { return LetOp }
