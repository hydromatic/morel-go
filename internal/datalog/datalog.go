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

// Package datalog implements the Souffle-flavored Datalog dialect
// behind the Datalog built-in structure: a parser, an analyzer
// (declarations, rule safety, stratification), and a translator
// that emits Morel source whose recursion is a Relational.iterate
// fixpoint. The emitted text and every error message are pinned
// byte for byte.
package datalog

import (
	"fmt"
	"strconv"
	"strings"
)

// Error is a Datalog error. Its message is user-facing output —
// the validate built-in returns it as a string — so it carries
// the reference implementation's exact wording, including
// capitalization.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

// errorf builds an Error from a format string.
func errorf(format string, args ...any) error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// The two Datalog column types.
const (
	typeInt    = "int"
	typeString = "string"
)

// Program is a parsed Datalog program: its statements in source
// order, with declarations, inputs, and outputs indexed.
type Program struct {
	Statements []Statement
	Decls      map[string]*Declaration
	Inputs     []*Input
	Outputs    []*Output
}

// NewProgram indexes a statement list into a program.
func NewProgram(stmts []Statement) *Program {
	p := &Program{
		Statements: stmts,
		Decls:      map[string]*Declaration{},
	}
	for _, s := range stmts {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch s := s.(type) {
		case *Declaration:
			p.Decls[s.Name] = s
		case *Input:
			p.Inputs = append(p.Inputs, s)
		case *Output:
			p.Outputs = append(p.Outputs, s)
		}
	}
	return p
}

// Statement is a Datalog statement: a declaration, an input or
// output directive, a fact, or a rule.
type Statement interface {
	stmt()
}

// Declaration is ".decl name(param:type, ...)".
type Declaration struct {
	Name   string
	Params []Param
}

// Param is one "name:type" in a declaration.
type Param struct {
	Name string
	Type string
}

// Input is ".input relation [file]"; the file defaults to
// "relation.csv".
type Input struct {
	Relation string
	FileName string
}

// EffectiveFileName is the input's file, defaulted.
func (i *Input) EffectiveFileName() string {
	if i.FileName != "" {
		return i.FileName
	}
	return i.Relation + ".csv"
}

// Output is ".output relation".
type Output struct {
	Relation string
}

// Fact is "atom." with constant arguments.
type Fact struct {
	Atom *Atom
}

// Rule is "head :- body.".
type Rule struct {
	Head *Atom
	Body []BodyItem
}

func (*Declaration) stmt() {}
func (*Input) stmt()       {}
func (*Output) stmt()      {}
func (*Fact) stmt()        {}
func (*Rule) stmt()        {}

// BodyItem is one conjunct of a rule body: an atom, possibly
// negated, or a comparison.
type BodyItem interface {
	bodyItem()
}

// BodyAtom is a positive or negated atom in a rule body.
type BodyAtom struct {
	Atom    *Atom
	Negated bool
}

// Comparison is "term op term" in a rule body; Op is the Datalog
// spelling ("=", "!=", "<", "<=", ">", ">=").
type Comparison struct {
	Left  Term
	Right Term
	Op    string
}

func (*BodyAtom) bodyItem()   {}
func (*Comparison) bodyItem() {}

// Atom is "name(term, ...)".
type Atom struct {
	Name string
	Args []Term
}

// Term is an atom argument: a variable, a constant, or an
// arithmetic expression. Its String form appears in error
// messages.
type Term interface {
	term()
	String() string
}

// Variable is a Datalog variable; following Souffle, every bare
// identifier is a variable regardless of case.
type Variable struct {
	Name string
}

// Constant is an int or string literal.
type Constant struct {
	Type string
	Int  int
	Str  string
}

// Arith is "left op right" with op one of + - * /.
type Arith struct {
	Left  Term
	Right Term
	Op    string
}

func (v *Variable) String() string { return v.Name }

func (c *Constant) String() string {
	if c.Type == typeString {
		return quoteString(c.Str)
	}
	return strconv.Itoa(c.Int)
}

func (a *Arith) String() string {
	return a.Left.String() + " " + a.Op + " " + a.Right.String()
}

func (*Variable) term() {}
func (*Constant) term() {}
func (*Arith) term()    {}

// quoteString renders a string constant as a quoted literal,
// escaping backslash and double-quote.
func quoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := range len(s) {
		c := s[i]
		if c == '\\' || c == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}
