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

package eval_test

import (
	"errors"
	"testing"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/token"
)

// asInt converts a Val known to be an int.
func asInt(t *testing.T, v eval.Val) int32 {
	t.Helper()
	i, ok := v.(int32)
	if !ok {
		t.Fatalf("got %T, want int32", v)
	}
	return i
}

// asFn converts a Val known to be a built-in function.
func asFn(t *testing.T, v eval.Val) eval.Fn {
	t.Helper()
	f, ok := v.(eval.Fn)
	if !ok {
		t.Fatalf("got %T, want Fn", v)
	}
	return f
}

// apply applies a function value.
func apply(t *testing.T, f eval.Val, arg eval.Val) eval.Val {
	t.Helper()
	v, err := asFn(t, f)(arg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestCurry checks that a curried built-in is
// partial-application-safe: each partial application is an
// ordinary function value that can be stored and applied later.
func TestCurry(t *testing.T) {
	sub := eval.Curry2(func(a, b eval.Val) (eval.Val, error) {
		return asInt(t, a) - asInt(t, b), nil
	})
	partial := apply(t, sub, int32(10))
	if got := apply(t, partial, int32(3)); got != int32(7) {
		t.Errorf("got %v, want 7", got)
	}

	clamp := eval.Curry3(
		func(lo, hi, x eval.Val) (eval.Val, error) {
			return min(max(asInt(t, x), asInt(t, lo)),
				asInt(t, hi)), nil
		})
	f1 := apply(t, clamp, int32(0))
	f2 := apply(t, f1, int32(9))
	if got := apply(t, f2, int32(100)); got != int32(9) {
		t.Errorf("got %v, want 9", got)
	}
}

func TestMorelError(t *testing.T) {
	var err error = &eval.MorelError{
		Span: token.Span{},
		Exn:  "Div",
	}
	if err.Error() != "uncaught exception Div" {
		t.Errorf("got %q", err.Error())
	}
	var morelErr *eval.MorelError
	if !errors.As(err, &morelErr) || morelErr.Exn != "Div" {
		t.Error("MorelError does not unwrap")
	}
}

// TestCon checks that constructor values compare by datatype and
// ordinal, with the argument included.
func TestCon(t *testing.T) {
	some4 := eval.Con{
		Arg:      int32(4),
		Datatype: "option",
		Name:     "SOME",
		Ordinal:  1,
	}
	some4b := eval.Con{
		Arg:      int32(4),
		Datatype: "option",
		Name:     "SOME",
		Ordinal:  1,
	}
	none := eval.Con{Datatype: "option", Name: "NONE"}
	if some4 != some4b {
		t.Error("equal constructor values differ")
	}
	if some4 == none {
		t.Error("SOME 4 equals NONE")
	}
}
