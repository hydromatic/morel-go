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

// Package compile turns the AST into the typed Core IR: the
// TypeResolver deduces a type for every node by unification, and
// the Resolver converts the AST to Core using those types.
package compile

import (
	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// Error is a compilation error at a source location.
type Error struct {
	Span token.Span
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Binding gives the type of a name in the environment that a
// declaration is compiled in, such as a built-in value or the
// result of an earlier declaration.
type Binding struct {
	Name string
	Type types.Type
}

// ItValDecl wraps an expression statement as the declaration
// "val it = exp".
func ItValDecl(exp ast.Expr) *ast.ValDecl {
	span := exp.Span()
	pat := ast.NewIDPat(span, "it")
	bind := ast.NewValBind(span, pat, exp)
	return ast.NewValDecl(span, false, []*ast.ValBind{bind})
}
