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
	"errors"
	"strconv"

	"github.com/hydromatic/morel-go/internal/types"
)

// charCount is the size of the char type: the 8-bit character
// set.
const charCount = 256

// ValRange is one contiguous range of values, with optional open
// or absent bounds.
type ValRange struct {
	Lo, Hi         Val
	LoOpen, HiOpen bool
	NoLo, NoHi     bool
}

// ValRangeSet is a set of disjoint ranges.
type ValRangeSet []ValRange

// contains reports whether a value falls in one of the ranges.
func (rs ValRangeSet) contains(v Val) bool {
	for _, r := range rs {
		if r.contains(v) {
			return true
		}
	}
	return false
}

// contains reports whether a value falls in the range.
func (r ValRange) contains(v Val) bool {
	if !r.NoLo {
		c := compareVals(v, r.Lo)
		if c < 0 || (c == 0 && r.LoOpen) {
			return false
		}
	}
	if !r.NoHi {
		c := compareVals(v, r.Hi)
		if c > 0 || (c == 0 && r.HiOpen) {
			return false
		}
	}
	return true
}

// RangeExtent is the set of values a sourceless query variable
// ranges over: an element type, restricted per path within that
// type by a range set. The path "/" is the whole value; a tuple or
// record appends the field label ("/1/", "/name/") and a datatype
// appends the constructor name ("/SOME/"). An empty RangeSets
// means the whole type. Values holds the materialized set when it
// is finite, and is nil when it is infinite.
type RangeExtent struct {
	T         types.Type
	RangeSets map[string]ValRangeSet
	Values    []Val
}

// NewRangeExtent returns the extent of a type under per-path
// range restrictions (nil for the whole type), materializing its
// values if the extent is finite.
func NewRangeExtent(sys *types.System, t types.Type,
	rangeSets map[string]ValRangeSet,
) *RangeExtent {
	re := &RangeExtent{T: t, RangeSets: rangeSets}
	values := []Val{}
	if populateExtent(sys, t, "/", rangeSets, func(v Val) {
		values = append(values, v)
	}) {
		re.Values = values
	}
	return re
}

// description renders the extent: the type alone when the extent
// is the whole type.
func (re *RangeExtent) description() string {
	if len(re.RangeSets) == 0 {
		return re.T.String()
	}
	return re.T.String() + " " + strconv.Itoa(len(re.RangeSets)) +
		" ranges"
}

// ExtentValues is the "$.extent" internal builtin: it returns an
// extent's materialized values, and fails if the extent is
// infinite — the compiler must ground every infinite extent
// before evaluation.
func ExtentValues(v Val) (Val, error) {
	re, ok := v.(*RangeExtent)
	if !ok {
		return nil, errors.New("not an extent")
	}
	if re.Values == nil {
		return nil, errors.New("infinite: " + re.description())
	}
	return re.Values, nil
}

// populateExtent emits every value of a type permitted by the
// range sets, in a canonical order, and reports whether the type
// is finite (enumerable). Finite are bool, unit, char, int
// restricted to bounded ranges, datatypes whose constructor
// arguments are all finite, and tuples and records of finite
// fields. Everything else — unrestricted int, real, string,
// functions, collections, free type variables — is infinite.
func populateExtent(sys *types.System, t types.Type, path string,
	rangeSets map[string]ValRangeSet, emit func(Val),
) bool {
	if rs, ok := rangeSets[path]; ok {
		if t == sys.Int {
			return enumerateIntRanges(rs, emit)
		}
		inner := emit
		emit = func(v Val) {
			if rs.contains(v) {
				inner(v)
			}
		}
	}
	// lint: sort until '^	}' where '^	case '
	switch t := t.(type) {
	case *types.Named:
		return populateDatatype(sys, t, path, rangeSets, emit)
	case *types.Primitive:
		// lint: sort until '^		}' where '^		case '
		switch t {
		case sys.Bool:
			emit(false)
			emit(true)
			return true
		case sys.Char:
			for c := range rune(charCount) {
				emit(c)
			}
			return true
		case sys.Unit:
			emit(unitVal)
			return true
		}
		return false
	case *types.Record:
		fieldTypes := make([]types.Type, len(t.Fields))
		labels := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			fieldTypes[i] = f.Type
			labels[i] = f.Label
		}
		return populateFields(sys, fieldTypes, labels, path,
			rangeSets, emit)
	case *types.Tuple:
		return populateFields(sys, t.Args, tupleLabels(len(t.Args)),
			path, rangeSets, emit)
	default:
		return false
	}
}

// populateDatatype emits every value of a datatype: each
// constructor in name order, a constructor with an argument once
// per value of its argument type. Named types that are not
// datatypes (collections) are infinite.
func populateDatatype(sys *types.System, t *types.Named,
	path string, rangeSets map[string]ValRangeSet, emit func(Val),
) bool {
	cons := sys.Constructors(t.Name)
	if cons == nil {
		return false
	}
	for _, con := range cons {
		if con.Arg == nil {
			emit(Con{
				Datatype: t.Name,
				Name:     con.Name,
				Ordinal:  con.Ordinal,
			})
			continue
		}
		argType := sys.Substitute(con.Arg, t.Args)
		ok := populateExtent(sys, argType, path+con.Name+"/",
			rangeSets, func(v Val) {
				emit(Con{
					Arg:      v,
					Datatype: t.Name,
					Name:     con.Name,
					Ordinal:  con.Ordinal,
				})
			})
		if !ok {
			return false
		}
	}
	return true
}

// populateFields emits the cartesian product of the fields'
// values, the last field varying fastest.
func populateFields(sys *types.System, fieldTypes []types.Type,
	labels []string, path string,
	rangeSets map[string]ValRangeSet, emit func(Val),
) bool {
	fieldVals := make([][]Val, len(fieldTypes))
	for i, ft := range fieldTypes {
		var vals []Val
		ok := populateExtent(sys, ft, path+labels[i]+"/",
			rangeSets, func(v Val) { vals = append(vals, v) })
		if !ok {
			return false
		}
		fieldVals[i] = vals
	}
	product(fieldVals, emit)
	return true
}

// product emits every combination of one value per field, the
// last field varying fastest.
func product(fieldVals [][]Val, emit func(Val)) {
	for _, vals := range fieldVals {
		if len(vals) == 0 {
			return
		}
	}
	idx := make([]int, len(fieldVals))
	for {
		row := make([]Val, len(fieldVals))
		for i, vals := range fieldVals {
			row[i] = vals[idx[i]]
		}
		emit(row)
		i := len(idx) - 1
		for ; i >= 0; i-- {
			idx[i]++
			if idx[i] < len(fieldVals[i]) {
				break
			}
			idx[i] = 0
		}
		if i < 0 {
			return
		}
	}
}

// tupleLabels returns the labels of a tuple's fields: "1", "2",
// and so on.
func tupleLabels(n int) []string {
	labels := make([]string, n)
	for i := range n {
		labels[i] = strconv.Itoa(i + 1)
	}
	return labels
}

// enumerateIntRanges emits every integer of a set of ranges, and
// reports false if any range is unbounded.
func enumerateIntRanges(rs ValRangeSet, emit func(Val)) bool {
	for _, r := range rs {
		if r.NoLo || r.NoHi {
			return false
		}
		lo, ok := r.Lo.(int32)
		if !ok {
			return false
		}
		hi, ok := r.Hi.(int32)
		if !ok {
			return false
		}
		if r.LoOpen {
			lo++
		}
		if r.HiOpen {
			hi--
		}
		for i := lo; i <= hi; i++ {
			emit(i)
			if i == hi {
				break
			}
		}
	}
	return true
}
