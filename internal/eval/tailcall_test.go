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

//nolint:testpackage // white-box: a tail call's sentinel is unexported
package eval

import (
	"errors"
	"testing"

	"github.com/hydromatic/morel-go/internal/token"
)

// countDown is a closure equivalent to
//
//	fun countDown n = if n = 0 then 0 else countDown (n - 1)
//
// with the recursive call in tail position. The closure refers to
// itself through a cell, as a recursive binding does.
func countDown() *Closure {
	cell := &recCell{}
	// Slot 0 holds the argument; slot 1 holds the self-reference.
	body := &tailDownCode{cell: cell}
	c := &Closure{
		Param:  SlotPat{Slot: 0},
		Body:   body,
		NSlots: 2,
	}
	cell.v = c
	return c
}

// tailDownCode is the body of countDown: it returns 0 at 0, and
// otherwise a tail call of the closure to n-1.
type tailDownCode struct {
	cell *recCell
}

func (c *tailDownCode) Eval(f *Frame) (Val, error) {
	n, _ := f.Slots[0].(int32)
	if n == 0 {
		return int32(0), nil
	}
	// The sentinel an application in tail position evaluates to.
	return &tailCall{fn: c.cell, arg: n - 1}, nil
}

func (c *tailDownCode) Describe() string { return "tailDown" }

// TestTrampolineConsumesSentinel checks that a tail call's
// sentinel never escapes: Closure.Apply returns the value the
// recursion produced, not the sentinel that carried it.
func TestTrampolineConsumesSentinel(t *testing.T) {
	got, err := countDown().Apply(int32(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := got.(*tailCall); leaked {
		t.Fatal("a tail-call sentinel escaped Closure.Apply")
	}
	if got != int32(0) {
		t.Errorf("got %v, want 0", got)
	}
}

// TestTrampolineIsConstantStack checks that a tail call replaces
// the activation rather than nesting: a recursion far deeper than
// the Go stack could hold completes. Without the trampoline this
// is a fatal stack overflow, which no test could recover from.
func TestTrampolineIsConstantStack(t *testing.T) {
	const deep = 20_000_000
	got, err := countDown().Apply(int32(deep))
	if err != nil {
		t.Fatal(err)
	}
	if got != int32(0) {
		t.Errorf("got %v, want 0", got)
	}
}

// TestTrampolineCallsNonClosure checks the last hop, where a tail
// call names something other than a closure — a built-in, which
// cannot tail-call back.
func TestTrampolineCallsNonClosure(t *testing.T) {
	double := Fn(func(v Val) (Val, error) {
		n, _ := v.(int32)
		return n * 2, nil
	})
	c := &Closure{
		Param:  SlotPat{Slot: 0},
		Body:   &tailToCode{fn: double},
		NSlots: 1,
	}
	got, err := c.Apply(int32(21))
	if err != nil {
		t.Fatal(err)
	}
	if got != int32(42) {
		t.Errorf("got %v, want 42", got)
	}
}

// tailToCode is a body whose whole value is a tail call of a fixed
// function to the argument in slot 0.
type tailToCode struct {
	fn Val
}

func (c *tailToCode) Eval(f *Frame) (Val, error) {
	return &tailCall{fn: c.fn, arg: f.Slots[0]}, nil
}

func (c *tailToCode) Describe() string { return "tailTo" }

// TestTrampolineStampsSpan checks that an exception raised by a
// tail call is reported where the call is, as it would be if the
// call had not been in tail position.
func TestTrampolineStampsSpan(t *testing.T) {
	span := token.Span{
		Start: token.Pos{Line: 7, Col: 3},
		End:   token.Pos{Line: 7, Col: 9},
	}
	boom := Fn(func(Val) (Val, error) {
		return nil, &MorelError{Exn: ExnDiv}
	})
	c := &Closure{
		Param:  SlotPat{Slot: 0},
		Body:   &tailSpanCode{fn: boom, span: span},
		NSlots: 1,
	}
	_, err := c.Apply(int32(1))
	var morelErr *MorelError
	if !errors.As(err, &morelErr) {
		t.Fatalf("got %v, want a Morel error", err)
	}
	if morelErr.Span != span {
		t.Errorf("span %v, want %v", morelErr.Span, span)
	}
}

// tailSpanCode is a body that tail-calls a function, recording
// where the call is.
type tailSpanCode struct {
	fn   Val
	span token.Span
}

func (c *tailSpanCode) Eval(f *Frame) (Val, error) {
	return &tailCall{fn: c.fn, arg: f.Slots[0], span: c.span}, nil
}

func (c *tailSpanCode) Describe() string { return "tailSpan" }

// End tailcall_test.go
