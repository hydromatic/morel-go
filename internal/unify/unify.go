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

package unify

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// Action is called when a variable's term becomes known during
// unification. It may add term pairs, e.g. to equate a record
// field's type with the type of a selector applied to the record.
type Action func(v *Var, t Term, s *Substitution,
	add func(left, right Term))

// VarAction registers an Action on a variable. Several actions
// may be registered on the same variable; all of them fire.
type VarAction struct {
	Var    *Var
	Action Action
}

// ConstraintAction is called when a constraint has been narrowed
// down to a single candidate.
type ConstraintAction func(actualArg, candidate Term,
	add func(left, right Term))

// Candidate is one alternative within a Constraint. A nil Action
// just adds the equation "arg = Term" when the candidate is
// selected.
type Candidate struct {
	Term   Term
	Action ConstraintAction
}

// Constraint requires Arg to unify with one of the Candidates.
// As unification progresses, candidates that cannot unify are
// eliminated; when one remains, its action fires. Constraints
// model overloaded operators, and will also carry hooks such as
// "default to int" and collection-kind resolution.
type Constraint struct {
	Arg        *Var
	Candidates []Candidate
}

// Equiv returns a ConstraintAction that equates two fixed terms.
func Equiv(t1, t2 Term) ConstraintAction {
	return func(_, _ Term, add func(left, right Term)) {
		add(t1, t2)
	}
}

// Overload returns the constraint arising from a call to an
// overloaded function. Each element of argResults gives an
// argument type and the corresponding result type: if arg
// unifies with the argument type of exactly one candidate, then
// result is equated with that candidate's result type.
func Overload(arg, result *Var, argResults []TermPair) Constraint {
	candidates := make([]Candidate, len(argResults))
	for i, ar := range argResults {
		resultType := ar.Right
		candidates[i] = Candidate{
			Term: ar.Left,
			Action: func(actualArg, candidate Term,
				add func(left, right Term),
			) {
				add(actualArg, candidate)
				add(result, resultType)
			},
		}
	}
	return Constraint{Arg: arg, Candidates: candidates}
}

// Unifier creates variables and atoms with unique names, and
// unifies terms.
type Unifier struct {
	varByName   map[string]*Var
	atomByName  map[string]*Sequence
	nameOrdinal map[string]int
}

// New returns an empty Unifier.
func New() *Unifier {
	return &Unifier{
		varByName:   map[string]*Var{},
		atomByName:  map[string]*Sequence{},
		nameOrdinal: map[string]int{},
	}
}

// Atom returns the atom with the given name, creating it if
// necessary.
func (u *Unifier) Atom(name string) *Sequence {
	if s, ok := u.atomByName[name]; ok {
		return s
	}
	s := &Sequence{Op: name}
	u.atomByName[name] = s
	return s
}

// AtomUnique creates an atom whose name is the prefix followed by
// an ordinal that makes it unique among atoms and variables.
func (u *Unifier) AtomUnique(prefix string) *Sequence {
	name, _ := u.newName(prefix)
	s := &Sequence{Op: name}
	u.atomByName[name] = s
	return s
}

// Unify finds a substitution that satisfies the term pairs, or
// returns an error describing why unification failed. Actions
// fire as their variables resolve; constraints narrow as
// unification progresses.
func (u *Unifier) Unify(pairs []TermPair, actions []VarAction,
	constraints []Constraint,
) (*Substitution, error) {
	termActions := map[*Var][]Action{}
	for _, va := range actions {
		termActions[va.Var] = append(termActions[va.Var],
			va.Action)
	}
	w := newWork(pairs, constraints)
	for {
		if len(w.seqSeq) > 0 {
			pair := w.seqSeq[0]
			w.seqSeq = w.seqSeq[1:]
			err := w.decompose(pair)
			if err != nil {
				return nil, err
			}
			continue
		}
		if len(w.varAny) > 0 {
			pair := w.varAny[0]
			w.varAny = w.varAny[1:]
			err := u.eliminate(w, pair, termActions)
			if err != nil {
				return nil, err
			}
			continue
		}
		return &Substitution{Map: w.result}, nil
	}
}

// Variable creates a variable named "Tn", for an ordinal n that
// makes the name unique among atoms and variables.
func (u *Unifier) Variable() *Var {
	name, ordinal := u.newName("T")
	v := &Var{Name: name, Ordinal: ordinal}
	u.varByName[name] = v
	return v
}

// VariableNamed returns the variable with the given name,
// creating it if necessary.
func (u *Unifier) VariableNamed(name string) *Var {
	if v, ok := u.varByName[name]; ok {
		return v
	}
	v := &Var{Name: name, Ordinal: -1}
	u.varByName[name] = v
	return v
}

// VariableOrdinal returns the variable "Tn" with the given
// ordinal, creating it if necessary.
func (u *Unifier) VariableOrdinal(ordinal int) *Var {
	name := "T" + strconv.Itoa(ordinal)
	if v, ok := u.varByName[name]; ok {
		return v
	}
	v := &Var{Name: name, Ordinal: ordinal}
	u.varByName[name] = v
	return v
}

// eliminate handles one "variable = term" pair: it checks for
// cycles, records the variable's value, fires actions, and
// substitutes throughout the remaining work.
func (u *Unifier) eliminate(w *work, pair TermPair,
	termActions map[*Var][]Action,
) error {
	variable, _ := pair.Left.(*Var)
	term := pair.Right
	if term.contains(variable) {
		return fmt.Errorf("cycle: variable %s in %s", variable,
			term)
	}
	// If the term is a variable that has already been resolved,
	// map to its ultimate target.
	for {
		v2, ok := term.(*Var)
		if !ok {
			break
		}
		t2, ok := w.result[v2]
		if !ok {
			break
		}
		term = t2
	}
	if term.eq(variable) {
		return nil
	}
	prior, hadPrior := w.result[variable]
	w.result[variable] = term
	if hadPrior && !prior.eq(term) {
		w.add(prior, term)
	}
	if len(termActions) > 0 {
		active := map[*Var]bool{}
		u.act(variable, term, w, &Substitution{Map: w.result},
			termActions, active)
	}
	return w.substituteAll(variable, term)
}

// act fires the actions registered on a variable, unless the
// variable is already in the active set (which prevents infinite
// recursion).
func (u *Unifier) act(variable *Var, term Term, w *work,
	s *Substitution, termActions map[*Var][]Action,
	active map[*Var]bool,
) {
	if active[variable] {
		return
	}
	active[variable] = true
	u.act2(variable, term, w, s, termActions, active)
	delete(active, variable)
}

func (u *Unifier) act2(variable *Var, term Term, w *work,
	s *Substitution, termActions map[*Var][]Action,
	active map[*Var]bool,
) {
	for _, action := range termActions[variable] {
		action(variable, term, s, w.add)
	}
	if v, ok := term.(*Var); ok {
		// Copy the pairs, because an action may append while we
		// iterate.
		for _, tp := range w.allTermPairs() {
			if tp.Left.eq(term) {
				u.act(variable, tp.Right, w, s, termActions,
					active)
			}
		}
		// The term is itself a variable; fire its actions too.
		// The depth limit prevents swapping back.
		const maxDepth = 2
		if len(active) < maxDepth {
			u.act(v, variable, w, s, termActions, active)
		}
	}
	// Any variable that resolved to this variable has also just
	// been unified; fire its actions.
	var resolved []*Var
	for v2, t2 := range s.Map {
		if t2.eq(variable) {
			resolved = append(resolved, v2)
		}
	}
	sort.Slice(resolved, func(i, j int) bool {
		return varLess(resolved[i], resolved[j])
	})
	for _, v2 := range resolved {
		u.act(v2, term, w, s, termActions, active)
	}
}

// newName finds an ordinal that makes prefix+ordinal unique among
// atoms and variables, and returns the name and the ordinal.
func (u *Unifier) newName(prefix string) (string, int) {
	for {
		ordinal := u.nameOrdinal[prefix]
		u.nameOrdinal[prefix] = ordinal + 1
		name := prefix + strconv.Itoa(ordinal)
		if u.varByName[name] == nil && u.atomByName[name] == nil {
			return name, ordinal
		}
	}
}

// kind classifies a term pair, determining which queue it belongs
// to (or, for kindDelete, that it can be discarded).
type kind int

const (
	kindDelete kind = iota
	kindNonVarVar
	kindSeqSeq
	kindVarAny
)

func kindOf(left, right Term) kind {
	if left.eq(right) {
		return kindDelete
	}
	if _, ok := left.(*Sequence); ok {
		if _, ok := right.(*Sequence); ok {
			return kindSeqSeq
		}
		return kindNonVarVar
	}
	return kindVarAny
}

// mutableConstraint is a Constraint whose candidates are narrowed
// in place during unification.
type mutableConstraint struct {
	arg        Term
	candidates []Candidate
}

// work is the workspace of one Unify call: queues of pending term
// pairs, the constraints, and the substitution built so far.
type work struct {
	// seqSeq holds pairs where both terms are sequences; varAny
	// holds pairs whose left term is a variable.
	seqSeq      []TermPair
	varAny      []TermPair
	constraints []*mutableConstraint
	result      map[*Var]Term
}

func newWork(pairs []TermPair, constraints []Constraint) *work {
	w := &work{result: map[*Var]Term{}}
	for _, p := range pairs {
		w.add(p.Left, p.Right)
	}
	for _, c := range constraints {
		candidates := make([]Candidate, len(c.Candidates))
		copy(candidates, c.Candidates)
		w.constraints = append(w.constraints, &mutableConstraint{
			arg:        c.Arg,
			candidates: candidates,
		})
	}
	return w
}

func (w *work) add(left, right Term) {
	// lint: sort until '^\t}' where '^\tcase '
	switch kindOf(left, right) {
	case kindDelete:
	case kindNonVarVar:
		w.varAny = append(w.varAny, TermPair{right, left})
	case kindSeqSeq:
		w.seqSeq = append(w.seqSeq, TermPair{left, right})
	case kindVarAny:
		w.varAny = append(w.varAny, TermPair{left, right})
	}
}

// add2 is like add but first applies the current result, for
// terms whose variables may already have been substituted away.
func (w *work) add2(left, right Term) {
	w.add(left.apply(w.result), right.apply(w.result))
}

func (w *work) allTermPairs() []TermPair {
	pairs := make([]TermPair, 0, len(w.seqSeq)+len(w.varAny))
	pairs = append(pairs, w.seqSeq...)
	pairs = append(pairs, w.varAny...)
	return pairs
}

// decompose handles one "sequence = sequence" pair: conflict if
// the operators differ, otherwise equate the children pairwise.
func (w *work) decompose(pair TermPair) error {
	left, _ := pair.Left.(*Sequence)
	right, _ := pair.Right.(*Sequence)
	if left.Op != right.Op || len(left.Terms) != len(right.Terms) {
		return fmt.Errorf("conflict: %s vs %s", left, right)
	}
	for i, t := range left.Terms {
		w.add(t, right.Terms[i])
	}
	return nil
}

// substituteAll applies "variable = term" to all pending pairs
// and constraints.
func (w *work) substituteAll(variable *Var, term Term) error {
	w.sub(variable, term, &w.seqSeq, kindSeqSeq)
	w.sub(variable, term, &w.varAny, kindVarAny)
	return w.subConstraints(variable, term)
}

// sub applies "variable = term" to each pair in a queue, moving
// pairs whose kind changed to the right place.
func (w *work) sub(variable *Var, term Term, queue *[]TermPair,
	k kind,
) {
	kept := (*queue)[:0]
	for _, pair := range *queue {
		left2 := pair.Left.apply1(variable, term)
		right2 := pair.Right.apply1(variable, term)
		if left2 == pair.Left && right2 == pair.Right {
			kept = append(kept, pair)
			continue
		}
		k2 := kindOf(left2, right2)
		switch {
		case k2 == k:
			kept = append(kept, TermPair{left2, right2})
		case k2 == kindNonVarVar && k == kindVarAny:
			kept = append(kept, TermPair{right2, left2})
		default:
			// The pair belongs in another queue (or, if it became
			// trivial, is discarded).
			w.add(left2, right2)
		}
	}
	*queue = kept
}

// subConstraints applies "variable = term" to each constraint,
// eliminating candidates that can no longer unify. A constraint
// narrowed to one candidate fires its action; a constraint with
// no candidates left fails.
func (w *work) subConstraints(variable *Var, term Term) error {
	for _, c := range w.constraints {
		changes := 0
		argChanged := false
		arg2 := c.arg.apply1(variable, term)
		if arg2 != c.arg {
			changes++
			argChanged = true
			c.arg = arg2
		}
		kept := c.candidates[:0]
		for _, candidate := range c.candidates {
			t2 := candidate.Term.apply1(variable, term)
			if t2 != candidate.Term {
				changes++
				candidate.Term = t2
				if !arg2.couldUnifyWith(t2) {
					continue
				}
			} else if argChanged && !arg2.couldUnifyWith(t2) {
				continue
			}
			kept = append(kept, candidate)
		}
		c.candidates = kept
		if changes > 0 {
			switch len(c.candidates) {
			case 0:
				return errors.New("no valid overloads")
			case 1:
				candidate := c.candidates[0]
				if candidate.Action == nil {
					w.add2(c.arg, candidate.Term)
				} else {
					candidate.Action(c.arg, candidate.Term, w.add2)
				}
			}
		}
	}
	return nil
}
