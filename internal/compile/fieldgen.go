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
	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// maybeTupleCase looks through a single-arm case matching a tuple
// pattern against the variable — the residue of an inlined
// tuple-argument predicate: the arm's variables become selections
// of the case's variable, and the body's conjuncts join the
// search in the conjunct's place.
func maybeTupleCase(ctx *genContext, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	for i, c := range constraints {
		cs, ok := c.(*core.Case)
		if !ok || cs.T != ctx.sys.Bool || len(cs.Matches) != 1 {
			continue
		}
		scrut, ok := cs.Exp.(*core.ID)
		if !ok {
			continue
		}
		tp, ok := cs.Matches[0].Pat.(*core.TuplePat)
		if !ok {
			continue
		}
		binds := map[*core.IDPat]core.Exp{}
		ok = true
		for j, sub := range tp.Args {
			id, isID := sub.(*core.IDPat)
			if !isID {
				ok = false
				break
			}
			binds[id] = fieldOf(ctx.sys, scrut.Pat, j)
		}
		if !ok {
			continue
		}
		cs2 := make([]core.Exp, 0, len(constraints))
		cs2 = append(cs2, constraints[:i]...)
		decomposeConjuncts(
			substituteExp(cs.Matches[0].Exp, binds), &cs2)
		cs2 = append(cs2, constraints[i+1:]...)
		if g := maybeGenerator(ctx, pat, cs2); g != nil {
			return g
		}
	}
	return nil
}

// maybeFields grounds a tuple- or record-typed variable
// field-by-field: the constraints' selections of the variable
// become fresh field variables, each field is inverted
// independently, and the field generators join — a variable a
// later generator shares with an earlier one is renamed and
// equated — yielding the reassembled row, deduplicated.
func maybeFields(ctx *genContext, pat *core.IDPat,
	constraints []core.Exp,
) *generator {
	n, fieldTs := rowArity(pat.T)
	if n <= 1 {
		return nil
	}
	fields := make([]*core.IDPat, n)
	for i := range n {
		fields[i] = &core.IDPat{T: fieldTs[i], Name: "f"}
	}
	cs2 := make([]core.Exp, len(constraints))
	replaced := false
	for i, c := range constraints {
		cs2[i] = replaceSelections(c, pat, fields, &replaced)
	}
	if !replaced {
		return nil
	}
	ctx2 := &genContext{
		sys:     ctx.sys,
		extents: map[*core.IDPat]bool{},
		recFns:  ctx.recFns,
	}
	for p := range ctx.extents {
		ctx2.extents[p] = true
	}
	for _, f := range fields {
		ctx2.extents[f] = true
	}
	gens := make([]*generator, n)
	for i, f := range fields {
		gens[i] = maybeGenerator(ctx2, f, cs2)
		if gens[i] == nil {
			return nil
		}
	}
	return combineFieldGens(ctx.sys, pat, fields, gens)
}

// condsOn rewrites a generator's immediate conditions onto its
// scan's fresh pattern copies.
func condsOn(g *generator,
	fresh map[*core.IDPat]*core.IDPat,
) []core.Exp {
	if len(g.conds) == 0 {
		return nil
	}
	out := make([]core.Exp, len(g.conds))
	for i, c := range g.conds {
		out[i] = substituteFresh(c, fresh)
	}
	return out
}

// rowArity is the field count and types of a tuple or record
// type. A type alias is read through: a record reached through
// one is still a record.
func rowArity(t types.Type) (int, []types.Type) {
	switch t := types.Unalias(t).(type) {
	case *types.Record:
		ts := make([]types.Type, len(t.Fields))
		for i, f := range t.Fields {
			ts[i] = f.Type
		}
		return len(ts), ts
	case *types.Tuple:
		return len(t.Args), t.Args
	default:
		return 0, nil
	}
}

// replaceSelections rewrites selections of the variable's fields
// to the fresh field variables.
func replaceSelections(e core.Exp, pat *core.IDPat,
	fields []*core.IDPat, replaced *bool,
) core.Exp {
	r := &rewriter{}
	r.exp = func(x core.Exp) (core.Exp, bool) {
		apply, ok := x.(*core.Apply)
		if !ok {
			return nil, false
		}
		sel, ok := apply.Fn.(*core.Selector)
		if !ok {
			return nil, false
		}
		id, ok := apply.Arg.(*core.ID)
		if !ok || id.Pat != pat || sel.Index >= len(fields) {
			return nil, false
		}
		*replaced = true
		return &core.ID{Pat: fields[sel.Index]}, true
	}
	return r.rewriteExp(e)
}

// combineFieldGens joins the field generators into one query
// yielding the whole row. A variable two generators share is a
// join key: the later scan renames it and equates the copies.
func combineFieldGens(sys *types.System, pat *core.IDPat,
	fields []*core.IDPat, gens []*generator,
) *generator {
	mapped := map[*core.IDPat]*core.IDPat{}
	var steps []core.FromStep
	seenConj := map[core.Exp]bool{}
	for _, g := range gens {
		// Two fields inverted from the same conjunct share one
		// generator; scan it once.
		dup := false
		for c := range g.provenance {
			if seenConj[c] {
				dup = true
			}
			seenConj[c] = true
		}
		if dup {
			continue
		}
		fresh := map[*core.IDPat]*core.IDPat{}
		scanPat := clonePat(g.pat, fresh)
		var joins []core.Exp
		for orig, copy := range fresh {
			if prev, ok := mapped[orig]; ok {
				// A variable an earlier scan bound: this copy
				// renames and equates to it.
				copy.Name += "$"
				joins = append(joins, eqExp(sys,
					&core.ID{Pat: copy}, &core.ID{Pat: prev}))
			} else {
				mapped[orig] = copy
			}
		}
		steps = append(steps, &core.Scan{Pat: scanPat, Exp: g.exp})
		joins = append(joins, condsOn(g, fresh)...)
		if len(joins) > 0 {
			steps = append(steps,
				&core.Where{Exp: composeConjuncts(sys, joins)})
		}
	}
	args := make([]core.Exp, len(fields))
	for i, f := range fields {
		m := mapped[f]
		if m == nil {
			return nil
		}
		args[i] = &core.ID{Pat: m}
	}
	steps = append(steps, &core.Yield{
		Exp: &core.Tuple{T: pat.T, Args: args},
	})
	built := &core.From{
		T:     sys.Named("bag", pat.T),
		Steps: steps,
		Kind:  ast.FromOp,
	}
	return &generator{
		exp:      distinctScan(sys, pat, built),
		pat:      pat,
		freePats: freePatsOf(built),
		card:     finite,
		unique:   true,
	}
}
