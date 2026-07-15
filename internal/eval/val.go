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

	"github.com/hydromatic/morel-go/internal/token"
)

// Con is a datatype constructor value, e.g. "SOME 4". Equality
// and comparison use (Datatype, Ordinal); Name is for printing.
// Arg is nil for a constant constructor such as "NONE".
type Con struct {
	Arg      Val
	Datatype string
	Name     string
	Ordinal  int
}

// Names of the built-in exceptions that the runtime raises.
const (
	ExnBind      = "Bind"
	ExnChr       = "Chr"
	ExnDiv       = "Div"
	ExnDomain    = "Domain"
	ExnEmpty     = "Empty"
	ExnOverflow  = "Overflow"
	ExnSize      = "Size"
	ExnSubscript = "Subscript"
)

// MorelError is a Morel runtime error: the name of the exception
// (e.g. "Div") and the source position it was raised at.
type MorelError struct {
	Span token.Span
	Exn  string
}

func (e *MorelError) Error() string {
	return "uncaught exception " + e.Exn
}

// exnDescriptions are the bracketed descriptions that some
// built-in exceptions carry in their reports.
var exnDescriptions = map[string]string{
	ExnBind:     "nonexhaustive binding failure",
	ExnDiv:      "divide by zero",
	"Domain":    "domain error",
	"Match":     "nonexhaustive match failure",
	ExnOverflow: "overflow",
	ExnSize:     "size",
	"Subscript": "subscript out of bounds",
}

// Describe renders the exception as the shell reports it:
//
//	uncaught exception Bind [nonexhaustive binding failure]
//	  raised at: stdIn:1.5-1.20
func (e *MorelError) Describe() string {
	s := "uncaught exception " + e.Exn
	if d, ok := exnDescriptions[e.Exn]; ok {
		s += " [" + d + "]"
	}
	if e.Span != (token.Span{}) {
		s += "\n  raised at: stdIn:" + e.Span.String()
	}
	return s
}

// stampSpan gives a Morel error that has no position yet the
// given one; the innermost location that knows where it is wins.
func stampSpan(err error, span token.Span) error {
	var morelErr *MorelError
	if errors.As(err, &morelErr) &&
		morelErr.Span == (token.Span{}) {
		morelErr.Span = span
	}
	return err
}

// restampSpan replaces a Morel error's position with the given one.
// An error surfacing from an application's function subexpression is
// a curried built-in rejecting a partial argument (such as Real.fmt
// on an invalid precision); it is reported at the whole
// application, not at the partial.
func restampSpan(err error, span token.Span) error {
	var morelErr *MorelError
	if errors.As(err, &morelErr) {
		morelErr.Span = span
	}
	return err
}
