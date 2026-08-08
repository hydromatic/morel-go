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
	"fmt"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/token"
)

// Code is a compiled expression. It is an interface, not a bare
// function, because Sys.plan describes compiled statements.
type Code interface {
	Eval(f *Frame) (Val, error)
	Describe() string
}

// Frame holds the values of a statement's variables, in slots
// whose indices were assigned at compile time.
type Frame struct {
	Slots []Val
}

// NewFrame returns a frame with the given number of slots.
func NewFrame(slots int) *Frame {
	return &Frame{Slots: make([]Val, slots)}
}

// Constant returns code that yields a fixed value.
func Constant(v Val) Code {
	return &constantCode{v: v}
}

type constantCode struct {
	v Val
}

func (c *constantCode) Eval(*Frame) (Val, error) {
	return c.v, nil
}

func (c *constantCode) Describe() string {
	return "constant(" + PlanString(c.v) + ")"
}

// GetSlot returns code that reads a variable's slot.
func GetSlot(slot int, name string) Code {
	return &getCode{slot: slot, name: name}
}

type getCode struct {
	name string
	slot int
}

func (c *getCode) Eval(f *Frame) (Val, error) {
	return f.Slots[c.slot], nil
}

func (c *getCode) Describe() string {
	// The plan describes a local by its frame slot, 1-based: the
	// parameter (slot 0, allocated first) is offset 1, the next
	// local offset 2, and so on. This is a fixed slot index counted
	// from the frame's base, not a live stack depth from the top.
	return "stack(offset " + strconv.Itoa(c.slot+1) +
		", name " + c.name + ")"
}

// Closure is a user function value: the pattern that binds its
// parameter, its compiled body, the values it captured from its
// defining scope (and the slots they occupy in its frame), and
// its frame size.
type Closure struct {
	Param         Pat
	Body          Code
	Captured      []Val
	CapturedSlots []int
	NSlots        int
}

// tailCall is what an application in tail position evaluates to
// instead of calling the function: the trampoline in Closure.Apply
// makes the call. A tail-recursive function therefore runs in
// constant Go stack space, however deep the recursion.
//
// A sentinel never escapes an activation. Every tail application
// is compiled inside a function body (only the body of a "fn" is
// compiled in tail position), and "case" and "let" return their
// body's value unchanged, so the sentinel arrives at the
// Closure.Apply that began the activation and goes no further.
//
// span is where the application is, so that the trampoline can
// report an exception raised by the call at the same place a
// non-tail call would.
type tailCall struct {
	fn   Val
	arg  Val
	span token.Span
}

// Apply calls the closure, bouncing on the tail calls its body
// returns until one produces a value. Every call after the first
// replaces the one before it rather than nesting, so the Go stack
// does not grow with the recursion.
func (c *Closure) Apply(arg Val) (Val, error) {
	v, err := c.applyBody(arg)
	for err == nil {
		tc, isTail := v.(*tailCall)
		if !isTail {
			return v, nil
		}
		fn := tc.fn
		for {
			cell, isCell := fn.(*recCell)
			if !isCell {
				break
			}
			fn = cell.v
		}
		if closure, isClosure := fn.(*Closure); isClosure {
			// Bounce: run the body directly, not through Apply,
			// so that trampolines do not nest.
			v, err = closure.applyBody(tc.arg)
		} else {
			// The call left the closures behind — a built-in, say.
			// It cannot tail-call back, so this is the last hop.
			v, err = ApplyVal(fn, tc.arg)
		}
		if err != nil {
			err = stampSpan(err, tc.span)
		}
	}
	return nil, err
}

// applyBody binds the argument and evaluates the body: a fresh
// frame gets the captured values and the argument, and the body
// runs in it.
//
// The value may be a tail-call sentinel, so only Closure.Apply,
// which bounces on those, may call this.
func (c *Closure) applyBody(arg Val) (Val, error) {
	f := NewFrame(c.NSlots)
	for i, slot := range c.CapturedSlots {
		f.Slots[slot] = c.Captured[i]
	}
	if !c.Param.Match(arg, f) {
		return nil, &MorelError{Exn: ExnBind}
	}
	return c.Body.Eval(f)
}

// Capture says that a closure's frame slot receives the value of
// a slot of the defining frame.
type Capture struct {
	From int
	To   int
}

// MakeClosure returns code that creates a closure, capturing the
// given slots of the current frame.
func MakeClosure(param Pat, paramName string, body Code,
	captures []Capture, nSlots int,
) Code {
	return &makeClosureCode{
		param:     param,
		paramName: paramName,
		body:      body,
		captures:  captures,
		nSlots:    nSlots,
	}
}

type makeClosureCode struct {
	param     Pat
	paramName string
	body      Code
	captures  []Capture
	nSlots    int
}

func (c *makeClosureCode) Eval(f *Frame) (Val, error) {
	captured := make([]Val, len(c.captures))
	capturedSlots := make([]int, len(c.captures))
	for i, capture := range c.captures {
		captured[i] = f.Slots[capture.From]
		capturedSlots[i] = capture.To
	}
	return &Closure{
		Param:         c.param,
		Body:          c.body,
		Captured:      captured,
		CapturedSlots: capturedSlots,
		NSlots:        c.nSlots,
	}, nil
}

func (c *makeClosureCode) Describe() string {
	return "match(" + c.paramName + ", " + c.body.Describe() + ")"
}

// Apply returns code that evaluates a function and an argument
// and applies one to the other; span is where an exception
// raised by the application is reported. When the function is a
// built-in, fnName is its qualified name (e.g. "Int.+") and
// fnArity is the number of arguments it takes (1, 2, or 3), used
// to render the compiled plan; fnName is empty for any other
// function.
func Apply(fn, arg Code, span token.Span, fnName string,
	fnArity, fnCurried int, tail bool,
) Code {
	return &applyCode{
		fn:        fn,
		arg:       arg,
		span:      span,
		fnName:    fnName,
		fnArity:   fnArity,
		fnCurried: fnCurried,
		tail:      tail,
	}
}

type applyCode struct {
	fn      Code
	arg     Code
	fnName  string
	span    token.Span
	fnArity int
	// fnCurried is the built-in's total number of curried
	// arguments (arrows in its type), used to collapse a fully
	// applied curried built-in to apply2/apply3 in the plan.
	fnCurried int
	// tail is true when the application is in tail position, shown
	// as tailApply rather than apply in the plan.
	tail bool
}

func (c *applyCode) Eval(f *Frame) (Val, error) {
	fnVal, err := c.fn.Eval(f)
	if err != nil {
		return nil, restampSpan(err, c.span)
	}
	argVal, err := c.arg.Eval(f)
	if err != nil {
		return nil, err
	}
	if c.tail {
		// Hand the call to the trampoline in Closure.Apply rather
		// than making it here, so that the stack does not grow.
		return &tailCall{fn: fnVal, arg: argVal, span: c.span}, nil
	}
	v, err := ApplyVal(fnVal, argVal)
	if err != nil {
		return nil, stampSpan(err, c.span)
	}
	return v, nil
}

// ApplyVal applies a function value — a built-in or a closure —
// to an argument.
func ApplyVal(fn, arg Val) (Val, error) {
	// lint: sort until '^	}' where '^	case '
	switch fn := fn.(type) {
	case *Closure:
		return fn.Apply(arg)
	case *recCell:
		return ApplyVal(fn.v, arg)
	case Fn:
		return fn(arg)
	case func(Val) (Val, error):
		// A bare function reference in the Builtins table has
		// this unnamed type rather than Fn.
		return fn(arg)
	default:
		return nil, fmt.Errorf("cannot apply %T", fn)
	}
}

func (c *applyCode) Describe() string {
	// A fully applied curried built-in "F a1 ... aN" is described
	// as applyN(fnValue F, a1, ..., aN).
	if name, args := c.curriedSpine(); name != "" {
		return applyN(name, args)
	}
	// A built-in whose argument is a tuple of the arity it expects
	// is described the same way, with the tuple's elements spread.
	// (apply2/apply3 have no tail variant.)
	if c.fnName != "" {
		if tup, ok := c.arg.(*tupleCode); ok &&
			len(tup.args) == c.fnArity &&
			(c.fnArity == 2 || c.fnArity == 3) {
			return applyN(c.fnName, tup.args)
		}
		return c.applyWord() + "(fnValue " + c.fnName +
			", argCode " + c.arg.Describe() + ")"
	}
	return c.applyWord() + "(fnCode " + c.fn.Describe() +
		", argCode " + c.arg.Describe() + ")"
}

// applyWord is "tailApply" for an application in tail position,
// else "apply".
func (c *applyCode) applyWord() string {
	if c.tail {
		return "tailApply"
	}
	return "apply"
}

// curriedSpine returns the built-in name and argument codes of a
// fully applied curried built-in "F a1 ... aN" (2 or 3 args), or
// "" if this application is not one.
func (c *applyCode) curriedSpine() (string, []Code) {
	args := []Code{c.arg}
	cur := c.fn
	for {
		inner, ok := cur.(*applyCode)
		if !ok {
			return "", nil
		}
		args = append([]Code{inner.arg}, args...)
		if inner.fnName != "" {
			if inner.fnCurried == len(args) &&
				(len(args) == 2 || len(args) == 3) {
				return inner.fnName, args
			}
			return "", nil
		}
		cur = inner.fn
	}
}

// applyN renders "applyN(fnValue name, arg0, arg1, ...)".
func applyN(name string, args []Code) string {
	var b strings.Builder
	b.WriteString("apply")
	b.WriteString(strconv.Itoa(len(args)))
	b.WriteString("(fnValue ")
	b.WriteString(name)
	for _, a := range args {
		b.WriteString(", ")
		b.WriteString(a.Describe())
	}
	b.WriteString(")")
	return b.String()
}

// recCell is the placeholder that a recursive binding's slot
// holds while its expression evaluates. Anything that captures
// the slot captures the cell; when the binding completes, the
// cell is filled, so every captured reference — however deeply
// the capturing closure was created — sees the final value. (A
// self-referential closure is a cycle; Go's collector handles
// it.) Application dereferences cells; a recursive value is a
// function in any program that terminates.
type recCell struct {
	v Val
}

// LetRec returns code that binds mutually recursive values: each
// slot holds a cell while the inits evaluate, each init's value
// fills its cell and replaces it in the slot, and then the body
// runs.
func LetRec(slots []int, inits []Code, body Code) Code {
	return &letRecCode{slots: slots, inits: inits, body: body}
}

type letRecCode struct {
	slots []int
	inits []Code
	body  Code
}

func (c *letRecCode) Eval(f *Frame) (Val, error) {
	cells := make([]*recCell, len(c.slots))
	for i, slot := range c.slots {
		cells[i] = &recCell{}
		f.Slots[slot] = cells[i]
	}
	for i, init := range c.inits {
		v, err := init.Eval(f)
		if err != nil {
			return nil, err
		}
		cells[i].v = v
		f.Slots[c.slots[i]] = v
	}
	return c.body.Eval(f)
}

func (c *letRecCode) Describe() string {
	return "letRec(" + c.body.Describe() + ")"
}

// Let returns code that evaluates an expression, matches it
// against a pattern (binding the pattern's variables into their
// slots; a non-match raises Bind), and evaluates a body in their
// scope.
func Let(pat Pat, init, body Code, span token.Span) Code {
	return &letCode{pat: pat, init: init, body: body, span: span}
}

type letCode struct {
	pat  Pat
	init Code
	body Code
	span token.Span
}

func (c *letCode) Eval(f *Frame) (Val, error) {
	v, err := c.init.Eval(f)
	if err != nil {
		return nil, err
	}
	if !c.pat.Match(v, f) {
		return nil, &MorelError{Exn: ExnBind, Span: c.span}
	}
	return c.body.Eval(f)
}

func (c *letCode) Describe() string {
	return "let1(expCode " + c.init.Describe() + ", resultCode " +
		c.body.Describe() + ")"
}

// Tuple returns code that evaluates its elements into a []Val.
func Tuple(args []Code) Code {
	return &tupleCode{args: args}
}

type tupleCode struct {
	args []Code
}

func (c *tupleCode) Eval(f *Frame) (Val, error) {
	vals := make([]Val, len(c.args))
	for i, arg := range c.args {
		v, err := arg.Eval(f)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

func (c *tupleCode) Describe() string {
	if len(c.args) == 0 {
		return "tuple"
	}
	var b strings.Builder
	b.WriteString("tuple(")
	for i, a := range c.args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(a.Describe())
	}
	b.WriteString(")")
	return b.String()
}

// Unit returns code that yields the unit value.
func Unit() Code {
	return Constant(core.Unit{})
}
