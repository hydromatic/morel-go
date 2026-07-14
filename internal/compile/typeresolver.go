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
	case *ast.RecordPat:
		return r.deduceRecordPat(p, termMap, v)
	case *ast.TuplePat:
		terms := make([]unify.Term, len(p.Args))
		for i, arg := range p.Args {
			vArg := r.u.Variable()
			err := r.deducePat(arg, termMap, vArg)
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
	termMap *[]patTerm, v *unify.Var,
) error {
	fields := make([]labelTerm, len(pat.Fields))
	byLabel := map[string]ast.Pat{}
	for i, f := range pat.Fields {
		fields[i] = labelTerm{label: f.Label}
		byLabel[f.Label] = f.Pat
	}
	sortFields(fields)
	for i := range fields {
		vArg := r.u.Variable()
		err := r.deducePat(byLabel[fields[i].label], termMap,
			vArg)
		if err != nil {
			return err
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
