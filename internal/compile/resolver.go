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
	"strings"

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
	if !ok || len(d.Binds) != 1 || d.Rec {
		return nil, nil, &Error{
			Span: decl.Span(),
			Msg: "cannot convert to core: " +
				decl.Op().String(),
		}
	}
	bind := d.Binds[0]
	idPat, err := r.toIDPat(bind.Pat)
	if err != nil {
		return nil, nil, err
	}
	exp, err := r.toExp(env, bind.Exp)
	if err != nil {
		return nil, nil, err
	}
	valDecl := &core.NonRecValDecl{Pat: idPat, Exp: exp}
	return valDecl, env.bind(idPat), nil
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
	case *ast.Apply:
		return r.toApply(env, e, t)
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
	case *ast.Let:
		return r.flattenLet(env, e.Decls, e.Exp)
	case *ast.Literal:
		return r.toLiteral(e, t)
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
	return &core.Apply{T: t, Fn: fn, Arg: arg}, nil
}

// toFn converts a function with a single name-binding rule; match
// lists and structured patterns arrive with case-into-fn merging.
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
	if len(fn.Matches) != 1 {
		return nil, &Error{
			Span: fn.Span(),
			Msg:  "cannot convert to core: fn with match list",
		}
	}
	match := fn.Matches[0]
	idPat, err := r.toIDPat(match.Pat)
	if err != nil {
		return nil, err
	}
	exp, err := r.toExp(env.bind(idPat), match.Exp)
	if err != nil {
		return nil, err
	}
	return &core.Fn{T: fnType, IDPat: idPat, Exp: exp}, nil
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
	case ast.CharLiteralOp:
		return []rune(text)[0], nil
	case ast.IntLiteralOp:
		i, err := strconv.ParseInt(
			strings.ReplaceAll(text, "~", "-"), 10, 32)
		if err != nil {
			return nil, &Error{Msg: "invalid literal: " + text}
		}
		return int32(i), nil
	case ast.RealLiteralOp:
		f, err := strconv.ParseFloat(
			strings.ReplaceAll(text, "~", "-"), 32)
		if err != nil {
			return nil, &Error{Msg: "invalid literal: " + text}
		}
		return float32(f), nil
	case ast.StringLiteralOp:
		return text, nil
	case ast.UnitLiteralOp:
		return core.Unit{}, nil
	default:
		return nil, &Error{
			Msg: "cannot convert literal " + kind.String(),
		}
	}
}
