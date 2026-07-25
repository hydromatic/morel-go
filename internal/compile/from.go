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

// fromState is the state threaded through a query's steps: the
// current row's fields, its element type (curElem, nil to derive
// from the fields), the orderedness so far (nil until the first
// collection scan), the environment steps are typed in, and, once a
// standalone "compute" has run, the scalar result it produced.
type fromState struct {
	r       *typeResolver
	rootEnv typeEnv
	env     typeEnv
	fields  []labelTerm
	curElem unify.Term
	ord     unify.Term
	scalar  unify.Term
}

// elemTerm resolves the current element type: the explicit element
// if set, otherwise the sole field's type or a record of them.
func (st *fromState) elemTerm() unify.Term {
	if st.curElem != nil {
		return st.curElem
	}
	return st.r.rowElem(st.fields)
}

// deduceFrom types a query expression: "from pat in exp" scans a
// collection, "pat = exp" binds a value, "where" filters, "yield"
// transforms, "group"/"compute" aggregate, and the passthrough and
// forcer steps adjust orderedness. Its rows carry named fields; the
// element type is the sole field's type when one field is bound and
// a record of them otherwise.
//
// The query carries an orderedness that decides whether its result
// is a list or a bag: an empty "from" is an ordered "unit list";
// the first scan shares its source's orderedness; a comma scan
// meets the two; "order" forces a list and "unorder" a bag. A
// standalone "compute" produces a scalar rather than a collection.
func (r *typeResolver) deduceFrom(rootEnv typeEnv, from *ast.From,
	v *unify.Var,
) error {
	err := checkStepPlacement(from)
	if err != nil {
		return err
	}
	st := &fromState{r: r, rootEnv: rootEnv, env: rootEnv}
	steps := from.Steps
	for i := 0; i < len(steps); i++ {
		// A "group" absorbs the "compute" that follows it, so the
		// aggregates are typed over the pre-group rows.
		if g, ok := steps[i].(*ast.GroupStep); ok {
			var compute *ast.ComputeStep
			if i+1 < len(steps) {
				if c, ok := steps[i+1].(*ast.ComputeStep); ok {
					compute = c
					i++
				}
			}
			err := st.groupStep(g, compute)
			if err != nil {
				return err
			}
			continue
		}
		err := st.step(steps[i])
		if err != nil {
			return err
		}
	}
	// "exists" and "forall" reduce a query to a boolean; the last
	// step of a "forall" must be "require".
	if from.Kind == ast.ForallOp {
		err := checkForallRequire(from)
		if err != nil {
			return err
		}
	}
	if from.Kind == ast.ExistsOp || from.Kind == ast.ForallOp {
		r.regEquiv(from, v, r.primTerm(boolName))
		return nil
	}
	// "into" and a standalone "compute" reduce the query to a scalar.
	if st.scalar != nil {
		r.regEquiv(from, v, st.scalar)
		return nil
	}
	// An empty "from", or one with only scalar scans, is ordered.
	ord := st.ord
	if ord == nil {
		ord = r.u.Atom(orderedName)
	}
	r.regEquiv(from, v, r.collectionTerm(st.elemTerm(), ord))
	return nil
}

// checkStepPlacement reports the first misplaced "compute", "into",
// or "require" step: "compute" and "into" belong only in a "from",
// "require" only in a "forall", and each must be the query's last
// step. A "compute" absorbed by a preceding "group" is part of
// that group, so it is exempt.
func checkStepPlacement(from *ast.From) error {
	kind := queryKindName(from.Kind)
	steps := from.Steps
	for i, step := range steps {
		var op string
		var wrongContainer bool
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch step.(type) {
		case *ast.ComputeStep:
			if i > 0 {
				if _, ok := steps[i-1].(*ast.GroupStep); ok {
					continue
				}
			}
			op, wrongContainer = "compute", from.Kind != ast.FromOp
		case *ast.IntoStep:
			op, wrongContainer = "into", from.Kind != ast.FromOp
		case *ast.RequireStep:
			op, wrongContainer = "require", from.Kind != ast.ForallOp
		default:
			continue
		}
		if wrongContainer {
			return &Error{
				Span: step.Span(),
				Msg:  "'" + op + "' step must not occur in '" + kind + "'",
			}
		}
		if i != len(steps)-1 {
			return &Error{
				Span: steps[i+1].Span(),
				Msg:  "'" + op + "' step must be last in '" + kind + "'",
			}
		}
	}
	return nil
}

// queryKindName is the lowercase name of a query's kind, for error
// messages: "from", "exists", or "forall".
func queryKindName(kind ast.Op) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ast.ExistsOp:
		return "exists"
	case ast.ForallOp:
		return "forall"
	default:
		return "from"
	}
}

// checkForallRequire reports an error unless a "forall" query's
// last step is "require".
func checkForallRequire(from *ast.From) error {
	var last ast.FromStep
	if len(from.Steps) > 0 {
		last = from.Steps[len(from.Steps)-1]
	}
	if _, ok := last.(*ast.RequireStep); !ok {
		span := from.Span()
		if last != nil {
			span = last.Span()
		}
		return &Error{
			Span: span,
			Msg:  "last step of 'forall' must be 'require'",
		}
	}
	return nil
}

// step types one query step (other than a group), updating st.
func (st *fromState) step(step ast.FromStep) error {
	r := st.r
	// lint: sort until '^\t}' where '^\tcase '
	switch s := step.(type) {
	case *ast.ComputeStep:
		// A standalone "compute" (not absorbed by a group) reduces
		// the whole input to a scalar.
		fields, err := r.deduceCompute(st.env, s.Exp, nil,
			st.elemTerm(), r.orDefaultOrd(st.ord))
		if err != nil {
			return err
		}
		st.scalar = r.rowElem(fields)
		return nil
	case *ast.DistinctStep:
		return nil
	case *ast.IntoStep:
		return st.intoStep(s)
	case *ast.OrderStep:
		err := r.deduceExp(st.env, s.Exp, r.u.Variable())
		if err != nil {
			return err
		}
		st.ord = r.u.Atom(orderedName)
		return nil
	case *ast.RequireStep:
		return st.boolStep(s.Exp)
	case *ast.Scan:
		return st.scanStep(s)
	case *ast.SetOpStep:
		newOrd, err := r.deduceSetOp(st.rootEnv, st.elemTerm(),
			r.orDefaultOrd(st.ord), s.Exps)
		if err != nil {
			return err
		}
		st.ord = newOrd
		return nil
	case *ast.SkipStep:
		return r.deduceCount(st.rootEnv, s.Exp)
	case *ast.TakeStep:
		return r.deduceCount(st.rootEnv, s.Exp)
	case *ast.ThroughStep:
		return st.throughStep(s)
	case *ast.UnorderStep:
		st.ord = r.u.Atom(unorderedName)
		return nil
	case *ast.WhereStep:
		return st.boolStep(s.Exp)
	case *ast.YieldAllStep:
		return st.yieldAllStep(s)
	case *ast.YieldStep:
		return st.yieldStep(s)
	default:
		return r.unsupportedStep(step)
	}
}

// scanStep types a scan step. An outer join's source must be
// independent for right and full joins (it is typed in the root
// scope), its "on" condition sees the unwrapped types, and the
// nullable side's variables become option-typed downstream: the
// newly scanned ones for a left join, the earlier ones for a
// right join, both for a full join.
func (st *fromState) scanStep(s *ast.Scan) error {
	r := st.r
	env := st.env
	if optionalizesLeft(s.Join) && s.Exp != nil {
		inputNames := map[string]bool{}
		for _, f := range st.fields {
			inputNames[f.label] = true
		}
		err := checkJoinIndependent(s.Exp, inputNames)
		if err != nil {
			return err
		}
		env = st.rootEnv
	}
	newFields, sourceOrd, err := r.deduceScan(env, st.env, s)
	if err != nil {
		return err
	}
	st.ord = r.meetSourceOrd(st.ord, sourceOrd)
	if s.Kind == ast.ScanUnbounded {
		// Iterating all values of a type has no order, so an
		// unbounded scan makes the whole query a bag.
		st.ord = r.u.Atom(unorderedName)
	}
	if optionalizesLeft(s.Join) {
		for i := range st.fields {
			st.fields[i].term = optionTerm(st.fields[i].term)
		}
	}
	if optionalizesRight(s.Join) {
		for i := range newFields {
			newFields[i].term = optionTerm(newFields[i].term)
		}
	}
	st.fields = append(st.fields, newFields...)
	st.curElem = nil
	st.env = r.bindStep(st.rootEnv, st.fields, nil)
	return nil
}

// groupStep types a "group" step and its optional "compute",
// updating st; the output orderedness is the input's (unchanged).
func (st *fromState) groupStep(g *ast.GroupStep,
	compute *ast.ComputeStep,
) error {
	fields, elem, err := st.r.deduceGroup(st.env, g,
		compute, st.elemTerm(), st.r.orDefaultOrd(st.ord))
	if err != nil {
		return err
	}
	st.fields = fields
	st.curElem = elem
	st.env = st.r.bindStep(st.rootEnv, fields, elem)
	return nil
}

// boolStep types a step whose expression must be a boolean, such as
// "where" or "require".
func (st *fromState) boolStep(exp ast.Expr) error {
	vBool := st.r.u.Variable()
	err := st.r.deduceExp(st.env, exp, vBool)
	if err != nil {
		return err
	}
	st.r.equiv(vBool, st.r.primTerm(boolName))
	return nil
}

// intoStep types "into f": f reduces the whole input collection to
// a scalar, adapting to the input's orderedness by the function's
// kind, exactly as an aggregate does.
func (st *fromState) intoStep(s *ast.IntoStep) error {
	rv := st.r.u.Variable()
	// "into f" applies f to the whole result, so f is typed in the
	// root scope; the query variables are out of scope inside it.
	err := st.r.deduceAggregate(st.rootEnv, s.Exp, st.elemTerm(),
		st.r.orDefaultOrd(st.ord), rv)
	if err != nil {
		return err
	}
	st.scalar = rv
	return nil
}

// throughStep types "through pat in f": f maps the input collection
// to a new one, pat binds the new element, and the result's
// orderedness comes from f's result type.
func (st *fromState) throughStep(s *ast.ThroughStep) error {
	r := st.r
	elem := r.u.Variable()
	var termMap []patTerm
	err := r.deducePat(s.Pat, &termMap, nil, elem)
	if err != nil {
		return err
	}
	inColl := r.collectionTerm(st.elemTerm(),
		r.asOrdVar(r.orDefaultOrd(st.ord)))
	outOrd := r.u.Variable()
	vFn := r.u.Variable()
	err = r.deduceExp(st.env, s.Exp, vFn)
	if err != nil {
		return err
	}
	r.equiv(vFn, r.fnTerm(inColl, r.collectionTerm(elem, outOrd)))
	fields := make([]labelTerm, len(termMap))
	for i, pt := range termMap {
		fields[i] = labelTerm{label: pt.name, term: pt.term}
	}
	st.fields = fields
	st.curElem = elem
	st.ord = outOrd
	st.env = r.bindStep(st.rootEnv, fields, elem)
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
// and contributes no orderedness. A sourceless scan iterates all
// values of the pattern's type — inference against the later
// steps decides what that is — and contributes no orderedness
// here; the query itself becomes unordered. A "join ... on"
// condition is a boolean typed over the earlier fields and the
// pattern this scan binds.
func (r *typeResolver) deduceScan(env, onEnv typeEnv,
	scan *ast.Scan,
) ([]labelTerm, *unify.Var, error) {
	vElem := r.u.Variable()
	var sourceOrd *unify.Var
	// lint: sort until '^\t}' where '^\tcase '
	switch scan.Kind {
	case ast.ScanEq:
		err := r.deduceExp(env, scan.Exp, vElem)
		if err != nil {
			return nil, nil, err
		}
	case ast.ScanIn:
		vColl := r.u.Variable()
		err := r.deduceExp(env, scan.Exp, vColl)
		if err != nil {
			return nil, nil, err
		}
		sourceOrd = r.u.Variable()
		r.equiv(vColl, r.collectionTerm(vElem, sourceOrd))
	case ast.ScanUnbounded:
		// The pattern's type is free here.
	default:
		return nil, nil, &Error{
			Span: scan.Span(),
			Msg:  "cannot deduce type for " + scan.Op().String(),
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
		// this scan binds — unwrapped, for an outer join — and
		// must be a boolean.
		vBool := r.u.Variable()
		err := r.deduceExp(bindFields(onEnv, fields), scan.On, vBool)
		if err != nil {
			return nil, nil, err
		}
		r.equiv(vBool, r.primTerm(boolName))
	}
	return fields, sourceOrd, nil
}

// yieldAllStep types "yieldAll [binder in] exp": exp is a
// collection whose element becomes the row, named by the binder
// or "current"; the query's orderedness meets the collection's.
func (st *fromState) yieldAllStep(s *ast.YieldAllStep) error {
	r := st.r
	vElem := r.u.Variable()
	vColl := r.u.Variable()
	err := r.deduceExp(st.env, s.Exp, vColl)
	if err != nil {
		return err
	}
	sourceOrd := r.u.Variable()
	r.equiv(vColl, r.collectionTerm(vElem, sourceOrd))
	st.ord = r.meetSourceOrd(st.ord, sourceOrd)
	label := s.Binder
	if label == "" {
		label = currentName
	}
	st.fields = []labelTerm{{label: label, term: vElem}}
	st.curElem = vElem
	st.env = r.bindStep(st.rootEnv, st.fields, vElem)
	return nil
}

// yieldStep types a "yield [binder =] exp" step, updating the
// current fields, element type, and environment. A binder names the
// whole row: it exposes a single field of the binder's name and the
// value's own (unwrapped) type, so bare field names go out of scope.
func (st *fromState) yieldStep(s *ast.YieldStep) error {
	if s.Binder != "" {
		vYield := st.r.u.Variable()
		err := st.r.deduceExp(st.env, s.Exp, vYield)
		if err != nil {
			return err
		}
		st.fields = []labelTerm{{label: s.Binder, term: vYield}}
		st.curElem = vYield
		st.env = st.r.bindStep(st.rootEnv, st.fields, vYield)
		return nil
	}
	yieldFields, vYield, err := st.r.deduceYield(st.env, s.Exp)
	if err != nil {
		return err
	}
	st.fields = yieldFields
	st.curElem = vYield
	st.env = st.r.bindStep(st.rootEnv, st.fields, vYield)
	return nil
}

// deduceYield types the value a "yield" (or group key) exposes,
// returning the fields it exposes to later steps and its element
// type. A record literal exposes its fields by name; any other
// expression exposes a single field, labelled by its implicit
// label — the name for "yield x", the field for "yield e.b" — or
// "current" when it has none.
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
	case *ast.Elements:
		return elementsName
	case *ast.ID:
		return e.Name
	case *ast.InfixCall:
		// An aggregate "fn over e" takes its label from the function.
		if e.Kind == ast.OverOp {
			return implicitLabel(e.A0)
		}
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

// unsupportedStep reports a query step whose form is not yet
// supported.
func (r *typeResolver) unsupportedStep(step ast.FromStep) error {
	return &Error{
		Span: step.Span(),
		Msg:  "cannot deduce type for " + step.Op().String(),
	}
}
