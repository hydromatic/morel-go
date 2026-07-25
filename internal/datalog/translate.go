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

package datalog

import (
	"strconv"
	"strings"
)

// Translate renders a validated program as Morel source, exactly
// as the reference translator does — the corpus pins the emitted
// text. Each relation with facts or rules becomes a let binding,
// in declaration order: facts alone become a list literal; rules
// become a Relational.iterate fixpoint whose seed holds the facts
// (plus, for a recursive relation, the sources of its passthrough
// base rules) and whose step unions the remaining rules. The body
// is a record of the .output relations.
func Translate(prog *Program) string {
	rels := gather(prog)
	out := outputExpr(prog, rels)
	var bound []*relation
	for _, s := range prog.Statements {
		d, ok := s.(*Declaration)
		if !ok {
			continue
		}
		r := rels[d.Name]
		if len(r.facts) > 0 || len(r.rules) > 0 {
			bound = append(bound, r)
		}
	}
	if len(bound) == 0 {
		return out
	}
	var b strings.Builder
	b.WriteString("let\n")
	for _, r := range bound {
		appendRelation(&b, r)
	}
	b.WriteString("in\n  " + out + "\nend")
	return b.String()
}

// relation is one declared relation with its facts and rules.
type relation struct {
	decl  *Declaration
	facts []*Fact
	rules []*Rule
}

// gather groups the program's facts and rules by relation.
func gather(prog *Program) map[string]*relation {
	rels := map[string]*relation{}
	for name, d := range prog.Decls {
		rels[name] = &relation{decl: d}
	}
	for _, s := range prog.Statements {
		switch s := s.(type) {
		case *Fact:
			r := rels[s.Atom.Name]
			r.facts = append(r.facts, s)
		case *Rule:
			r := rels[s.Head.Name]
			r.rules = append(r.rules, s)
		}
	}
	return rels
}

// outputExpr renders the result record: each .output relation, in
// directive order, yields its rows as records named by the
// declaration's parameters. An arity-0 or arity-1 relation is
// referenced bare; a query is parenthesized only when there are
// several outputs. A relation with no facts or rules is the empty
// list. With no .output directives the result is unit.
func outputExpr(prog *Program, rels map[string]*relation) string {
	if len(prog.Outputs) == 0 {
		return "()"
	}
	parts := make([]string, len(prog.Outputs))
	for i, out := range prog.Outputs {
		r := rels[out.Relation]
		var e string
		switch {
		case r == nil || (len(r.facts) == 0 && len(r.rules) == 0):
			e = "[]"
		case len(r.decl.Params) <= 1:
			e = out.Relation
		default:
			names := make([]string, len(r.decl.Params))
			for j, p := range r.decl.Params {
				names[j] = p.Name
			}
			e = "from (" + strings.Join(names, ", ") + ") in " +
				out.Relation
			if len(prog.Outputs) > 1 {
				e = "(" + e + ")"
			}
		}
		parts[i] = out.Relation + " = " + e
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// appendRelation emits one relation's let binding.
func appendRelation(b *strings.Builder, r *relation) {
	name := r.decl.Name
	if len(r.rules) == 0 {
		b.WriteString("  val " + name + " = " +
			factList(r.facts) + "\n")
		return
	}
	recursive := false
	for _, rule := range r.rules {
		if referencesRelation(rule, name) {
			recursive = true
		}
	}
	var seedParts []string
	if len(r.facts) > 0 {
		seedParts = append(seedParts, factList(r.facts))
	}
	var stepRules []*Rule
	for _, rule := range r.rules {
		atom, passthrough := rule.Body[0].(*BodyAtom)
		if recursive && passthrough && isPassthrough(rule) {
			seedParts = append(seedParts, atom.Atom.Name)
		} else {
			stepRules = append(stepRules, rule)
		}
	}
	seed := "[]"
	if len(seedParts) > 0 {
		seed = strings.Join(seedParts, " @ ")
	}
	params := "(_, _)"
	if recursive {
		capName := capitalize(name)
		params = "(all" + capName + ", new" + capName + ")"
	}
	var step string
	if len(stepRules) == 1 {
		step = ruleFrom(stepRules[0], name, recursive)
	} else {
		froms := make([]string, len(stepRules))
		for i, rule := range stepRules {
			froms[i] = "(" + ruleFrom(rule, name, recursive) + ")"
		}
		step = strings.Join(froms, "\n        @ ")
	}
	b.WriteString("  val " + name + " =\n" +
		"    Relational.iterate " + seed + "\n" +
		"      (fn " + params + " =>\n" +
		"        " + step + ")\n")
}

// factList renders facts as a Morel list literal: arity 1 as bare
// values, higher arities as tuples.
func factList(facts []*Fact) string {
	parts := make([]string, len(facts))
	for i, f := range facts {
		args := make([]string, len(f.Atom.Args))
		for j, t := range f.Atom.Args {
			if c, ok := t.(*Constant); ok {
				args[j] = c.morel()
			}
		}
		parts[i] = tupleOf(args)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// tupleOf renders values as a Morel tuple: unit for none, bare
// for one.
func tupleOf(parts []string) string {
	switch len(parts) {
	case 0:
		return "()"
	case 1:
		return parts[0]
	default:
		return "(" + strings.Join(parts, ", ") + ")"
	}
}

// referencesRelation reports whether a rule's body mentions a
// relation.
func referencesRelation(r *Rule, name string) bool {
	for _, item := range r.Body {
		if atom, ok := item.(*BodyAtom); ok &&
			atom.Atom.Name == name {
			return true
		}
	}
	return false
}

// isPassthrough reports whether a rule merely copies another
// relation: a single positive atom over a different relation,
// with the head's variables repeated in the same order. Such a
// base rule of a recursive relation seeds the iteration.
func isPassthrough(r *Rule) bool {
	if len(r.Body) != 1 {
		return false
	}
	atom, ok := r.Body[0].(*BodyAtom)
	if !ok || atom.Negated || atom.Atom.Name == r.Head.Name ||
		len(atom.Atom.Args) != len(r.Head.Args) {
		return false
	}
	for i, t := range r.Head.Args {
		hv, ok := t.(*Variable)
		if !ok {
			return false
		}
		bv, ok := atom.Atom.Args[i].(*Variable)
		if !ok || bv.Name != hv.Name {
			return false
		}
	}
	return true
}

// ruleCtx is the per-rule translation state: the Datalog-variable
// to Morel-name mapping, the names taken, the fresh-variable
// counter, and the accumulated scans and constraints.
type ruleCtx struct {
	names map[string]string
	used  map[string]bool
	fresh int
	scans []string
	cons  []string
}

// ruleFrom renders one rule as a Morel from expression. Positive
// atoms scan their relations — the recursive relation's first
// occurrence scans the frontier ("newX"), later occurrences the
// accumulation ("allX"). A repeated variable, a constant, or an
// arithmetic argument scans as a fresh variable equated in the
// where clause; comparisons join the where clause in body order;
// a negated atom becomes "not (row elem relation)". The head
// becomes the yield.
func ruleFrom(r *Rule, relName string, recursive bool) string {
	c := &ruleCtx{
		names: map[string]string{},
		used:  map[string]bool{},
	}
	selfSeen := false
	for _, item := range r.Body {
		switch item := item.(type) {
		case *BodyAtom:
			if item.Negated {
				c.negation(item.Atom)
				continue
			}
			src := item.Atom.Name
			if recursive && src == relName {
				if selfSeen {
					src = "all" + capitalize(relName)
				} else {
					src = "new" + capitalize(relName)
					selfSeen = true
				}
			}
			c.scan(item.Atom, src)
		case *Comparison:
			op := item.Op
			if op == "!=" {
				op = "<>"
			}
			c.cons = append(c.cons, c.expr(item.Left, "", false)+
				" "+op+" "+c.expr(item.Right, "", false))
		}
	}
	args := make([]string, len(r.Head.Args))
	for i, t := range r.Head.Args {
		args[i] = c.expr(t, "", false)
	}
	s := "from " + strings.Join(c.scans, ", ")
	if len(c.cons) > 0 {
		s += " where " + strings.Join(c.cons, " andalso ")
	}
	return s + " yield " + tupleOf(args)
}

// scan adds one positive atom as a scan over src.
func (c *ruleCtx) scan(atom *Atom, src string) {
	elts := make([]string, len(atom.Args))
	for i, t := range atom.Args {
		switch t := t.(type) {
		case *Variable:
			if existing, ok := c.names[t.Name]; ok {
				f := c.freshName()
				c.cons = append(c.cons, existing+" = "+f)
				elts[i] = f
			} else {
				elts[i] = c.bind(t.Name)
			}
		default:
			f := c.freshName()
			c.cons = append(c.cons, f+" = "+c.expr(t, "", false))
			elts[i] = f
		}
	}
	c.scans = append(c.scans, tupleOf(elts)+" in "+src)
}

// negation renders a negated atom as a non-membership test.
func (c *ruleCtx) negation(atom *Atom) {
	args := make([]string, len(atom.Args))
	for i, t := range atom.Args {
		args[i] = c.expr(t, "", false)
	}
	c.cons = append(c.cons,
		"not ("+tupleOf(args)+" elem "+atom.Name+")")
}

// bind assigns a Datalog variable its Morel name: the lowercased
// variable name, or a fresh name if that is taken.
func (c *ruleCtx) bind(varName string) string {
	name := strings.ToLower(varName)
	if c.used[name] {
		name = c.freshName()
	} else {
		c.used[name] = true
	}
	c.names[varName] = name
	return name
}

// freshName mints the next "vN" name.
func (c *ruleCtx) freshName() string {
	name := "v" + strconv.Itoa(c.fresh)
	c.fresh++
	c.used[name] = true
	return name
}

// expr renders a term as a Morel expression. An arithmetic
// subexpression is parenthesized when its precedence is below its
// parent's, or when it is the right operand of a same-precedence
// "-" or "/".
func (c *ruleCtx) expr(t Term, parentOp string, right bool) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *Arith:
		s := c.expr(t.Left, t.Op, false) + " " + t.Op + " " +
			c.expr(t.Right, t.Op, true)
		if needsParens(t.Op, parentOp, right) {
			return "(" + s + ")"
		}
		return s
	case *Constant:
		return t.morel()
	case *Variable:
		if name, ok := c.names[t.Name]; ok {
			return name
		}
		return c.bind(t.Name)
	default:
		return t.String()
	}
}

// needsParens decides parenthesization of an arithmetic
// subexpression under a parent operator.
func needsParens(op, parentOp string, right bool) bool {
	if parentOp == "" {
		return false
	}
	child, parent := precedence(op), precedence(parentOp)
	return child < parent || (right && child == parent &&
		(parentOp == "-" || parentOp == "/"))
}

// Operator precedence levels: additive binds looser than
// multiplicative.
const (
	addPrec = 1
	mulPrec = 2
)

// precedence is an arithmetic operator's level.
func precedence(op string) int {
	if op == "+" || op == "-" {
		return addPrec
	}
	return mulPrec
}

// morel renders a constant as a Morel literal.
func (c *Constant) morel() string {
	if c.Type == typeString {
		return quoteString(c.Str)
	}
	return strconv.Itoa(c.Int)
}

// capitalize upper-cases a relation name's first letter, for the
// "allX"/"newX" iteration variables.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
