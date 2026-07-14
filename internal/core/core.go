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

// Package core defines Morel's typed intermediate representation.
// Core is smaller than the AST: every node carries its type,
// "if" and "fn" match lists become "case" (Case is the only
// destructuring construct), and each "let" binds one declaration.
package core

import (
	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// Exp is an expression node.
type Exp interface {
	Op() ast.Op
	Type() types.Type
	exp()
}

// Pat is a pattern node.
type Pat interface {
	Op() ast.Op
	Type() types.Type
	pat()
}

// Decl is a declaration node.
type Decl interface {
	Op() ast.Op
	decl()
}

// Unit is the value of the unit literal, "()".
type Unit struct{}

// Literal is a constant expression. Value holds one of the
// runtime representations: int32, float32, string, rune, bool,
// or Unit.
type Literal struct {
	T     types.Type
	Kind  ast.Op
	Value any
}

// Op implements Exp.
func (l *Literal) Op() ast.Op { return l.Kind }

// Type implements Exp.
func (l *Literal) Type() types.Type { return l.T }

func (*Literal) exp() {}

// ID is a reference to a variable. It points to the IDPat that
// declared the variable, so its type is the pattern's type.
type ID struct {
	Pat *IDPat
}

// Op implements Exp.
func (*ID) Op() ast.Op { return ast.IDOp }

// Type implements Exp.
func (i *ID) Type() types.Type { return i.Pat.T }

func (*ID) exp() {}

// Apply is the application of a function to an argument. Span
// is where a runtime exception raised by the application is
// reported.
type Apply struct {
	T    types.Type
	Fn   Exp
	Arg  Exp
	Span token.Span
}

// Op implements Exp.
func (*Apply) Op() ast.Op { return ast.ApplyOp }

// Type implements Exp.
func (a *Apply) Type() types.Type { return a.T }

func (*Apply) exp() {}

// Fn is a function with a single parameter. A source function
// with a match list or a structured pattern becomes a Fn whose
// body is a Case.
type Fn struct {
	T     *types.Fn
	IDPat *IDPat
	Exp   Exp
}

// Op implements Exp.
func (*Fn) Op() ast.Op { return ast.FnOp }

// Type implements Exp.
func (f *Fn) Type() types.Type { return f.T }

func (*Fn) exp() {}

// Tuple is a tuple or record value; a record's fields are in
// canonical (label-sorted) order, so both print and match
// positionally.
type Tuple struct {
	T    types.Type
	Args []Exp
}

// Op implements Exp.
func (*Tuple) Op() ast.Op { return ast.TupleOp }

// Type implements Exp.
func (t *Tuple) Type() types.Type { return t.T }

func (*Tuple) exp() {}

// Con is a reference to a datatype constructor: a value (for a
// constant constructor such as NONE) or a function that wraps
// its argument (SOME).
type Con struct {
	T        types.Type
	Datatype string
	Name     string
	Ordinal  int
	HasArg   bool
}

// Op implements Exp.
func (*Con) Op() ast.Op { return ast.IDOp }

// Type implements Exp.
func (c *Con) Type() types.Type { return c.T }

func (*Con) exp() {}

// List is a list value, "[e1, e2, ...]".
type List struct {
	T    types.Type
	Args []Exp
}

// Op implements Exp.
func (*List) Op() ast.Op { return ast.ListOp }

// Type implements Exp.
func (l *List) Type() types.Type { return l.T }

func (*List) exp() {}

// Case matches an expression against a list of patterns; "if c
// then a else b" becomes "case c of true => a | _ => b". Span
// is the match list's position, where a Bind failure is
// reported.
type Case struct {
	T       types.Type
	Exp     Exp
	Matches []Match
	Span    token.Span
}

// Op implements Exp.
func (*Case) Op() ast.Op { return ast.CaseOp }

// Type implements Exp.
func (c *Case) Type() types.Type { return c.T }

func (*Case) exp() {}

// Match is one rule of a Case.
type Match struct {
	Pat Pat
	Exp Exp
}

// Let binds one declaration in the scope of an expression; a
// source "let" with several declarations becomes nested Lets.
type Let struct {
	Decl Decl
	Exp  Exp
}

// Op implements Exp.
func (*Let) Op() ast.Op { return ast.LetOp }

// Type implements Exp.
func (l *Let) Type() types.Type { return l.Exp.Type() }

func (*Let) exp() {}

// IDPat is a pattern that binds a name.
type IDPat struct {
	T    types.Type
	Name string
}

// Op implements Pat.
func (*IDPat) Op() ast.Op { return ast.IDPatOp }

// Type implements Pat.
func (p *IDPat) Type() types.Type { return p.T }

func (*IDPat) pat() {}

// WildcardPat is the pattern "_".
type WildcardPat struct {
	T types.Type
}

// Op implements Pat.
func (*WildcardPat) Op() ast.Op { return ast.WildcardPatOp }

// Type implements Pat.
func (p *WildcardPat) Type() types.Type { return p.T }

func (*WildcardPat) pat() {}

// LiteralPat is a constant pattern.
type LiteralPat struct {
	T     types.Type
	Kind  ast.Op
	Value any
}

// Op implements Pat.
func (p *LiteralPat) Op() ast.Op { return p.Kind }

// Type implements Pat.
func (p *LiteralPat) Type() types.Type { return p.T }

func (*LiteralPat) pat() {}

// PatIDs returns the IDPats that a pattern binds, in
// left-to-right order. It is the single utility for collecting a
// pattern's names — everywhere that binds, captures, or allocates
// pattern variables walks the pattern through here.
func PatIDs(p Pat) []*IDPat {
	var ids []*IDPat
	walkPat(p, &ids)
	return ids
}

func walkPat(p Pat, ids *[]*IDPat) {
	// lint: sort until '^	}' where '^	case '
	switch p := p.(type) {
	case *Con0Pat:
	case *ConPat:
		walkPat(p.Arg, ids)
	case *ConsPat:
		walkPat(p.Head, ids)
		walkPat(p.Tail, ids)
	case *IDPat:
		*ids = append(*ids, p)
	case *ListPat:
		for _, arg := range p.Args {
			walkPat(arg, ids)
		}
	case *LiteralPat, *WildcardPat:
	case *TuplePat:
		for _, arg := range p.Args {
			walkPat(arg, ids)
		}
	}
}

// TuplePat is a tuple or record pattern, with fields in
// canonical order.
type TuplePat struct {
	T    types.Type
	Args []Pat
}

// Op implements Pat.
func (*TuplePat) Op() ast.Op { return ast.TuplePatOp }

// Type implements Pat.
func (p *TuplePat) Type() types.Type { return p.T }

func (*TuplePat) pat() {}

// ListPat matches a list of exactly its length; "nil" is a
// ListPat with no elements.
type ListPat struct {
	T    types.Type
	Args []Pat
}

// Op implements Pat.
func (*ListPat) Op() ast.Op { return ast.ListPatOp }

// Type implements Pat.
func (p *ListPat) Type() types.Type { return p.T }

func (*ListPat) pat() {}

// ConsPat matches a non-empty list, "x :: xs".
type ConsPat struct {
	T    types.Type
	Head Pat
	Tail Pat
}

// Op implements Pat.
func (*ConsPat) Op() ast.Op { return ast.ConsPatOp }

// Type implements Pat.
func (p *ConsPat) Type() types.Type { return p.T }

func (*ConsPat) pat() {}

// Con0Pat matches a constant constructor, e.g. "NONE".
type Con0Pat struct {
	T        types.Type
	Datatype string
	Name     string
	Ordinal  int
}

// Op implements Pat.
func (*Con0Pat) Op() ast.Op { return ast.ConPatOp }

// Type implements Pat.
func (p *Con0Pat) Type() types.Type { return p.T }

func (*Con0Pat) pat() {}

// ConPat matches a constructor application, e.g. "SOME x".
type ConPat struct {
	T        types.Type
	Datatype string
	Name     string
	Ordinal  int
	Arg      Pat
}

// Op implements Pat.
func (*ConPat) Op() ast.Op { return ast.ConPatOp }

// Type implements Pat.
func (p *ConPat) Type() types.Type { return p.T }

func (*ConPat) pat() {}

// NonRecValDecl is a non-recursive value declaration. Span
// covers the pattern through the expression, where a Bind
// failure is reported.
type NonRecValDecl struct {
	Pat  Pat
	Exp  Exp
	Span token.Span
}

// Op implements Decl.
func (*NonRecValDecl) Op() ast.Op { return ast.ValDeclOp }

func (*NonRecValDecl) decl() {}

// RecValDecl is a recursive value declaration: its names are in
// scope in all of its own expressions.
type RecValDecl struct {
	Binds []*NonRecValDecl
}

// Op implements Decl.
func (*RecValDecl) Op() ast.Op { return ast.ValDeclOp }

func (*RecValDecl) decl() {}
