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
	"slices"

	"github.com/hydromatic/morel-go/internal/types"
)

// Stepping through a discrete domain: the successor and
// predecessor of a value, and the first and last values of the
// domain.
//
// Where a domain is bounded its values are also numbered, and a
// range over it can be enumerated by reading off the positions
// between its endpoints (see Discrete.Ordinal). Stepping is what
// answers for the domains that numbering cannot reach: a product
// with an unbounded component, such as "bool * int", has no last
// value and so no count, but "(false, 1)" still has a successor.

// Succ is the next value of the domain after v, and false if v is
// the domain's last value or has no successor.
func (d *Discrete) Succ(v Val) (Val, bool) {
	return step(d.sys, d.t, v, true)
}

// Pred is the previous value of the domain before v, and false if
// v is the domain's first value or has no predecessor.
func (d *Discrete) Pred(v Val) (Val, bool) {
	return step(d.sys, d.t, v, false)
}

// Least is the first value of the domain, and false if the domain
// runs on without end below -- as int does, and as any product or
// datatype containing an int does.
func (d *Discrete) Least() (Val, bool) {
	return end(d.sys, d.t, false)
}

// Greatest is the last value of the domain, and false if the
// domain runs on without end above.
func (d *Discrete) Greatest() (Val, bool) {
	return end(d.sys, d.t, true)
}

// descendingDatatype is the datatype whose values reverse the
// order of what they wrap, so that "order i desc" sorts
// descending. Its domain is the inner domain reversed: the
// successor of "DESC 3" is "DESC 2".
//
// It is declared in list.go, where compareVals reverses on it.

// step is the successor of v (forward) or its predecessor
// (backward) in the domain of t, and false where there is none.
func step(sys *types.System, t types.Type, v Val, forward bool) (Val,
	bool,
) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Named:
		return stepNamed(sys, t, v, forward)
	case *types.Primitive:
		return stepPrimitive(sys, t, v, forward)
	case *types.Record:
		return stepProduct(sys, fieldTypes(t), v, forward)
	case *types.Tuple:
		return stepProduct(sys, t.Args, v, forward)
	}
	return nil, false
}

// stepPrimitive steps a primitive value. An int runs on without
// end in both directions; a char stops at 0 and at the last code
// point; a bool has two values; unit has one, and so no step at
// all.
func stepPrimitive(sys *types.System, t types.Type, v Val,
	forward bool,
) (Val, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t {
	case sys.Bool:
		b, isBool := v.(bool)
		if !isBool || b == forward {
			return nil, false
		}
		return forward, true
	case sys.Char:
		n, isChar := v.(int32)
		if !isChar {
			return nil, false
		}
		if forward {
			if n >= charCount-1 {
				return nil, false
			}
			return n + 1, true
		}
		if n <= 0 {
			return nil, false
		}
		return n - 1, true
	case sys.Int:
		n, isInt := v.(int32)
		if !isInt {
			return nil, false
		}
		if forward {
			return n + 1, true
		}
		return n - 1, true
	}
	return nil, false
}

// stepNamed steps a datatype value. The domain runs through the
// constructors in declaration order, each constructor's argument
// domain filling its stretch, so a step either moves within the
// argument or moves on to the next constructor and takes its
// first value.
func stepNamed(sys *types.System, t *types.Named, v Val,
	forward bool,
) (Val, bool) {
	con, isCon := v.(Con)
	if !isCon {
		return nil, false
	}
	cons := constructors(sys, t)
	if t.Name == descendingDatatype {
		// "descending" reverses the order of what it wraps, so its
		// successor is the inner predecessor.
		if len(cons) != 1 || cons[0].Arg == nil {
			return nil, false
		}
		inner, ok := step(sys, cons[0].Arg, con.Arg, !forward)
		if !ok {
			return nil, false
		}
		return Con{
			Datatype: con.Datatype, Name: con.Name,
			Ordinal: con.Ordinal, Arg: inner,
		}, true
	}
	i := slices.IndexFunc(cons, func(c types.Constructor) bool {
		return c.Ordinal == con.Ordinal
	})
	if i < 0 {
		return nil, false
	}
	if cons[i].Arg != nil {
		// Within this constructor's stretch, if the argument steps.
		if inner, ok := step(sys, cons[i].Arg, con.Arg, forward); ok {
			return Con{
				Datatype: con.Datatype, Name: con.Name,
				Ordinal: con.Ordinal, Arg: inner,
			}, true
		}
	}
	return conEnd(sys, t, cons, i, forward)
}

// conEnd moves to the constructor beyond the one at i and takes
// its first value in the direction of travel: the least value of
// the next constructor going forward, the greatest of the previous
// one going back.
func conEnd(sys *types.System, t *types.Named,
	cons []types.Constructor, i int, forward bool,
) (Val, bool) {
	j := i - 1
	if forward {
		j = i + 1
	}
	if j < 0 || j >= len(cons) {
		return nil, false
	}
	next := Con{
		Datatype: t.Name, Name: cons[j].Name, Ordinal: cons[j].Ordinal,
	}
	if cons[j].Arg == nil {
		return next, true
	}
	arg, ok := end(sys, cons[j].Arg, !forward)
	if !ok {
		return nil, false
	}
	next.Arg = arg
	return next, true
}

// stepProduct steps a tuple or record. The components are ordered
// lexicographically, with the first the most significant, so the
// last component moves fastest: step it, and where it has no step
// left, wrap it round to the other end of its domain and carry
// into the component before.
func stepProduct(sys *types.System, argTypes []types.Type, v Val,
	forward bool,
) (Val, bool) {
	vals, isProduct := v.([]Val)
	if !isProduct || len(vals) != len(argTypes) {
		return nil, false
	}
	out := slices.Clone(vals)
	for i, at := range slices.Backward(argTypes) {
		if next, ok := step(sys, at, out[i], forward); ok {
			out[i] = next
			return out, true
		}
		// This component is at the end of its domain. It wraps to
		// the other end and the carry goes on to the component
		// before -- which a component with no other end cannot do.
		wrapped, ok := end(sys, at, !forward)
		if !ok {
			return nil, false
		}
		out[i] = wrapped
	}
	return nil, false
}

// end is the last value of a type's domain (top) or its first
// (bottom), and false where the domain runs on without end that
// way. It is what an unbounded endpoint of a range stands for, and
// what a component wraps round to when a product carries.
func end(sys *types.System, t types.Type, top bool) (Val, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Named:
		return namedEnd(sys, t, top)
	case *types.Primitive:
		return primitiveEnd(sys, t, top)
	case *types.Record:
		return productEnd(sys, fieldTypes(t), top)
	case *types.Tuple:
		return productEnd(sys, t.Args, top)
	}
	return nil, false
}

// primitiveEnd is the end of a primitive domain. int has neither.
func primitiveEnd(sys *types.System, t types.Type, top bool) (Val,
	bool,
) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t {
	case sys.Bool:
		return top, true
	case sys.Char:
		if top {
			return int32(charCount - 1), true
		}
		return int32(0), true
	case sys.Unit:
		return unitVal, true
	}
	return nil, false
}

// namedEnd is the end of a datatype's domain: the last value of
// its last constructor, or the first of its first.
func namedEnd(sys *types.System, t *types.Named, top bool) (Val,
	bool,
) {
	cons := constructors(sys, t)
	if len(cons) == 0 {
		return nil, false
	}
	if t.Name == descendingDatatype {
		// Reversed, so the top of a "descending" domain is built
		// from the bottom of the domain it wraps.
		if cons[0].Arg == nil {
			return nil, false
		}
		inner, ok := end(sys, cons[0].Arg, !top)
		if !ok {
			return nil, false
		}
		return Con{
			Datatype: t.Name, Name: cons[0].Name,
			Ordinal: cons[0].Ordinal, Arg: inner,
		}, true
	}
	c := cons[0]
	if top {
		c = cons[len(cons)-1]
	}
	out := Con{Datatype: t.Name, Name: c.Name, Ordinal: c.Ordinal}
	if c.Arg == nil {
		return out, true
	}
	arg, ok := end(sys, c.Arg, top)
	if !ok {
		return nil, false
	}
	out.Arg = arg
	return out, true
}

// productEnd is the end of a product's domain: every component at
// the same end of its own.
func productEnd(sys *types.System, argTypes []types.Type, top bool,
) (Val, bool) {
	out := make([]Val, len(argTypes))
	for i, at := range argTypes {
		v, ok := end(sys, at, top)
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// fieldTypes are the types of a record's fields, in the canonical
// order that compareVals compares them in.
func fieldTypes(t *types.Record) []types.Type {
	out := make([]types.Type, len(t.Fields))
	for i, f := range t.Fields {
		out[i] = f.Type
	}
	return out
}
