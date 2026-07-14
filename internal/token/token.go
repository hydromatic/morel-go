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

// Package token defines the lexical tokens of Morel and their
// source positions.
package token

// Kind identifies a lexical token.
type Kind int

// The token kinds.
const (
	EOF Kind = iota

	// Identifiers and literals.

	Ident
	QuotedIdent
	TyVar
	Label
	IntLit
	RealLit
	ScientificLit
	StringLit
	CharLit

	// Keywords.

	And
	Andalso
	As
	Case
	Datatype
	Div
	Else
	End
	Eqtype
	Exception
	Fn
	Fun
	If
	In
	Let
	Mod
	O
	Of
	Op
	Orelse
	Raise
	Rec
	Sig
	Signature
	Then
	Type
	Val

	// Keywords for relational extensions.

	Compute
	Current
	Distinct
	Elem
	Elements
	Except
	Exists
	Forall
	From
	Full
	Group
	Implies
	Intersect
	Into
	Join
	Left
	Notelem
	On
	Order
	Ordinal
	Require
	Right
	Skip
	Take
	Through
	Typeof
	TypeString
	Union
	Unorder
	Where
	With
	Yield
	YieldAll

	// Keywords for overloaded operators.

	Inst
	Over

	// Separators.

	LParen
	RParen
	LBrace
	RBrace
	LBracket
	RBracket
	Semi
	Bar
	Dot
	Comma
	Underscore
	RArrow
	RThinArrow
	Ellipsis

	// Operators.

	Eq
	Gt
	Lt
	Colon
	Le
	Ge
	Ne
	Plus
	Minus
	Caret
	Star
	Slash
	Tilde
	Cons
	At
)

var kindNames = map[Kind]string{
	EOF:           "EOF",
	Ident:         "identifier",
	QuotedIdent:   "quoted identifier",
	TyVar:         "type variable",
	Label:         "label",
	IntLit:        "integer literal",
	RealLit:       "real literal",
	ScientificLit: "scientific literal",
	StringLit:     "string literal",
	CharLit:       "char literal",
	And:           "and",
	Andalso:       "andalso",
	As:            "as",
	Case:          "case",
	Datatype:      "datatype",
	Div:           "div",
	Else:          "else",
	End:           "end",
	Eqtype:        "eqtype",
	Exception:     "exception",
	Fn:            "fn",
	Fun:           "fun",
	If:            "if",
	In:            "in",
	Let:           "let",
	Mod:           "mod",
	O:             "o",
	Of:            "of",
	Op:            "op",
	Orelse:        "orelse",
	Raise:         "raise",
	Rec:           "rec",
	Sig:           "sig",
	Signature:     "signature",
	Then:          "then",
	Type:          "type",
	Val:           "val",
	Compute:       "compute",
	Current:       "current",
	Distinct:      "distinct",
	Elem:          "elem",
	Elements:      "elements",
	Except:        "except",
	Exists:        "exists",
	Forall:        "forall",
	From:          "from",
	Full:          "full",
	Group:         "group",
	Implies:       "implies",
	Intersect:     "intersect",
	Into:          "into",
	Join:          "join",
	Left:          "left",
	Notelem:       "notelem",
	On:            "on",
	Order:         "order",
	Ordinal:       "ordinal",
	Require:       "require",
	Right:         "right",
	Skip:          "skip",
	Take:          "take",
	Through:       "through",
	Typeof:        "typeof",
	TypeString:    "type_string",
	Union:         "union",
	Unorder:       "unorder",
	Where:         "where",
	With:          "with",
	Yield:         "yield",
	YieldAll:      "yieldAll",
	Inst:          "inst",
	Over:          "over",
	LParen:        "(",
	RParen:        ")",
	LBrace:        "{",
	RBrace:        "}",
	LBracket:      "[",
	RBracket:      "]",
	Semi:          ";",
	Bar:           "|",
	Dot:           ".",
	Comma:         ",",
	Underscore:    "_",
	RArrow:        "=>",
	RThinArrow:    "->",
	Ellipsis:      "...",
	Eq:            "=",
	Gt:            ">",
	Lt:            "<",
	Colon:         ":",
	Le:            "<=",
	Ge:            ">=",
	Ne:            "<>",
	Plus:          "+",
	Minus:         "-",
	Caret:         "^",
	Star:          "*",
	Slash:         "/",
	Tilde:         "~",
	Cons:          "::",
	At:            "@",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return "unknown"
}

var keywords = func() map[string]Kind {
	m := map[string]Kind{}
	for k := And; k <= Over; k++ {
		m[kindNames[k]] = k
	}
	return m
}()

// Lookup returns the keyword kind for an identifier, or Ident if
// it is not a keyword.
func Lookup(ident string) Kind {
	if k, ok := keywords[ident]; ok {
		return k
	}
	return Ident
}

// Token is a lexical token: its kind, its literal text, and its
// position.
type Token struct {
	Text string
	Span Span
	Kind Kind
}
