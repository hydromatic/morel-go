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

// Package eval holds runtime values and the built-in functions.
package eval

import (
	"fmt"
	"math"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/parse"
)

// Val is a runtime value. Interpretation is driven by static
// types, so values are bare. The concrete
// types are: int32 (int), float32 (real; computed in float64),
// string, rune (char), bool, core.Unit, []Val (lists, tuples,
// and records in canonical field order), Con (a datatype
// constructor value), and function values (Fn for built-ins,
// Closure for user functions).
type Val = any

// Fn is the implementation of a built-in function: one argument
// at a time, returning a value or an error that carries a source
// position. Every built-in follows this convention uniformly. A
// built-in whose Morel type is curried returns another Fn at
// each step, so partial application yields an ordinary function
// value; a built-in whose Morel argument is a tuple receives a
// []Val.
type Fn func(arg Val) (Val, error)

// Curry2 adapts a two-argument function to the built-in
// convention: applying the result to the first argument returns
// an Fn awaiting the second.
func Curry2(f func(a, b Val) (Val, error)) Fn {
	return func(a Val) (Val, error) {
		return Fn(func(b Val) (Val, error) {
			return f(a, b)
		}), nil
	}
}

// Curry3 adapts a three-argument function to the built-in
// convention.
func Curry3(f func(a, b, c Val) (Val, error)) Fn {
	return func(a Val) (Val, error) {
		return Curry2(func(b, c Val) (Val, error) {
			return f(a, b, c)
		}), nil
	}
}

// Builtins maps a built-in function's name to its
// implementation. (The full registry, validated against
// lib/*.sig, arrives with the standard library.)
var Builtins = map[string]Fn{
	// lint: sort until '^}' where '^\t"'
	"Sys.parseTree": parseTree,
	"abs":           absFn,
	"chr":           chrFn,
	"concat":        concatFn,
	"explode":       explodeFn,
	"hd":            hdFn,
	"implode":       implodeFn,
	"length":        lengthFn,
	"map":           mapFn,
	"not":           notFn,
	"null":          nullFn,
	"op *":          arith(mulInt, mulReal),
	"op +":          arith(addInt, addReal),
	"op -":          arith(subInt, subReal),
	"op /":          arith(nil, divReal),
	"op ::":         consFn,
	"op <":          compareFn(func(c int) bool { return c < 0 }),
	"op <=":         compareFn(func(c int) bool { return c <= 0 }),
	"op <>":         equalFn(true),
	"op =":          equalFn(false),
	"op >":          compareFn(func(c int) bool { return c > 0 }),
	"op >=":         compareFn(func(c int) bool { return c >= 0 }),
	"op @":          atFn,
	"op ^":          caretFn,
	"op div":        arith(divInt, nil),
	"op mod":        arith(modInt, nil),
	"op ~":          negFn,
	"ord":           ordFn,
	"rev":           revFn,
	"size":          sizeFn,
	"str":           strFn,
	"tl":            tlFn,
}

// The scalar accessors panic on the wrong type: built-in
// arguments are guaranteed by type inference.

func asBool(v Val) bool {
	b, ok := v.(bool)
	if !ok {
		panic(fmt.Sprintf("expected bool, got %T", v))
	}
	return b
}

func asChar(v Val) rune {
	c, ok := v.(rune)
	if !ok {
		panic(fmt.Sprintf("expected char, got %T", v))
	}
	return c
}

func asInt(v Val) int32 {
	i, ok := v.(int32)
	if !ok {
		panic(fmt.Sprintf("expected int, got %T", v))
	}
	return i
}

func asString(v Val) string {
	s, ok := v.(string)
	if !ok {
		panic(fmt.Sprintf("expected string, got %T", v))
	}
	return s
}

// absFn is "abs x". It is overloaded on int and real, so it
// switches on the runtime type.
func absFn(arg Val) (Val, error) {
	switch v := arg.(type) {
	case int32:
		if v < 0 {
			return -v, nil
		}
		return v, nil
	case float32:
		return float32(math.Abs(float64(v))), nil
	default:
		panic(fmt.Sprintf("expected int or real, got %T", arg))
	}
}

// chrFn is "chr i", the character with code i.
func chrFn(arg Val) (Val, error) {
	i := asInt(arg)
	const maxChar = 255
	if i < 0 || i > maxChar {
		return nil, &MorelError{Exn: "Chr"}
	}
	// rune and int32 are one type; the result is a char only
	// statically.
	return i, nil
}

// arith adapts a pair of binary implementations, one per numeric
// type, to a built-in that dispatches on its operands' runtime
// type.
func arith(intFn func(a, b int32) (Val, error),
	realFn func(a, b float32) (Val, error),
) Fn {
	return func(arg Val) (Val, error) {
		vals, ok := arg.([]Val)
		if !ok || len(vals) != 2 {
			panic(fmt.Sprintf("expected pair, got %T", arg))
		}
		switch a := vals[0].(type) {
		case int32:
			return intFn(a, asInt(vals[1]))
		case float32:
			f, isReal := vals[1].(float32)
			if !isReal {
				panic(fmt.Sprintf("expected real, got %T",
					vals[1]))
			}
			return realFn(a, f)
		default:
			panic(fmt.Sprintf("expected int or real, got %T",
				vals[0]))
		}
	}
}

// checkIntRange rejects a result outside int32, raising
// Overflow as SML integer arithmetic does.
func checkIntRange(i int64) (Val, error) {
	if i < math.MinInt32 || i > math.MaxInt32 {
		return nil, &MorelError{Exn: ExnOverflow}
	}
	return int32(i), nil
}

func addInt(a, b int32) (Val, error) {
	return checkIntRange(int64(a) + int64(b))
}

func subInt(a, b int32) (Val, error) {
	return checkIntRange(int64(a) - int64(b))
}

func mulInt(a, b int32) (Val, error) {
	return checkIntRange(int64(a) * int64(b))
}

// divInt is SML's "div": floor division, rounding toward
// negative infinity (unlike Go's, which rounds toward zero).
func divInt(a, b int32) (Val, error) {
	if b == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	q := int64(a) / int64(b)
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return checkIntRange(q)
}

// modInt is SML's "mod": the remainder of floor division, with
// the divisor's sign.
func modInt(a, b int32) (Val, error) {
	if b == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r, nil
}

// The real operations compute in float64 and round once to
// float32.

func addReal(a, b float32) (Val, error) {
	return float32(float64(a) + float64(b)), nil
}

func subReal(a, b float32) (Val, error) {
	return float32(float64(a) - float64(b)), nil
}

func mulReal(a, b float32) (Val, error) {
	return float32(float64(a) * float64(b)), nil
}

func divReal(a, b float32) (Val, error) {
	return float32(float64(a) / float64(b)), nil
}

// negFn is "~ x". It is overloaded on int and real, so it
// switches on the runtime type.
func negFn(arg Val) (Val, error) {
	switch v := arg.(type) {
	case int32:
		if v == math.MinInt32 {
			return nil, &MorelError{Exn: ExnOverflow}
		}
		return -v, nil
	case float32:
		return -v, nil
	default:
		panic(fmt.Sprintf("expected int or real, got %T", arg))
	}
}

// notFn is "not b".
func notFn(arg Val) (Val, error) {
	return !asBool(arg), nil
}

// ordFn is "ord c", the character code of c.
func ordFn(arg Val) (Val, error) {
	return asChar(arg), nil
}

// sizeFn is "size s", the number of characters in s.
func sizeFn(arg Val) (Val, error) {
	//nolint:gosec // a string's length fits in an int.
	return int32(len(asString(arg))), nil
}

// strFn is "str c", the single-character string containing c.
func strFn(arg Val) (Val, error) {
	return string(asChar(arg)), nil
}

// parseTree parses its argument as a declaration or expression
// and returns the S-expression form of the parse tree.
func parseTree(arg Val) (Val, error) {
	s, ok := arg.(string)
	if !ok {
		panic("parseTree: argument is not a string")
	}
	n, err := parse.DeclOrExpr("parseTree", s)
	if err != nil {
		return nil, err
	}
	return ast.Dump(n), nil
}
