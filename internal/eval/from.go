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

import "sort"

// A query runs as a sequence of stages over a list of rows. A row
// is a snapshot of the query variables' frame slots; a stage
// restores each snapshot into those slots to evaluate its
// expressions, and produces the next list of rows. Scans nest
// (the cartesian product); where filters; order sorts; distinct
// deduplicates; skip and take slice.

// FromStage is one stage of a query pipeline.
type FromStage interface {
	// transform maps the input rows to the output rows, using frame
	// f (and the query's slots) to evaluate its expressions.
	transform(q *fromCode, f *Frame, rows [][]Val) ([][]Val, error)
}

// ScanStage iterates Source, binding Pat to each element. Source is
// evaluated per input row, so a later scan may depend on the
// variables an earlier one bound.
type ScanStage struct {
	Source Code
	Pat    Pat
}

func (s *ScanStage) transform(q *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	var out [][]Val
	for _, row := range rows {
		q.restore(f, row)
		coll, err := s.Source.Eval(f)
		if err != nil {
			return nil, err
		}
		elems, _ := coll.([]Val)
		for _, elem := range elems {
			if s.Pat.Match(elem, f) {
				out = append(out, q.snapshot(f))
			}
		}
	}
	return out, nil
}

// WhereStage keeps the rows for which Cond is true.
type WhereStage struct {
	Cond Code
}

func (s *WhereStage) transform(q *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	out := rows[:0:0]
	for _, row := range rows {
		q.restore(f, row)
		v, err := s.Cond.Eval(f)
		if err != nil {
			return nil, err
		}
		if b, _ := v.(bool); b {
			out = append(out, row)
		}
	}
	return out, nil
}

// OrderStage sorts the rows by the value of Key (ascending).
type OrderStage struct {
	Key Code
}

func (s *OrderStage) transform(q *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	keys := make([]Val, len(rows))
	for i, row := range rows {
		q.restore(f, row)
		k, err := s.Key.Eval(f)
		if err != nil {
			return nil, err
		}
		keys[i] = k
	}
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return compareVals(keys[idx[a]], keys[idx[b]]) < 0
	})
	out := make([][]Val, len(rows))
	for i, j := range idx {
		out[i] = rows[j]
	}
	return out, nil
}

// DistinctStage removes duplicate rows.
type DistinctStage struct{}

// The error result is always nil, but the signature satisfies the
// FromStage interface, whose other stages can fail.
//
//nolint:unparam
func (*DistinctStage) transform(_ *fromCode, _ *Frame, rows [][]Val,
) ([][]Val, error) {
	var out [][]Val
	for _, row := range rows {
		dup := false
		for _, kept := range out {
			if compareVals(Val(row), Val(kept)) == 0 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, row)
		}
	}
	return out, nil
}

// SkipStage drops the first Count rows; TakeStage keeps them. The
// count is evaluated once, in the root scope.
type SkipStage struct {
	Count Code
}

func (s *SkipStage) transform(_ *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	n, err := countOf(s.Count, f)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		n = 0
	}
	if n > len(rows) {
		n = len(rows)
	}
	return rows[n:], nil
}

// TakeStage keeps the first Count rows.
type TakeStage struct {
	Count Code
}

func (s *TakeStage) transform(_ *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	n, err := countOf(s.Count, f)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		n = 0
	}
	if n > len(rows) {
		n = len(rows)
	}
	return rows[:n], nil
}

func countOf(code Code, f *Frame) (int, error) {
	v, err := code.Eval(f)
	if err != nil {
		return 0, err
	}
	n, _ := v.(int32)
	return int(n), nil
}

// SetOpKind is the kind of a set operation.
type SetOpKind int

const (
	// SetUnion combines the input with the arguments.
	SetUnion SetOpKind = iota
	// SetIntersect keeps rows present in the input and every argument.
	SetIntersect
	// SetExcept removes the arguments' rows from the input.
	SetExcept
)

// SetOpStage combines the input rows with the argument collections
// by union, intersect, or except. With Distinct, duplicates are
// removed; otherwise multiplicity is respected (a multiset union,
// meet, or difference).
type SetOpStage struct {
	Args     []Code
	Kind     SetOpKind
	Distinct bool
	// Slots holds the current row's variable slots in field
	// (name) order; a row's value is unit for none, the sole
	// slot's value for one, the record of them for several. Rows
	// are always full frame snapshots, whatever earlier steps
	// bound.
	Slots []int
}

func (s *SetOpStage) transform(q *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	left := make([]Val, len(rows))
	for i, row := range rows {
		q.restore(f, row)
		left[i] = s.rowValue(f)
	}
	args := make([][]Val, len(s.Args))
	for i, code := range s.Args {
		v, err := code.Eval(f)
		if err != nil {
			return nil, err
		}
		args[i], _ = v.([]Val)
	}
	var result []Val
	// lint: sort until '^	}' where '^	case '
	switch s.Kind {
	case SetExcept:
		result = exceptOp(left, args, s.Distinct)
	case SetIntersect:
		result = intersectOp(left, args, s.Distinct)
	case SetUnion:
		result = unionOp(left, args, s.Distinct)
	}
	out := make([][]Val, len(result))
	for i, v := range result {
		out[i] = s.snapshot(q, f, v)
	}
	return out, nil
}

// rowValue is the value of the row now in the frame: unit for no
// variables, the sole slot's value for one, the record of them
// for several.
func (s *SetOpStage) rowValue(f *Frame) Val {
	switch len(s.Slots) {
	case 0:
		return unitVal
	case 1:
		return f.Slots[s.Slots[0]]
	default:
		vals := make([]Val, len(s.Slots))
		for i, slot := range s.Slots {
			vals[i] = f.Slots[slot]
		}
		return vals
	}
}

// snapshot is the full frame snapshot for a row value, the
// inverse of rowValue; slots outside the row are cleared.
func (s *SetOpStage) snapshot(q *fromCode, f *Frame, v Val) []Val {
	for _, slot := range q.slots {
		f.Slots[slot] = nil
	}
	switch len(s.Slots) {
	case 0:
	case 1:
		f.Slots[s.Slots[0]] = v
	default:
		vals, _ := v.([]Val)
		for i, slot := range s.Slots {
			if i < len(vals) {
				f.Slots[slot] = vals[i]
			}
		}
	}
	return q.snapshot(f)
}

// dedup returns the distinct values in first-occurrence order.
func dedup(vals []Val) []Val {
	var out []Val
	seen := map[string]bool{}
	for _, v := range vals {
		k := PlanString(v)
		if !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}

// counts tallies values by key.
func counts(vals []Val) map[string]int {
	m := map[string]int{}
	for _, v := range vals {
		m[PlanString(v)]++
	}
	return m
}

// unionOp is the input followed by every argument; distinct dedups.
func unionOp(left []Val, args [][]Val, distinct bool) []Val {
	out := append([]Val(nil), left...)
	for _, arg := range args {
		out = append(out, arg...)
	}
	if distinct {
		return dedup(out)
	}
	return out
}

// exceptOp removes the arguments' rows from the input: as a
// multiset difference, or (distinct) the input's distinct rows
// absent from every argument.
func exceptOp(left []Val, args [][]Val, distinct bool) []Val {
	remove := map[string]int{}
	for _, arg := range args {
		for _, v := range arg {
			remove[PlanString(v)]++
		}
	}
	src := left
	if distinct {
		src = dedup(left)
	}
	var out []Val
	for _, v := range src {
		k := PlanString(v)
		if remove[k] > 0 {
			if !distinct {
				remove[k]--
			}
			continue
		}
		out = append(out, v)
	}
	return out
}

// intersectOp keeps the input's rows present in every argument: at
// the meet multiplicity, or (distinct) deduplicated.
func intersectOp(left []Val, args [][]Val, distinct bool) []Val {
	// The meet count of each key across the arguments.
	var meet map[string]int
	for _, arg := range args {
		ac := counts(arg)
		if meet == nil {
			meet = ac
			continue
		}
		for k, m := range meet {
			if ac[k] < m {
				meet[k] = ac[k]
			}
		}
	}
	if meet == nil {
		meet = map[string]int{}
	}
	src := left
	if distinct {
		src = dedup(left)
	}
	var out []Val
	for _, v := range src {
		k := PlanString(v)
		if meet[k] > 0 {
			meet[k]--
			out = append(out, v)
		}
	}
	return out
}

// From returns code that evaluates a query: it runs the stages
// over the rows, then collects the value of collect for each — a
// list or a bag (the same representation). slots are the frame
// slots the query's variables occupy, saved and restored as each
// row flows through the stages.
func From(slots []int, stages []FromStage, collect Code) Code {
	return &fromCode{slots: slots, stages: stages, collect: collect}
}

type fromCode struct {
	collect Code
	slots   []int
	stages  []FromStage
}

func (c *fromCode) Eval(f *Frame) (Val, error) {
	rows := [][]Val{c.snapshot(f)}
	for _, stage := range c.stages {
		var err error
		rows, err = stage.transform(c, f, rows)
		if err != nil {
			return nil, err
		}
	}
	out := []Val{}
	for _, row := range rows {
		c.restore(f, row)
		v, err := c.collect.Eval(f)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (c *fromCode) Describe() string {
	return "from(" + c.collect.Describe() + ")"
}

func (c *fromCode) snapshot(f *Frame) []Val {
	row := make([]Val, len(c.slots))
	for i, slot := range c.slots {
		row[i] = f.Slots[slot]
	}
	return row
}

func (c *fromCode) restore(f *Frame, row []Val) {
	for i, slot := range c.slots {
		f.Slots[slot] = row[i]
	}
}
