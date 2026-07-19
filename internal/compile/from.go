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
	"github.com/hydromatic/morel-go/internal/unify"
)

// deduceFrom types a query expression: "from pat in exp" scans a
// collection, "pat = exp" binds a value, "where exp" filters, and
// "yield exp" transforms. Its rows carry a set of named fields;
// the query's element type is the sole field's type when exactly
// one field is bound, and a record of the fields otherwise. A
// "yield" of a record literal exposes that record's fields to
// later steps; any other yield leaves no named fields.
//
// The query carries an orderedness that decides whether its result
// is a list or a bag: an empty "from" is an ordered "unit list";
// the first scan shares its source's orderedness (so "from x in
// aBag" is a bag); a further comma scan meets the two (a list only
// if both are lists); "where" and "yield" pass orderedness
// through. Group/compute, joins with conditions, and the other
// step forms are not yet supported and fall through to the "cannot
// deduce" error.
func (r *typeResolver) deduceFrom(rootEnv typeEnv, from *ast.From,
	v *unify.Var,
) error {
	if from.Kind != ast.FromOp {
		return r.unsupportedFrom(from)
	}
	var fields []labelTerm
	// curElem is the current element type; nil means derive it from
	// the fields (the sole field's type, or a record of them).
	var curElem unify.Term
	// ord is the query's orderedness so far, nil until the first
	// collection scan sets it.
	var ord unify.Term
	// elemTerm resolves the current element type.
	elemTerm := func() unify.Term {
		if curElem != nil {
			return curElem
		}
		return r.rowElem(fields)
	}
	env := rootEnv
	for _, step := range from.Steps {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch s := step.(type) {
		case *ast.DistinctStep:
			// "distinct" changes neither element type nor orderedness.
		case *ast.OrderStep:
			// The sort key is typed in the step env; "order" forces
			// the result to be a list.
			err := r.deduceExp(env, s.Exp, r.u.Variable())
			if err != nil {
				return err
			}
			ord = r.u.Atom(orderedName)
		case *ast.Scan:
			newFields, sourceOrd, err := r.deduceScan(env, s)
			if err != nil {
				return err
			}
			ord = r.meetSourceOrd(ord, sourceOrd)
			fields = append(fields, newFields...)
			curElem = nil
			env = r.bindStep(rootEnv, fields, nil)
		case *ast.SetOpStep:
			newOrd, err := r.deduceSetOp(rootEnv, elemTerm(),
				r.orDefaultOrd(ord), s.Exps)
			if err != nil {
				return err
			}
			ord = newOrd
		case *ast.SkipStep:
			err := r.deduceCount(rootEnv, s.Exp)
			if err != nil {
				return err
			}
		case *ast.TakeStep:
			err := r.deduceCount(rootEnv, s.Exp)
			if err != nil {
				return err
			}
		case *ast.UnorderStep:
			ord = r.u.Atom(unorderedName)
		case *ast.WhereStep:
			vBool := r.u.Variable()
			err := r.deduceExp(env, s.Exp, vBool)
			if err != nil {
				return err
			}
			r.equiv(vBool, r.primTerm(boolName))
		case *ast.YieldStep:
			yieldFields, vYield, err := r.deduceYield(env, s.Exp)
			if err != nil {
				return err
			}
			fields = yieldFields
			curElem = vYield
			env = r.bindStep(rootEnv, fields, vYield)
		default:
			return r.unsupportedFrom(from)
		}
	}
	// An empty "from", or one with only scalar scans, is ordered.
	if ord == nil {
		ord = r.u.Atom(orderedName)
	}
	r.regEquiv(from, v, r.collectionTerm(elemTerm(), ord))
	return nil
}

// deduceSetOp types a "union"/"intersect"/"except" step: each
// argument is a collection of the same element type, typed in the
// root env, and the result is a list only if the input and every
// argument are. It returns the result's orderedness.
func (r *typeResolver) deduceSetOp(rootEnv typeEnv, elem,
	inputOrd unify.Term, exps []ast.Expr,
) (unify.Term, error) {
	ords := []*unify.Var{r.asOrdVar(inputOrd)}
	for _, arg := range exps {
		vColl := r.u.Variable()
		err := r.deduceExp(rootEnv, arg, vColl)
		if err != nil {
			return nil, err
		}
		argOrd := r.u.Variable()
		r.equiv(vColl, r.collectionTerm(elem, argOrd))
		ords = append(ords, argOrd)
	}
	return r.naryMeetOrderedness(ords), nil
}

// deduceCount types a "skip" or "take" count, which is evaluated in
// the root environment (not the step env) and must be an int.
func (r *typeResolver) deduceCount(rootEnv typeEnv, exp ast.Expr,
) error {
	vCount := r.u.Variable()
	err := r.deduceExp(rootEnv, exp, vCount)
	if err != nil {
		return err
	}
	r.equiv(vCount, r.primTerm(intName))
	return nil
}

// meetSourceOrd folds a scan source's orderedness into the query's
// orderedness so far: the first collection scan adopts the source's
// orderedness, a later one meets the two (a list only if both are);
// a scalar scan (sourceOrd nil) leaves it unchanged.
func (r *typeResolver) meetSourceOrd(ord unify.Term,
	sourceOrd *unify.Var,
) unify.Term {
	switch {
	case sourceOrd == nil:
		return ord
	case ord == nil:
		return sourceOrd
	default:
		meet := r.u.Variable()
		r.meetOrderedness(meet, r.asOrdVar(ord), sourceOrd)
		return meet
	}
}

// asOrdVar returns an orderedness term as a variable, wrapping a
// concrete atom in a fresh variable so it can feed a meet.
func (r *typeResolver) asOrdVar(ord unify.Term) *unify.Var {
	if v, ok := ord.(*unify.Var); ok {
		return v
	}
	v := r.u.Variable()
	r.equiv(v, ord)
	return v
}

// orDefaultOrd returns ord, or the "ordered" atom when ord is nil.
func (r *typeResolver) orDefaultOrd(ord unify.Term) unify.Term {
	if ord == nil {
		return r.u.Atom(orderedName)
	}
	return ord
}

// naryMeetOrderedness returns the meet of several orderednesses: a
// list only if all are lists.
func (r *typeResolver) naryMeetOrderedness(ords []*unify.Var,
) unify.Term {
	result := ords[0]
	for _, o := range ords[1:] {
		meet := r.u.Variable()
		r.meetOrderedness(meet, result, o)
		result = meet
	}
	return result
}

// deduceScan types a scan, returning the fields its pattern binds
// and, for a collection scan, its source's orderedness (nil for a
// scalar scan). "pat in exp" iterates a collection, binding the
// pattern to the element type and sharing the collection's
// orderedness; "pat = exp" binds the pattern to the value of exp
// and contributes no orderedness. A "join ... on" condition is a
// boolean typed over the earlier fields and the pattern this scan
// binds. An unbounded scan is not yet supported.
func (r *typeResolver) deduceScan(env typeEnv, scan *ast.Scan,
) ([]labelTerm, *unify.Var, error) {
	if scan.Kind != ast.ScanIn && scan.Kind != ast.ScanEq {
		return nil, nil, &Error{
			Span: scan.Span(),
			Msg:  "cannot deduce type for " + scan.Op().String(),
		}
	}
	vElem := r.u.Variable()
	var sourceOrd *unify.Var
	if scan.Kind == ast.ScanIn {
		vColl := r.u.Variable()
		err := r.deduceExp(env, scan.Exp, vColl)
		if err != nil {
			return nil, nil, err
		}
		sourceOrd = r.u.Variable()
		r.equiv(vColl, r.collectionTerm(vElem, sourceOrd))
	} else {
		err := r.deduceExp(env, scan.Exp, vElem)
		if err != nil {
			return nil, nil, err
		}
	}
	var termMap []patTerm
	err := r.deducePat(scan.Pat, &termMap, nil, vElem)
	if err != nil {
		return nil, nil, err
	}
	fields := make([]labelTerm, len(termMap))
	for i, pt := range termMap {
		fields[i] = labelTerm{label: pt.name, term: pt.term}
	}
	if scan.On != nil {
		// The join condition sees the earlier fields and the ones
		// this scan binds, and must be a boolean.
		vBool := r.u.Variable()
		err := r.deduceExp(bindFields(env, fields), scan.On, vBool)
		if err != nil {
			return nil, nil, err
		}
		r.equiv(vBool, r.primTerm(boolName))
	}
	return fields, sourceOrd, nil
}

// deduceYield types a "yield exp" step, returning the fields the
// yielded value exposes to later steps and its element type. A
// record literal exposes its fields by name; any other expression
// exposes a single field, labelled by its implicit label — the
// name for "yield x", the field for "yield e.b" — or "current"
// when it has none.
func (r *typeResolver) deduceYield(env typeEnv, exp ast.Expr,
) ([]labelTerm, unify.Term, error) {
	if rec, ok := exp.(*ast.Record); ok && rec.With == nil {
		fields, err := r.deduceRecordFields(env, rec)
		if err != nil {
			return nil, nil, err
		}
		term := r.recordTerm(fields)
		r.reg2(rec, term)
		return fields, term, nil
	}
	vYield := r.u.Variable()
	err := r.deduceExp(env, exp, vYield)
	if err != nil {
		return nil, nil, err
	}
	label := implicitLabel(exp)
	if label == "" {
		label = currentName
	}
	return []labelTerm{{label: label, term: vYield}}, vYield, nil
}

// implicitLabel is the label a query step derives from an
// expression when none is given: the name of an identifier
// ("yield x" is field x), the field of a selection ("yield e.b" is
// field b), or "" when the expression has no implicit label.
func implicitLabel(exp ast.Expr) string {
	// lint: sort until '^	}' where '^	case '
	switch e := exp.(type) {
	case *ast.Apply:
		if sel, ok := e.Fn.(*ast.RecordSelector); ok {
			return sel.Name
		}
	case *ast.ID:
		return e.Name
	}
	return ""
}

// rowElem is the element type of a query whose rows have the
// given fields: the sole field's type for one field, a record of
// the fields otherwise (unit for none).
func (r *typeResolver) rowElem(fields []labelTerm) unify.Term {
	if len(fields) == 1 {
		return fields[0].term
	}
	sorted := make([]labelTerm, len(fields))
	copy(sorted, fields)
	sortFields(sorted)
	return r.recordTerm(sorted)
}

// bindFields extends env with each field as a binding.
func bindFields(env typeEnv, fields []labelTerm) typeEnv {
	for _, f := range fields {
		env = bind(env, f.label, f.term)
	}
	return env
}

// Keywords bound in each query step: "current" is the current row,
// "ordinal" its position.
const (
	currentName = "current"
	ordinalName = "ordinal"
)

// bindStep is the environment a query step is typed in: the root
// environment extended with each field, with "current" bound to
// the step's row — the sole field's type for one field, a record of
// the fields otherwise, or an explicit element (a yield's value) —
// and "ordinal" bound to an int.
func (r *typeResolver) bindStep(rootEnv typeEnv, fields []labelTerm,
	elem unify.Term,
) typeEnv {
	env := bindFields(rootEnv, fields)
	if elem == nil {
		elem = r.rowElem(fields)
	}
	env = bind(env, currentName, elem)
	return bind(env, ordinalName, r.primTerm(intName))
}

func (r *typeResolver) unsupportedFrom(from *ast.From) error {
	return &Error{
		Span: from.Span(),
		Msg:  "cannot deduce type for " + from.Op().String(),
	}
}
