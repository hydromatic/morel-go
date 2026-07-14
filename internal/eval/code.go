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
	return fmt.Sprintf("constant(%v)", c.v)
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
	return "get(name " + c.name + ")"
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

// Apply calls the closure: a fresh frame gets the captured
// values and the argument, and the body runs in it.
func (c *Closure) Apply(arg Val) (Val, error) {
	f := NewFrame(c.NSlots)
	for i, slot := range c.CapturedSlots {
		f.Slots[slot] = c.Captured[i]
	}
	if !c.Param.Match(arg, f) {
		return nil, &MorelError{Exn: "Bind"}
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
func MakeClosure(param Pat, body Code, captures []Capture,
	nSlots int,
) Code {
	return &makeClosureCode{
		param:    param,
		body:     body,
		captures: captures,
		nSlots:   nSlots,
	}
}

type makeClosureCode struct {
	param    Pat
	body     Code
	captures []Capture
	nSlots   int
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
	return "closure(" + c.body.Describe() + ")"
}

// Apply returns code that evaluates a function and an argument
// and applies one to the other.
func Apply(fn, arg Code) Code {
	return &applyCode{fn: fn, arg: arg}
}

type applyCode struct {
	fn  Code
	arg Code
}

func (c *applyCode) Eval(f *Frame) (Val, error) {
	fnVal, err := c.fn.Eval(f)
	if err != nil {
		return nil, err
	}
	argVal, err := c.arg.Eval(f)
	if err != nil {
		return nil, err
	}
	return ApplyVal(fnVal, argVal)
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
	default:
		return nil, fmt.Errorf("cannot apply %T", fn)
	}
}

func (c *applyCode) Describe() string {
	return "apply(fnValue " + c.fn.Describe() + ", argCode " +
		c.arg.Describe() + ")"
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

// Let returns code that evaluates an expression, stores it in a
// variable's slot, and evaluates a body in its scope.
func Let(slot int, init, body Code) Code {
	return &letCode{slot: slot, init: init, body: body}
}

type letCode struct {
	init Code
	body Code
	slot int
}

func (c *letCode) Eval(f *Frame) (Val, error) {
	v, err := c.init.Eval(f)
	if err != nil {
		return nil, err
	}
	f.Slots[c.slot] = v
	return c.body.Eval(f)
}

func (c *letCode) Describe() string {
	return "let(" + strconv.Itoa(c.slot) + ", " +
		c.init.Describe() + ", " + c.body.Describe() + ")"
}
