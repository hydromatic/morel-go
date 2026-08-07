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
	"github.com/hydromatic/morel-go/internal/core"
)

// cloneExp deep-copies an expression, giving every pattern
// declared within it a fresh identity; fresh maps each original
// pattern to its copy. Patterns are identified by pointer, so an
// expression that is substituted at more than one site must be
// cloned to keep each site's bindings distinct. A variable whose
// pattern is not declared within the expression is free; its
// pattern is shared, not copied.
func cloneExp(e core.Exp, fresh map[*core.IDPat]*core.IDPat,
) core.Exp {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.Apply:
		return &core.Apply{
			T:    e.T,
			Fn:   cloneExp(e.Fn, fresh),
			Arg:  cloneExp(e.Arg, fresh),
			Span: e.Span,
		}
	case *core.Case:
		exp := cloneExp(e.Exp, fresh)
		matches := make([]core.Match, len(e.Matches))
		for i, m := range e.Matches {
			matches[i] = m
			matches[i].Pat = clonePat(m.Pat, fresh)
			matches[i].Exp = cloneExp(m.Exp, fresh)
		}
		return &core.Case{
			T: e.T, Exp: exp, Matches: matches, Span: e.Span,
		}
	case *core.Fn:
		pat := cloneIDPat(e.IDPat, fresh)
		return &core.Fn{T: e.T, IDPat: pat, Exp: cloneExp(e.Exp, fresh)}
	case *core.From:
		// The counter is declared by the query, so a copy gets its
		// own; map it before the steps that read it are cloned.
		ordinal := e.Ordinal
		if ordinal != nil {
			ordinal = cloneIDPat(ordinal, fresh)
		}
		steps := make([]core.FromStep, len(e.Steps))
		for i, s := range e.Steps {
			steps[i] = cloneStep(s, fresh)
		}
		return &core.From{
			T:       e.T,
			Steps:   steps,
			Kind:    e.Kind,
			Ordinal: ordinal,
		}
	case *core.ID:
		if pat, ok := fresh[e.Pat]; ok {
			return &core.ID{Pat: pat}
		}
		return e
	case *core.Let:
		decl := cloneDecl(e.Decl, fresh)
		return &core.Let{Decl: decl, Exp: cloneExp(e.Exp, fresh)}
	case *core.List:
		return &core.List{T: e.T, Args: cloneExps(e.Args, fresh)}
	case *core.Ordinal:
		if pat, ok := fresh[e.Pat]; ok {
			return &core.Ordinal{T: e.T, Pat: pat}
		}
		return e
	case *core.RangeList:
		items := make([]core.RangeItem, len(e.Items))
		for i, item := range e.Items {
			items[i] = item
			items[i].Lo = cloneExp(item.Lo, fresh)
			if item.Hi != nil {
				items[i].Hi = cloneExp(item.Hi, fresh)
			}
		}
		return &core.RangeList{T: e.T, Items: items}
	case *core.Tuple:
		return &core.Tuple{T: e.T, Args: cloneExps(e.Args, fresh)}
	default:
		// Leaves without bindings: Literal, Con, Selector, Unit.
		return e
	}
}

// cloneExps deep-copies a slice of expressions.
func cloneExps(exps []core.Exp, fresh map[*core.IDPat]*core.IDPat,
) []core.Exp {
	out := make([]core.Exp, len(exps))
	for i, e := range exps {
		out[i] = cloneExp(e, fresh)
	}
	return out
}

// cloneIDPat copies a variable pattern and records the copy.
func cloneIDPat(pat *core.IDPat, fresh map[*core.IDPat]*core.IDPat,
) *core.IDPat {
	pat2 := &core.IDPat{T: pat.T, Name: pat.Name}
	fresh[pat] = pat2
	return pat2
}

// clonePat deep-copies a pattern, recording every variable copy.
func clonePat(p core.Pat, fresh map[*core.IDPat]*core.IDPat,
) core.Pat {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := p.(type) {
	case *core.AsPat:
		return &core.AsPat{
			T:    p.T,
			Pat:  cloneIDPat(p.Pat, fresh),
			Body: clonePat(p.Body, fresh),
		}
	case *core.ConPat:
		return &core.ConPat{
			T:        p.T,
			Datatype: p.Datatype,
			Name:     p.Name,
			Ordinal:  p.Ordinal,
			Arg:      clonePat(p.Arg, fresh),
		}
	case *core.ConsPat:
		return &core.ConsPat{
			T:    p.T,
			Head: clonePat(p.Head, fresh),
			Tail: clonePat(p.Tail, fresh),
		}
	case *core.IDPat:
		return cloneIDPat(p, fresh)
	case *core.ListPat:
		return &core.ListPat{T: p.T, Args: clonePats(p.Args, fresh)}
	case *core.TuplePat:
		return &core.TuplePat{T: p.T, Args: clonePats(p.Args, fresh)}
	default:
		// WildcardPat, LiteralPat, Con0Pat bind no variables.
		return p
	}
}

// clonePats deep-copies a slice of patterns.
func clonePats(pats []core.Pat, fresh map[*core.IDPat]*core.IDPat,
) []core.Pat {
	out := make([]core.Pat, len(pats))
	for i, p := range pats {
		out[i] = clonePat(p, fresh)
	}
	return out
}

// cloneDecl deep-copies a declaration.
func cloneDecl(d core.Decl, fresh map[*core.IDPat]*core.IDPat,
) core.Decl {
	switch d := d.(type) {
	case *core.NonRecValDecl:
		return &core.NonRecValDecl{
			Pat:  clonePat(d.Pat, fresh),
			Exp:  cloneExp(d.Exp, fresh),
			Span: d.Span,
		}
	case *core.RecValDecl:
		binds := make([]*core.NonRecValDecl, len(d.Binds))
		// Recursive names are in scope in every body, so all the
		// patterns are copied before any body.
		for i, b := range d.Binds {
			binds[i] = &core.NonRecValDecl{
				Pat:  clonePat(b.Pat, fresh),
				Span: b.Span,
			}
		}
		for i, b := range d.Binds {
			binds[i].Exp = cloneExp(b.Exp, fresh)
		}
		return &core.RecValDecl{Binds: binds}
	default:
		return d
	}
}

// cloneStep deep-copies a query step.
func cloneStep(s core.FromStep, fresh map[*core.IDPat]*core.IDPat,
) core.FromStep {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := s.(type) {
	case *core.Group:
		keys := make([]core.GroupKey, len(s.Keys))
		for i, k := range s.Keys {
			keys[i].Exp = cloneExp(k.Exp, fresh)
			keys[i].Pat = cloneIDPat(k.Pat, fresh)
		}
		aggs := make([]core.GroupAgg, len(s.Aggs))
		for i, a := range s.Aggs {
			aggs[i].Fn = cloneExp(a.Fn, fresh)
			if a.Arg != nil {
				aggs[i].Arg = cloneExp(a.Arg, fresh)
			}
			aggs[i].Pat = cloneIDPat(a.Pat, fresh)
		}
		return &core.Group{Keys: keys, Aggs: aggs}
	case *core.Into:
		return &core.Into{Fn: cloneExp(s.Fn, fresh)}
	case *core.Order:
		return &core.Order{Exp: cloneExp(s.Exp, fresh)}
	case *core.Scan:
		exp := cloneExp(s.Exp, fresh)
		return &core.Scan{Pat: clonePat(s.Pat, fresh), Exp: exp}
	case *core.SetOp:
		return &core.SetOp{
			Kind:     s.Kind,
			Args:     cloneExps(s.Args, fresh),
			Distinct: s.Distinct,
		}
	case *core.Skip:
		return &core.Skip{Exp: cloneExp(s.Exp, fresh)}
	case *core.Take:
		return &core.Take{Exp: cloneExp(s.Exp, fresh)}
	case *core.Through:
		fn := cloneExp(s.Fn, fresh)
		return &core.Through{Pat: clonePat(s.Pat, fresh), Fn: fn}
	case *core.Where:
		return &core.Where{Exp: cloneExp(s.Exp, fresh)}
	case *core.Yield:
		var exp core.Exp
		if s.Exp != nil {
			exp = cloneExp(s.Exp, fresh)
		}
		// A nil Fields slice means a final yield, so nil-ness
		// must survive the copy.
		var fields []core.YieldField
		if s.Fields != nil {
			fields = make([]core.YieldField, len(s.Fields))
			for i, f := range s.Fields {
				fields[i].Exp = cloneExp(f.Exp, fresh)
				fields[i].Pat = cloneIDPat(f.Pat, fresh)
			}
		}
		return &core.Yield{Exp: exp, Fields: fields}
	default:
		return s
	}
}
