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

import (
	"sort"

	"github.com/hydromatic/morel-go/internal/core"
)

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
	for i, row := range rows {
		q.restore(f, row)
		q.setOrdinal(f, i)
		coll, err := s.Source.Eval(f)
		if err != nil {
			return nil, err
		}
		elems := asList(coll)
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
	for i, row := range rows {
		q.restore(f, row)
		q.setOrdinal(f, i)
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

// YieldStage rebinds each row to the fields a mid-query "yield"
// exposes: it computes every field from the input row, then clears
// the query's slots and writes the new field values, so later
// steps see the yielded variables in place of the old ones.
type YieldStage struct {
	Fields []YieldFieldCode
}

// YieldFieldCode is one field a mid-query "yield" rebinds: Code
// computes it from an input row, Slot is where its value is written.
type YieldFieldCode struct {
	Code Code
	Slot int
}

func (s *YieldStage) transform(q *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	out := make([][]Val, 0, len(rows))
	for i, row := range rows {
		q.restore(f, row)
		q.setOrdinal(f, i)
		vals := make([]Val, len(s.Fields))
		for j, fld := range s.Fields {
			v, err := fld.Code.Eval(f)
			if err != nil {
				return nil, err
			}
			vals[j] = v
		}
		for _, slot := range q.slots {
			f.Slots[slot] = nil
		}
		for j, fld := range s.Fields {
			f.Slots[fld.Slot] = vals[j]
		}
		out = append(out, q.snapshot(f))
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
		q.setOrdinal(f, i)
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

// DistinctStage removes duplicate rows, comparing only the
// current row variables' slots — a full frame snapshot also
// carries slots of variables an earlier yield rebound away, which
// must not distinguish rows.
type DistinctStage struct {
	Slots []int
}

// The error result is always nil, but the signature satisfies the
// FromStage interface, whose other stages can fail.
//
//nolint:unparam
func (s *DistinctStage) transform(q *fromCode, f *Frame,
	rows [][]Val,
) ([][]Val, error) {
	seen := map[string]bool{}
	var out [][]Val
	for _, row := range rows {
		q.restore(f, row)
		key := make([]Val, len(s.Slots))
		for i, slot := range s.Slots {
			key[i] = f.Slots[slot]
		}
		ks := PlanString(Val(key))
		if seen[ks] {
			continue
		}
		seen[ks] = true
		out = append(out, row)
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
		args[i] = asList(v)
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

// GroupStage partitions the rows by their key values and, for each
// group, produces a row of the key values and the aggregate
// results. A key's Code and an aggregate's Arg read an input row;
// the results are written to output slots that later stages read.
type GroupStage struct {
	Keys []GroupKeyCode
	Aggs []GroupAggCode
}

// GroupKeyCode is a grouping key: Code computes it from an input
// row, Slot is where its value is written for each group.
type GroupKeyCode struct {
	Code Code
	Slot int
}

// GroupAggCode is an aggregate: Fn is the aggregate function, Arg
// the per-row value it aggregates (nil to count the rows), and Slot
// where its result is written.
type GroupAggCode struct {
	Fn   Code
	Arg  Code
	Slot int
}

type groupRows struct {
	key  []Val
	rows [][]Val
}

func (s *GroupStage) transform(q *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	order, groups, err := s.partition(q, f, rows)
	if err != nil {
		return nil, err
	}
	out := make([][]Val, 0, len(order))
	for _, k := range order {
		g := groups[k]
		aggs := make([]Val, len(s.Aggs))
		for i, a := range s.Aggs {
			aggs[i], err = a.eval(q, f, g.rows)
			if err != nil {
				return nil, err
			}
		}
		for _, slot := range q.slots {
			f.Slots[slot] = nil
		}
		for i, key := range s.Keys {
			f.Slots[key.Slot] = g.key[i]
		}
		for i, a := range s.Aggs {
			f.Slots[a.Slot] = aggs[i]
		}
		out = append(out, q.snapshot(f))
	}
	return out, nil
}

// partition groups the rows by key value, preserving the order in
// which each key first appears.
func (s *GroupStage) partition(q *fromCode, f *Frame, rows [][]Val,
) ([]string, map[string]*groupRows, error) {
	var order []string
	groups := map[string]*groupRows{}
	for _, row := range rows {
		q.restore(f, row)
		key := make([]Val, len(s.Keys))
		for i, k := range s.Keys {
			v, err := k.Code.Eval(f)
			if err != nil {
				return nil, nil, err
			}
			key[i] = v
		}
		gk := PlanString(Val(key))
		g := groups[gk]
		if g == nil {
			g = &groupRows{key: key}
			groups[gk] = g
			order = append(order, gk)
		}
		g.rows = append(g.rows, row)
	}
	// A group with no keys ("group {}") is over a zero-field
	// relation: it always produces exactly one output row, even
	// when the input is empty.
	if len(s.Keys) == 0 && len(order) == 0 {
		gk := PlanString(Val([]Val{}))
		groups[gk] = &groupRows{key: []Val{}}
		order = append(order, gk)
	}
	return order, groups, nil
}

// eval computes an aggregate over a group's rows: it applies the
// aggregate function to the collection of the argument's value per
// row (a unit per row for a bare aggregate, which just counts).
func (a *GroupAggCode) eval(q *fromCode, f *Frame, rows [][]Val,
) (Val, error) {
	fn, err := a.Fn.Eval(f)
	if err != nil {
		return nil, err
	}
	args := make([]Val, len(rows))
	for i, row := range rows {
		if a.Arg == nil {
			args[i] = core.Unit{}
			continue
		}
		q.restore(f, row)
		args[i], err = a.Arg.Eval(f)
		if err != nil {
			return nil, err
		}
	}
	return ApplyVal(fn, args)
}

// ThroughStage applies Fn to the collection of input rows (each
// built by Row) and binds Pat to every element of the result, which
// become the new rows.
type ThroughStage struct {
	Row Code
	Fn  Code
	Pat Pat
}

func (s *ThroughStage) transform(q *fromCode, f *Frame, rows [][]Val,
) ([][]Val, error) {
	in := make([]Val, len(rows))
	for i, row := range rows {
		q.restore(f, row)
		v, err := s.Row.Eval(f)
		if err != nil {
			return nil, err
		}
		in[i] = v
	}
	fn, err := s.Fn.Eval(f)
	if err != nil {
		return nil, err
	}
	out, err := ApplyVal(fn, in)
	if err != nil {
		return nil, err
	}
	elems := asList(out)
	rows2 := make([][]Val, 0, len(elems))
	for _, elem := range elems {
		for _, slot := range q.slots {
			f.Slots[slot] = nil
		}
		if s.Pat.Match(elem, f) {
			rows2 = append(rows2, q.snapshot(f))
		}
	}
	return rows2, nil
}

// Into returns code that applies fn to the collection a query
// produces, yielding a scalar.
func Into(query, fn Code) Code {
	return &intoCode{query: query, fn: fn}
}

type intoCode struct {
	query Code
	fn    Code
}

func (c *intoCode) Eval(f *Frame) (Val, error) {
	coll, err := c.query.Eval(f)
	if err != nil {
		return nil, err
	}
	fn, err := c.fn.Eval(f)
	if err != nil {
		return nil, err
	}
	return ApplyVal(fn, coll)
}

func (c *intoCode) Describe() string {
	return "into(" + c.fn.Describe() + ")"
}

// Exists returns code that reduces a query to whether it has any
// row. Forall returns whether every row's value (the "require"
// predicate) is true.
func Exists(query Code) Code { return &quantCode{query: query} }

// Forall returns code that reduces a query to whether every row's
// value is true.
func Forall(query Code) Code {
	return &quantCode{query: query, forall: true}
}

type quantCode struct {
	query  Code
	forall bool
}

func (c *quantCode) Eval(f *Frame) (Val, error) {
	v, err := c.query.Eval(f)
	if err != nil {
		return nil, err
	}
	rows, _ := v.([]Val)
	if !c.forall {
		return len(rows) > 0, nil
	}
	for _, row := range rows {
		if b, _ := row.(bool); !b {
			return false, nil
		}
	}
	return true, nil
}

func (c *quantCode) Describe() string {
	return "quant(" + c.query.Describe() + ")"
}

// From returns code that evaluates a query: it runs the stages
// over the rows, then collects the value of collect for each — a
// list or a bag (the same representation). slots are the frame
// slots the query's variables occupy, saved and restored as each
// row flows through the stages. ordinalSlot is the frame slot that
// "ordinal" reads, holding each row's position at the current step,
// or -1 when the query does not use "ordinal".
func From(slots []int, stages []FromStage, collect Code,
	ordinalSlot int,
) Code {
	return &fromCode{
		slots:       slots,
		stages:      stages,
		collect:     collect,
		ordinalSlot: ordinalSlot,
	}
}

type fromCode struct {
	collect     Code
	slots       []int
	stages      []FromStage
	ordinalSlot int
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
	for i, row := range rows {
		c.restore(f, row)
		c.setOrdinal(f, i)
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

// setOrdinal records a row's position at the current step in the
// "ordinal" slot, when the query uses "ordinal".
func (c *fromCode) setOrdinal(f *Frame, i int) {
	if c.ordinalSlot >= 0 {
		//nolint:gosec // row counts are far below int32 range
		f.Slots[c.ordinalSlot] = int32(i)
	}
}

func (c *fromCode) restore(f *Frame, row []Val) {
	for i, slot := range c.slots {
		f.Slots[slot] = row[i]
	}
}
