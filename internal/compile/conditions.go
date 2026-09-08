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
	"github.com/hydromatic/morel-go/internal/ast"
)

// Carrying a "check" condition from one record type to another.
//
// A record modifier that adds, removes or renames a field gives a
// record of a different shape, and a condition is typed against
// the exact record type it was written for, because records are
// not width-subtyped. So a condition can be carried over only if
// it is rewritten to hold of the new record, and only if every
// field it depends on is still there.

// conditionRecord is the name a condition's record is rebound to
// when it is rewritten. No user-written name can collide with it.
const conditionRecord = "$r"

// inheritCheck returns check rewritten to hold of a record whose
// fields are named by fields, or nil if it cannot be.
//
// fields maps each field of the record the condition was written
// for to its name in the new record. A field missing from the map
// is one the modifier removed or assigned to, and a condition that
// depends on it cannot be carried over: it would no longer
// typecheck, or the value that is there now was never shown to
// satisfy it.
//
// Nil is returned also where the condition uses the record as a
// whole rather than by selecting fields from it, and where its
// match is one this cannot rewrite. Both are answered
// conservatively: a condition that is dropped claims less, which
// is sound.
func inheritCheck(check *ast.Fn,
	fields map[string]string,
) *ast.Fn {
	allID := true
	for _, m := range check.Matches {
		if _, isID := m.Pat.(*ast.IDPat); !isID {
			allID = false
			break
		}
	}
	if allID {
		return renameCheck(check, fields)
	}
	if len(check.Matches) == 1 {
		return selectCheck(check, fields)
	}
	return nil
}

// renameCheck rewrites a condition that names the record and
// selects fields from it -- "r => r.a < 10" -- by renaming what it
// selects.
//
// Every use of the name must be a selection. One that is not uses
// the record as a whole, which a record of another shape is not.
func renameCheck(check *ast.Fn,
	fields map[string]string,
) *ast.Fn {
	matches := make([]*ast.Match, len(check.Matches))
	for i, m := range check.Matches {
		idPat, isID := m.Pat.(*ast.IDPat)
		if !isID {
			return nil
		}
		name := idPat.Name
		if !selectsOnly(m.Exp, name, fields) {
			return nil
		}
		exp := rewriteSelectors(m.Exp, name, fields)
		matches[i] = ast.NewMatch(m.Span(), m.Pat, exp)
	}
	return ast.NewFn(check.Span(), matches)
}

// selectCheck rewrites a condition that destructures the record --
// "{a, b} => a < 10" -- into one that selects from it, so that it
// holds of a record with fields the pattern does not mention:
//
//	{a, b} => a < 10
//	==>
//	$r => let val a = #a $r and b = #b $r in a < 10 end
//
// Only an irrefutable pattern is rewritten. A refutable one --
// "{a = 0, b}" -- decides by not matching, and a "val" that does
// not match raises Bind rather than answering false.
func selectCheck(check *ast.Fn,
	fields map[string]string,
) *ast.Fn {
	m := check.Matches[0]
	recordPat, isRecord := m.Pat.(*ast.RecordPat)
	if !isRecord || recordPat.Ellipsis {
		return nil
	}
	span := m.Pat.Span()
	recordID := ast.NewID(span, conditionRecord)
	var binds []*ast.ValBind
	for _, f := range recordPat.Fields {
		label, kept := fields[f.Label]
		if !kept || !irrefutablePat(f.Pat) {
			return nil
		}
		binds = append(binds, ast.NewValBind(span, f.Pat,
			ast.NewApply(span, ast.NewRecordSelector(span, label),
				recordID)))
	}
	pat := ast.NewIDPat(span, conditionRecord)
	if len(binds) == 0 {
		// The pattern binds nothing, so the condition does not
		// depend on any field and holds of any record.
		return ast.NewFn(check.Span(),
			[]*ast.Match{ast.NewMatch(m.Span(), pat, m.Exp)})
	}
	let := ast.NewLet(m.Span(),
		[]ast.Decl{ast.NewValDecl(span, false, false, binds)}, m.Exp)
	return ast.NewFn(check.Span(),
		[]*ast.Match{ast.NewMatch(m.Span(), pat, let)})
}

// irrefutablePat reports whether a pattern matches every value.
func irrefutablePat(p ast.Pat) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := p.(type) {
	case *ast.AnnotatedPat:
		return irrefutablePat(p.Pat)
	case *ast.IDPat, *ast.WildcardPat:
		return true
	case *ast.TuplePat:
		for _, a := range p.Args {
			if !irrefutablePat(a) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// selectsOnly reports whether every use of name in exp is a
// selection of a field the map keeps, and whether exp is built
// only of forms this can rewrite.
func selectsOnly(exp ast.Expr, name string,
	fields map[string]string,
) bool {
	ok := true
	walkCondition(exp, func(e ast.Expr) bool {
		if field, isSel := selectionOf(e, name); isSel {
			if _, kept := fields[field]; !kept {
				ok = false
			}
			// Do not descend; the name is used correctly.
			return false
		}
		if id, isID := e.(*ast.ID); isID && id.Name == name {
			// A use that is not a selection: the condition wants the
			// record as a whole.
			ok = false
		}
		return true
	}, func() { ok = false })
	return ok
}

// selectionOf reads "#f name", the selection of a field from the
// record a condition was given.
func selectionOf(e ast.Expr, name string) (string, bool) {
	apply, isApply := e.(*ast.Apply)
	if !isApply {
		return "", false
	}
	field, arg, isSel := selection(apply)
	if !isSel {
		return "", false
	}
	id, isID := arg.(*ast.ID)
	if !isID || id.Name != name {
		return "", false
	}
	return field, true
}

// rewriteSelectors renames the field of every selection of name.
func rewriteSelectors(exp ast.Expr, name string,
	fields map[string]string,
) ast.Expr {
	return mapCondition(exp, func(e ast.Expr) ast.Expr {
		field, isSel := selectionOf(e, name)
		if !isSel {
			return nil
		}
		label, kept := fields[field]
		if !kept || label == field {
			return e
		}
		apply, _ := e.(*ast.Apply)
		return ast.NewApply(apply.Span(),
			ast.NewRecordSelector(apply.Fn.Span(), label), apply.Arg)
	})
}

// selection reads an application of a record selector, "#f e".
func selection(apply *ast.Apply) (string, ast.Expr, bool) {
	sel, isSel := apply.Fn.(*ast.RecordSelector)
	if !isSel {
		return "", nil, false
	}
	return sel.Name, apply.Arg, true
}

// walkCondition visits every expression of a condition, calling
// visit before descending; visit returns false to stop descending.
// unknown is called for a form the walk does not know, so that a
// caller can answer conservatively.
func walkCondition(exp ast.Expr, visit func(ast.Expr) bool,
	unknown func(),
) {
	if !visit(exp) {
		return
	}
	each := func(exps ...ast.Expr) {
		for _, e := range exps {
			if e != nil {
				walkCondition(e, visit, unknown)
			}
		}
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.AnnotatedExp:
		each(e.Exp)
	case *ast.Apply:
		each(e.Fn, e.Arg)
	case *ast.ID, *ast.Literal, *ast.RecordSelector:
	case *ast.If:
		each(e.Cond, e.IfTrue, e.IfFalse)
	case *ast.InfixCall:
		each(e.A0, e.A1)
	case *ast.ListExp:
		each(e.Args...)
	case *ast.PrefixCall:
		each(e.A)
	case *ast.Record:
		if e.Base != nil || len(e.Modifiers) > 0 {
			unknown()
			return
		}
		for _, f := range e.Fields {
			each(f.Exp)
		}
	case *ast.Tuple:
		each(e.Args...)
	default:
		unknown()
	}
}

// mapCondition rewrites a condition bottom-up. f returns nil to
// leave a node to the walk, or a replacement (which is not
// descended into).
func mapCondition(exp ast.Expr, f func(ast.Expr) ast.Expr) ast.Expr {
	if e2 := f(exp); e2 != nil {
		return e2
	}
	sub := func(e ast.Expr) ast.Expr {
		if e == nil {
			return nil
		}
		return mapCondition(e, f)
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.AnnotatedExp:
		return ast.NewAnnotatedExp(e.Span(), sub(e.Exp), e.Type)
	case *ast.Apply:
		return ast.NewApply(e.Span(), sub(e.Fn), sub(e.Arg))
	case *ast.If:
		return ast.NewIf(e.Span(), sub(e.Cond), sub(e.IfTrue),
			sub(e.IfFalse))
	case *ast.InfixCall:
		return ast.NewInfixCall(e.Span(), e.Kind, sub(e.A0),
			sub(e.A1))
	case *ast.ListExp:
		return ast.NewListExp(e.Span(), mapExprs(e.Args, f))
	case *ast.PrefixCall:
		return ast.NewPrefixCall(e.Span(), e.Kind, sub(e.A))
	case *ast.Record:
		fields := make([]ast.Field, len(e.Fields))
		for i, fl := range e.Fields {
			fields[i] = fl
			fields[i].Exp = sub(fl.Exp)
		}
		return ast.NewRecord(e.Span(), fields)
	case *ast.Tuple:
		return ast.NewTuple(e.Span(), mapExprs(e.Args, f))
	default:
		return exp
	}
}

func mapExprs(exps []ast.Expr, f func(ast.Expr) ast.Expr) []ast.Expr {
	out := make([]ast.Expr, len(exps))
	for i, e := range exps {
		out[i] = mapCondition(e, f)
	}
	return out
}
