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

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
)

// toAggNode converts one hoisted aggregate of a compute field: an
// "over" aggregate, whose function sees the group keys and whose
// argument ranges over the pre-group rows; or "elements", which is
// the identity aggregate over the whole row ("Fn.id over
// current").
func (r *resolver) toAggNode(cur, keyEnv *coreEnv,
	rowVars []*core.IDPat, n ast.Node, label string,
) (core.GroupAgg, error) {
	t, err := r.typeMap.TypeOf(n)
	if err != nil {
		return core.GroupAgg{}, err
	}
	switch n := n.(type) {
	case *ast.Elements:
		row := r.buildRow(rowVars)
		fn := &core.ID{Pat: &core.IDPat{
			T:    r.typeMap.sys.Fn(t, t),
			Name: "Fn.id",
		}}
		return core.GroupAgg{
			Pat:  &core.IDPat{T: t, Name: label},
			Fn:   fn,
			Arg:  row,
			Span: n.Span(),
		}, nil
	case *ast.InfixCall:
		fn, err := r.toExp(keyEnv, n.A0)
		if err != nil {
			return core.GroupAgg{}, err
		}
		arg, err := r.toExp(cur, n.A1)
		if err != nil {
			return core.GroupAgg{}, err
		}
		return core.GroupAgg{
			Pat:  &core.IDPat{T: t, Name: label},
			Fn:   fn,
			Arg:  arg,
			Span: n.Span(),
		}, nil
	default:
		return core.GroupAgg{}, &Error{
			Span: n.Span(),
			Msg:  "cannot convert to core: aggregate",
		}
	}
}

// toComputeFields converts a compute clause's fields. A field
// that is exactly one aggregate binds it directly; a field that
// combines aggregates within a larger expression hoists each to a
// hidden variable and computes the field from them in a following
// yield.
func (r *resolver) toComputeFields(cur, keyEnv *coreEnv,
	rowVars []*core.IDPat, compute *ast.ComputeStep,
	fieldPats []*core.IDPat,
) ([]core.GroupAgg, []core.YieldField, []*core.IDPat, error) {
	var aggs []core.GroupAgg
	var residuals []core.YieldField
	for _, f := range r.stepFields(compute.Exp) {
		t, err := r.typeMap.TypeOf(f.exp)
		if err != nil {
			return nil, nil, nil, err
		}
		nodes := aggNodes(f.exp)
		if len(nodes) == 1 && nodes[0] == ast.Node(f.exp) {
			// The field is exactly one aggregate; its result is
			// the field.
			agg, aggErr := r.toAggNode(cur, keyEnv, rowVars,
				f.exp, f.label)
			if aggErr != nil {
				return nil, nil, nil, aggErr
			}
			aggs = append(aggs, agg)
			fieldPats = append(fieldPats, agg.Pat)
			continue
		}
		// Hoist each aggregate to a hidden variable; the field is
		// computed from them.
		aggEnv := keyEnv
		for _, n := range nodes {
			agg, aggErr := r.toAggNode(cur, keyEnv, rowVars, n,
				"$agg_"+strconv.Itoa(len(aggs)))
			if aggErr != nil {
				return nil, nil, nil, aggErr
			}
			aggs = append(aggs, agg)
			r.aggSubst[n] = agg.Pat
			aggEnv = aggEnv.bind(agg.Pat)
		}
		exp, err := r.toExp(aggEnv, f.exp)
		if err != nil {
			return nil, nil, nil, err
		}
		pat := &core.IDPat{T: t, Name: f.label}
		residuals = append(residuals, core.YieldField{
			Pat: pat, Exp: exp,
		})
		fieldPats = append(fieldPats, pat)
	}
	return aggs, residuals, fieldPats, nil
}

// toElements converts an "elements" reference: the hidden
// variable its enclosing compute field hoisted it to.
func (r *resolver) toElements(e *ast.Elements) (core.Exp, error) {
	if pat, ok := r.aggSubst[e]; ok {
		return &core.ID{Pat: pat}, nil
	}
	return nil, &Error{
		Span: e.Span(),
		Msg:  "'elements' is only valid in a 'compute' clause",
	}
}

// toOverExp converts an "over" aggregate reference: the hidden
// variable its enclosing compute field hoisted it to.
func (r *resolver) toOverExp(e *ast.InfixCall) (core.Exp, error) {
	if pat, ok := r.aggSubst[e]; ok {
		return &core.ID{Pat: pat}, nil
	}
	return nil, &Error{
		Span: e.Span(),
		Msg:  "'over' is only valid in 'compute'",
	}
}

// aggNodes collects the aggregating subexpressions of a compute
// field — "fn over arg" aggregates and "elements" — without
// descending into a hoisted node itself or into a nested query's
// own compute clause, whose aggregates belong to that query.
func aggNodes(exp ast.Expr) []ast.Node {
	w := &aggWalker{}
	w.exp(exp)
	return w.nodes
}

// aggWalker accumulates a compute field's aggregate nodes.
type aggWalker struct {
	nodes []ast.Node
}

// step walks one step of a nested query.
func (w *aggWalker) step(s ast.FromStep) {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := s.(type) {
	case *ast.ComputeStep:
		// Its aggregates belong to the nested query.
	case *ast.GroupStep:
		w.exp(s.Exp)
	case *ast.IntoStep:
		w.exp(s.Exp)
	case *ast.OrderStep:
		w.exp(s.Exp)
	case *ast.RequireStep:
		w.exp(s.Exp)
	case *ast.Scan:
		if s.Exp != nil {
			w.exp(s.Exp)
		}
		if s.On != nil {
			w.exp(s.On)
		}
	case *ast.SetOpStep:
		for _, e := range s.Exps {
			w.exp(e)
		}
	case *ast.SkipStep:
		w.exp(s.Exp)
	case *ast.TakeStep:
		w.exp(s.Exp)
	case *ast.ThroughStep:
		w.exp(s.Exp)
	case *ast.WhereStep:
		w.exp(s.Exp)
	case *ast.YieldStep:
		w.exp(s.Exp)
	}
}

// exp walks an expression.
func (w *aggWalker) exp(e ast.Expr) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *ast.AnnotatedExp:
		w.exp(e.Exp)
	case *ast.Apply:
		w.exp(e.Fn)
		w.exp(e.Arg)
	case *ast.Case:
		w.exp(e.Exp)
		w.matches(e.Matches)
	case *ast.Elements:
		w.nodes = append(w.nodes, e)
	case *ast.Fn:
		w.matches(e.Matches)
	case *ast.From:
		for _, s := range e.Steps {
			w.step(s)
		}
	case *ast.If:
		w.exp(e.Cond)
		w.exp(e.IfTrue)
		w.exp(e.IfFalse)
	case *ast.InfixCall:
		if e.Kind == ast.OverOp {
			w.nodes = append(w.nodes, e)
			return
		}
		w.exp(e.A0)
		w.exp(e.A1)
	case *ast.Let:
		for _, d := range e.Decls {
			if vd, ok := d.(*ast.ValDecl); ok {
				for _, b := range vd.Binds {
					w.exp(b.Exp)
				}
			}
		}
		w.exp(e.Exp)
	case *ast.ListExp:
		w.exps(e.Args)
	case *ast.PrefixCall:
		w.exp(e.A)
	case *ast.Raise:
		w.exp(e.E)
	case *ast.RangeList:
		for _, item := range e.Items {
			if item.Lo != nil {
				w.exp(item.Lo)
			}
			if item.Hi != nil {
				w.exp(item.Hi)
			}
		}
	case *ast.Record:
		if e.With != nil {
			w.exp(e.With)
		}
		for _, f := range e.Fields {
			w.exp(f.Exp)
		}
	case *ast.Tuple:
		w.exps(e.Args)
	case *ast.TypeStringExp:
		w.exp(e.Exp)
	}
}

// exps walks each expression.
func (w *aggWalker) exps(es []ast.Expr) {
	for _, e := range es {
		w.exp(e)
	}
}

// matches walks each match's result expression.
func (w *aggWalker) matches(ms []*ast.Match) {
	for _, m := range ms {
		w.exp(m.Exp)
	}
}
