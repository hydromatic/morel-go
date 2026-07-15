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

// deduceFrom types a query expression over lists: "from pat in
// exp" scans, "where exp" filters, and "yield exp" transforms.
// Its rows carry a set of named fields; the query's element type
// is the sole field's type when exactly one field is bound, and
// a record of the fields otherwise. A "yield" of a record literal
// exposes that record's fields to later steps; any other yield
// leaves no named fields.
//
// Bags, joins, group/compute, and the other step forms are not
// yet supported and fall through to the "cannot deduce" error.
func (r *typeResolver) deduceFrom(rootEnv typeEnv, from *ast.From,
	v *unify.Var,
) error {
	if from.Kind != ast.FromOp {
		return r.unsupportedFrom(from)
	}
	var fields []labelTerm
	// elem is set by an explicit yield; otherwise the element
	// type is derived from the fields at the end.
	var elem unify.Term
	env := rootEnv
	for _, step := range from.Steps {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch s := step.(type) {
		case *ast.Scan:
			newFields, err := r.deduceScan(env, s)
			if err != nil {
				return err
			}
			fields = append(fields, newFields...)
			elem = nil
			env = bindFields(rootEnv, fields)
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
			elem = vYield
			env = bindFields(rootEnv, fields)
		default:
			return r.unsupportedFrom(from)
		}
	}
	if elem == nil {
		elem = r.rowElem(fields)
	}
	r.regEquiv(from, v, unify.Apply(listTyCon, elem))
	return nil
}

// deduceScan types a scan and returns the fields its pattern
// binds. "pat in exp" iterates a list, binding the pattern to the
// element type; "pat = exp" binds the pattern to the value of
// exp. An unbounded scan, or one with a join condition, is not
// yet supported.
func (r *typeResolver) deduceScan(env typeEnv, scan *ast.Scan,
) ([]labelTerm, error) {
	if scan.On != nil ||
		scan.Kind != ast.ScanIn && scan.Kind != ast.ScanEq {
		return nil, &Error{
			Span: scan.Span(),
			Msg:  "cannot deduce type for " + scan.Op().String(),
		}
	}
	vElem := r.u.Variable()
	if scan.Kind == ast.ScanIn {
		vColl := r.u.Variable()
		err := r.deduceExp(env, scan.Exp, vColl)
		if err != nil {
			return nil, err
		}
		r.equiv(vColl, unify.Apply(listTyCon, vElem))
	} else {
		err := r.deduceExp(env, scan.Exp, vElem)
		if err != nil {
			return nil, err
		}
	}
	var termMap []patTerm
	err := r.deducePat(scan.Pat, &termMap, nil, vElem)
	if err != nil {
		return nil, err
	}
	fields := make([]labelTerm, len(termMap))
	for i, pt := range termMap {
		fields[i] = labelTerm{label: pt.name, term: pt.term}
	}
	return fields, nil
}

// deduceYield types a "yield exp" step, returning the fields the
// yielded value exposes to later steps and its element type. A
// record literal exposes its fields by name; anything else
// exposes none.
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
	return nil, vYield, nil
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

func (r *typeResolver) unsupportedFrom(from *ast.From) error {
	return &Error{
		Span: from.Span(),
		Msg:  "cannot deduce type for " + from.Op().String(),
	}
}
