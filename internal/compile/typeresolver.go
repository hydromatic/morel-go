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
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/internal/unify"
)

// Type constructors used in unification terms.
const (
	fnTyCon     = "fn"
	listTyCon   = "list"
	recordTyCon = "record"
	tupleTyCon  = "tuple"
)

// Resolved is the outcome of type deduction: the declaration and
// a type for every node in it.
type Resolved struct {
	Decl    ast.Decl
	TypeMap *TypeMap
}

// Deduce infers a type for every node of a declaration, in an
// environment given by bindings.
func Deduce(sys *types.System, bindings []Binding,
	decl ast.Decl,
) (*Resolved, error) {
	r := &typeResolver{
		sys:      sys,
		u:        unify.New(),
		nodeTerm: map[ast.Node]unify.Term{},
	}
	var env typeEnv = emptyTypeEnv{}
	if len(bindings) > 0 {
		byName := make(map[string]*Binding, len(bindings))
		for i := range bindings {
			byName[bindings[i].Name] = &bindings[i]
		}
		env = &bindingTypeEnv{parent: env, bindings: byName}
	}
	var termMap []patTerm
	err := r.deduceDecl(env, decl, &termMap)
	if err != nil {
		return nil, err
	}
	subst, err := r.u.Unify(r.pairs, r.actions, r.constraints)
	if err != nil {
		return nil, &Error{
			Span: decl.Span(),
			Msg:  "Cannot deduce type: " + err.Error(),
		}
	}
	typeMap := &TypeMap{
		sys:      sys,
		nodeTerm: r.nodeTerm,
		subst:    subst,
	}
	return &Resolved{Decl: decl, TypeMap: typeMap}, nil
}

// patTerm records that a pattern binds a name to a term; the
// caller adds the name to the environment.
type patTerm struct {
	pat  *ast.IDPat
	term unify.Term
}

// typeResolver assigns a unification variable to every AST node,
// generates term equivalences from the structure of the tree,
// and hands them to the unifier.
type typeResolver struct {
	sys         *types.System
	u           *unify.Unifier
	pairs       []unify.TermPair
	nodeTerm    map[ast.Node]unify.Term
	actions     []unify.VarAction
	constraints []unify.Constraint
}

// equiv declares that a term is equivalent to a variable.
func (r *typeResolver) equiv(v *unify.Var, t unify.Term) {
	if unify.Term(v) != t {
		r.pairs = append(r.pairs,
			unify.TermPair{Left: t, Right: v})
	}
}

// reg registers that a node's type is a variable.
func (r *typeResolver) reg(node ast.Node, v *unify.Var) {
	r.nodeTerm[node] = v
}

// regEquiv registers that a node's type is a term, equivalent to
// the variable.
func (r *typeResolver) regEquiv(node ast.Node, v *unify.Var,
	t unify.Term,
) {
	r.equiv(v, t)
	r.nodeTerm[node] = t
}

func (r *typeResolver) fnTerm(param, result unify.Term) unify.Term {
	return unify.Apply(fnTyCon, param, result)
}

func (r *typeResolver) primTerm(name string) unify.Term {
	return r.u.Atom(name)
}

// typeTerm converts a type to a term. Type variables become
// unification variables via subst, fresh at their first
// occurrence, so each conversion instantiates a polymorphic type.
func (r *typeResolver) typeTerm(t types.Type,
	subst map[int]*unify.Var,
) unify.Term {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Fn:
		return r.fnTerm(r.typeTerm(t.Param, subst),
			r.typeTerm(t.Result, subst))
	case *types.List:
		return unify.Apply(listTyCon, r.typeTerm(t.Elem, subst))
	case *types.Primitive:
		return r.u.Atom(t.String())
	case *types.Record:
		labels := make([]string, len(t.Fields))
		terms := make([]unify.Term, len(t.Fields))
		for i, f := range t.Fields {
			labels[i] = f.Label
			terms[i] = r.typeTerm(f.Type, subst)
		}
		return unify.Apply(recordLabel(labels), terms...)
	case *types.Tuple:
		terms := make([]unify.Term, len(t.Args))
		for i, arg := range t.Args {
			terms[i] = r.typeTerm(arg, subst)
		}
		return unify.Apply(tupleTyCon, terms...)
	case *types.Var:
		v, ok := subst[t.Ordinal]
		if !ok {
			v = r.u.Variable()
			subst[t.Ordinal] = v
		}
		return v
	default:
		panic("cannot convert type " + t.String())
	}
}

// recordLabel builds the term operator for a record type with the
// given labels, e.g. "record:a:b".
func recordLabel(labels []string) string {
	return recordTyCon + ":" + strings.Join(labels, ":")
}

func (r *typeResolver) deduceDecl(env typeEnv, decl ast.Decl,
	termMap *[]patTerm,
) error {
	switch d := decl.(type) {
	case *ast.ValDecl:
		if d.Rec {
			return &Error{
				Span: d.Span(),
				Msg:  "cannot deduce type for val rec",
			}
		}
		for _, b := range d.Binds {
			err := r.deduceValBind(env, b, termMap)
			if err != nil {
				return err
			}
		}
		r.nodeTerm[decl] = r.primTerm("unit")
		return nil
	default:
		return &Error{
			Span: decl.Span(),
			Msg: "cannot deduce type for " +
				decl.Op().String(),
		}
	}
}

func (r *typeResolver) deduceValBind(env typeEnv,
	bind *ast.ValBind, termMap *[]patTerm,
) error {
	vPat := r.u.Variable()
	err := r.deducePat(bind.Pat, termMap, vPat)
	if err != nil {
		return err
	}
	err = r.deduceExp(env, bind.Exp, vPat)
	if err != nil {
		return err
	}
	r.nodeTerm[bind] = r.primTerm("unit")
	return nil
}

func (r *typeResolver) deducePat(pat ast.Pat,
	termMap *[]patTerm, v *unify.Var,
) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.IDPat:
		*termMap = append(*termMap, patTerm{pat: p, term: v})
		r.reg(pat, v)
		return nil
	case *ast.LiteralPat:
		return r.deduceLiteral(pat, p.Kind, p.Value, v)
	case *ast.WildcardPat:
		r.reg(pat, v)
		return nil
	default:
		return &Error{
			Span: pat.Span(),
			Msg: "cannot deduce type for pattern " +
				pat.Op().String(),
		}
	}
}

func bindAll(env typeEnv, termMap []patTerm) typeEnv {
	for _, pt := range termMap {
		env = bind(env, pt.pat.Name, pt.term)
	}
	return env
}

func (r *typeResolver) deduceExp(env typeEnv, exp ast.Expr,
	v *unify.Var,
) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.Apply:
		return r.deduceApply(env, e, v)
	case *ast.Fn:
		vResult := r.u.Variable()
		for _, m := range e.Matches {
			err := r.deduceMatch(env, m, v, vResult)
			if err != nil {
				return err
			}
		}
		r.reg(exp, v)
		return nil
	case *ast.ID:
		term, ok := env.get(r, e.Name)
		if !ok {
			return &Error{
				Span: e.Span(),
				Msg: "unbound variable or constructor: " +
					e.Name,
			}
		}
		r.regEquiv(exp, v, term)
		return nil
	case *ast.If:
		return r.deduceIf(env, e, v)
	case *ast.Let:
		env2 := env
		for _, d := range e.Decls {
			var termMap []patTerm
			err := r.deduceDecl(env2, d, &termMap)
			if err != nil {
				return err
			}
			env2 = bindAll(env2, termMap)
		}
		err := r.deduceExp(env2, e.Exp, v)
		if err != nil {
			return err
		}
		r.reg(exp, v)
		return nil
	case *ast.Literal:
		return r.deduceLiteral(exp, e.Kind, e.Value, v)
	default:
		return &Error{
			Span: exp.Span(),
			Msg: "cannot deduce type for " +
				exp.Op().String(),
		}
	}
}

// deduceLiteral handles literal expressions and literal patterns,
// whose types depend only on the literal kind.
func (r *typeResolver) deduceLiteral(node ast.Node, kind ast.Op,
	value string, v *unify.Var,
) error {
	var name string
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ast.BoolLiteralOp:
		name = "bool"
	case ast.CharLiteralOp:
		name = "char"
	case ast.IntLiteralOp:
		err := checkIntRange(node, value)
		if err != nil {
			return err
		}
		name = "int"
	case ast.RealLiteralOp:
		name = "real"
	case ast.StringLiteralOp:
		name = "string"
	case ast.UnitLiteralOp:
		name = "unit"
	default:
		return &Error{
			Span: node.Span(),
			Msg: "cannot deduce type for literal " +
				kind.String(),
		}
	}
	r.regEquiv(node, v, r.primTerm(name))
	return nil
}

// checkIntRange rejects an int literal that does not fit in a
// signed 32-bit integer.
func checkIntRange(node ast.Node, value string) error {
	text := strings.ReplaceAll(value, "~", "-")
	_, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		return &Error{
			Span: node.Span(),
			Msg: "literal '" + value +
				"' is too large for type int",
		}
	}
	return nil
}

func (r *typeResolver) deduceApply(env typeEnv, apply *ast.Apply,
	v *unify.Var,
) error {
	vFn := r.u.Variable()
	vArg := r.u.Variable()
	err := r.deduceExp(env, apply.Fn, vFn)
	if err != nil {
		return err
	}
	err = r.deduceExp(env, apply.Arg, vArg)
	if err != nil {
		return err
	}
	r.equiv(vFn, r.fnTerm(vArg, v))
	r.reg(apply, v)
	return nil
}

func (r *typeResolver) deduceIf(env typeEnv, ifExp *ast.If,
	v *unify.Var,
) error {
	vCond := r.u.Variable()
	err := r.deduceExp(env, ifExp.Cond, vCond)
	if err != nil {
		return err
	}
	r.equiv(vCond, r.primTerm("bool"))
	err = r.deduceExp(env, ifExp.IfTrue, v)
	if err != nil {
		return err
	}
	err = r.deduceExp(env, ifExp.IfFalse, v)
	if err != nil {
		return err
	}
	r.reg(ifExp, v)
	return nil
}

// deduceMatch handles one match rule "pat => exp" of a fn: the
// rule's type is "typeof(pat) -> typeof(exp)".
func (r *typeResolver) deduceMatch(env typeEnv, match *ast.Match,
	argVariable, resultVariable *unify.Var,
) error {
	vPat := r.u.Variable()
	var termMap []patTerm
	err := r.deducePat(match.Pat, &termMap, vPat)
	if err != nil {
		return err
	}
	env2 := bindAll(env, termMap)
	err = r.deduceExp(env2, match.Exp, resultVariable)
	if err != nil {
		return err
	}
	r.regEquiv(match, argVariable,
		r.fnTerm(vPat, resultVariable))
	return nil
}
