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
}

func (o Op) String() string {
	if s, ok := opNames[o]; ok {
		return s
	}
	return "unknown"
}
