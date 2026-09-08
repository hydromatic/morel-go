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
	"slices"
	"strconv"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// The enforcer inserts the checks that make a checked type mean
// something.
//
// The invariant it keeps is the whole soundness argument: a
// condition is claimed only where the type says so, and a check
// is inserted wherever a value flows into a claim. Everywhere
// else the name has reduced to the base type, so nothing is
// claimed and nothing can be breached.
//
// A claim is the type the user *wrote*, never the type inference
// deduced. Inference gives the meet, and for a checked type the
// meet is the type it abbreviates, so a deduced type has no
// condition left to check: "fun decr (n: nat) = n - 1" has type
// "int -> int", and the parameter is still checked because the
// check reads the annotation.

// compileChecks compiles the conditions of a checked type and
// stores them against the syntax they were written as, so that a
// statement that claims the type can enforce it. Compiling them
// once, where they are written, is what lets a type declared in
// one statement be claimed in another.
func (r *resolver) compileChecks(env *coreEnv,
	checks []*ast.Fn,
) error {
	for _, c := range checks {
		if _, done := r.typeMap.sys.CheckCode(c); done {
			continue
		}
		exp, err := r.toExp(env, c)
		if err != nil {
			return err
		}
		r.typeMap.sys.SetCheckCode(c, makeTotal(r.typeMap.sys, exp))
	}
	return nil
}

// makeTotal appends "| _ => false" to a condition whose match
// does not cover every value. A value that matches no branch
// fails the check; without this the condition would raise Match.
func makeTotal(sys *types.System, exp core.Exp) core.Exp {
	fn, ok := exp.(*core.Fn)
	if !ok {
		return exp
	}
	kase, ok := fn.Exp.(*core.Case)
	if !ok {
		// A single irrefutable branch, "i => i >= 0"; it matches
		// every value already.
		return exp
	}
	if CaseExhaustive(sys, kase) {
		// Appending to a match that already covers every value
		// would make the appended branch redundant, which is an
		// error; and there is nothing for it to catch.
		return exp
	}
	matches := make([]core.Match, 0, len(kase.Matches)+1)
	matches = append(matches, kase.Matches...)
	matches = append(matches, core.Match{
		Pat: &core.WildcardPat{T: kase.Exp.Type()},
		Exp: boolLiteral(sys, false),
	})
	return &core.Fn{
		T:     fn.T,
		IDPat: fn.IDPat,
		Exp:   &core.Case{T: kase.T, Exp: kase.Exp, Matches: matches},
	}
}

// checkedAt checks a value against the type its pattern claims,
// if the pattern claims one.
//
// The claim is read from the pattern, not from the type inference
// deduced for the expression: inference gives the meet, which for
// a checked type is the type it abbreviates, so a deduced type
// has no condition left to check.
func (r *resolver) checkedAt(pat ast.Pat, exp core.Exp,
	span token.Span,
) (core.Exp, error) {
	t, err := r.claimedPatType(pat)
	if err != nil || t == nil {
		return exp, err
	}
	return r.checked(exp, t, "", span)
}

// claimedPatType returns the type a pattern claims, or nil if it
// claims nothing.
//
// A claim is an annotation the user *wrote*, not a type inference
// deduced. The two differ: inference gives the meet, which for a
// checked type is the type it abbreviates, so a deduced type has
// no condition left to check. They differ the other way too --
// "val it = ~n" is deduced "nat", because nothing met the "nat",
// but the user claimed nothing there.
func (r *resolver) claimedPatType(pat ast.Pat) (types.Type, error) {
	erased, err := r.typeMap.TypeOf(pat)
	if err != nil {
		erased = nil
	}
	t, err := r.claimedPart(pat, erased)
	if err != nil || t == nil || !r.hasCheck(t) {
		return nil, err
	}
	return t, nil
}

// claimedPart is claimedPatType for one part of a pattern: the
// type that part claims, or nil if it claims nothing.
//
// A tuple or record pattern claims per component, so an
// annotation on a component claims that component and a component
// the pattern does not annotate claims nothing. erased is the type
// the value has, used for the parts that claim nothing.
//
//nolint:nilnil // a nil type means nothing is claimed
func (r *resolver) claimedPart(pat ast.Pat,
	erased types.Type,
) (types.Type, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.AnnotatedPat:
		t := r.typeMap.AliasedTypeOf(pat)
		if t == nil {
			return nil, nil
		}
		err := rejectCheckedFunction(t, t, p.Type.Span())
		if err != nil {
			return nil, err
		}
		return t, nil
	case *ast.RecordPat:
		return r.claimedRecord(p, erased)
	case *ast.TuplePat:
		return r.claimedTuple(p, erased)
	default:
		return nil, nil
	}
}

// claimedRecord is claimedPart for a record pattern: a field the
// pattern does not annotate claims nothing.
//
//nolint:nilnil // a nil type means nothing is claimed
func (r *resolver) claimedRecord(p *ast.RecordPat,
	erased types.Type,
) (types.Type, error) {
	fields := recordLikeFields(erased)
	if len(fields) == 0 {
		return nil, nil
	}
	byLabel := map[string]ast.Pat{}
	for _, f := range p.Fields {
		byLabel[f.Label] = f.Pat
	}
	claimed := make([]types.Field, len(fields))
	claims := false
	for i, f := range fields {
		claimed[i] = f
		sub, ok := byLabel[f.Label]
		if !ok {
			continue
		}
		t, err := r.claimedPart(sub, f.Type)
		if err != nil {
			return nil, err
		}
		if t != nil {
			claimed[i].Type = t
			claims = true
		}
	}
	if !claims {
		return nil, nil
	}
	return r.typeMap.sys.Record(claimed), nil
}

// claimedTuple is claimedPart for a tuple pattern.
//
//nolint:nilnil // a nil type means nothing is claimed
func (r *resolver) claimedTuple(p *ast.TuplePat,
	erased types.Type,
) (types.Type, error) {
	tuple, isTuple := types.Unalias(erased).(*types.Tuple)
	if !isTuple || len(tuple.Args) != len(p.Args) {
		return nil, nil
	}
	claimed := make([]types.Type, len(tuple.Args))
	claims := false
	for i, sub := range p.Args {
		claimed[i] = tuple.Args[i]
		t, err := r.claimedPart(sub, tuple.Args[i])
		if err != nil {
			return nil, err
		}
		if t != nil {
			claimed[i] = t
			claims = true
		}
	}
	if !claims {
		return nil, nil
	}
	return r.typeMap.sys.Tuple(claimed...), nil
}

// rejectCheckedFunction reports a claim of a type that carries a
// condition on a function's parameter or result.
//
// A check on a value is made when the value is made, and a
// function value is not a value its type can be checked against:
// to check "nat -> nat", every argument the function is ever given
// and every result it ever returns would have to be checked, which
// means replacing it with a proxy. Rather than do that silently,
// or accept the claim and check nothing, the claim is rejected.
//
// A condition on the function type *itself* is given the function
// value, and is checked like any other; where a condition lands is
// decided by parenthesization.
func rejectCheckedFunction(t, claimed types.Type,
	span token.Span,
) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.AliasType:
		return rejectCheckedFunction(t.Base, claimed, span)
	case *types.Fn:
		if carriesCheck(t.Param) || carriesCheck(t.Result) {
			return &Error{
				Span: span,
				Msg: "cannot claim a checked function type '" +
					claimed.String() + "'",
			}
		}
		return nil
	case *types.List:
		return rejectCheckedFunction(t.Elem, claimed, span)
	case *types.Named:
		return rejectEach(t.Args, claimed, span)
	case *types.Record:
		args := make([]types.Type, len(t.Fields))
		for i, f := range t.Fields {
			args[i] = f.Type
		}
		return rejectEach(args, claimed, span)
	case *types.Tuple:
		return rejectEach(t.Args, claimed, span)
	default:
		return nil
	}
}

// rejectEach applies rejectCheckedFunction to each of a
// composite's parts.
func rejectEach(parts []types.Type, claimed types.Type,
	span token.Span,
) error {
	for _, part := range parts {
		err := rejectCheckedFunction(part, claimed, span)
		if err != nil {
			return err
		}
	}
	return nil
}

// carriesCheck reports whether a type, or any type within it,
// carries a condition. Unlike hasCheck it needs no resolver,
// because it asks about the type alone.
func carriesCheck(t types.Type) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.AliasType:
		return len(t.Checks) > 0 || carriesCheck(t.Base)
	case *types.Fn:
		return carriesCheck(t.Param) || carriesCheck(t.Result)
	case *types.List:
		return carriesCheck(t.Elem)
	case *types.Named:
		return slices.ContainsFunc(t.Args, carriesCheck)
	case *types.Record:
		return slices.ContainsFunc(t.Fields,
			func(f types.Field) bool { return carriesCheck(f.Type) })
	case *types.Tuple:
		return slices.ContainsFunc(t.Args, carriesCheck)
	default:
		return false
	}
}

// hasCheck reports whether a type, or any type within it, carries
// a condition.
func (r *resolver) hasCheck(t types.Type) bool {
	cond, err := r.deepCondition(t, nil, nil, "",
		&walkState{span: token.Span{}})
	return err == nil && cond != nil
}

// containsCheck reports whether an expression makes a check.
func containsCheck(e core.Exp) bool {
	found := false
	r := &rewriter{}
	r.exp = func(x core.Exp) (core.Exp, bool) {
		if _, isCheck := x.(*core.Check); isCheck {
			found = true
		}
		return nil, false
	}
	r.rewriteExp(e)
	return found
}

// walkState is what stays the same while a claim is walked:
// whether a failure raises, and the position to blame it at.
type walkState struct {
	span token.Span
	// walking maps each datatype being walked to the predicate
	// being built for it, so that a datatype met again is called
	// rather than expanded.
	walking map[string]*core.IDPat
	raising bool
}

// deepCondition returns a condition that holds when a value
// satisfies every condition its claimed type carries, at any
// depth; nil if the type claims nothing.
//
// Two types are walked in step. The *claimed* type keeps its
// aliases, and so knows where the conditions are; the *erased*
// type is what the expressions being built are typed with. A
// single walk would build a selector typed "nat", which a
// predicate typed "int -> bool" rejects.
//
// A nil value asks only whether anything is checked, and the
// answer is read for nullness alone.
//
// Components are checked before the whole, so the message names
// the innermost failure. A component raises for itself -- that is
// what CheckRequire is for -- so that the message says which
// component and quotes it; the outermost condition is left bare,
// for the check that wraps the whole value to report.
//
//nolint:nilnil // a nil condition means nothing is checked
func (r *resolver) deepCondition(claimed, erased types.Type,
	value core.Exp, blame string, w *walkState,
) (core.Exp, error) {
	sys := r.typeMap.sys
	// lint: sort until '^\t}' where '^\tcase '
	switch t := claimed.(type) {
	case *types.AliasType:
		cond, err := r.deepCondition(t.Base, erased, value, blame, w)
		if err != nil || len(t.Checks) == 0 {
			return cond, err
		}
		own, err := r.ownCondition(t, value, w.span)
		if err != nil {
			return nil, err
		}
		if w.raising && blame != "" && value != nil {
			own = &core.Check{
				T:        sys.Bool,
				Cond:     own,
				Value:    value,
				TypeName: checkedTypeName(t),
				Blame:    blame,
				Kind:     core.CheckRequire,
				Span:     w.span,
			}
		}
		return andAlso(sys, cond, own), nil

	case *types.List:
		return r.elementsCondition(t.Elem, erased, value, blame, w,
			"List.all")

	case *types.Named:
		if t.Name == "bag" && len(t.Args) == 1 {
			return r.elementsCondition(t.Args[0], erased, value,
				blame, w, "Bag.all")
		}
		if t.Name == "vector" && len(t.Args) == 1 {
			// A vector is walked element by element, as a list is.
			// It is not a collection -- a query cannot scan one --
			// and it is an eqtype, so datatypeCondition finds no
			// constructors to walk. Without this it falls between
			// the two and answers "nothing here is checked", which
			// every enclosing walk believes.
			return r.elementsCondition(t.Args[0], erased, value,
				blame, w, "Vector.all")
		}
		return r.datatypeCondition(t, erased, value, blame, w)

	case *types.Record:
		return r.fieldsCondition(t.Fields, erased, value, blame, w,
			"field ")

	case *types.Tuple:
		fields := make([]types.Field, len(t.Args))
		for i, arg := range t.Args {
			fields[i] = types.Field{
				Label: strconv.Itoa(i + 1), Type: arg,
			}
		}
		return r.fieldsCondition(fields, erased, value, blame, w,
			"component ")

	default:
		return nil, nil
	}
}

// datatypeCondition returns a condition that holds when every
// value a datatype's constructors carry satisfies the conditions
// the type arguments carry.
//
// Without this a condition under a type parameter is claimed and
// never checked: "val w: nat option = SOME ~1" printed
// "nat option" and held a value that is not one. Records and
// collections are already walked; a datatype is the remaining way
// to reach a type.
//
//nolint:nilnil // a nil condition means nothing is checked
func (r *resolver) datatypeCondition(t *types.Named,
	erased types.Type, value core.Exp, blame string, w *walkState,
) (core.Exp, error) {
	sys := r.typeMap.sys
	if pat, walking := w.walking[t.String()]; walking {
		// Met again, so this datatype contains itself. Call the
		// predicate being built for it rather than expanding it a
		// second time. Asked only whether anything is checked,
		// answer no: the recursion constrains nothing the walk does
		// not find elsewhere.
		if value == nil || pat == nil {
			return nil, nil
		}
		return &core.Apply{
			T: sys.Bool, Fn: &core.ID{Pat: pat}, Arg: value,
		}, nil
	}
	cons := sys.Constructors(t.Name)
	if len(cons) == 0 {
		return nil, nil
	}
	// Ask first whether anything is checked, so that a datatype
	// carrying no condition costs no names and no code.
	if w.walking == nil {
		w.walking = map[string]*core.IDPat{}
	}
	w.walking[t.String()] = nil
	checks := false
	for _, c := range cons {
		if r.conSurface(c, t) == nil {
			continue
		}
		probe, err := r.deepCondition(r.conSurface(c, t), nil, nil,
			"", w)
		if err != nil {
			delete(w.walking, t.String())
			return nil, err
		}
		if probe != nil {
			checks = true
			break
		}
	}
	if !checks || value == nil {
		delete(w.walking, t.String())
		if checks {
			return boolLiteral(sys, true), nil
		}
		return nil, nil
	}
	erasedNamed, isNamed := erased.(*types.Named)
	if !isNamed {
		delete(w.walking, t.String())
		return nil, nil
	}
	fnT, isFn := sys.Fn(erased, sys.Bool).(*types.Fn)
	if !isFn {
		delete(w.walking, t.String())
		return nil, nil
	}
	predPat := &core.IDPat{T: fnT, Name: r.freshRecordName()}
	w.walking[t.String()] = predPat
	argPat := &core.IDPat{T: erased, Name: r.freshRecordName()}
	matches, err := r.conMatches(cons, t, erasedNamed, blame, w)
	delete(w.walking, t.String())
	if err != nil {
		return nil, err
	}
	// The condition is a function rather than an expression, and is
	// applied to the value, because a datatype may contain itself.
	pred := &core.Fn{
		T:     fnT,
		IDPat: argPat,
		Exp: &core.Case{
			T:       sys.Bool,
			Exp:     &core.ID{Pat: argPat},
			Matches: matches,
		},
	}
	return &core.Let{
		Decl: &core.RecValDecl{
			Binds: []*core.NonRecValDecl{
				{Pat: predPat, Exp: pred, Span: w.span},
			},
		},
		Exp: &core.Apply{
			T: sys.Bool, Fn: &core.ID{Pat: predPat}, Arg: value,
		},
	}, nil
}

// conSurface is a constructor's argument type as written, with
// the datatype instance's arguments substituted for its
// parameters; nil for a constructor that carries nothing.
func (r *resolver) conSurface(c types.Constructor,
	t *types.Named,
) types.Type {
	arg := c.Surface
	if arg == nil {
		arg = c.Arg
	}
	if arg == nil {
		return nil
	}
	if len(t.Args) == 0 {
		return arg
	}
	return r.typeMap.sys.Substitute(arg, t.Args)
}

// conMatches builds one branch per constructor: a constructor
// that carries something checks it, blamed by the constructor's
// name; one that carries nothing has nothing to check.
func (r *resolver) conMatches(cons []types.Constructor,
	t *types.Named, erased *types.Named, blame string,
	w *walkState,
) ([]core.Match, error) {
	sys := r.typeMap.sys
	matches := make([]core.Match, 0, len(cons))
	for _, c := range cons {
		claimed := r.conSurface(c, t)
		if claimed == nil {
			matches = append(matches, core.Match{
				Pat: &core.Con0Pat{
					T:        erased,
					Datatype: erased.Name,
					Name:     c.Name,
					Ordinal:  c.Ordinal,
				},
				Exp: boolLiteral(sys, true),
			})
			continue
		}
		// The erased argument type comes from the erased instance,
		// so that its parts are erased too: substituting the claimed
		// arguments would leave a component typed "nat", which the
		// predicate that reads it rejects.
		erasedArg := c.Arg
		if erasedArg == nil {
			erasedArg = types.Unalias(claimed)
		} else if len(erased.Args) > 0 {
			erasedArg = sys.Substitute(erasedArg, erased.Args)
		}
		conPat := &core.IDPat{T: erasedArg, Name: r.freshRecordName()}
		cond, err := r.deepCondition(claimed, erasedArg,
			&core.ID{Pat: conPat}, appendBlame(blame, "", c.Name), w)
		if err != nil {
			return nil, err
		}
		if cond == nil {
			cond = boolLiteral(sys, true)
		}
		matches = append(matches, core.Match{
			Pat: &core.ConPat{
				T:        erased,
				Datatype: erased.Name,
				Name:     c.Name,
				Ordinal:  c.Ordinal,
				Arg:      conPat,
			},
			Exp: cond,
		})
	}
	return matches, nil
}

// ownCondition is the conjunction of a checked type's own
// conditions, applied to a value.
func (r *resolver) ownCondition(t *types.AliasType, value core.Exp,
	span token.Span,
) (core.Exp, error) {
	sys := r.typeMap.sys
	var conjuncts []core.Exp
	for _, c := range t.Checks {
		code, found := sys.CheckCode(c)
		if !found {
			// A condition written inside a type -- a field declared
			// "int check i => i >= 0" -- belongs to no declaration
			// of its own, so nothing compiled it where it was
			// written. It is typed wherever the type is used, so it
			// can be compiled here.
			err := r.compileChecks(nil, []*ast.Fn{c})
			if err != nil {
				return nil, err
			}
			code, found = sys.CheckCode(c)
		}
		if !found {
			return nil, &Error{
				Span: span, Msg: "condition was not compiled",
			}
		}
		pred, isExp := code.(core.Exp)
		if !isExp {
			return nil, &Error{
				Span: span, Msg: "condition is not an expression",
			}
		}
		if value == nil {
			// Only whether anything is checked is being asked.
			return boolLiteral(sys, true), nil
		}
		conjuncts = append(conjuncts, &core.Apply{
			T: sys.Bool, Fn: pred, Arg: value,
		})
	}
	return composeConjuncts(sys, conjuncts), nil
}

// fieldsCondition walks the fields of a record or the components
// of a tuple, selecting each from the value.
func (r *resolver) fieldsCondition(fields []types.Field,
	erased types.Type, value core.Exp, blame string, w *walkState,
	kind string,
) (core.Exp, error) {
	erasedFields := recordLikeFields(erased)
	var cond core.Exp
	for i, f := range fields {
		var fieldValue core.Exp
		var erasedType types.Type
		if i < len(erasedFields) {
			erasedType = erasedFields[i].Type
		}
		if value != nil && erasedType != nil {
			fieldValue = &core.Apply{
				T: erasedType,
				Fn: &core.Selector{
					T:     r.typeMap.sys.Fn(value.Type(), erasedType),
					Name:  f.Label,
					Index: i,
				},
				Arg: value,
			}
		}
		fieldCond, err := r.deepCondition(f.Type, erasedType,
			fieldValue, appendBlame(blame, kind, f.Label), w)
		if err != nil {
			return nil, err
		}
		cond = andAlso(r.typeMap.sys, cond, fieldCond)
	}
	return cond, nil
}

// elementsCondition walks the elements of a collection: every one
// must satisfy the element type's condition.
//
//nolint:nilnil // a nil condition means nothing is checked
func (r *resolver) elementsCondition(elem types.Type,
	erased types.Type, value core.Exp, blame string, w *walkState,
	all string,
) (core.Exp, error) {
	probe, err := r.deepCondition(elem, nil, nil, "", w)
	if err != nil || probe == nil {
		return nil, err
	}
	sys := r.typeMap.sys
	if value == nil {
		return boolLiteral(sys, true), nil
	}
	erasedElem := collectionElem(erased)
	if erasedElem == nil {
		return nil, nil
	}
	elemPat := &core.IDPat{T: erasedElem, Name: r.freshRecordName()}
	elemCond, err := r.deepCondition(elem, erasedElem,
		&core.ID{Pat: elemPat}, blame+"[_]", w)
	if err != nil || elemCond == nil {
		return nil, err
	}
	predT, isFn := sys.Fn(erasedElem, sys.Bool).(*types.Fn)
	if !isFn {
		return nil, nil
	}
	pred := &core.Fn{T: predT, IDPat: elemPat, Exp: elemCond}
	allT := sys.Fn(predT, sys.Fn(value.Type(), sys.Bool))
	return &core.Apply{
		T: sys.Bool,
		Fn: &core.Apply{
			T:   sys.Fn(value.Type(), sys.Bool),
			Fn:  &core.ID{Pat: r.freePat(all, allT)},
			Arg: pred,
		},
		Arg: value,
	}, nil
}

// appendBlame adds a segment to a blame path. Within an outer
// path only the name is added, so a nested field reads
// "field lead.empno".
func appendBlame(blame, kind, name string) string {
	if blame == "" {
		return kind + name
	}
	return blame + "." + name
}

// andAlso conjoins two conditions, either of which may be nil.
func andAlso(sys *types.System, a, b core.Exp) core.Exp {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	default:
		return boolCase(sys, a, b, boolLiteral(sys, false))
	}
}

// checked returns an expression that gives the value of exp,
// having checked that it satisfies the conditions of t, and
// raises Constraint if it does not.
//
//	let val v = e in $check (c1 v andalso c2 v, v) end
//
// The "let" is what stops the expression being evaluated twice,
// once to read it and once for the result.
func (r *resolver) checked(exp core.Exp, t types.Type,
	blame string, span token.Span,
) (core.Exp, error) {
	if !r.hasCheck(t) {
		return exp, nil
	}
	return r.letValue(exp, span, func(id *core.ID) (core.Exp, error) {
		cond, err := r.condition(t, id, blame, span)
		if err != nil {
			return nil, err
		}
		return &core.Check{
			T:        exp.Type(),
			Cond:     cond,
			Value:    id,
			TypeName: checkedTypeName(t),
			Blame:    blame,
			Kind:     core.CheckRaise,
			Span:     span,
		}, nil
	})
}

// scanTypeCondition returns the condition of the checked type a
// scan is over, or nil if it is not over one.
//
// A scan over a checked type enumerates the values of that type,
// so the type's condition belongs in the scan's filter, where the
// planner can use it to generate the values rather than generate
// and reject them. It does not raise: which values the type has
// is the question being asked, not something already claimed of a
// value in hand.
//
//nolint:nilnil // a nil condition means the scan is unconstrained
func (r *resolver) scanTypeCondition(pat ast.Pat,
	corePat core.Pat,
) (core.Exp, error) {
	t, err := r.claimedPatType(pat)
	if err != nil || t == nil {
		return nil, err
	}
	// The erased type comes from the value, not the pattern: a
	// record pattern reaches core as a tuple, whose fields are
	// named 1, 2.
	value := r.rowValue(corePat, types.Unalias(t))
	if value == nil {
		return nil, nil
	}
	return r.deepCondition(t, value.Type(), value, "",
		&walkState{span: pat.Span()})
}

// rowValue returns an expression for the row a scan's pattern
// binds, or nil if the pattern is one this cannot reassemble.
func (r *resolver) rowValue(corePat core.Pat,
	erased types.Type,
) core.Exp {
	switch p := corePat.(type) {
	case *core.IDPat:
		return &core.ID{Pat: p}
	case *core.TuplePat:
		// A record pattern, "{i, j}", reaches core as a tuple of the
		// fields in field order, so the names come from the type the
		// user wrote, not from the pattern, whose fields are named
		// 1, 2.
		fields := recordLikeFields(erased)
		if len(fields) != len(p.Args) {
			return nil
		}
		args := make([]core.Exp, len(p.Args))
		for i, arg := range p.Args {
			id, isID := arg.(*core.IDPat)
			if !isID {
				return nil
			}
			args[i] = &core.ID{Pat: id}
		}
		return &core.Tuple{T: erased, Args: args}
	default:
		return nil
	}
}

// checkedBranch checks the value entering a branch whose pattern
// claims a checked type.
//
// A branch is what a function's parameter and a "case" have in
// common, so both are checked here, and a function of several
// branches is checked in whichever branch claims -- the parameter
// of the function as a whole claims nothing, because another
// branch may match instead.
//
//	(n: nat) => e
//
// becomes
//
//	v => let val n = $check (c v, "nat") in e end
//
// rather than checking and discarding, which an optimizer would
// be entitled to remove: the body reads the name the check binds,
// so the check cannot be dropped.
//
// Only a pattern that binds the whole value is rewritten.
// Replacing a destructuring pattern with a fresh name would make
// an irrefutable branch of a refutable one.
func (r *resolver) checkedBranch(pat core.Pat, body core.Exp,
	m *ast.Match,
) (core.Pat, core.Exp, error) {
	t, err := r.claimedPatType(m.Pat)
	if err != nil {
		return nil, nil, err
	}
	if t == nil {
		return pat, body, nil
	}
	rawPat := &core.IDPat{T: pat.Type(), Name: r.freshRecordName()}
	checked, err := r.checked(&core.ID{Pat: rawPat}, t, "", m.Span())
	if err != nil {
		return nil, nil, err
	}
	if idPat, isID := pat.(*core.IDPat); isID {
		return rawPat, &core.Let{
			Decl: &core.NonRecValDecl{
				Pat: idPat, Exp: checked, Span: m.Span(),
			},
			Exp: body,
		}, nil
	}
	// A pattern that destructures binds no one name that covers the
	// whole value, so the check goes on the value and the pattern is
	// matched against what it gives back. The pattern is the one
	// that was written, so it decides as it did before; it is only
	// reached through the check.
	return rawPat, &core.Case{
		T:       body.Type(),
		Exp:     checked,
		Matches: []core.Match{{Pat: pat, Exp: body, Span: m.Span()}},
	}, nil
}

// checkedConArg checks the argument of a datatype constructor
// against the type the constructor was declared to hold.
//
// The blame names the constructor, and the path within its
// argument follows: "argument of BoxPair.2".
func (r *resolver) checkedConArg(fn, arg core.Exp,
	span token.Span,
) (core.Exp, error) {
	con, isCon := fn.(*core.Con)
	if !isCon || !con.HasArg {
		return arg, nil
	}
	tc, found := r.typeMap.sys.LookupTyCon(con.Name)
	if !found || tc.Surface == nil || !r.hasCheck(tc.Surface) {
		return arg, nil
	}
	return r.checked(arg, tc.Surface, "argument of "+con.Name, span)
}

// toCast converts "e as t" or "e asOpt t".
//
// "as" gives back the value, having checked the conditions of t,
// and raises Constraint if they do not hold. "asOpt" asks rather
// than claims: it answers SOME v if they hold and NONE if they do
// not.
//
// Where the type converted to carries no condition the conversion
// is free: widening discards a condition and checks nothing.
func (r *resolver) toCast(env *coreEnv, e *ast.Cast) (
	core.Exp, error,
) {
	exp, err := r.toExp(env, e.Exp)
	if err != nil {
		return nil, err
	}
	t := r.typeMap.AliasedTypeOf(e.Type)
	if t == nil {
		t, err = r.typeMap.TypeOf(e)
		if err != nil {
			return nil, err
		}
	}
	span := e.Span()
	err = rejectCheckedFunction(t, t, e.Type.Span())
	if err != nil {
		return nil, err
	}
	if !e.Opt {
		return r.checked(exp, t, "", span)
	}
	optType, err := r.typeMap.TypeOf(e)
	if err != nil {
		return nil, err
	}
	return r.checkedOpt(exp, t, optType, span)
}

// checkedOpt returns an expression that gives SOME v if the
// conditions of t hold of the value, and NONE if they do not.
//
//	let val v = e in if c1 v andalso c2 v then SOME v else NONE end
func (r *resolver) checkedOpt(exp core.Exp, t, optType types.Type,
	span token.Span,
) (core.Exp, error) {
	sys := r.typeMap.sys
	return r.letValue(exp, span, func(id *core.ID) (core.Exp, error) {
		cond, err := r.condition(t, id, "", span)
		if err != nil {
			return nil, err
		}
		if r.hasCheck(t) {
			cond = &core.Check{
				T:        sys.Bool,
				Cond:     cond,
				Value:    id,
				TypeName: checkedTypeName(t),
				Kind:     core.CheckAttempt,
				Span:     span,
			}
		}
		return boolCase(sys, cond,
			someExp(sys, id, optType), noneExp(optType)), nil
	})
}

// someExp is "SOME v", and noneExp is "NONE", at an option type.
func someExp(sys *types.System, v core.Exp,
	optType types.Type,
) core.Exp {
	return &core.Apply{
		T: optType,
		Fn: &core.Con{
			T:        sys.Fn(v.Type(), optType),
			Datatype: typeOption,
			Name:     "SOME",
			Ordinal:  1,
			HasArg:   true,
		},
		Arg: v,
	}
}

func noneExp(optType types.Type) core.Exp {
	return &core.Con{
		T:        optType,
		Datatype: typeOption,
		Name:     "NONE",
		Ordinal:  0,
	}
}

// condition returns the condition that a value satisfies every
// condition its claimed type carries, at any depth.
func (r *resolver) condition(t types.Type, value core.Exp,
	blame string, span token.Span,
) (core.Exp, error) {
	// The value's own type is the erased one: core expressions are
	// typed with aliases expanded, and it is what the selectors and
	// predicates the walk builds must agree with.
	cond, err := r.deepCondition(t, value.Type(), value, blame,
		&walkState{span: span, raising: true})
	if err != nil {
		return nil, err
	}
	if cond == nil {
		return boolLiteral(r.typeMap.sys, true), nil
	}
	return cond, nil
}

// checkedTypeName names a checked type in a message. A type that
// has no name is called "value": there is nothing to call it, and
// printing its condition would repeat what the message already
// says failed.
func checkedTypeName(t types.Type) string {
	if a, ok := t.(*types.AliasType); ok && a.Name == "" {
		return "value"
	}
	return t.String()
}
