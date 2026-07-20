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

package core

import (
	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/types"
)

// From is a query expression: a sequence of steps that scan
// collections, filter and transform rows, and produce a
// collection. Its type gives the result's element type and
// orderedness (a list or a bag). Kind is FromOp for an ordinary
// query, or ExistsOp/ForallOp for a quantifier that reduces to a
// boolean.
type From struct {
	T     types.Type
	Steps []FromStep
	Kind  ast.Op
}

// Op implements Exp.
func (*From) Op() ast.Op { return ast.FromOp }

// Type implements Exp.
func (f *From) Type() types.Type { return f.T }

func (*From) exp() {}

// FromStep is one step of a query.
type FromStep interface {
	Op() ast.Op
	fromStep()
}

// Scan is "pat in exp": it iterates the collection exp, binding
// pat to each element.
type Scan struct {
	Pat Pat
	Exp Exp
}

// Op implements FromStep.
func (*Scan) Op() ast.Op { return ast.ScanOp }

func (*Scan) fromStep() {}

// Where is "where exp": it keeps the rows for which the boolean
// exp holds.
type Where struct {
	Exp Exp
}

// Op implements FromStep.
func (*Where) Op() ast.Op { return ast.WhereOp }

func (*Where) fromStep() {}

// Yield is "yield exp": it replaces each row with the value of
// exp.
type Yield struct {
	Exp Exp
}

// Op implements FromStep.
func (*Yield) Op() ast.Op { return ast.YieldOp }

func (*Yield) fromStep() {}

// Order is "order exp": it sorts the rows by the value of exp,
// ascending.
type Order struct {
	Exp Exp
}

// Op implements FromStep.
func (*Order) Op() ast.Op { return ast.OrderOp }

func (*Order) fromStep() {}

// Distinct is "distinct": it removes duplicate rows.
type Distinct struct{}

// Op implements FromStep.
func (*Distinct) Op() ast.Op { return ast.DistinctOp }

func (*Distinct) fromStep() {}

// Skip is "skip exp": it drops the first exp rows.
type Skip struct {
	Exp Exp
}

// Op implements FromStep.
func (*Skip) Op() ast.Op { return ast.SkipOp }

func (*Skip) fromStep() {}

// Take is "take exp": it keeps the first exp rows.
type Take struct {
	Exp Exp
}

// Op implements FromStep.
func (*Take) Op() ast.Op { return ast.TakeOp }

func (*Take) fromStep() {}

// Group is "group keys compute aggregates": it partitions the rows
// by the key values and, for each group, produces a row of the key
// fields and the aggregate fields. Each field binds an output
// variable that later steps see.
type Group struct {
	Keys []GroupKey
	Aggs []GroupAgg
}

// GroupKey is one grouping key: Pat binds its output variable, Exp
// computes its value from an input row.
type GroupKey struct {
	Pat *IDPat
	Exp Exp
}

// GroupAgg is one aggregate: Pat binds its output variable, Fn is
// the aggregate function, and Arg is the per-row expression it
// aggregates (nil to aggregate the rows themselves).
type GroupAgg struct {
	Pat *IDPat
	Fn  Exp
	Arg Exp
}

// Op implements FromStep.
func (*Group) Op() ast.Op { return ast.GroupOp }

func (*Group) fromStep() {}

// Into is "into f": it applies f to the whole collection,
// producing a scalar.
type Into struct {
	Fn Exp
}

// Op implements FromStep.
func (*Into) Op() ast.Op { return ast.IntoOp }

func (*Into) fromStep() {}

// Through is "through pat in f": it applies f to the collection and
// binds pat to each element of the result.
type Through struct {
	Pat Pat
	Fn  Exp
}

// Op implements FromStep.
func (*Through) Op() ast.Op { return ast.ThroughOp }

func (*Through) fromStep() {}

// SetOp is "union"/"intersect"/"except [distinct] exp, ...": it
// combines the query rows so far with the argument collections.
// Kind is UnionOp, IntersectOp, or ExceptOp.
type SetOp struct {
	Kind     ast.Op
	Args     []Exp
	Distinct bool
}

// Op implements FromStep.
func (s *SetOp) Op() ast.Op { return s.Kind }

func (*SetOp) fromStep() {}
