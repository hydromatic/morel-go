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

package compile

import (
	"slices"
	"strconv"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/token"
)

// funToVal converts a "fun" declaration to an equivalent
// "val rec". For example,
//
//	fun f x y = e
//
// becomes "val rec f = fn x => fn y => e", and the clauses of
//
//	fun gcd (a, 0) = a | gcd (a, b) = gcd (b, a mod b)
//
// become a "case" over a tuple of fresh variables.
func funToVal(funDecl *ast.FunDecl) *ast.ValDecl {
	binds := make([]*ast.ValBind, len(funDecl.Binds))
	for i, funBind := range funDecl.Binds {
		binds[i] = funBindToValBind(funBind)
	}
	return ast.NewValDecl(funDecl.Span(), true, binds)
}

func funBindToValBind(funBind *ast.FunBind) *ast.ValBind {
	span := funBind.Span()
	name := funBind.Matches[0].Name
	var vars []ast.Pat
	var exp ast.Expr
	var returnType ast.Type
	if len(funBind.Matches) == 1 {
		match := funBind.Matches[0]
		exp = match.Exp
		vars = match.Pats
		returnType = match.ReturnType
	} else {
		varNames := make([]string,
			len(funBind.Matches[0].Pats))
		for i := range varNames {
			varNames[i] = "v" + strconv.Itoa(i)
			vars = append(vars, ast.NewIDPat(span, varNames[i]))
		}
		matches := make([]*ast.Match, len(funBind.Matches))
		for i, funMatch := range funBind.Matches {
			matches[i] = ast.NewMatch(funMatch.Span(),
				patTuple(funMatch.Span(), funMatch.Pats),
				funMatch.Exp)
			if funMatch.ReturnType != nil {
				returnType = funMatch.ReturnType
			}
		}
		exp = ast.NewCase(span, idTuple(span, varNames),
			matches)
	}
	if returnType != nil {
		exp = ast.NewAnnotatedExp(exp.Span(), exp, returnType)
	}
	for _, v := range slices.Backward(vars) {
		match := ast.NewMatch(span, v, exp)
		exp = ast.NewFn(span, []*ast.Match{match})
	}
	return ast.NewValBind(span, ast.NewIDPat(span, name), exp)
}

// idTuple converts variable names to a variable reference or a
// tuple of them.
func idTuple(span token.Span, names []string) ast.Expr {
	if len(names) == 1 {
		return ast.NewID(span, names[0])
	}
	ids := make([]ast.Expr, len(names))
	for i, name := range names {
		ids[i] = ast.NewID(span, name)
	}
	return ast.NewTuple(span, ids)
}

// patTuple converts a clause's patterns to a single pattern or a
// tuple pattern.
func patTuple(span token.Span, pats []ast.Pat) ast.Pat {
	if len(pats) == 1 {
		return pats[0]
	}
	return ast.NewTuplePat(span, pats)
}
