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

package compile

import (
	"fmt"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// expander grounds one query: it accumulates the query's filter
// conjuncts, deduces a generator for each unbounded variable, and
// rebuilds the query with each infinite extent scan replaced by
// its variable's generator. A used variable with no finite
// generator is an error: the query cannot be evaluated.
type expander struct {
	sys         *types.System
	cache       *generatorCache
	constraints []core.Exp
	// scanPats are the variables bound by any of the query's own
	// scans; a generator dependency outside them is bound by an
	// outer scope and needs no scheduling.
	scanPats map[*core.IDPat]bool
	// extentPats are the variables of extent scans, keyed to the
	// span their errors report.
	extentPats map[*core.IDPat]token.Span
	// used are the variables any step references; an unused
	// extent variable's scan is simply dropped.
	used map[*core.IDPat]bool
}

// expandFrom grounds a query, returning it unchanged (the same
// pointer) if nothing needed rewriting.
func expandFrom(sys *types.System, from *core.From,
) (*core.From, error) {
	x := &expander{
		sys:        sys,
		cache:      &generatorCache{m: map[*core.IDPat][]*generator{}},
		scanPats:   map[*core.IDPat]bool{},
		extentPats: map[*core.IDPat]token.Span{},
		used:       map[*core.IDPat]bool{},
	}
	x.deduce(from)
	x.markUsed(from)
	// A used variable whose best generator is still infinite is
	// not grounded: no predicate could be inverted for it.
	for _, step := range from.Steps {
		scan, ok := step.(*core.Scan)
		if !ok || extentOf(scan.Exp) == nil {
			continue
		}
		for _, pat := range core.PatIDs(scan.Pat) {
			if !x.used[pat] {
				continue
			}
			g := x.cache.best(pat)
			if g == nil || g.card == infinite {
				return nil, x.notGrounded(pat)
			}
		}
	}
	return x.rebuild(from)
}

// notGrounded is the error for a variable no generator grounds.
func (x *expander) notGrounded(pat *core.IDPat) error {
	return &Error{
		Span: x.extentPats[pat],
		Msg: fmt.Sprintf("pattern '%s' is not grounded",
			pat.Name),
	}
}

// deduce walks the steps accumulating constraints and deducing
// generators: an extent scan registers its extent, and each
// filter conjunct in order may improve an unbounded variable's
// generator, seeing every earlier conjunct as context.
func (x *expander) deduce(from *core.From) {
	for _, step := range from.Steps {
		switch s := step.(type) {
		case *core.Scan:
			for _, pat := range core.PatIDs(s.Pat) {
				x.scanPats[pat] = true
			}
			apply, isApply := s.Exp.(*core.Apply)
			if extentOf(s.Exp) != nil && isApply {
				for _, pat := range core.PatIDs(s.Pat) {
					x.extentPats[pat] = apply.Span
				}
				x.cache.add(extentGenerator(s))
			}
		case *core.Where:
			var conjuncts []core.Exp
			decomposeConjuncts(s.Exp, &conjuncts)
			for _, conjunct := range conjuncts {
				if isBoolLiteral(conjunct, true) {
					continue
				}
				x.constraints = append(x.constraints, conjunct)
				x.improveGenerators()
			}
		}
	}
}

// improveGenerators retries the variables whose best generator is
// still infinite against the constraints accumulated so far.
func (x *expander) improveGenerators() {
	for pat := range x.extentPats {
		g := x.cache.best(pat)
		if g == nil || g.card != infinite {
			continue
		}
		for _, conjunct := range x.constraints {
			if g2 := maybePointGenerator(x.sys, pat,
				conjunct); g2 != nil {
				x.cache.add(g2)
				break
			}
		}
	}
}

// markUsed records every variable a step references. A query that
// collects its row implicitly (no trailing yield, into, or group)
// also uses every scan variable, since the row is built from
// them; "exists" and "forall" collect nothing.
func (x *expander) markUsed(from *core.From) {
	for _, step := range from.Steps {
		for _, pat := range stepRefs(step) {
			if x.scanPats[pat] {
				x.used[pat] = true
			}
		}
	}
	if from.Kind == ast.ExistsOp || from.Kind == ast.ForallOp {
		return
	}
	if !implicitCollect(from.Steps) {
		return
	}
	for pat := range x.scanPats {
		x.used[pat] = true
	}
}

// implicitCollect reports whether the query's result rows are
// built from its scan variables rather than a final yield, into,
// group, or through.
func implicitCollect(steps []core.FromStep) bool {
	for _, step := range steps {
		switch s := step.(type) {
		case *core.Group, *core.Into, *core.Through:
			return false
		case *core.Yield:
			if s.Fields == nil {
				return false
			}
		}
	}
	return true
}

// stepRefs returns the variables a step's expressions use.
func stepRefs(step core.FromStep) []*core.IDPat {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	a.step(step)
	var pats []*core.IDPat
	for pat, info := range a.uses {
		if info.count > 0 && !info.declared {
			pats = append(pats, pat)
		}
	}
	return pats
}

// patState tracks scheduling of a variable's generator scan.
type patState int

const (
	unscheduled patState = iota
	inProgress
	done
)

// rebuild replaces each extent scan with its variables'
// generator scans, ordered so that a generator follows the scans
// binding its dependencies, and drops the filter conjuncts that
// the sealed generators subsume.
func (x *expander) rebuild(from *core.From) (*core.From, error) {
	r := &rebuilder{x: x, state: map[*core.IDPat]patState{}}
	for _, step := range from.Steps {
		// A generator whose dependencies a step uses must be
		// scheduled before the step.
		for _, pat := range stepRefs(step) {
			r.addGeneratorScan(pat)
		}
		switch s := step.(type) {
		case *core.Scan:
			if extentOf(s.Exp) != nil {
				for _, pat := range core.PatIDs(s.Pat) {
					if x.used[pat] {
						r.addGeneratorScan(pat)
					} else {
						r.changed = true
					}
				}
				continue
			}
			for _, pat := range core.PatIDs(s.Pat) {
				r.state[pat] = done
			}
			r.steps = append(r.steps, s)
		case *core.Where:
			r.filterStep(s)
		default:
			r.steps = append(r.steps, step)
		}
	}
	err := x.checkAllGrounded(from, r.state)
	if err != nil {
		return nil, err
	}
	if !r.changed {
		return from, nil
	}
	return &core.From{T: from.T, Steps: r.steps, Kind: from.Kind},
		nil
}

// checkAllGrounded reports an error for a used variable never
// scheduled: it is caught in a dependency cycle — its generator
// waits on a variable whose generator waits on it.
func (x *expander) checkAllGrounded(from *core.From,
	state map[*core.IDPat]patState,
) error {
	for _, step := range from.Steps {
		scan, ok := step.(*core.Scan)
		if !ok || extentOf(scan.Exp) == nil {
			continue
		}
		for _, pat := range core.PatIDs(scan.Pat) {
			if x.used[pat] && state[pat] != done {
				return x.notGrounded(pat)
			}
		}
	}
	return nil
}

// rebuilder accumulates the rewritten steps of one query.
type rebuilder struct {
	x       *expander
	steps   []core.FromStep
	state   map[*core.IDPat]patState
	changed bool
}

// addGeneratorScan schedules the generator scan that binds a
// variable, after the scans binding the generator's own
// dependencies. A dependency that is not yet bound defers the
// scan: a later step, or the grounding check, will retry it.
func (r *rebuilder) addGeneratorScan(pat *core.IDPat) {
	if r.state[pat] != unscheduled || !r.isExtentPat(pat) {
		return
	}
	g := r.x.cache.best(pat)
	if g == nil {
		return
	}
	r.state[pat] = inProgress
	for _, dep := range g.freePats {
		r.addGeneratorScan(dep)
	}
	for _, dep := range g.freePats {
		// A dependency bound by this query must already be
		// scheduled; one from an outer scope is always bound.
		if r.x.scanPats[dep] && r.state[dep] != done {
			delete(r.state, pat)
			return
		}
	}
	r.steps = append(r.steps, &core.Scan{Pat: g.pat, Exp: g.exp})
	if !r.x.isExtentScanExp(g.exp) {
		// Re-emitting the variable's own extent scan is not a
		// change; substituting anything else is.
		r.changed = true
	}
	for _, id := range core.PatIDs(g.pat) {
		r.state[id] = done
	}
}

// isExtentPat reports whether the variable came from an extent
// scan.
func (r *rebuilder) isExtentPat(pat *core.IDPat) bool {
	_, ok := r.x.extentPats[pat]
	return ok
}

// isExtentScanExp reports whether the expression is one of the
// query's original extent-scan sources.
func (x *expander) isExtentScanExp(e core.Exp) bool {
	return extentOf(e) != nil
}

// filterStep re-emits a filter without the conjuncts that sealed
// generators subsume; a filter with nothing left is dropped.
func (r *rebuilder) filterStep(s *core.Where) {
	subsumed := map[core.Exp]bool{}
	for _, gens := range r.x.cache.m {
		for _, g := range gens {
			if !g.sealed {
				continue
			}
			for conjunct := range g.provenance {
				subsumed[conjunct] = true
			}
		}
	}
	var conjuncts []core.Exp
	decomposeConjuncts(s.Exp, &conjuncts)
	var remaining []core.Exp
	for _, conjunct := range conjuncts {
		if isBoolLiteral(conjunct, true) || subsumed[conjunct] {
			r.changed = true
			continue
		}
		remaining = append(remaining, conjunct)
	}
	if len(remaining) == 0 {
		r.changed = true
		return
	}
	if len(remaining) == len(conjuncts) {
		r.steps = append(r.steps, s)
		return
	}
	r.steps = append(r.steps,
		&core.Where{Exp: composeConjuncts(r.x.sys, remaining)})
}
