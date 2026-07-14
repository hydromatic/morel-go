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

package eval

// Pat is a compiled pattern. Match tests a value, binding the
// pattern's variables into the frame's slots, and reports
// whether it matched.
type Pat interface {
	Match(v Val, f *Frame) bool
}

// LiteralPat matches a constant.
type LiteralPat struct {
	V Val
}

// Match implements Pat.
func (p LiteralPat) Match(v Val, _ *Frame) bool {
	return v == p.V
}

// SlotPat binds a value to a variable's slot; it always matches.
type SlotPat struct {
	Slot int
}

// Match implements Pat.
func (p SlotPat) Match(v Val, f *Frame) bool {
	f.Slots[p.Slot] = v
	return true
}

// WildcardPat matches anything, binding nothing.
type WildcardPat struct{}

// Match implements Pat.
func (WildcardPat) Match(Val, *Frame) bool {
	return true
}

// MatchClause is one rule of a compiled case: a pattern and the
// code to run when it matches.
type MatchClause struct {
	Pat  Pat
	Body Code
}

// Case returns code that evaluates a scrutinee and runs the body
// of the first clause whose pattern matches; if none matches, it
// raises Bind.
func Case(scrutinee Code, clauses []MatchClause) Code {
	return &caseCode{scrutinee: scrutinee, clauses: clauses}
}

type caseCode struct {
	scrutinee Code
	clauses   []MatchClause
}

func (c *caseCode) Eval(f *Frame) (Val, error) {
	v, err := c.scrutinee.Eval(f)
	if err != nil {
		return nil, err
	}
	for _, clause := range c.clauses {
		if clause.Pat.Match(v, f) {
			return clause.Body.Eval(f)
		}
	}
	return nil, &MorelError{Exn: "Bind"}
}

func (c *caseCode) Describe() string {
	return "case(" + c.scrutinee.Describe() + ")"
}
