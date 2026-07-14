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

import "github.com/hydromatic/morel-go/internal/token"

// Con is a datatype constructor value, e.g. "SOME 4". Equality
// and comparison use (Datatype, Ordinal); Name is for printing.
// Arg is nil for a constant constructor such as "NONE".
type Con struct {
	Arg      Val
	Datatype string
	Name     string
	Ordinal  int
}

// ExnBind is the exception raised when pattern matching fails in
// a binding.
const ExnBind = "Bind"

// MorelError is a Morel runtime error: the name of the exception
// (e.g. "Div") and the source position it was raised at.
type MorelError struct {
	Span token.Span
	Exn  string
}

func (e *MorelError) Error() string {
	return "uncaught exception " + e.Exn
}
