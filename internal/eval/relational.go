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

// The Relational structure's aggregate functions, used with
// "compute" and "into": count, sum, min, max, empty, and nonEmpty.
// Each takes a collection (a bag or a list, both []Val) and reduces
// it.

// RelationalAggregates returns the aggregate functions, keyed by
// their bare names; the kernel binds each both at top level and as
// a "Relational" member.
func RelationalAggregates() map[string]Fn {
	return map[string]Fn{
		"count":    relCountFn,
		"empty":    relEmptyFn,
		"max":      relMaxFn,
		"min":      relMinFn,
		"nonEmpty": relNonEmptyFn,
		"sum":      relSumFn,
	}
}

// RelationalFunctions returns the non-aggregate Relational
// functions, keyed by their bare names; the kernel binds each both
// at top level and as a "Relational" member.
func RelationalFunctions() map[string]Fn {
	return map[string]Fn{
		"compare": relCompareFn,
	}
}

// relCompareFn is "compare (x, y)": LESS, EQUAL, or GREATER
// according to the structural order of x and y.
func relCompareFn(arg Val) (Val, error) {
	pair := arg.([]Val) //nolint:forcetypeassert // arg is a pair
	return orderVal(compareVals(pair[0], pair[1])), nil
}

// relCountFn is "count": the number of elements.
func relCountFn(arg Val) (Val, error) {
	//nolint:gosec // a collection never has more than 2^31 elements
	return int32(len(asList(arg))), nil
}

// relEmptyFn is "empty": whether the collection has no elements.
func relEmptyFn(arg Val) (Val, error) {
	return len(asList(arg)) == 0, nil
}

// relNonEmptyFn is "nonEmpty": whether the collection has elements.
func relNonEmptyFn(arg Val) (Val, error) {
	return len(asList(arg)) > 0, nil
}

// relSumFn is "sum": the total of a numeric collection. An empty
// collection sums to the integer zero.
func relSumFn(arg Val) (Val, error) {
	list := asList(arg)
	if len(list) == 0 {
		return int32(0), nil
	}
	switch list[0].(type) {
	case float32:
		var sum float64
		for _, v := range list {
			sum += float64(asReal(v))
		}
		return float32(sum), nil
	case uint64:
		var sum uint64
		for _, v := range list {
			sum += asWord(v)
		}
		return sum, nil
	default:
		var sum int32
		for _, v := range list {
			sum += asInt(v)
		}
		return sum, nil
	}
}

// relMinFn is "min": the least element; Empty on an empty
// collection.
func relMinFn(arg Val) (Val, error) {
	return relExtreme(arg, -1)
}

// relMaxFn is "max": the greatest element; Empty on an empty
// collection.
func relMaxFn(arg Val) (Val, error) {
	return relExtreme(arg, 1)
}

// relExtreme returns the least (sign -1) or greatest (sign 1)
// element, comparing type-directed.
func relExtreme(arg Val, sign int) (Val, error) {
	list := asList(arg)
	if len(list) == 0 {
		return nil, &MorelError{Exn: ExnEmpty}
	}
	best := list[0]
	for _, v := range list[1:] {
		if compareVals(v, best)*sign > 0 {
			best = v
		}
	}
	return best, nil
}
