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
	_, known := mapCondition(exp, func(e ast.Expr) ast.Expr {
		if field, isSel := selectionOf(e, name); isSel {
			if _, kept := fields[field]; !kept {
				ok = false
			}
			// Do not descend; the name is used correctly.
			return e
		}
		if id, isID := e.(*ast.ID); isID && id.Name == name {
			// A use that is not a selection: the condition wants the
			// record as a whole.
			ok = false
		}
		return nil
	})
	return ok && known
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
	// selectsOnly has already reported that every form here is one
	// the walk knows, so the rewrite is faithful.
	out, _ := mapCondition(exp, func(e ast.Expr) ast.Expr {
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
	return out
}

// selection reads an application of a record selector, "#f e".
func selection(apply *ast.Apply) (string, ast.Expr, bool) {
	sel, isSel := apply.Fn.(*ast.RecordSelector)
	if !isSel {
		return "", nil, false
	}
	return sel.Name, apply.Arg, true
}

// mapCondition rewrites a condition bottom-up, and is the one
// place that says what a condition may be built of.
//
// f returns nil to leave a node to the walk, or a node to put in
// its place, which is not descended into; returning the node
// itself is how a caller inspects one without rewriting it.
//
// It reports false where it met a form it does not know. A caller
// must then answer conservatively: such a form may use the record
// as a whole, and what is returned is not a faithful rewrite of
// it. Deciding what is known and rebuilding it are the same walk,
// so the two cannot come to disagree -- they did, and a record
// with a base was rebuilt without one.
func mapCondition(exp ast.Expr, f func(ast.Expr) ast.Expr) (ast.Expr,
	bool,
) {
	if e2 := f(exp); e2 != nil {
		return e2, true
	}
	known := true
	sub := func(e ast.Expr) ast.Expr {
		if e == nil {
			return nil
		}
		e2, ok := mapCondition(e, f)
		known = known && ok
		return e2
	}
	subs := func(exps []ast.Expr) []ast.Expr {
		out := make([]ast.Expr, len(exps))
		for i, e := range exps {
			out[i] = sub(e)
		}
		return out
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.AnnotatedExp:
		return ast.NewAnnotatedExp(e.Span(), sub(e.Exp), e.Type), known
	case *ast.Apply:
		return ast.NewApply(e.Span(), sub(e.Fn), sub(e.Arg)), known
	case *ast.ID, *ast.Literal, *ast.RecordSelector:
		return exp, true
	case *ast.If:
		return ast.NewIf(e.Span(), sub(e.Cond), sub(e.IfTrue),
			sub(e.IfFalse)), known
	case *ast.InfixCall:
		return ast.NewInfixCall(e.Span(), e.Kind, sub(e.A0),
			sub(e.A1)), known
	case *ast.ListExp:
		return ast.NewListExp(e.Span(), subs(e.Args)), known
	case *ast.PrefixCall:
		return ast.NewPrefixCall(e.Span(), e.Kind, sub(e.A)), known
	case *ast.Record:
		if e.Base != nil || len(e.Modifiers) > 0 {
			// NewRecord would drop them, so this is not a form the
			// walk can rebuild.
			return exp, false
		}
		fields := make([]ast.Field, len(e.Fields))
		for i, fl := range e.Fields {
			fields[i] = fl
			fields[i].Exp = sub(fl.Exp)
		}
		return ast.NewRecord(e.Span(), fields), known
	case *ast.Tuple:
		return ast.NewTuple(e.Span(), subs(e.Args)), known
	default:
		return exp, false
	}
}
