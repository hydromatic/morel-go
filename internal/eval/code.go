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
	fn, ok := fnVal.(Fn)
	if !ok {
		return nil, fmt.Errorf("cannot apply %T", fnVal)
	}
	return fn(argVal)
}

func (c *applyCode) Describe() string {
	return "apply(fnValue " + c.fn.Describe() + ", argCode " +
		c.arg.Describe() + ")"
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
