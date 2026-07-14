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

// Op identifies the kind of an AST node. Op.String returns the
// lowercase form that appears in
// Sys.parseTree output.
type Op int

// The node kinds.
const (
	IntLiteralOp Op = iota
	RealLiteralOp
	StringLiteralOp
	CharLiteralOp
	BoolLiteralOp
	UnitLiteralOp
	IDOp
	ApplyOp
	RecordSelectorOp
	TupleOp
	ListOp
	RecordOp

	// Infix and prefix operators, in increasing precedence.

	ImpliesOp
	OrelseOp
	AndalsoOp
	ComposeOp
	LeOp
	LtOp
	GeOp
	GtOp
	EqOp
	NeOp
	ElemOp
	NotElemOp
	ConsOp
	AtOp
	PlusOp
	MinusOp
	CaretOp
	NegateOp
	TimesOp
	DivideOp
	DivOp
	ModOp

	// Conditionals, functions, and matches.

	IfOp
	FnOp
	CaseOp
	MatchOp

	// Patterns.

	IDPatOp
	WildcardPatOp
	IntLiteralPatOp
	RealLiteralPatOp
	StringLiteralPatOp
	CharLiteralPatOp
	TuplePatOp
	ListPatOp
	RecordPatOp
	ConsPatOp
	AsPatOp

	// Declarations.

	ValDeclOp
	ValBindOp
	FunDeclOp
	FunBindOp
	FunMatchOp
	LetOp
	DatatypeDeclOp
	TypeDeclOp

	// Types and annotations.

	TyVarOp
	NamedTypeOp
	TupleTypeOp
	FunctionTypeOp
	RecordTypeOp
	AnnotatedExpOp
	AnnotatedPatOp
)

var opNames = map[Op]string{
	IntLiteralOp:     "int_literal",
	RealLiteralOp:    "real_literal",
	StringLiteralOp:  "string_literal",
	CharLiteralOp:    "char_literal",
	BoolLiteralOp:    "bool_literal",
	UnitLiteralOp:    "unit_literal",
	IDOp:             "id",
	ApplyOp:          "apply",
	RecordSelectorOp: "record_selector",
	TupleOp:          "tuple",
	ListOp:           "list",
	RecordOp:         "record",
	ImpliesOp:        "implies",
	OrelseOp:         "orelse",
	AndalsoOp:        "andalso",
	ComposeOp:        "compose",
	LeOp:             "le",
	LtOp:             "lt",
	GeOp:             "ge",
	GtOp:             "gt",
	EqOp:             "eq",
	NeOp:             "ne",
	ElemOp:           "elem",
	NotElemOp:        "not_elem",
	ConsOp:           "cons",
	AtOp:             "at",
	PlusOp:           "plus",
	MinusOp:          "minus",
	CaretOp:          "caret",
	NegateOp:         "negate",
	TimesOp:          "times",
	DivideOp:         "divide",
	DivOp:            "div",
	ModOp:            "mod",

	IfOp:    "if",
	FnOp:    "fn",
	CaseOp:  "case",
	MatchOp: "match",

	IDPatOp:            "id_pat",
	WildcardPatOp:      "wildcard_pat",
	IntLiteralPatOp:    "int_literal_pat",
	RealLiteralPatOp:   "real_literal_pat",
	StringLiteralPatOp: "string_literal_pat",
	CharLiteralPatOp:   "char_literal_pat",
	TuplePatOp:         "tuple_pat",
	ListPatOp:          "list_pat",
	RecordPatOp:        "record_pat",
	ConsPatOp:          "cons_pat",
	AsPatOp:            "as_pat",

	ValDeclOp:  "val",
	ValBindOp:  "val_bind",
	FunDeclOp:  "fun",
	FunBindOp:  "fun_bind",
	FunMatchOp: "fun_match",
	LetOp:      "let",

	DatatypeDeclOp: "datatype_decl",
	TypeDeclOp:     "type_decl",
	TyVarOp:        "ty_var",
	NamedTypeOp:    "named",
	TupleTypeOp:    "tuple_type",
	FunctionTypeOp: "function_type",
	RecordTypeOp:   "record_type",
	AnnotatedExpOp: "annotated_exp",
	AnnotatedPatOp: "annotated_pat",
}

func (o Op) String() string {
	if s, ok := opNames[o]; ok {
		return s
	}
	return "unknown"
}
