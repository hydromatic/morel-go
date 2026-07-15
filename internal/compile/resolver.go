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
	"sort"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/token"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// Resolve converts a type-resolved declaration to Core.
func Resolve(resolved *Resolved) (core.Decl, error) {
	r := &resolver{typeMap: resolved.TypeMap}
	decl, _, err := r.toDecl(nil, resolved.Decl)
	return decl, err
}

// resolver converts AST nodes to Core, attaching the types that
// the TypeResolver deduced.
type resolver struct {
	typeMap *TypeMap
}

// coreEnv maps a name in scope to the IDPat that declared it, so
// every reference resolves to its declaration.
type coreEnv struct {
	parent *coreEnv
	pat    *core.IDPat
}

func (e *coreEnv) get(name string) *core.IDPat {
	for env := e; env != nil; env = env.parent {
		if env.pat.Name == name {
			return env.pat
		}
	}
	return nil
}

func (e *coreEnv) bind(pat *core.IDPat) *coreEnv {
	return &coreEnv{parent: e, pat: pat}
}

// toDecl converts a declaration, returning also the environment
// for whatever follows the declaration.
func (r *resolver) toDecl(env *coreEnv, decl ast.Decl) (core.Decl,
	*coreEnv, error,
) {
	d, ok := decl.(*ast.ValDecl)
	if !ok {
		return nil, nil, &Error{
			Span: decl.Span(),
			Msg: "cannot convert to core: " +
				decl.Op().String(),
		}
	}
	if d.Rec {
		return r.toRecDecl(env, d)
	}
	if len(d.Binds) != 1 {
		return r.toParallelDecl(env, d)
	}
	bind := d.Binds[0]
	pat, err := r.toPat(bind.Pat)
	if err != nil {
		return nil, nil, err
	}
	exp, err := r.toExp(env, bind.Exp)
	if err != nil {
		return nil, nil, err
	}
	env2 := env
	for _, id := range core.PatIDs(pat) {
		env2 = env2.bind(id)
	}
	valDecl := &core.NonRecValDecl{
		Pat:  pat,
		Exp:  exp,
		Span: bind.Span(),
	}
	return valDecl, env2, nil
}

// toParallelDecl converts a non-recursive "and" group ("val x =
// e1 and y = e2"), which binds its patterns in parallel: each
// expression sees only the outer environment, not its siblings.
// It is modelled as a single tuple binding, "val (p1, p2) = (e1,
// e2)", which the compiler already destructures into the
// individual names.
func (r *resolver) toParallelDecl(env *coreEnv, d *ast.ValDecl) (
	core.Decl, *coreEnv, error,
) {
	pats := make([]core.Pat, len(d.Binds))
	exps := make([]core.Exp, len(d.Binds))
	patTypes := make([]types.Type, len(d.Binds))
	expTypes := make([]types.Type, len(d.Binds))
	for i, bind := range d.Binds {
		pat, err := r.toPat(bind.Pat)
		if err != nil {
			return nil, nil, err
		}
		exp, err := r.toExp(env, bind.Exp)
		if err != nil {
			return nil, nil, err
		}
		pats[i], exps[i] = pat, exp
		patTypes[i], expTypes[i] = pat.Type(), exp.Type()
	}
	patTuple := &core.TuplePat{
		T:    r.typeMap.sys.Tuple(patTypes...),
		Args: pats,
	}
	expTuple := &core.Tuple{
		T:    r.typeMap.sys.Tuple(expTypes...),
		Args: exps,
	}
	env2 := env
	for _, id := range core.PatIDs(patTuple) {
		env2 = env2.bind(id)
	}
	return &core.NonRecValDecl{
		Pat:  patTuple,
		Exp:  expTuple,
		Span: d.Span(),
	}, env2, nil
}

// toRecDecl converts a recursive declaration; its names are in
// scope in all of its own expressions.
func (r *resolver) toRecDecl(env *coreEnv, d *ast.ValDecl) (
	core.Decl, *coreEnv, error,
) {
	idPats := make([]*core.IDPat, len(d.Binds))
	env2 := env
	for i, bind := range d.Binds {
		idPat, err := r.toIDPat(bind.Pat)
		if err != nil {
			return nil, nil, err
		}
		idPats[i] = idPat
		env2 = env2.bind(idPat)
	}
	binds := make([]*core.NonRecValDecl, len(d.Binds))
	for i, bind := range d.Binds {
		exp, err := r.toExp(env2, bind.Exp)
		if err != nil {
			return nil, nil, err
		}
		binds[i] = &core.NonRecValDecl{
			Pat:  idPats[i],
			Exp:  exp,
			Span: bind.Span(),
		}
	}
	return &core.RecValDecl{Binds: binds}, env2, nil
}

// toIDPat converts a pattern that binds (at most) one name; a
// wildcard becomes a pattern whose name is never referenced.
func (r *resolver) toIDPat(pat ast.Pat) (*core.IDPat, error) {
	t, err := r.typeMap.TypeOf(pat)
	if err != nil {
		return nil, err
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.AnnotatedPat:
		// The annotation constrained the type during inference; the
		// core pattern is just the pattern it wraps.
		return r.toIDPat(p.Pat)
	case *ast.IDPat:
		return &core.IDPat{T: t, Name: p.Name}, nil
	case *ast.WildcardPat:
		return &core.IDPat{T: t, Name: "_"}, nil
	}
	return nil, &Error{
		Span: pat.Span(),
		Msg: "cannot convert to core: pattern " +
			pat.Op().String(),
	}
}

func (r *resolver) toExp(env *coreEnv, exp ast.Expr) (core.Exp,
	error,
) {
	t, err := r.typeMap.TypeOf(exp)
	if err != nil {
		return nil, err
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.AnnotatedExp:
		return r.toExp(env, e.Exp)
	case *ast.Apply:
		return r.toApply(env, e, t)
	case *ast.Case:
		return r.toCase(env, e, t)
	case *ast.Fn:
		return r.toFn(env, e, t)
	case *ast.ID:
		pat := env.get(e.Name)
		if pat == nil {
			// The name is not declared in this compilation unit
			// (e.g. a built-in value), so make a declaration
			// site for it.
			pat = &core.IDPat{T: t, Name: e.Name}
		}
		return &core.ID{Pat: pat}, nil
	case *ast.If:
		return r.toIf(env, e, t)
	case *ast.InfixCall:
		return r.toInfix(env, e, t)
	case *ast.Let:
		return r.flattenLet(env, e.Decls, e.Exp)
	case *ast.ListExp:
		args := make([]core.Exp, len(e.Args))
		for i, arg := range e.Args {
			a, err := r.toExp(env, arg)
			if err != nil {
				return nil, err
			}
			args[i] = a
		}
		return &core.List{T: t, Args: args}, nil
	case *ast.Literal:
		return r.toLiteral(e, t)
	case *ast.PrefixCall:
		if e.Kind != ast.NegateOp {
			return nil, &Error{
				Span: e.Span(),
				Msg: "cannot convert to core: " +
					e.Kind.String(),
			}
		}
		arg, err := r.toExp(env, e.A)
		if err != nil {
			return nil, err
		}
		fnPat := &core.IDPat{
			T:    r.typeMap.sys.Fn(t, t),
			Name: "op ~",
		}
		return &core.Apply{
			T:    t,
			Fn:   &core.ID{Pat: fnPat},
			Arg:  arg,
			Span: e.Span(),
		}, nil
	case *ast.Record:
		return r.toRecord(env, e, t)
	case *ast.Tuple:
		args := make([]core.Exp, len(e.Args))
		for i, arg := range e.Args {
			a, err := r.toExp(env, arg)
			if err != nil {
				return nil, err
			}
			args[i] = a
		}
		return &core.Tuple{T: t, Args: args}, nil
	default:
		return nil, &Error{
			Span: exp.Span(),
			Msg: "cannot convert to core: " +
				exp.Op().String(),
		}
	}
}

func (r *resolver) toLiteral(literal *ast.Literal,
	t types.Type,
) (core.Exp, error) {
	value, err := literalValue(literal.Kind, literal.Value)
	if err != nil {
		return nil, &Error{
			Span: literal.Span(),
			Msg:  err.Error(),
		}
	}
	exp := &core.Literal{T: t, Kind: literal.Kind, Value: value}
	return exp, nil
}

func (r *resolver) toApply(env *coreEnv, apply *ast.Apply,
	t types.Type,
) (core.Exp, error) {
	fn, err := r.toExp(env, apply.Fn)
	if err != nil {
		return nil, err
	}
	arg, err := r.toExp(env, apply.Arg)
	if err != nil {
		return nil, err
	}
	apply2 := &core.Apply{
		T:    t,
		Fn:   fn,
		Arg:  arg,
		Span: apply.Span(),
	}
	return apply2, nil
}

// toFn converts a function. A single rule that binds one name
// becomes a Fn directly; otherwise the parameter is a fresh
// variable and the match list becomes a case over it.
func (r *resolver) toFn(env *coreEnv, fn *ast.Fn,
	t types.Type,
) (core.Exp, error) {
	fnType, ok := t.(*types.Fn)
	if !ok {
		return nil, &Error{
			Span: fn.Span(),
			Msg:  "function does not have function type",
		}
	}
	if len(fn.Matches) == 1 {
		match := fn.Matches[0]
		switch match.Pat.(type) {
		case *ast.IDPat, *ast.WildcardPat:
			idPat, err := r.toIDPat(match.Pat)
			if err != nil {
				return nil, err
			}
			exp, err := r.toExp(env.bind(idPat), match.Exp)
			if err != nil {
				return nil, err
			}
			fnExp := &core.Fn{
				T: fnType, IDPat: idPat,
				Exp: exp,
			}
			return fnExp, nil
		}
	}
	param := &core.IDPat{T: fnType.Param, Name: "v"}
	matches, err := r.toMatches(env, fn.Matches)
	if err != nil {
		return nil, err
	}
	body := &core.Case{
		T:       fnType.Result,
		Exp:     &core.ID{Pat: param},
		Matches: matches,
		Span:    matchesSpan(fn.Matches),
	}
	return &core.Fn{T: fnType, IDPat: param, Exp: body}, nil
}

// toIf translates "if c then a else b" as if the user had written
// "case c of true => a | _ => b".
func (r *resolver) toIf(env *coreEnv, ifExp *ast.If,
	t types.Type,
) (core.Exp, error) {
	cond, err := r.toExp(env, ifExp.Cond)
	if err != nil {
		return nil, err
	}
	ifTrue, err := r.toExp(env, ifExp.IfTrue)
	if err != nil {
		return nil, err
	}
	ifFalse, err := r.toExp(env, ifExp.IfFalse)
	if err != nil {
		return nil, err
	}
	boolType := cond.Type()
	caseExp := &core.Case{
		T:   t,
		Exp: cond,
		Matches: []core.Match{
			{
				Pat: &core.LiteralPat{
					T:     boolType,
					Kind:  ast.BoolLiteralOp,
					Value: true,
				},
				Exp: ifTrue,
			},
			{
				Pat: &core.WildcardPat{T: boolType},
				Exp: ifFalse,
			},
		},
	}
	return caseExp, nil
}

// toInfix converts an infix operator application. The logical
// connectives become cases ("a andalso b" runs b only if a is
// true); any other operator becomes the application of its
// top-level binding to the operand pair.
func (r *resolver) toInfix(env *coreEnv, call *ast.InfixCall,
	t types.Type,
) (core.Exp, error) {
	a0, err := r.toExp(env, call.A0)
	if err != nil {
		return nil, err
	}
	a1, err := r.toExp(env, call.A1)
	if err != nil {
		return nil, err
	}
	sys := r.typeMap.sys
	// lint: sort until '^\t}' where '^\tcase '
	switch call.Kind {
	case ast.AndalsoOp:
		return boolCase(sys, a0, a1,
			&core.Literal{
				T: sys.Bool, Kind: ast.BoolLiteralOp,
				Value: false,
			}), nil
	case ast.ImpliesOp:
		return boolCase(sys, a0, a1,
			&core.Literal{
				T: sys.Bool, Kind: ast.BoolLiteralOp,
				Value: true,
			}), nil
	case ast.OrelseOp:
		return boolCase(sys, a0,
			&core.Literal{
				T: sys.Bool, Kind: ast.BoolLiteralOp,
				Value: true,
			}, a1), nil
	default:
	}
	name, ok := infixOpNames[call.Kind]
	if !ok {
		return nil, &Error{
			Span: call.Span(),
			Msg: "cannot convert to core: " +
				call.Kind.String(),
		}
	}
	argType := sys.Tuple(a0.Type(), a1.Type())
	fnPat := &core.IDPat{
		T:    sys.Fn(argType, t),
		Name: name,
	}
	arg := &core.Tuple{
		T:    argType,
		Args: []core.Exp{a0, a1},
	}
	return &core.Apply{
		T:    t,
		Fn:   &core.ID{Pat: fnPat},
		Arg:  arg,
		Span: call.Span(),
	}, nil
}

// boolCase builds "case cond of true => ifTrue | _ => ifFalse".
func boolCase(sys *types.System, cond, ifTrue,
	ifFalse core.Exp,
) core.Exp {
	return &core.Case{
		T:   ifTrue.Type(),
		Exp: cond,
		Matches: []core.Match{
			{
				Pat: &core.LiteralPat{
					T:     sys.Bool,
					Kind:  ast.BoolLiteralOp,
					Value: true,
				},
				Exp: ifTrue,
			},
			{
				Pat: &core.WildcardPat{T: sys.Bool},
				Exp: ifFalse,
			},
		},
	}
}

// toRecord converts a record expression to a tuple whose
// elements are the fields in canonical order.
func (r *resolver) toRecord(env *coreEnv, record *ast.Record,
	t types.Type,
) (core.Exp, error) {
	if record.With != nil {
		return nil, &Error{
			Span: record.Span(),
			Msg:  "cannot convert to core: record update",
		}
	}
	type fieldExp struct {
		label string
		exp   ast.Expr
	}
	fields := make([]fieldExp, len(record.Fields))
	for i, f := range record.Fields {
		label := f.Label
		if label == "" {
			id, ok := f.Exp.(*ast.ID)
			if !ok {
				return nil, &Error{
					Span: record.Span(),
					Msg:  "cannot derive label for expression",
				}
			}
			label = id.Name
		}
		fields[i] = fieldExp{label: label, exp: f.Exp}
	}
	sort.Slice(fields, func(i, j int) bool {
		return types.LabelLess(fields[i].label, fields[j].label)
	})
	args := make([]core.Exp, len(fields))
	for i, f := range fields {
		arg, err := r.toExp(env, f.exp)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}
	return &core.Tuple{T: t, Args: args}, nil
}

// toCase converts "case e of pat => exp | ...". Each rule's
// pattern variables are in scope in its body.
func (r *resolver) toCase(env *coreEnv, caseExp *ast.Case,
	t types.Type,
) (core.Exp, error) {
	scrutinee, err := r.toExp(env, caseExp.Exp)
	if err != nil {
		return nil, err
	}
	matches, err := r.toMatches(env, caseExp.Matches)
	if err != nil {
		return nil, err
	}
	caseExp2 := &core.Case{
		T:       t,
		Exp:     scrutinee,
		Matches: matches,
		Span:    matchesSpan(caseExp.Matches),
	}
	return caseExp2, nil
}

// matchesSpan is the position of a match list, from the first
// rule's start to the last rule's end; a Bind failure is
// reported there.
func matchesSpan(matches []*ast.Match) token.Span {
	return token.Span{
		Start: matches[0].Span().Start,
		End:   matches[len(matches)-1].Span().End,
	}
}

func (r *resolver) toMatches(env *coreEnv,
	matches []*ast.Match,
) ([]core.Match, error) {
	result := make([]core.Match, len(matches))
	for i, m := range matches {
		pat, err := r.toPat(m.Pat)
		if err != nil {
			return nil, err
		}
		env2 := env
		for _, id := range core.PatIDs(pat) {
			env2 = env2.bind(id)
		}
		body, err := r.toExp(env2, m.Exp)
		if err != nil {
			return nil, err
		}
		result[i] = core.Match{Pat: pat, Exp: body}
	}
	return result, nil
}

// toPat converts a pattern.
func (r *resolver) toPat(pat ast.Pat) (core.Pat, error) {
	t, err := r.typeMap.TypeOf(pat)
	if err != nil {
		return nil, err
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.AnnotatedPat:
		// The annotation constrained the type during inference; the
		// core pattern is just the pattern it wraps.
		return r.toPat(p.Pat)
	case *ast.ConsPat:
		return r.toConsPat(p, t)
	case *ast.IDPat:
		if _, isCon := r.typeMap.sys.LookupTyCon(p.Name); isCon {
			// A constant constructor. For now only "nil" (the
			// empty list) has a runtime form.
			if p.Name == "nil" {
				return &core.ListPat{T: t}, nil
			}
			return nil, &Error{
				Span: pat.Span(),
				Msg: "cannot convert to core: constructor " +
					p.Name,
			}
		}
		return &core.IDPat{T: t, Name: p.Name}, nil
	case *ast.ListPat:
		return r.toListPat(p, t)
	case *ast.LiteralPat:
		value, err := literalValue(p.Kind, p.Value)
		if err != nil {
			return nil, &Error{
				Span: p.Span(),
				Msg:  err.Error(),
			}
		}
		literalPat := &core.LiteralPat{
			T:     t,
			Kind:  p.Kind,
			Value: value,
		}
		return literalPat, nil
	case *ast.RecordPat:
		if p.Ellipsis {
			return nil, &Error{
				Span: pat.Span(),
				Msg: "cannot convert to core: pattern " +
					"with ellipsis",
			}
		}
		sorted := make([]ast.PatField, len(p.Fields))
		copy(sorted, p.Fields)
		sort.Slice(sorted, func(i, j int) bool {
			return types.LabelLess(sorted[i].Label,
				sorted[j].Label)
		})
		args := make([]core.Pat, len(sorted))
		for i, f := range sorted {
			arg, err := r.toPat(f.Pat)
			if err != nil {
				return nil, err
			}
			args[i] = arg
		}
		return &core.TuplePat{T: t, Args: args}, nil
	case *ast.TuplePat:
		if len(p.Args) == 0 {
			return &core.WildcardPat{T: t}, nil
		}
		args := make([]core.Pat, len(p.Args))
		for i, argPat := range p.Args {
			arg, err := r.toPat(argPat)
			if err != nil {
				return nil, err
			}
			args[i] = arg
		}
		return &core.TuplePat{T: t, Args: args}, nil
	case *ast.WildcardPat:
		return &core.WildcardPat{T: t}, nil
	default:
		return nil, &Error{
			Span: pat.Span(),
			Msg: "cannot convert to core: pattern " +
				pat.Op().String(),
		}
	}
}

func (r *resolver) toConsPat(p *ast.ConsPat,
	t types.Type,
) (core.Pat, error) {
	head, err := r.toPat(p.A0)
	if err != nil {
		return nil, err
	}
	tail, err := r.toPat(p.A1)
	if err != nil {
		return nil, err
	}
	return &core.ConsPat{T: t, Head: head, Tail: tail}, nil
}

func (r *resolver) toListPat(p *ast.ListPat,
	t types.Type,
) (core.Pat, error) {
	args := make([]core.Pat, len(p.Args))
	for i, argPat := range p.Args {
		arg, err := r.toPat(argPat)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}
	return &core.ListPat{T: t, Args: args}, nil
}

// flattenLet converts "let d1 d2 ... in e end" to nested Lets,
// one declaration each.
func (r *resolver) flattenLet(env *coreEnv, decls []ast.Decl,
	exp ast.Expr,
) (core.Exp, error) {
	if len(decls) == 0 {
		return r.toExp(env, exp)
	}
	decl, env2, err := r.toDecl(env, decls[0])
	if err != nil {
		return nil, err
	}
	body, err := r.flattenLet(env2, decls[1:], exp)
	if err != nil {
		return nil, err
	}
	return &core.Let{Decl: decl, Exp: body}, nil
}

// literalValue converts a literal's text to its runtime value.
func literalValue(kind ast.Op, text string) (any, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ast.CharLiteralOp, ast.CharLiteralPatOp:
		return []rune(text)[0], nil
	case ast.IntLiteralOp, ast.IntLiteralPatOp:
		i, err := strconv.ParseInt(
			strings.ReplaceAll(text, "~", "-"), 10, 32)
		if err != nil {
			return nil, &Error{Msg: "invalid literal: " + text}
		}
		return int32(i), nil
	case ast.RealLiteralOp, ast.RealLiteralPatOp:
		f, err := strconv.ParseFloat(
			strings.ReplaceAll(text, "~", "-"), 32)
		if err != nil {
			return nil, &Error{Msg: "invalid literal: " + text}
		}
		return float32(f), nil
	case ast.StringLiteralOp, ast.StringLiteralPatOp:
		return text, nil
	case ast.UnitLiteralOp:
		return core.Unit{}, nil
	default:
		return nil, &Error{
			Msg: "cannot convert literal " + kind.String(),
		}
	}
}
