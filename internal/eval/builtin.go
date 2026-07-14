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
	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/parse"
)

// Val is a runtime value. Interpretation is driven by static
// types, as in morel-java, so values are bare: int, float32,
// string, rune, bool, unit, []Val, closures.
type Val = any

// Fn is the implementation of a built-in function: one argument
// at a time (curried), returning a value or an error that carries
// a source position.
type Fn func(arg Val) (Val, error)

// Builtins maps a built-in function's fully-qualified name to its
// implementation.
var Builtins = map[string]Fn{
	"Sys.parseTree": parseTree,
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
