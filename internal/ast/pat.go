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

type patBase struct{ base }

func (patBase) pat() {}

func pb(span token.Span) patBase {
	return patBase{base{span}}
}

// IDPat is a pattern that binds a name.
type IDPat struct {
	patBase

	Name string
}

// NewIDPat returns a name-binding pattern.
func NewIDPat(span token.Span, name string) *IDPat {
	return &IDPat{patBase: pb(span), Name: name}
}

// Op implements Node.
func (*IDPat) Op() Op { return IDPatOp }

// WildcardPat is the pattern "_".
type WildcardPat struct {
	patBase
}

// NewWildcardPat returns a wildcard pattern.
func NewWildcardPat(span token.Span) *WildcardPat {
	return &WildcardPat{patBase: pb(span)}
}

// Op implements Node.
func (*WildcardPat) Op() Op { return WildcardPatOp }

// LiteralPat is a constant pattern.
type LiteralPat struct {
	patBase

	Value string
	Kind  Op
}

// NewLiteralPat returns a constant pattern.
func NewLiteralPat(span token.Span, kind Op,
	value string,
) *LiteralPat {
	return &LiteralPat{patBase: pb(span), Value: value, Kind: kind}
}

// Op implements Node.
func (p *LiteralPat) Op() Op { return p.Kind }

// TuplePat is a tuple pattern, "(p1, p2)"; the unit pattern "()"
// is a tuple pattern with no arguments.
type TuplePat struct {
	patBase

	Args []Pat
}

// NewTuplePat returns a tuple pattern.
func NewTuplePat(span token.Span, args []Pat) *TuplePat {
	return &TuplePat{patBase: pb(span), Args: args}
}

// Op implements Node.
func (*TuplePat) Op() Op { return TuplePatOp }

// ListPat is a list pattern, "[p1, p2]".
type ListPat struct {
	patBase

	Args []Pat
}

// NewListPat returns a list pattern.
func NewListPat(span token.Span, args []Pat) *ListPat {
	return &ListPat{patBase: pb(span), Args: args}
}

// Op implements Node.
func (*ListPat) Op() Op { return ListPatOp }

// PatField is one field of a record pattern. An implicit field
// "{a}" has Label "a" and pattern "a".
type PatField struct {
	Label string
	Pat   Pat
}

// RecordPat is a record pattern, "{a = p, b, ...}"; Ellipsis
// reports whether the pattern ends with "...".
type RecordPat struct {
	patBase

	Fields   []PatField
	Ellipsis bool
}

// NewRecordPat returns a record pattern.
func NewRecordPat(span token.Span, fields []PatField,
	ellipsis bool,
) *RecordPat {
	return &RecordPat{
		patBase:  pb(span),
		Fields:   fields,
		Ellipsis: ellipsis,
	}
}

// Op implements Node.
func (*RecordPat) Op() Op { return RecordPatOp }

// ConsPat is the pattern "p1 :: p2".
type ConsPat struct {
	patBase

	A0 Pat
	A1 Pat
}

// NewConsPat returns a cons pattern.
func NewConsPat(span token.Span, a0, a1 Pat) *ConsPat {
	return &ConsPat{patBase: pb(span), A0: a0, A1: a1}
}

// Op implements Node.
func (*ConsPat) Op() Op { return ConsPatOp }

// AsPat is a layered pattern, "name as p".
type AsPat struct {
	patBase

	Name string
	Pat  Pat
}

// NewAsPat returns a layered pattern.
func NewAsPat(span token.Span, name string, pat Pat) *AsPat {
	return &AsPat{patBase: pb(span), Name: name, Pat: pat}
}

// Op implements Node.
func (*AsPat) Op() Op { return AsPatOp }

// ConPat is the application of a constructor to a pattern,
// "SOME x".
type ConPat struct {
	patBase

	Name string
	Arg  Pat
}

// NewConPat returns a constructor-application pattern.
func NewConPat(span token.Span, name string, arg Pat) *ConPat {
	return &ConPat{patBase: pb(span), Name: name, Arg: arg}
}

// Op implements Node.
func (*ConPat) Op() Op { return ConPatOp }
