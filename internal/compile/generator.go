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
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// cardinality is how many values a generator produces per binding
// of its free variables.
type cardinality int

const (
	// single: exactly one value, e.g. "x = 5".
	single cardinality = iota

	// finite: finitely many values, e.g. a materialized extent.
	finite

	// infinite: unboundedly many; the generator does not ground
	// its pattern.
	infinite
)

// A generator is an efficient way to produce the values of a
// query variable that a predicate constrains: an inverse of a
// class of predicate. The grounding pass replaces each scan over
// an infinite extent with the best generator deduced for its
// pattern.
type generator struct {
	// exp is the collection expression producing the values.
	exp core.Exp

	// pat is the pattern the generator grounds.
	pat core.Pat

	// freePats are the variables exp reads: the generator can be
	// scheduled only after scans binding them.
	freePats []*core.IDPat

	card cardinality

	// unique means exp produces no duplicates; a non-unique
	// generator's scan must be wrapped in "distinct".
	unique bool

	// sealed means every value exp produces satisfies all the
	// conjuncts in provenance, so those conjuncts may be deleted
	// from the query's filters. An unsealed generator's
	// provenance is advisory only.
	sealed bool

	// provenance is the set of original filter conjuncts the
	// generator subsumes, identified by pointer.
	provenance map[core.Exp]bool
}

// generatorCache accumulates the generators deduced for each
// pattern. Entries are only added, never removed, and a later
// entry for a pattern is always at least as good as an earlier
// one, so the best generator is the most recent.
type generatorCache struct {
	m map[*core.IDPat][]*generator
}

// add indexes a generator under every variable of its pattern.
func (c *generatorCache) add(g *generator) {
	for _, id := range core.PatIDs(g.pat) {
		c.m[id] = append(c.m[id], g)
	}
}

// best returns the most recently added generator for a pattern,
// or nil.
func (c *generatorCache) best(pat *core.IDPat) *generator {
	gens := c.m[pat]
	if len(gens) == 0 {
		return nil
	}
	return gens[len(gens)-1]
}

// extentGenerator wraps a scan over an extent: the values of the
// pattern's type. It is the first generator registered for every
// unbounded pattern; finite by itself when the type is finite.
func extentGenerator(scan *core.Scan) *generator {
	card := finite
	if isInfiniteExtent(scan.Exp) {
		card = infinite
	}
	return &generator{
		exp:    scan.Exp,
		pat:    scan.Pat,
		card:   card,
		unique: true,
		sealed: true,
	}
}

// maybeGenerator deduces a generator for a variable from the
// constraints accumulated so far, trying predicate classes in
// priority order: the first membership conjunct mentioning the
// variable, else the first equality on it.
func maybeGenerator(sys *types.System, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	var elemMatch, pointMatch core.Exp
	for _, c := range constraints {
		if elemMatch == nil && matchesElem(c, pat) {
			elemMatch = c
		}
		if pointMatch == nil && pointValue(c, pat) != nil {
			pointMatch = c
		}
	}
	if elemMatch != nil {
		if g := collectionGenerator(elemMatch); g != nil {
			return g
		}
	}
	if pointMatch != nil {
		return pointGenerator(sys, pat, pointMatch)
	}
	return nil
}

// binaryCall decodes an application of a named top-level operator
// to a pair, returning the two operands.
func binaryCall(e core.Exp, name string) (core.Exp, core.Exp) {
	apply, ok := e.(*core.Apply)
	if !ok {
		return nil, nil
	}
	fn, ok := apply.Fn.(*core.ID)
	if !ok || fn.Pat.Name != name {
		return nil, nil
	}
	tuple, ok := apply.Arg.(*core.Tuple)
	if !ok || len(tuple.Args) != 2 {
		return nil, nil
	}
	return tuple.Args[0], tuple.Args[1]
}

// pointValue returns the expression an equality conjunct pins the
// variable to, or nil. The constrained side must be exactly the
// variable.
func pointValue(conjunct core.Exp, pat *core.IDPat) core.Exp {
	a, b := binaryCall(conjunct, eqOpName)
	if id, ok := a.(*core.ID); ok && id.Pat == pat {
		return b
	}
	if id, ok := b.(*core.ID); ok && id.Pat == pat {
		return a
	}
	return nil
}

// pointGenerator inverts an equality conjunct "x = e" or "e = x"
// into a generator producing the single value of e; e's free
// variables become the generator's dependencies.
func pointGenerator(sys *types.System, pat *core.IDPat,
	conjunct core.Exp,
) *generator {
	point := pointValue(conjunct, pat)
	return &generator{
		exp: &core.List{
			T:    sys.Named("bag", pat.T),
			Args: []core.Exp{point},
		},
		pat:        pat,
		freePats:   freePatsOf(point),
		card:       single,
		unique:     true,
		sealed:     true,
		provenance: map[core.Exp]bool{conjunct: true},
	}
}

// elemName is the top-level binding of the membership operator.
const elemName = opElem

// matchesElem reports whether the conjunct is a membership test
// whose element side mentions the variable.
func matchesElem(conjunct core.Exp, pat *core.IDPat) bool {
	lhs, _ := binaryCall(conjunct, elemName)
	return lhs != nil && containsRef(lhs, pat)
}

// containsRef reports whether the expression mentions the
// variable: it is the variable itself, or a tuple with the
// variable somewhere within.
func containsRef(e core.Exp, pat *core.IDPat) bool {
	switch e := e.(type) {
	case *core.ID:
		return e.Pat == pat
	case *core.Tuple:
		for _, arg := range e.Args {
			if containsRef(arg, pat) {
				return true
			}
		}
	}
	return false
}

// collectionGenerator inverts a membership conjunct "x elem coll"
// into a generator scanning coll. The element side may be a tuple
// of variables and literals: the scan's pattern then grounds
// every variable at once, its literals filtering the rows. The
// collection's free variables become dependencies. The collection
// is scanned as-is — duplicates are deliberately kept.
func collectionGenerator(conjunct core.Exp) *generator {
	lhs, coll := binaryCall(conjunct, elemName)
	pat, ok := patForExp(lhs)
	if !ok {
		return nil
	}
	return &generator{
		exp:        coll,
		pat:        pat,
		freePats:   freePatsOf(coll),
		card:       finite,
		unique:     true,
		sealed:     true,
		provenance: map[core.Exp]bool{conjunct: true},
	}
}

// patForExp converts a membership conjunct's element side to the
// pattern its scan binds: a variable to its pattern, a tuple to a
// tuple of converted components, and a literal to a literal
// pattern (a filter).
func patForExp(e core.Exp) (core.Pat, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.ID:
		return e.Pat, true
	case *core.Literal:
		return &core.LiteralPat{
			T: e.T, Kind: e.Kind, Value: e.Value,
		}, true
	case *core.Tuple:
		args := make([]core.Pat, len(e.Args))
		for i, arg := range e.Args {
			p, ok := patForExp(arg)
			if !ok {
				return nil, false
			}
			args[i] = p
		}
		return &core.TuplePat{T: e.T, Args: args}, true
	default:
		return nil, false
	}
}

// freePatsOf returns the variables used but not declared in an
// expression.
func freePatsOf(e core.Exp) []*core.IDPat {
	a := &analyzer{uses: map[*core.IDPat]*useInfo{}}
	a.exp(e)
	var pats []*core.IDPat
	for pat, info := range a.uses {
		if info.count > 0 && !info.declared {
			pats = append(pats, pat)
		}
	}
	return pats
}
