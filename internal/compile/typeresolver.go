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
	decl2, err := r.deduceDecl(env, decl, &termMap)
	if err != nil {
		return nil, err
	}
	// Unify; if an operator's type is undetermined and it has a
	// preferred type (e.g. int for "+"), apply the preference and
	// unify again.
	for {
		subst, err := r.u.Unify(r.pairs, r.actions,
			r.constraints)
		if err != nil {
			return nil, &Error{
				Span: decl.Span(),
				Msg:  "Cannot deduce type: " + err.Error(),
			}
		}
		again := false
		for len(r.preferred) > 0 {
			pt := r.preferred[0]
			r.preferred = r.preferred[1:]
			if _, isVar := subst.Resolve(pt.v).(*unify.Var); isVar {
				r.equiv(pt.v, r.primTerm(pt.prim))
				again = true
				break
			}
		}
		if again {
			continue
		}
		typeMap := &TypeMap{
			sys:      sys,
			nodeTerm: r.nodeTerm,
			subst:    subst,
		}
		err = r.checkNumericOperators(typeMap)
		if err != nil {
			return nil, err
		}
		return &Resolved{Decl: decl2, TypeMap: typeMap}, nil
	}
}

// numericCall is an application of an overloaded numeric
// operator, to be checked against the operator's overload class
// once unification has resolved its type.
type numericCall struct {
	name  string
	apply *ast.Apply
}

// numericOpDomain gives the types for which each overloaded
// numeric operator is defined (its SML overload class). 'div'
// and 'mod' are int-only until 'Word' arrives (morel#396); '/'
// is absent because it is real-only, so a bad operand is a
// unification conflict, not an excluded class member.
var numericOpDomain = map[string]map[string]bool{
	"abs":    {intName: true, realName: true},
	opTimes:  {intName: true, realName: true},
	opPlus:   {intName: true, realName: true},
	opMinus:  {intName: true, realName: true},
	opDiv:    {intName: true},
	opMod:    {intName: true},
	opNegate: {intName: true, realName: true},
}

// checkNumericOperators checks that every application of an
// overloaded numeric operator has a type in the operator's
// overload class, after resolving: the check is by name (a
// rebinding is still checked), the
// outermost bad application reports first, '~' reports its
// operand's span, and a type that is still a variable passes.
func (r *typeResolver) checkNumericOperators(m *TypeMap) error {
	for _, call := range r.numericCalls {
		t, err := m.TypeOf(call.apply)
		if err != nil {
			return err
		}
		if _, isVar := t.(*types.Var); isVar {
			continue
		}
		if numericOpDomain[call.name][t.String()] {
			continue
		}
		span := call.apply.Span()
		if call.name == opNegate {
			span = call.apply.Arg.Span()
		}
		return &Error{
			Span: span,
			Msg: "operator '" +
				strings.TrimPrefix(call.name, "op ") +
				"' is not defined for type '" +
				t.String() + "'",
		}
	}
	return nil
}

// patTerm records that a declaration binds a name to a term; the
// caller adds the name to the environment.
type patTerm struct {
	name string
	term unify.Term
}

// typeResolver assigns a unification variable to every AST node,
// generates term equivalences from the structure of the tree,
// and hands them to the unifier.
type typeResolver struct {
	sys          *types.System
	u            *unify.Unifier
	pairs        []unify.TermPair
	nodeTerm     map[ast.Node]unify.Term
	actions      []unify.VarAction
	constraints  []unify.Constraint
	preferred    []preferredType
	numericCalls []numericCall
	tyVarScopes  []map[string]*unify.Var
}

// preferredType records that, if unification leaves v
// undetermined, it should be unified with a primitive type and
// unification retried; this resolves "1 + 2" to int.
type preferredType struct {
	v    *unify.Var
	prim string
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
	case *types.Named:
		terms := make([]unify.Term, len(t.Args))
		for i, arg := range t.Args {
			terms[i] = r.typeTerm(arg, subst)
		}
		return unify.Apply(t.Name, terms...)
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

// astTypeTerm converts a type annotation to a term. A type
// variable resolves in the innermost annotation scope, created
// if absent, so annotations within one declaration share their
// type variables.
func (r *typeResolver) astTypeTerm(t ast.Type) (unify.Term,
	error,
) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *ast.FnType:
		param, err := r.astTypeTerm(t.Param)
		if err != nil {
			return nil, err
		}
		result, err := r.astTypeTerm(t.Result)
		if err != nil {
			return nil, err
		}
		return r.fnTerm(param, result), nil
	case *ast.NamedType:
		return r.astNamedTerm(t)
	case *ast.RecordType:
		fields := make([]labelTerm, len(t.Fields))
		for i, f := range t.Fields {
			term, err := r.astTypeTerm(f.Type)
			if err != nil {
				return nil, err
			}
			fields[i] = labelTerm{label: f.Label, term: term}
		}
		sortFields(fields)
		return r.recordTerm(fields), nil
	case *ast.TupleType:
		terms := make([]unify.Term, len(t.Args))
		for i, arg := range t.Args {
			term, err := r.astTypeTerm(arg)
			if err != nil {
				return nil, err
			}
			terms[i] = term
		}
		return r.tupleTerm(terms), nil
	case *ast.TyVar:
		if len(r.tyVarScopes) == 0 {
			return r.u.Variable(), nil
		}
		scope := r.tyVarScopes[len(r.tyVarScopes)-1]
		tv, ok := scope[t.Name]
		if !ok {
			tv = r.u.Variable()
			scope[t.Name] = tv
		}
		return tv, nil
	default:
		return nil, &Error{
			Span: t.Span(),
			Msg: "cannot deduce type for annotation " +
				t.Op().String(),
		}
	}
}

// astNamedTerm converts a named type annotation: a primitive, a
// list, or an instance of a datatype.
func (r *typeResolver) astNamedTerm(t *ast.NamedType) (unify.Term,
	error,
) {
	terms := make([]unify.Term, len(t.Args))
	for i, arg := range t.Args {
		term, err := r.astTypeTerm(arg)
		if err != nil {
			return nil, err
		}
		terms[i] = term
	}
	if t.Name == listTyCon && len(terms) == 1 {
		return unify.Apply(listTyCon, terms[0]), nil
	}
	if arity, ok := r.sys.DatatypeArity(t.Name); ok &&
		arity == len(terms) {
		return unify.Apply(t.Name, terms...), nil
	}
	if len(terms) == 0 && r.sys.Lookup(t.Name) != nil {
		return r.u.Atom(t.Name), nil
	}
	return nil, &Error{
		Span: t.Span(),
		Msg:  "unbound type constructor: " + t.Name,
	}
}

func (r *typeResolver) deduceDecl(env typeEnv, decl ast.Decl,
	termMap *[]patTerm,
) (ast.Decl, error) {
	// lint: sort until '^	}' where '^	case '
	switch d := decl.(type) {
	case *ast.DatatypeDecl:
		return r.deduceDatatypeDecl(d, termMap)
	case *ast.FunDecl:
		return r.deduceValDecl(env, funToVal(d), termMap)
	case *ast.ValDecl:
		return r.deduceValDecl(env, d, termMap)
	default:
		return nil, &Error{
			Span: decl.Span(),
			Msg: "cannot deduce type for " +
				decl.Op().String(),
		}
	}
}

func (r *typeResolver) deduceValDecl(env typeEnv,
	decl *ast.ValDecl, termMap *[]patTerm,
) (ast.Decl, error) {
	// If recursive, bind each name (presumably a function) to
	// its type variable before deducing the expressions' types.
	env2 := env
	vPats := make([]*unify.Var, len(decl.Binds))
	for i, b := range decl.Binds {
		vPats[i] = r.u.Variable()
		if decl.Rec {
			if idPat, ok := b.Pat.(*ast.IDPat); ok {
				env2 = bind(env2, idPat.Name, vPats[i])
			}
		}
	}
	for i, b := range decl.Binds {
		err := r.deduceValBind(env2, b, termMap, vPats[i])
		if err != nil {
			return nil, err
		}
	}
	r.nodeTerm[decl] = r.primTerm("unit")
	return decl, nil
}

func (r *typeResolver) deduceValBind(env typeEnv,
	bind *ast.ValBind, termMap *[]patTerm, vPat *unify.Var,
) error {
	// Type variables in this binding's annotations share one
	// scope, so in "fun f (x: 'a) (y: 'a) = ..." both 'a are the
	// same type.
	r.tyVarScopes = append(r.tyVarScopes,
		map[string]*unify.Var{})
	defer func() {
		r.tyVarScopes = r.tyVarScopes[:len(r.tyVarScopes)-1]
	}()
	err := r.deducePat(bind.Pat, termMap, nil, vPat)
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

// deduceConPat handles the application of a constructor to a
// pattern, e.g. "SOME x". The constructor's argument and result
// types share one instantiation, so "SOME x" has type
// "'a option" where "x" has type "'a".
func (r *typeResolver) deduceConPat(pat *ast.ConPat,
	termMap *[]patTerm, v *unify.Var,
) error {
	tc, ok := r.sys.LookupTyCon(pat.Name)
	if !ok || tc.Arg == nil {
		return &Error{
			Span: pat.Span(),
			Msg: "unbound constructor: " +
				pat.Name,
		}
	}
	vArg := r.u.Variable()
	err := r.deducePat(pat.Arg, termMap, nil, vArg)
	if err != nil {
		return err
	}
	subst := map[int]*unify.Var{}
	r.equiv(vArg, r.typeTerm(tc.Arg, subst))
	r.regEquiv(pat, v, r.typeTerm(tc.Result, subst))
	return nil
}

// deduceDatatypeDecl registers the declared datatypes and their
// constructors, and binds each constructor as a value. The
// datatypes are registered before any constructor argument type
// is converted, so constructors may refer to their own datatype
// (or a sibling's) recursively.
func (r *typeResolver) deduceDatatypeDecl(decl *ast.DatatypeDecl,
	termMap *[]patTerm,
) (ast.Decl, error) {
	for _, b := range decl.Binds {
		r.sys.DeclareDatatype(b.Name, len(b.TyVars))
	}
	for _, b := range decl.Binds {
		args := make([]types.Type, len(b.TyVars))
		tyVars := map[string]int{}
		for i, tv := range b.TyVars {
			args[i] = r.sys.Var(i)
			tyVars[tv] = i
		}
		result := r.sys.Named(b.Name, args...)
		for _, c := range b.Cons {
			var argType types.Type
			if c.Of != nil {
				t, err := r.sys.FromAST(c.Of, tyVars)
				if err != nil {
					return nil, &Error{
						Span: decl.Span(),
						Msg:  err.Error(),
					}
				}
				argType = t
			}
			r.sys.DeclareTyCon(c.Name, argType, result)
			conType := result
			if argType != nil {
				conType = r.sys.Fn(argType, result)
			}
			*termMap = append(*termMap, patTerm{
				name: c.Name,
				term: r.typeTerm(conType, map[int]*unify.Var{}),
			})
		}
	}
	r.nodeTerm[decl] = r.primTerm("unit")
	return decl, nil
}

func (r *typeResolver) deducePat(pat ast.Pat,
	termMap *[]patTerm, labelNames []string, v *unify.Var,
) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.AnnotatedPat:
		term, err := r.astTypeTerm(p.Type)
		if err != nil {
			return err
		}
		r.equiv(v, term)
		err = r.deducePat(p.Pat, termMap, nil, v)
		if err != nil {
			return err
		}
		r.reg(pat, v)
		return nil
	case *ast.ConPat:
		return r.deduceConPat(p, termMap, v)
	case *ast.ConsPat:
		vElem := r.u.Variable()
		vList := r.u.Variable()
		err := r.deducePat(p.A0, termMap, nil, vElem)
		if err != nil {
			return err
		}
		err = r.deducePat(p.A1, termMap, nil, vList)
		if err != nil {
			return err
		}
		listTerm := unify.Apply(listTyCon, vElem)
		r.equiv(vList, listTerm)
		r.regEquiv(pat, v, listTerm)
		return nil
	case *ast.IDPat:
		if tc, ok := r.sys.LookupTyCon(p.Name); ok {
			// A constant constructor, e.g. NONE; it binds
			// nothing.
			r.regEquiv(pat, v,
				r.typeTerm(tc.Result, map[int]*unify.Var{}))
			return nil
		}
		*termMap = append(*termMap,
			patTerm{name: p.Name, term: v})
		r.reg(pat, v)
		return nil
	case *ast.LiteralPat:
		return r.deduceLiteral(pat, p.Kind, p.Value, v)
	case *ast.RecordPat:
		return r.deduceRecordPat(p, termMap, labelNames, v)
	case *ast.TuplePat:
		terms := make([]unify.Term, len(p.Args))
		for i, arg := range p.Args {
			vArg := r.u.Variable()
			err := r.deducePat(arg, termMap, nil, vArg)
			if err != nil {
				return err
			}
			terms[i] = vArg
		}
		r.regEquiv(pat, v, r.tupleTerm(terms))
		return nil
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
		env = bind(env, pt.name, pt.term)
	}
	return env
}

func (r *typeResolver) deduceExp(env typeEnv, exp ast.Expr,
	v *unify.Var,
) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.AnnotatedExp:
		term, err := r.astTypeTerm(e.Type)
		if err != nil {
			return err
		}
		r.equiv(v, term)
		err = r.deduceExp(env, e.Exp, v)
		if err != nil {
			return err
		}
		r.reg(exp, v)
		return nil
	case *ast.Apply:
		return r.deduceApply(env, e, v)
	case *ast.Case:
		return r.deduceCase(env, e, v)
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
	case *ast.InfixCall:
		return r.deduceInfix(env, e, v)
	case *ast.Let:
		env2 := env
		for i, d := range e.Decls {
			var termMap []patTerm
			d2, err := r.deduceDecl(env2, d, &termMap)
			if err != nil {
				return err
			}
			e.Decls[i] = d2
			env2 = bindAll(env2, termMap)
		}
		err := r.deduceExp(env2, e.Exp, v)
		if err != nil {
			return err
		}
		r.reg(exp, v)
		return nil
	case *ast.ListExp:
		vElem := r.u.Variable()
		for _, arg := range e.Args {
			err := r.deduceExp(env, arg, vElem)
			if err != nil {
				return err
			}
		}
		r.regEquiv(exp, v, unify.Apply(listTyCon, vElem))
		return nil
	case *ast.Literal:
		return r.deduceLiteral(exp, e.Kind, e.Value, v)
	case *ast.PrefixCall:
		return r.deduceOpCall(env, opNegate, e,
			[]ast.Expr{e.A}, v)
	case *ast.Record:
		return r.deduceRecord(env, e, v)
	case *ast.RecordSelector:
		return &Error{
			Span: e.Span(),
			Msg: "unresolved flex record (can't tell what " +
				"fields there are besides #" + e.Name + ")",
		}
	case *ast.Tuple:
		terms := make([]unify.Term, len(e.Args))
		for i, arg := range e.Args {
			vArg := r.u.Variable()
			err := r.deduceExp(env, arg, vArg)
			if err != nil {
				return err
			}
			terms[i] = vArg
		}
		r.regEquiv(exp, v, r.tupleTerm(terms))
		return nil
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
	case ast.CharLiteralOp, ast.CharLiteralPatOp:
		name = "char"
	case ast.IntLiteralOp, ast.IntLiteralPatOp:
		err := checkIntRange(node, value)
		if err != nil {
			return err
		}
		name = intName
	case ast.RealLiteralOp, ast.RealLiteralPatOp:
		name = realName
	case ast.StringLiteralOp, ast.StringLiteralPatOp:
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
				"' is too large for type " + intName,
		}
	}
	return nil
}

func (r *typeResolver) deduceApply(env typeEnv, apply *ast.Apply,
	v *unify.Var,
) error {
	if id, ok := apply.Fn.(*ast.ID); ok {
		if _, isNumeric := numericOpDomain[id.Name]; isNumeric {
			r.numericCalls = append(r.numericCalls,
				numericCall{name: id.Name, apply: apply})
		}
	}
	vFn := r.u.Variable()
	vArg := r.u.Variable()
	r.equiv(vFn, r.fnTerm(vArg, v))
	if sel, ok := apply.Arg.(*ast.RecordSelector); ok {
		// "apply" is "f #field": "#field" has type "vArg" and
		// also "vRec -> vField"; when vRec resolves we can
		// deduce vField.
		vRec := r.u.Variable()
		vField := r.u.Variable()
		r.selectorAction(sel, vRec, vField)
		r.regEquiv(apply.Arg, vArg, r.fnTerm(vRec, vField))
	} else {
		err := r.deduceExp(env, apply.Arg, vArg)
		if err != nil {
			return err
		}
	}
	if sel, ok := apply.Fn.(*ast.RecordSelector); ok {
		// "apply" is "#field arg": when vArg (the argument
		// type) resolves to a record, we can deduce v.
		r.selectorAction(sel, vArg, v)
	} else {
		err := r.deduceExp(env, apply.Fn, vFn)
		if err != nil {
			return err
		}
	}
	if id, ok := apply.Fn.(*ast.ID); ok {
		if b, isBuiltin := topBuiltins[id.Name]; isBuiltin &&
			b.preferred != "" {
			r.preferred = append(r.preferred,
				preferredType{v: v, prim: b.preferred})
		}
	}
	r.reg(apply, v)
	return nil
}

// selectorAction registers an action for the record selector
// "#field": when the record type vArg becomes known, the
// selector's result type vResult is the field's type.
func (r *typeResolver) selectorAction(sel *ast.RecordSelector,
	vArg, vResult *unify.Var,
) {
	fieldName := sel.Name
	r.actions = append(r.actions, unify.VarAction{
		Var: vArg,
		Action: func(_ *unify.Var, t unify.Term,
			s *unify.Substitution, add func(l, r unify.Term),
		) {
			if fieldType := lookupField(t, fieldName, s); fieldType != nil {
				add(s.Resolve(vResult), fieldType)
			}
		},
	})
}

// deduceRecord handles a record expression, e.g. "{a=1, b=2}" or
// "{e with a=1}".
func (r *typeResolver) deduceRecord(env typeEnv,
	record *ast.Record, v *unify.Var,
) error {
	fields := make([]labelTerm, 0, len(record.Fields))
	byLabel := map[string]ast.Expr{}
	for _, f := range record.Fields {
		label := f.Label
		if label == "" {
			id, ok := f.Exp.(*ast.ID)
			if !ok {
				return &Error{
					Span: record.Span(),
					Msg:  "cannot derive label for expression",
				}
			}
			label = id.Name
		}
		if _, dup := byLabel[label]; dup {
			return &Error{
				Span: record.Span(),
				Msg: "duplicate field '" + label +
					"' in record",
			}
		}
		byLabel[label] = f.Exp
		fields = append(fields, labelTerm{label: label})
	}
	sortFields(fields)
	labelTypes := map[string]unify.Term{}
	for i := range fields {
		vArg := r.u.Variable()
		err := r.deduceExp(env, byLabel[fields[i].label], vArg)
		if err != nil {
			return err
		}
		fields[i].term = vArg
		labelTypes[fields[i].label] = vArg
	}
	if record.With == nil {
		r.regEquiv(record, v, r.recordTerm(fields))
		return nil
	}
	v2 := r.u.Variable()
	err := r.deduceExp(env, record.With, v2)
	if err != nil {
		return err
	}
	// When we know the type of the expression before 'with', we
	// can unify the types of the fields it has in common with
	// the explicit fields.
	r.actions = append(r.actions, unify.VarAction{
		Var: v2,
		Action: func(_ *unify.Var, t unify.Term,
			s *unify.Substitution, add func(l, r unify.Term),
		) {
			seq, ok := t.(*unify.Sequence)
			if !ok {
				return
			}
			for i, fieldName := range fieldList(seq) {
				if labelType, common := labelTypes[fieldName]; common {
					add(s.Resolve(labelType),
						s.Resolve(seq.Terms[i]))
				}
			}
		},
	})
	r.equiv(v, v2)
	r.reg(record, v)
	return nil
}

// deduceRecordPat handles a record pattern, e.g. "{a, b = p}" or
// "{a, ...}".
func (r *typeResolver) deduceRecordPat(pat *ast.RecordPat,
	termMap *[]patTerm, labelNames []string, v *unify.Var,
) error {
	byLabel := map[string]ast.Pat{}
	for _, f := range pat.Fields {
		byLabel[f.Label] = f.Pat
	}
	// The field set is the pattern's own labels or, in a match
	// list, the union of the labels of the sibling patterns.
	if labelNames == nil {
		for _, f := range pat.Fields {
			labelNames = append(labelNames, f.Label)
		}
	}
	fields := make([]labelTerm, len(labelNames))
	for i, label := range labelNames {
		fields[i] = labelTerm{label: label}
	}
	sortFields(fields)
	for i := range fields {
		vArg := r.u.Variable()
		if fieldPat, ok := byLabel[fields[i].label]; ok {
			err := r.deducePat(fieldPat, termMap, nil, vArg)
			if err != nil {
				return err
			}
		}
		fields[i].term = vArg
	}
	term := r.recordTerm(fields)
	if !pat.Ellipsis {
		r.regEquiv(pat, v, term)
		return nil
	}
	// The pattern has an ellipsis, so it matches any record with
	// at least these fields. When the source record's type
	// becomes known, unify the named fields' types.
	labelTypes := map[string]bool{}
	for _, f := range fields {
		labelTypes[f.label] = true
	}
	v2 := r.u.Variable()
	r.equiv(v2, term)
	r.actions = append(r.actions, unify.VarAction{
		Var: v,
		Action: func(_ *unify.Var, t unify.Term,
			s *unify.Substitution, add func(l, r unify.Term),
		) {
			seq, ok := t.(*unify.Sequence)
			if !ok {
				return
			}
			var fields2 []labelTerm
			for i, fieldName := range fieldList(seq) {
				if labelTypes[fieldName] {
					fields2 = append(fields2, labelTerm{
						label: fieldName,
						term:  seq.Terms[i],
					})
				}
			}
			add(s.Resolve(v2), r.recordTerm(fields2))
		},
	})
	r.reg(pat, v)
	return nil
}

// deduceInfix handles an infix operator application. The logical
// connectives type as bool directly; any other operator desugars
// to the application of its top-level binding, "a + b" becoming
// "(op +) (a, b)".
func (r *typeResolver) deduceInfix(env typeEnv, call *ast.InfixCall,
	v *unify.Var,
) error {
	switch call.Kind {
	case ast.AndalsoOp, ast.ImpliesOp, ast.OrelseOp:
		err := r.deduceExp(env, call.A0, v)
		if err != nil {
			return err
		}
		err = r.deduceExp(env, call.A1, v)
		if err != nil {
			return err
		}
		r.regEquiv(call, v, r.primTerm("bool"))
		return nil
	default:
		name, ok := infixOpNames[call.Kind]
		if !ok {
			return &Error{
				Span: call.Span(),
				Msg: "cannot deduce type for " +
					call.Kind.String(),
			}
		}
		return r.deduceOpCall(env, name, call,
			[]ast.Expr{call.A0, call.A1}, v)
	}
}

// deduceOpCall types an operator call as the application of the
// operator's top-level binding to its operands.
func (r *typeResolver) deduceOpCall(env typeEnv, name string,
	call ast.Expr, args []ast.Expr, v *unify.Var,
) error {
	span := call.Span()
	var arg ast.Expr
	if len(args) == 1 {
		arg = args[0]
	} else {
		arg = ast.NewTuple(span, args)
	}
	apply := ast.NewApply(span, ast.NewID(span, name), arg)
	err := r.deduceApply(env, apply, v)
	if err != nil {
		return err
	}
	r.reg(call, v)
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

// deduceCase handles "case exp of pat => exp | ...". Every rule's
// pattern unifies with the scrutinee's type. If any rule has a
// record pattern, all the rules' record patterns share the union
// of their field names, which lets a rule mention only the fields
// it needs.
func (r *typeResolver) deduceCase(env typeEnv, caseExp *ast.Case,
	v *unify.Var,
) error {
	v2 := r.u.Variable()
	err := r.deduceExp(env, caseExp.Exp, v2)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var labelNames []string
	if seq, ok := r.nodeTerm[caseExp.Exp].(*unify.Sequence); ok {
		for _, label := range fieldList(seq) {
			seen[label] = true
			labelNames = append(labelNames, label)
		}
	}
	for _, m := range caseExp.Matches {
		if recordPat, ok := m.Pat.(*ast.RecordPat); ok {
			for _, f := range recordPat.Fields {
				if !seen[f.Label] {
					seen[f.Label] = true
					labelNames = append(labelNames, f.Label)
				}
			}
		}
	}
	err = r.deduceMatchList(env, caseExp.Matches, labelNames,
		v2, v)
	if err != nil {
		return err
	}
	r.reg(caseExp, v)
	return nil
}

// deduceMatchList handles the rules of a case: each rule's
// pattern has the scrutinee's type, and each rule's expression
// has the result type.
func (r *typeResolver) deduceMatchList(env typeEnv,
	matches []*ast.Match, labelNames []string,
	argVariable, resultVariable *unify.Var,
) error {
	for _, m := range matches {
		var termMap []patTerm
		err := r.deducePat(m.Pat, &termMap, labelNames,
			argVariable)
		if err != nil {
			return err
		}
		env2 := bindAll(env, termMap)
		err = r.deduceExp(env2, m.Exp, resultVariable)
		if err != nil {
			return err
		}
	}
	return nil
}

// deduceMatch handles one match rule "pat => exp" of a fn: the
// rule's type is "typeof(pat) -> typeof(exp)".
func (r *typeResolver) deduceMatch(env typeEnv, match *ast.Match,
	argVariable, resultVariable *unify.Var,
) error {
	vPat := r.u.Variable()
	var termMap []patTerm
	err := r.deducePat(match.Pat, &termMap, nil, vPat)
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
