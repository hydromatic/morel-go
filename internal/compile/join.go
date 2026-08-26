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
	"strconv"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/internal/unify"
)

// Outer joins. A "left join" makes the newly scanned variable
// option-typed, a "right join" the variables already in scope,
// and a "full join" both; the "on" condition sees the unwrapped
// types. A right or full join's source must be independent of the
// query's input, because it generates rows when no input row
// matches.

// optionalizesLeft reports whether a join wraps the variables
// already in scope in option (right and full joins).
func optionalizesLeft(join ast.Op) bool {
	return join == ast.RightJoinOp || join == ast.FullJoinOp
}

// optionalizesRight reports whether a join wraps the newly
// scanned variables in option (left and full joins).
func optionalizesRight(join ast.Op) bool {
	return join == ast.LeftJoinOp || join == ast.FullJoinOp
}

// optionTerm wraps a term in option.
func optionTerm(t unify.Term) unify.Term {
	return unify.Apply(typeOption, t)
}

// checkJoinIndependent reports a reference to an input variable
// (or to "current" or "ordinal") in a right or full join's
// source, whose span is the offending reference. Names rebound by
// a let, fn, case, or nested scan are fine, and "current" and
// "ordinal" inside a nested query refer to that query.
func checkJoinIndependent(source ast.Expr,
	inputNames map[string]bool,
) error {
	w := &independenceWalker{
		inputNames: inputNames,
		shadowed:   map[string]int{},
	}
	w.exp(source, 0)
	return w.err
}

// independenceWalker scans a join source for input references.
type independenceWalker struct {
	inputNames map[string]bool
	shadowed   map[string]int
	err        error
}

// exp walks an expression at the given nested-query depth.
func (w *independenceWalker) exp(e ast.Expr, depth int) {
	if w.err != nil || e == nil {
		return
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *ast.AnnotatedExp:
		w.exp(e.Exp, depth)
	case *ast.Apply:
		w.exp(e.Fn, depth)
		w.exp(e.Arg, depth)
	case *ast.Case:
		w.exp(e.Exp, depth)
		for _, m := range e.Matches {
			w.match(m, depth)
		}
	case *ast.Fn:
		for _, m := range e.Matches {
			w.match(m, depth)
		}
	case *ast.From:
		w.from(e, depth+1)
	case *ast.ID:
		w.id(e, depth)
	case *ast.If:
		w.exp(e.Cond, depth)
		w.exp(e.IfTrue, depth)
		w.exp(e.IfFalse, depth)
	case *ast.InfixCall:
		w.exp(e.A0, depth)
		w.exp(e.A1, depth)
	case *ast.Let:
		var names []string
		for _, d := range e.Decls {
			if vd, ok := d.(*ast.ValDecl); ok {
				for _, b := range vd.Binds {
					w.exp(b.Exp, depth)
					names = append(names, patNames(b.Pat)...)
				}
			}
		}
		w.shadow(names, func() { w.exp(e.Exp, depth) })
	case *ast.ListExp:
		for _, a := range e.Args {
			w.exp(a, depth)
		}
	case *ast.PrefixCall:
		w.exp(e.A, depth)
	case *ast.Raise:
		w.exp(e.E, depth)
	case *ast.Record:
		if e.Base != nil {
			w.exp(e.Base, depth)
		}
		for _, f := range e.Fields {
			w.exp(f.Exp, depth)
		}
		for _, m := range e.Modifiers {
			m.ForEachExp(func(x ast.Expr) { w.exp(x, depth) })
		}
	case *ast.Tuple:
		for _, a := range e.Args {
			w.exp(a, depth)
		}
	}
}

// id checks one identifier reference.
func (w *independenceWalker) id(e *ast.ID, depth int) {
	name := e.Name
	if name == "current" || name == "ordinal" {
		if depth == 0 {
			w.fail(e, name)
		}
		return
	}
	if w.inputNames[name] && w.shadowed[name] == 0 {
		w.fail(e, name)
	}
}

// fail records the first offending reference.
func (w *independenceWalker) fail(e *ast.ID, name string) {
	if w.err == nil {
		w.err = &Error{
			Span: e.Span(),
			Msg: "join source must not reference '" + name +
				"' (right and full joins must be independent)",
		}
	}
}

// match walks one fn or case arm, with its pattern's names
// shadowed in the body.
func (w *independenceWalker) match(m *ast.Match, depth int) {
	w.shadow(patNames(m.Pat), func() { w.exp(m.Exp, depth) })
}

// from walks a nested query: each scan's source, then its steps,
// with scan patterns shadowing as they bind.
func (w *independenceWalker) from(e *ast.From, depth int) {
	var names []string
	unshadow := func() {
		for _, n := range names {
			w.shadowed[n]--
		}
	}
	defer unshadow()
	for _, step := range e.Steps {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch s := step.(type) {
		case *ast.Scan:
			if s.Exp != nil {
				w.exp(s.Exp, depth)
			}
			for _, n := range patNames(s.Pat) {
				w.shadowed[n]++
				names = append(names, n)
			}
			w.exp(s.On, depth)
		default:
			if es, ok := step.(interface{ StepExp() ast.Expr }); ok {
				w.exp(es.StepExp(), depth)
			}
		}
	}
}

// shadow runs body with the names shadowed.
func (w *independenceWalker) shadow(names []string, body func()) {
	for _, n := range names {
		w.shadowed[n]++
	}
	body()
	for _, n := range names {
		w.shadowed[n]--
	}
}

// patNames collects the names a pattern binds.
func patNames(p ast.Pat) []string {
	var names []string
	var walk func(p ast.Pat)
	walk = func(p ast.Pat) {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch p := p.(type) {
		case *ast.AnnotatedPat:
			walk(p.Pat)
		case *ast.AsPat:
			names = append(names, p.Name)
			walk(p.Pat)
		case *ast.ConPat:
			walk(p.Arg)
		case *ast.ConsPat:
			walk(p.A0)
			walk(p.A1)
		case *ast.IDPat:
			names = append(names, p.Name)
		case *ast.ListPat:
			for _, a := range p.Args {
				walk(a)
			}
		case *ast.RecordPat:
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *ast.TuplePat:
			for _, a := range p.Args {
				walk(a)
			}
		}
	}
	walk(p)
	return names
}

// safeFieldTerm resolves "e?.f" once the receiver's term is
// known: peel the functor layers (option, vector, and the
// collection term behind list and bag), project the field of the
// innermost record, and re-wrap it in the same layers. Nil when
// the term is not yet resolvable that way.
func safeFieldTerm(t unify.Term, field string,
	s *unify.Substitution,
) unify.Term {
	seq, ok := t.(*unify.Sequence)
	if !ok {
		return nil
	}
	switch {
	case (seq.Op == typeOption || seq.Op == typeVector) &&
		len(seq.Terms) == 1:
		inner := safeInner(seq.Terms[0], field, s)
		if inner == nil {
			return nil
		}
		return unify.Apply(seq.Op, inner)
	case seq.Op == collectionTyCon && len(seq.Terms) == 2:
		inner := safeInner(seq.Terms[0], field, s)
		if inner == nil {
			return nil
		}
		return unify.Apply(seq.Op, inner, seq.Terms[1])
	default:
		return nil
	}
}

// safeInner resolves the element side of one functor layer:
// another layer, or the record whose field is projected.
func safeInner(t unify.Term, field string,
	s *unify.Substitution,
) unify.Term {
	t = s.Resolve(t)
	if inner := safeFieldTerm(t, field, s); inner != nil {
		return inner
	}
	return lookupField(t, field, s)
}

// toSafeNav lowers "arg?.field" to functor maps: one map per layer
// (Option.map, List.map, Bag.map, Vector.map), the innermost
// function being the field selector, so NONE stays NONE and SOME r
// becomes SOME (#field r).
func (r *resolver) toSafeNav(env *coreEnv, apply *ast.Apply,
	sel *ast.RecordSelector, _ types.Type,
) (core.Exp, error) {
	arg, err := r.toExp(env, apply.Arg)
	if err != nil {
		return nil, err
	}
	return r.safeNavExp(arg, sel, arg.Type())
}

// safeNavExp builds the map chain projecting field through one
// functor layer of argT, recursing while the element itself is
// layered.
func (r *resolver) safeNavExp(arg core.Exp,
	sel *ast.RecordSelector, argT types.Type,
) (core.Exp, error) {
	sys := r.typeMap.sys
	mapName, elem, ok := peelOneFunctor(argT)
	if !ok {
		return nil, &Error{
			Span: sel.Span(),
			Msg: "'?.' applied to non-functor type " +
				argT.String() + " (expected option or list)",
		}
	}
	var fnExp core.Exp
	var resElem types.Type
	if _, _, layered := peelOneFunctor(elem); layered {
		v := &core.IDPat{T: elem, Name: "v"}
		body, err := r.safeNavExp(&core.ID{Pat: v}, sel, elem)
		if err != nil {
			return nil, err
		}
		resElem = body.Type()
		fnT, _ := sys.Fn(elem, resElem).(*types.Fn)
		fnExp = &core.Fn{T: fnT, IDPat: v, Exp: body}
	} else {
		index := fieldIndex(elem, sel.Name)
		if index < 0 {
			return nil, &Error{
				Span: sel.Span(),
				Msg: "no field '" + sel.Name + "' in type '" +
					elem.String() + "'",
			}
		}
		resElem = fieldType(elem, index)
		fnExp = &core.Selector{
			T:     sys.Fn(elem, resElem),
			Name:  sel.Name,
			Index: index,
		}
	}
	resT := rewrapFunctor(sys, argT, resElem)
	mapT := sys.Fn(sys.Fn(elem, resElem), sys.Fn(argT, resT))
	return &core.Apply{
		T: resT,
		Fn: &core.Apply{
			T: sys.Fn(argT, resT),
			Fn: &core.ID{Pat: &core.IDPat{
				T:    mapT,
				Name: mapName,
			}},
			Arg: fnExp,
		},
		Arg:  arg,
		Span: sel.Span(),
	}, nil
}

// fieldType is the type of a record or tuple field by canonical
// index.
func fieldType(t types.Type, index int) types.Type {
	switch tt := t.(type) {
	case *types.Record:
		return tt.Fields[index].Type
	case *types.Tuple:
		return tt.Args[index]
	}
	return t
}

// rewrapFunctor rebuilds argT's outer layer around a new element.
func rewrapFunctor(sys *types.System, argT,
	elem types.Type,
) types.Type {
	// lint: sort until '^\t}' where '^\tcase '
	switch tt := argT.(type) {
	case *types.Collection:
		return sys.Collection(elem)
	case *types.List:
		return sys.List(elem)
	case *types.Named:
		return sys.Named(tt.Name, elem)
	}
	return argT
}

// peelOneFunctor strips one safe-nav functor layer, returning the
// map builtin projecting through it and the element type.
func peelOneFunctor(t types.Type) (string, types.Type, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch tt := t.(type) {
	case *types.Collection:
		return "Bag.map", tt.Elem, true
	case *types.List:
		return "List.map", tt.Elem, true
	case *types.Named:
		if len(tt.Args) == 1 {
			// lint: sort until '^\t\t\t}' where '^\t\t\tcase '
			switch tt.Name {
			case bagTyCon:
				return "Bag.map", tt.Args[0], true
			case typeOption:
				return "Option.map", tt.Args[0], true
			case typeVector:
				return "Vector.map", tt.Args[0], true
			}
		}
	}
	return "", nil, false
}

// fieldIndex is the position of a field in a record or tuple
// type, in canonical order, or -1.
func fieldIndex(t types.Type, name string) int {
	// lint: sort until '^\t}' where '^\tcase '
	switch tt := t.(type) {
	case *types.Record:
		for i, f := range tt.Fields {
			if f.Label == name {
				return i
			}
		}
	case *types.Tuple:
		i, err := strconv.Atoi(name)
		if err == nil && i >= 1 && i <= len(tt.Args) {
			return i - 1
		}
	}
	return -1
}
