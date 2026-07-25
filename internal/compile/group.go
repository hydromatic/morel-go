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
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/internal/unify"
)

// elementsName is the keyword bound in a "compute" clause to the
// collection of grouped rows.
const elementsName = "elements"

// deduceGroup types a "group" step, optionally combined with the
// "compute" that follows it. The keys are typed like a yield — a
// record exposes its fields, any other expression is a single
// implicitly-labelled field — and the aggregates are typed over
// the collection of grouped rows. The output row is the keys, or
// the keys and aggregates together when there is a compute; its
// orderedness is the input's, so the caller leaves it unchanged.
// It returns the row's fields and element type.
func (r *typeResolver) deduceGroup(stepEnv typeEnv,
	group *ast.GroupStep, compute *ast.ComputeStep,
	inputElem, inputOrd unify.Term,
) ([]labelTerm, unify.Term, error) {
	keyFields, keyElem, err := r.deduceYield(stepEnv, group.Exp)
	if err != nil {
		return nil, nil, err
	}
	fields := keyFields
	elem := keyElem
	if compute != nil {
		aggFields, err := r.deduceCompute(stepEnv, compute.Exp,
			keyFields, inputElem, inputOrd)
		if err != nil {
			return nil, nil, err
		}
		fields = append(append([]labelTerm{}, keyFields...), aggFields...)
		elem = r.rowElem(fields)
	}
	if group.Binder != "" {
		// A binder names the whole group row: a record of the key and
		// aggregate fields, or the sole field's bare value when the
		// group is an atom. The plain rowElem collapse above would
		// wrongly drop a singleton record (group g = {} compute {c =
		// ...}) to a bare value.
		if groupRowIsAtom(group, compute, len(fields)) {
			elem = fields[0].term
		} else {
			sorted := make([]labelTerm, len(fields))
			copy(sorted, fields)
			sortFields(sorted)
			elem = r.recordTerm(sorted)
		}
		fields = []labelTerm{{label: group.Binder, term: elem}}
	}
	return fields, elem, nil
}

// deduceCompute types a "compute" clause — a record of aggregates,
// or a single aggregate — returning the aggregate fields. The
// clause is typed in an environment extended with the group keys
// and "elements" bound to the input collection.
func (r *typeResolver) deduceCompute(stepEnv typeEnv,
	computeExp ast.Expr, keyFields []labelTerm,
	inputElem, inputOrd unify.Term,
) ([]labelTerm, error) {
	elementsColl := r.collectionTerm(inputElem, inputOrd)
	aggEnv := bind(bindFields(stepEnv, keyFields), elementsName,
		elementsColl)
	if rec, ok := computeExp.(*ast.Record); ok && rec.With == nil {
		fields := make([]labelTerm, len(rec.Fields))
		for i, f := range rec.Fields {
			label := f.Label
			if label == "" {
				label = implicitLabel(f.Exp)
			}
			v := r.u.Variable()
			err := r.deduceAggregate(aggEnv, f.Exp, inputElem, inputOrd, v)
			if err != nil {
				return nil, err
			}
			// Register the aggregate expression's type so the
			// resolver can read it back when building the group.
			r.reg(f.Exp, v)
			fields[i] = labelTerm{label: label, term: v}
		}
		return fields, nil
	}
	label := implicitLabel(computeExp)
	if label == "" {
		label = currentName
	}
	v := r.u.Variable()
	err := r.deduceAggregate(aggEnv, computeExp, inputElem, inputOrd, v)
	if err != nil {
		return nil, err
	}
	r.reg(computeExp, v)
	return []labelTerm{{label: label, term: v}}, nil
}

// deduceAggregate types an aggregate "fn over arg" (or a bare "fn",
// which aggregates the whole input), binding its result to v. The
// aggregate function maps a collection of the argument's type to
// the result; how that collection's orderedness links to the input
// is decided by the function's kind, so that a bag-parameter
// aggregate (sum, count) accepts a list input.
func (r *typeResolver) deduceAggregate(aggEnv typeEnv,
	aggExp ast.Expr, inputElem, inputOrd unify.Term, v *unify.Var,
) error {
	fnExp, argExp := aggExp, ast.Expr(nil)
	if ic, ok := aggExp.(*ast.InfixCall); ok && ic.Kind == ast.OverOp {
		fnExp, argExp = ic.A0, ic.A1
	}
	vArg := r.u.Variable()
	if argExp == nil {
		// A bare aggregate is applied to the input rows themselves.
		r.equiv(vArg, inputElem)
	} else {
		err := r.deduceExp(aggEnv, argExp, vArg)
		if err != nil {
			return err
		}
	}
	vAgg := r.u.Variable()
	err := r.deduceExp(aggEnv, fnExp, vAgg)
	if err != nil {
		return err
	}
	cArg := r.u.Variable()
	r.equiv(vAgg, r.fnTerm(cArg, v))
	// lint: sort until '^	}' where '^	case '
	switch r.aggKind(fnExp) {
	case aggBag:
		// A bag-parameter aggregate is decoupled from the input's
		// orderedness, so it accepts a list input too.
		r.equiv(cArg, r.bagTerm(vArg))
	case aggList:
		r.equiv(cArg, r.listTerm(vArg))
	case aggPolymorphic:
		// Link the aggregate's collection to the input's orderedness.
		r.equiv(cArg, r.collectionTerm(vArg, r.asOrdVar(inputOrd)))
	case aggUserUnknown:
		// Leave the orderedness free; the function's own type decides.
		r.equiv(cArg, r.collectionTerm(vArg, r.u.Variable()))
	}
	return nil
}

// aggKindT classifies how an aggregate function's parameter links
// to the input collection's orderedness.
type aggKindT int

const (
	// aggUserUnknown is a name with no known type (a user function);
	// its orderedness is left free.
	aggUserUnknown aggKindT = iota
	// aggPolymorphic is an overloaded or otherwise unknown function;
	// its orderedness follows the input's.
	aggPolymorphic
	// aggBag is a function whose parameter is a bag.
	aggBag
	// aggList is a function whose parameter is a list.
	aggList
)

// aggKind classifies an aggregate function expression by its
// declared type: a named top-level function whose parameter is a
// bag or a list, an unknown name, or otherwise polymorphic.
func (r *typeResolver) aggKind(fnExp ast.Expr) aggKindT {
	id, ok := fnExp.(*ast.ID)
	if !ok {
		return aggPolymorphic
	}
	b, ok := r.bindings[id.Name]
	if !ok {
		return aggUserUnknown
	}
	return aggKindOfType(b.Type)
}

// aggKindOfType returns the aggregate kind of a function type: bag
// or list if its parameter is one, otherwise polymorphic.
func aggKindOfType(t types.Type) aggKindT {
	fn, ok := t.(*types.Fn)
	if !ok {
		return aggPolymorphic
	}
	switch p := fn.Param.(type) {
	case *types.List:
		return aggList
	case *types.Named:
		if p.Name == bagTyCon && len(p.Args) == 1 {
			return aggBag
		}
	}
	return aggPolymorphic
}
