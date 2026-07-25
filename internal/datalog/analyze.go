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

import "sort"

// Analyze validates a program in the reference implementation's
// order: declarations and arities, then rule safety, then
// stratification of negation. Error messages are pinned by the
// corpus (the shell prefixes them with "Compilation error: ").
func Analyze(prog *Program) error {
	err := checkDeclarations(prog)
	if err != nil {
		return err
	}
	err = checkSafety(prog)
	if err != nil {
		return err
	}
	return checkStratification(prog)
}

// checkDeclarations checks that every referenced relation is
// declared with matching arity, that constant arguments match the
// declared column types, and that fact arguments are constants.
func checkDeclarations(prog *Program) error {
	for _, in := range prog.Inputs {
		if prog.Decls[in.Relation] == nil {
			return errorf(
				"Relation '%s' used in .input but not declared",
				in.Relation,
			)
		}
	}
	for _, s := range prog.Statements {
		var err error
		switch s := s.(type) {
		case *Fact:
			err = checkFact(prog, s)
		case *Rule:
			err = checkRule(prog, s)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkFact checks a fact's atom and that its arguments are
// constants.
func checkFact(prog *Program, f *Fact) error {
	err := checkAtom(prog, f.Atom, "fact")
	if err != nil {
		return err
	}
	for _, t := range f.Atom.Args {
		if _, ok := t.(*Constant); !ok {
			return errorf(
				"Argument in fact is not constant: %s", t,
			)
		}
	}
	return nil
}

// checkRule checks a rule's head and body atoms.
func checkRule(prog *Program, r *Rule) error {
	err := checkAtom(prog, r.Head, "rule head")
	if err != nil {
		return err
	}
	for _, item := range r.Body {
		atom, ok := item.(*BodyAtom)
		if !ok {
			continue
		}
		err := checkAtom(prog, atom.Atom, "rule body")
		if err != nil {
			return err
		}
	}
	return nil
}

// checkAtom checks that an atom's relation is declared, its arity
// matches, and its constant arguments have the declared types.
func checkAtom(prog *Program, atom *Atom, context string) error {
	decl := prog.Decls[atom.Name]
	if decl == nil {
		return errorf("Relation '%s' used in %s but not declared",
			atom.Name, context)
	}
	if len(atom.Args) != len(decl.Params) {
		return errorf(
			"Atom %s/%d does not match declaration %s/%d",
			atom.Name, len(atom.Args), decl.Name,
			len(decl.Params),
		)
	}
	for i, t := range atom.Args {
		c, ok := t.(*Constant)
		if !ok || c.Type == decl.Params[i].Type {
			continue
		}
		return errorf("Type mismatch in %s %s(...): "+
			"expected %s, got %s for parameter %s",
			context, atom.Name, decl.Params[i].Type, c.Type,
			decl.Params[i].Name)
	}
	return nil
}

// checkSafety checks each rule's range restriction: every
// variable in the head, in a comparison, or in a negated atom
// must be grounded — appear as a direct argument of a positive
// body atom. Variables inside arithmetic do not ground, following
// Souffle.
func checkSafety(prog *Program) error {
	for _, s := range prog.Statements {
		r, ok := s.(*Rule)
		if !ok {
			continue
		}
		err := checkRuleSafety(r)
		if err != nil {
			return err
		}
	}
	return nil
}

// checkRuleSafety checks one rule's range restriction.
func checkRuleSafety(r *Rule) error {
	grounded := map[string]bool{}
	for _, item := range r.Body {
		atom, ok := item.(*BodyAtom)
		if !ok || atom.Negated {
			continue
		}
		for _, t := range atom.Atom.Args {
			if v, ok := t.(*Variable); ok {
				grounded[v.Name] = true
			}
		}
	}
	for _, t := range r.Head.Args {
		for _, v := range variablesOf(t) {
			if !grounded[v] {
				return errorf("Rule is unsafe. Variable '%s' in "+
					"head does not appear in positive body atom",
					v)
			}
		}
	}
	for _, item := range r.Body {
		var terms []Term
		var context string
		switch item := item.(type) {
		case *BodyAtom:
			if !item.Negated {
				continue
			}
			terms = item.Atom.Args
			context = "negated atom"
		case *Comparison:
			terms = []Term{item.Left, item.Right}
			context = "comparison"
		}
		for _, t := range terms {
			for _, v := range variablesOf(t) {
				if !grounded[v] {
					return errorf("Rule is unsafe. Variable "+
						"'%s' in %s does not appear in positive "+
						"body atom", v, context)
				}
			}
		}
	}
	return nil
}

// variablesOf collects a term's variables, left to right,
// including those inside arithmetic.
func variablesOf(t Term) []string {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *Arith:
		return append(variablesOf(t.Left),
			variablesOf(t.Right)...)
	case *Variable:
		return []string{t.Name}
	default:
		return nil
	}
}

// depEdge is a dependency of one relation on another: the head
// relation depends on each relation its rule bodies reference,
// negated or not.
type depEdge struct {
	to      string
	negated bool
}

// checkStratification rejects a negation cycle: a relation that
// depends on itself through at least one negated atom.
func checkStratification(prog *Program) error {
	graph := map[string][]depEdge{}
	for _, s := range prog.Statements {
		r, ok := s.(*Rule)
		if !ok {
			continue
		}
		for _, item := range r.Body {
			atom, ok := item.(*BodyAtom)
			if !ok {
				continue
			}
			graph[r.Head.Name] = append(graph[r.Head.Name],
				depEdge{to: atom.Atom.Name, negated: atom.Negated})
		}
	}
	heads := make([]string, 0, len(graph))
	for name := range graph {
		heads = append(heads, name)
	}
	sort.Strings(heads)
	for _, head := range heads {
		if negationCycle(graph, head) {
			return errorf("Program is not stratified. Negation "+
				"cycle detected involving relation: %s", head)
		}
	}
	return nil
}

// negationCycle reports whether some cycle through start contains
// a negated edge.
func negationCycle(graph map[string][]depEdge, start string) bool {
	type state struct {
		node    string
		negated bool
	}
	seen := map[state]bool{}
	var walk func(node string, negated bool) bool
	walk = func(node string, negated bool) bool {
		for _, e := range graph[node] {
			neg := negated || e.negated
			if e.to == start && neg {
				return true
			}
			st := state{node: e.to, negated: neg}
			if seen[st] {
				continue
			}
			seen[st] = true
			if walk(e.to, neg) {
				return true
			}
		}
		return false
	}
	return walk(start, false)
}
