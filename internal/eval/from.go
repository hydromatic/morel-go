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

package eval

// FromStage is one stage of a query pipeline: a scan that iterates
// a collection binding a pattern, or a where that filters rows.
type FromStage interface {
	fromStage()
}

// ScanStage iterates Source, binding Pat to each element. Source is
// evaluated in the current frame, so a later scan may depend on the
// variables an earlier one bound.
type ScanStage struct {
	Source Code
	Pat    Pat
}

func (*ScanStage) fromStage() {}

// WhereStage keeps the rows for which Cond is true.
type WhereStage struct {
	Cond Code
}

func (*WhereStage) fromStage() {}

// From returns code that evaluates a query: it runs the stages —
// nested scans and filters — and collects the value of collect for
// each surviving row, as a list or a bag (the same representation).
func From(stages []FromStage, collect Code) Code {
	return &fromCode{stages: stages, collect: collect}
}

type fromCode struct {
	collect Code
	stages  []FromStage
}

func (c *fromCode) Eval(f *Frame) (Val, error) {
	out := []Val{}
	err := c.run(f, 0, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *fromCode) Describe() string {
	return "from(" + c.collect.Describe() + ")"
}

// run processes stage i, calling itself for stage i+1 per surviving
// row; at the end it collects the row.
func (c *fromCode) run(f *Frame, i int, out *[]Val) error {
	if i == len(c.stages) {
		row, err := c.collect.Eval(f)
		if err != nil {
			return err
		}
		*out = append(*out, row)
		return nil
	}
	switch s := c.stages[i].(type) {
	case *ScanStage:
		coll, err := s.Source.Eval(f)
		if err != nil {
			return err
		}
		elems, _ := coll.([]Val)
		for _, elem := range elems {
			if !s.Pat.Match(elem, f) {
				continue
			}
			err := c.run(f, i+1, out)
			if err != nil {
				return err
			}
		}
		return nil
	case *WhereStage:
		v, err := s.Cond.Eval(f)
		if err != nil {
			return err
		}
		if b, _ := v.(bool); b {
			return c.run(f, i+1, out)
		}
		return nil
	default:
		return nil
	}
}
