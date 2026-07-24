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

// maybePointGenerator inverts an equality conjunct "x = e" or
// "e = x" into a generator producing the single value of e. The
// constrained side must be exactly the variable, and e's free
// variables become the generator's dependencies.
func maybePointGenerator(sys *types.System, pat *core.IDPat,
	conjunct core.Exp,
) *generator {
	apply, ok := conjunct.(*core.Apply)
	if !ok {
		return nil
	}
	fn, ok := apply.Fn.(*core.ID)
	if !ok || fn.Pat.Name != eqOpName {
		return nil
	}
	tuple, ok := apply.Arg.(*core.Tuple)
	if !ok || len(tuple.Args) != 2 {
		return nil
	}
	var point core.Exp
	if id, ok := tuple.Args[0].(*core.ID); ok && id.Pat == pat {
		point = tuple.Args[1]
	} else if id, ok := tuple.Args[1].(*core.ID); ok &&
		id.Pat == pat {
		point = tuple.Args[0]
	} else {
		return nil
	}
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
