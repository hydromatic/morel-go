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
	"math/big"
	"slices"

	"github.com/hydromatic/morel-go/internal/types"
)

// Discrete is the discrete domain of a type: its values, in the
// order that compareVals puts them in, numbered from zero.
//
// The values are counted, not listed. A domain is finite but not
// therefore small -- a nine-character word has 256^9 of them --
// so the number of values in a range is the difference of its
// endpoints' ordinals, and a range too large to expand is refused
// without walking it. Ordinals are big integers for the same
// reason: one that saturated would have to be checked by walking
// after all.
type Discrete struct {
	sys *types.System
	t   types.Type
	// size is the number of values in the domain, or nil if it has
	// no end.
	size *big.Int
}

// DiscreteFor returns the discrete domain of a type. Size is nil
// where the type has values without end (int, word, real,
// string), and where a part of it does.
func DiscreteFor(sys *types.System, t types.Type) *Discrete {
	return &Discrete{sys: sys, t: t, size: domainSize(sys, t)}
}

// Bounded reports whether the domain has a last value.
func (d *Discrete) Bounded() bool { return d.size != nil }

// Counted reports whether the domain numbers its values, so that
// a range over it can be measured without being walked. Every
// bounded domain does, and so does int, whose values run on
// without end but are their own ordinals.
func (d *Discrete) Counted() bool {
	return d.size != nil || d.t == d.sys.Int
}

// Size is the number of values in the domain; it is only defined
// when the domain is bounded.
func (d *Discrete) Size() *big.Int { return d.size }

// Least is the first value of the domain, and Greatest the last;
// they are the ends that an unbounded endpoint of a range stands
// for. Both need the domain to be bounded.
func (d *Discrete) Least() Val { return d.valueAt(big.NewInt(0)) }

// Greatest is the last value of the domain.
func (d *Discrete) Greatest() Val {
	return d.valueAt(new(big.Int).Sub(d.size, big.NewInt(1)))
}

// Ordinal is the position of a value in the domain, counting from
// zero.
func (d *Discrete) Ordinal(v Val) *big.Int {
	return ordinalOf(d.sys, d.t, v)
}

// valueAt is the value at a position, the inverse of Ordinal.
func (d *Discrete) valueAt(n *big.Int) Val {
	return valueAt(d.sys, d.t, n)
}

// domainSize is the number of values of a type, or nil where they
// have no end. A product is the product of its parts, and a
// datatype the sum of its constructors; either is unbounded as
// soon as one part is.
func domainSize(sys *types.System, t types.Type) *big.Int {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Named:
		total := big.NewInt(0)
		for _, con := range sys.Constructors(t.Name) {
			if con.Arg == nil {
				total.Add(total, big.NewInt(1))
				continue
			}
			n := domainSize(sys, con.Arg)
			if n == nil {
				return nil
			}
			total.Add(total, n)
		}
		return total
	case *types.Primitive:
		return primitiveSize(sys, t)
	case *types.Record:
		total := big.NewInt(1)
		for _, f := range t.Fields {
			n := domainSize(sys, f.Type)
			if n == nil {
				return nil
			}
			total.Mul(total, n)
		}
		return total
	case *types.Tuple:
		total := big.NewInt(1)
		for _, arg := range t.Args {
			n := domainSize(sys, arg)
			if n == nil {
				return nil
			}
			total.Mul(total, n)
		}
		return total
	}
	return nil
}

// primitiveSize is the number of values of a primitive type, or
// nil for one whose values have no end.
func primitiveSize(sys *types.System, t types.Type) *big.Int {
	// lint: sort until '^\t}' where '^\tcase '
	switch t {
	case sys.Bool:
		const boolCount = 2
		return big.NewInt(boolCount)
	case sys.Char:
		return big.NewInt(charCount)
	case sys.Unit:
		return big.NewInt(1)
	}
	return nil
}

// ordinalOf is the position of a value of a type, counting from
// zero, in the order that compareVals puts the type's values in.
// A product counts as a mixed-radix numeral, its first part the
// most significant, because that is how compareVals compares
// them; a datatype counts its constructors in turn, each
// constructor's argument continuing the count within its block.
func ordinalOf(sys *types.System, t types.Type, v Val) *big.Int {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Named:
		con, isCon := v.(Con)
		if !isCon {
			return big.NewInt(0)
		}
		total := big.NewInt(0)
		for _, c := range sys.Constructors(t.Name) {
			if c.Ordinal == con.Ordinal {
				if c.Arg == nil {
					return total
				}
				return total.Add(total,
					ordinalOf(sys, c.Arg, con.Arg))
			}
			if c.Arg == nil {
				total.Add(total, big.NewInt(1))
			} else {
				total.Add(total, domainSize(sys, c.Arg))
			}
		}
		return total
	case *types.Primitive:
		return primitiveOrdinal(sys, t, v)
	case *types.Record:
		vals, _ := v.([]Val)
		fieldTypes := make([]types.Type, len(t.Fields))
		for i, f := range t.Fields {
			fieldTypes[i] = f.Type
		}
		return productOrdinal(sys, fieldTypes, vals)
	case *types.Tuple:
		vals, _ := v.([]Val)
		return productOrdinal(sys, t.Args, vals)
	}
	return big.NewInt(0)
}

// productOrdinal counts a tuple or record as a mixed-radix
// numeral, its first part the most significant.
func productOrdinal(sys *types.System, argTypes []types.Type,
	vals []Val,
) *big.Int {
	n := big.NewInt(0)
	for i, at := range argTypes {
		size := domainSize(sys, at)
		if size == nil {
			return n
		}
		n.Mul(n, size)
		if i < len(vals) {
			n.Add(n, ordinalOf(sys, at, vals[i]))
		}
	}
	return n
}

// primitiveOrdinal is the position of a primitive value: a char
// is its code point, a bool false then true, and unit has one
// value.
func primitiveOrdinal(sys *types.System, t types.Type,
	v Val,
) *big.Int {
	switch t {
	case sys.Bool:
		if b, ok := v.(bool); ok && b {
			return big.NewInt(1)
		}
	case sys.Char, sys.Int:
		if c, ok := v.(int32); ok {
			return big.NewInt(int64(c))
		}
	}
	return big.NewInt(0)
}

// valueAt is the value at a position of a type's domain, the
// inverse of ordinalOf.
func valueAt(sys *types.System, t types.Type, n *big.Int) Val {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Named:
		rest := new(big.Int).Set(n)
		for _, c := range sys.Constructors(t.Name) {
			block := big.NewInt(1)
			if c.Arg != nil {
				block = domainSize(sys, c.Arg)
			}
			if rest.Cmp(block) < 0 {
				con := Con{
					Datatype: t.Name,
					Name:     c.Name,
					Ordinal:  c.Ordinal,
				}
				if c.Arg != nil {
					con.Arg = valueAt(sys, c.Arg, rest)
				}
				return con
			}
			rest.Sub(rest, block)
		}
		return unitVal
	case *types.Primitive:
		return primitiveValueAt(sys, t, n)
	case *types.Record:
		fieldTypes := make([]types.Type, len(t.Fields))
		for i, f := range t.Fields {
			fieldTypes[i] = f.Type
		}
		return productValueAt(sys, fieldTypes, n)
	case *types.Tuple:
		return productValueAt(sys, t.Args, n)
	}
	return unitVal
}

// productValueAt splits a mixed-radix numeral back into the parts
// of a tuple or record.
func productValueAt(sys *types.System, argTypes []types.Type,
	n *big.Int,
) Val {
	vals := make([]Val, len(argTypes))
	rest := new(big.Int).Set(n)
	for i, at := range slices.Backward(argTypes) {
		size := domainSize(sys, at)
		if size == nil {
			vals[i] = unitVal
			continue
		}
		q, r := new(big.Int).QuoRem(rest, size, new(big.Int))
		vals[i] = valueAt(sys, at, r)
		rest = q
	}
	return vals
}

// primitiveValueAt is the primitive value at a position.
func primitiveValueAt(sys *types.System, t types.Type,
	n *big.Int,
) Val {
	switch t {
	case sys.Bool:
		return n.Sign() != 0
	case sys.Char, sys.Int:
		//nolint:gosec // an ordinal in range fits an int32
		return int32(n.Int64())
	}
	return unitVal
}

// rangeMaxLength is the largest number of values that expanding a
// range may produce. It is a session property, and the shell sets
// it here when it changes, because the evaluator has no session to
// read it from.
const rangeMaxLengthBits = 24

var rangeMaxLength = new(big.Int).Sub(
	new(big.Int).Lsh(big.NewInt(1), rangeMaxLengthBits), big.NewInt(1))

// RangeMaxLength is the current limit.
func RangeMaxLength() *big.Int { return rangeMaxLength }

// SetRangeMaxLength sets the limit, for the shell to call when
// the property changes.
func SetRangeMaxLength(n *big.Int) { rangeMaxLength = n }
