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

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/types"
)

// A boolean connective resolves to a case — "a andalso b" is
// "case a of true => b | _ => false" — so the predicate analyses
// here recognize that shape rather than a connective node.

// asBoolCase decodes a case built by boolCase, returning its
// condition, true branch, and false branch.
func asBoolCase(e core.Exp) (core.Exp, core.Exp, core.Exp,
	bool,
) {
	c, isCase := e.(*core.Case)
	if !isCase || len(c.Matches) != 2 {
		return nil, nil, nil, false
	}
	truePat, isLit := c.Matches[0].Pat.(*core.LiteralPat)
	if !isLit || truePat.Value != true {
		return nil, nil, nil, false
	}
	if _, isWild := c.Matches[1].Pat.(*core.WildcardPat); !isWild {
		return nil, nil, nil, false
	}
	return c.Exp, c.Matches[0].Exp, c.Matches[1].Exp, true
}

// isBoolLiteral reports whether an expression is the given
// boolean constant.
func isBoolLiteral(e core.Exp, b bool) bool {
	lit, ok := e.(*core.Literal)
	return ok && lit.Value == b
}

// decomposeConjuncts splits a predicate into the conjuncts of its
// top-level "andalso" chain, appending them to out. A predicate
// that is not a conjunction is itself the sole conjunct.
func decomposeConjuncts(e core.Exp, out *[]core.Exp) {
	if cond, ifTrue, ifFalse, ok := asBoolCase(e); ok &&
		isBoolLiteral(ifFalse, false) {
		decomposeConjuncts(cond, out)
		decomposeConjuncts(ifTrue, out)
		return
	}
	*out = append(*out, e)
}

// composeConjuncts rebuilds a predicate from conjuncts: the
// "andalso" chain of them, or a bare "true" for none.
func composeConjuncts(sys *types.System,
	conjuncts []core.Exp,
) core.Exp {
	if len(conjuncts) == 0 {
		return boolLiteral(sys, true)
	}
	result := conjuncts[len(conjuncts)-1]
	for _, conjunct := range slices.Backward(
		conjuncts[:len(conjuncts)-1]) {
		result = boolCase(sys, conjunct, result,
			boolLiteral(sys, false))
	}
	return result
}

// boolLiteral returns a boolean constant expression.
func boolLiteral(sys *types.System, b bool) core.Exp {
	return &core.Literal{
		T: sys.Bool, Kind: ast.BoolLiteralOp, Value: b,
	}
}

// extentOf returns the extent a scan source calls the internal
// extent builtin on, or nil.
func extentOf(e core.Exp) *eval.RangeExtent {
	apply, ok := e.(*core.Apply)
	if !ok {
		return nil
	}
	id, ok := apply.Fn.(*core.ID)
	if !ok || id.Pat.Name != ExtentName {
		return nil
	}
	lit, ok := apply.Arg.(*core.Literal)
	if !ok {
		return nil
	}
	re, ok := lit.Value.(*eval.RangeExtent)
	if !ok {
		return nil
	}
	return re
}

// isInfiniteExtent reports whether a scan source is a call on an
// extent whose values cannot be materialized.
func isInfiniteExtent(e core.Exp) bool {
	re := extentOf(e)
	return re != nil && re.Values == nil
}

// ContainsUnbounded reports whether the declaration contains a
// query scan over an infinite extent: an unbounded variable that
// the grounding pass must replace with a finite generator before
// the query can evaluate.
func ContainsUnbounded(decl core.Decl) bool {
	found := false
	r := &rewriter{}
	r.exp = func(e core.Exp) (core.Exp, bool) {
		if from, ok := e.(*core.From); ok {
			for _, s := range from.Steps {
				scan, isScan := s.(*core.Scan)
				if isScan && isInfiniteExtent(scan.Exp) {
					found = true
				}
			}
		}
		return nil, false
	}
	r.rewriteDecl(decl)
	return found
}
