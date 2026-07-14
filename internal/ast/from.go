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

package ast

import "github.com/hydromatic/morel-go/internal/token"

// FromStep is one step of a from expression's pipeline.
type FromStep interface {
	Node
	fromStep()
}

type stepBase struct{ base }

func (stepBase) fromStep() {}

// ScanKind distinguishes the forms of a scan.
type ScanKind int

// The scan forms: "pat in exp", "pat = exp", and an unbounded
// "pat".
const (
	ScanIn ScanKind = iota
	ScanEq
	ScanUnbounded
)

// Scan is one source of a from expression: "pat in exp",
// "pat = exp", or an unbounded "pat"; On is the join condition,
// or nil. A scan introduced by "join" is the same node; joins
// unparse as comma scans.
type Scan struct {
	stepBase

	Pat  Pat
	Exp  Expr
	On   Expr
	Kind ScanKind
}

// NewScan returns a scan step.
func NewScan(span token.Span, kind ScanKind, pat Pat, exp,
	on Expr,
) *Scan {
	return &Scan{
		stepBase: stepBase{base{span}},
		Pat:      pat,
		Exp:      exp,
		On:       on,
		Kind:     kind,
	}
}

// Op implements Node.
func (*Scan) Op() Op { return ScanOp }

// WhereStep is a "where exp" step.
type WhereStep struct {
	stepBase

	Exp Expr
}

// NewWhereStep returns a where step.
func NewWhereStep(span token.Span, exp Expr) *WhereStep {
	return &WhereStep{stepBase: stepBase{base{span}}, Exp: exp}
}

// Op implements Node.
func (*WhereStep) Op() Op { return WhereOp }

// YieldStep is a "yield exp" step.
type YieldStep struct {
	stepBase

	Exp Expr
}

// NewYieldStep returns a yield step.
func NewYieldStep(span token.Span, exp Expr) *YieldStep {
	return &YieldStep{stepBase: stepBase{base{span}}, Exp: exp}
}

// Op implements Node.
func (*YieldStep) Op() Op { return YieldOp }

// exprStep is a step that holds one expression.
type exprStep struct {
	stepBase

	Exp Expr
}

// GroupStep is "group [binder =] exp".
type GroupStep struct {
	exprStep

	Binder string
}

// Op implements Node.
func (*GroupStep) Op() Op { return GroupOp }

// ComputeStep is "compute [binder =] exp".
type ComputeStep struct {
	exprStep

	Binder string
}

// Op implements Node.
func (*ComputeStep) Op() Op { return ComputeOp }

// OrderStep is "order exp".
type OrderStep struct{ exprStep }

// Op implements Node.
func (*OrderStep) Op() Op { return OrderOp }

// SkipStep is "skip exp".
type SkipStep struct{ exprStep }

// Op implements Node.
func (*SkipStep) Op() Op { return SkipOp }

// TakeStep is "take exp".
type TakeStep struct{ exprStep }

// Op implements Node.
func (*TakeStep) Op() Op { return TakeOp }

// IntoStep is "into exp".
type IntoStep struct{ exprStep }

// Op implements Node.
func (*IntoStep) Op() Op { return IntoOp }

// RequireStep is "require exp".
type RequireStep struct{ exprStep }

// Op implements Node.
func (*RequireStep) Op() Op { return RequireOp }

// DistinctStep is the bare "distinct" step.
type DistinctStep struct{ stepBase }

// Op implements Node.
func (*DistinctStep) Op() Op { return DistinctOp }

// UnorderStep is the bare "unorder" step.
type UnorderStep struct{ stepBase }

// Op implements Node.
func (*UnorderStep) Op() Op { return UnorderOp }

// SetOpStep is "union|intersect|except [distinct] exp".
type SetOpStep struct {
	exprStep

	Kind     Op
	Distinct bool
}

// NewSetOpStep returns a union, intersect, or except step.
func NewSetOpStep(span token.Span, kind Op, distinct bool,
	exp Expr,
) *SetOpStep {
	return &SetOpStep{
		exprStep: exprStep{
			stepBase: stepBase{base{span}},
			Exp:      exp,
		},
		Kind:     kind,
		Distinct: distinct,
	}
}

// Op implements Node.
func (s *SetOpStep) Op() Op { return s.Kind }

// ThroughStep is "through pat in exp".
type ThroughStep struct {
	exprStep

	Pat Pat
}

// NewThroughStep returns a through step.
func NewThroughStep(span token.Span, pat Pat,
	exp Expr,
) *ThroughStep {
	return &ThroughStep{
		exprStep: exprStep{
			stepBase: stepBase{base{span}},
			Exp:      exp,
		},
		Pat: pat,
	}
}

// Op implements Node.
func (*ThroughStep) Op() Op { return ThroughOp }

// NewStep returns a query step of the given kind holding one
// expression: where, yield, group, compute, order, skip, take,
// into, or require.
func NewStep(span token.Span, kind Op, exp Expr) FromStep {
	es := exprStep{stepBase: stepBase{base{span}}, Exp: exp}
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ComputeOp:
		return &ComputeStep{exprStep: es}
	case GroupOp:
		return &GroupStep{exprStep: es}
	case IntoOp:
		return &IntoStep{exprStep: es}
	case OrderOp:
		return &OrderStep{exprStep: es}
	case RequireOp:
		return &RequireStep{exprStep: es}
	case SkipOp:
		return &SkipStep{exprStep: es}
	case TakeOp:
		return &TakeStep{exprStep: es}
	case WhereOp:
		return &WhereStep{stepBase: es.stepBase, Exp: exp}
	case YieldOp:
		return &YieldStep{stepBase: es.stepBase, Exp: exp}
	default:
		panic("NewStep: unknown step kind")
	}
}

// NewBareStep returns a step with no operands: distinct or
// unorder.
func NewBareStep(span token.Span, kind Op) FromStep {
	sb := stepBase{base{span}}
	if kind == DistinctOp {
		return &DistinctStep{stepBase: sb}
	}
	return &UnorderStep{stepBase: sb}
}

// From is a query expression: "from scan, ... step ...". Kind is
// FromOp, ExistsOp, or ForallOp.
type From struct {
	exprBase

	Steps []FromStep
	Kind  Op
}

// NewFrom returns a query expression of the given kind.
func NewFrom(span token.Span, kind Op, steps []FromStep) *From {
	return &From{
		exprBase: exprBase{base{span}},
		Steps:    steps,
		Kind:     kind,
	}
}

// Op implements Node.
func (f *From) Op() Op { return f.Kind }
