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
// orderedness (a list or a bag).
type From struct {
	T     types.Type
	Steps []FromStep
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
