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
	"fmt"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// Warning is a non-fatal compile diagnostic, such as a
// nonexhaustive match.
type Warning struct {
	Span token.Span
	Msg  string
}

// String renders a warning as the script harness prints it.
func (w Warning) String() string {
	pos := "stdIn:" + w.Span.String()
	return pos + " Warning: " + w.Msg + "\n  raised at: " + pos
}

// CheckCoverage reports match-coverage diagnostics for every
// "case" (and the "fn"/"fun" that lower to one) in a declaration:
// a nonexhaustive match is a warning, a redundant match an error.
// It returns the warnings in source order and the first redundant
// error, if any.
func CheckCoverage(sys *types.System, decl core.Decl) (
	[]Warning, *Error,
) {
	c := &coverageChecker{sys: sys}
	c.walkDecl(decl)
	return c.warnings, c.err
}

type coverageChecker struct {
	sys      *types.System
	warnings []Warning
	err      *Error
}

func (c *coverageChecker) checkCase(kase *core.Case) {
	elemType := kase.Matches[0].Pat.Type()
	rows := make([][]core.Pat, 0, len(kase.Matches))
	var redundant *core.Match
	for i := range kase.Matches {
		m := &kase.Matches[i]
		// A match whose pattern is already covered by the earlier
		// patterns is redundant; the first such match is reported.
		if redundant == nil && !c.useful(rows, []core.Pat{m.Pat}) {
			redundant = m
		}
		rows = append(rows, []core.Pat{m.Pat})
	}
	// Exhaustiveness is over all the patterns: a wildcard is useful
	// only if some value is matched by none of them. Scrutinees of
	// the form "e mod k" range over 0..k-1, so patterns covering
	// that range are exhaustive though the structural check (which
	// treats int as infinite) cannot see it.
	exhaustive := !c.useful(rows,
		[]core.Pat{&core.WildcardPat{T: elemType}}) ||
		modExhaustive(kase)
	switch {
	case redundant != nil:
		msg := "match redundant"
		if !exhaustive {
			msg = "match redundant and nonexhaustive"
		}
		if c.err == nil {
			c.err = &Error{Span: redundant.Span, Msg: msg}
		}
	case !exhaustive:
		c.warnings = append(c.warnings,
			Warning{Span: kase.Span, Msg: "match nonexhaustive"})
	}
}

// modExhaustive reports whether a case over "e mod k" (k a
// positive int literal) has patterns covering 0..k-1, the whole
// range of the modulus, and is therefore exhaustive.
func modExhaustive(kase *core.Case) bool {
	apply, ok := kase.Exp.(*core.Apply)
	if !ok {
		return false
	}
	id, ok := apply.Fn.(*core.ID)
	if !ok || id.Pat.Name != "op mod" {
		return false
	}
	tup, ok := apply.Arg.(*core.Tuple)
	if !ok || len(tup.Args) != 2 {
		return false
	}
	lit, ok := tup.Args[1].(*core.Literal)
	if !ok {
		return false
	}
	k, ok := lit.Value.(int32)
	if !ok || k <= 0 {
		return false
	}
	present := map[int32]bool{}
	for _, m := range kase.Matches {
		lp, ok := m.Pat.(*core.LiteralPat)
		if !ok {
			return false
		}
		v, ok := lp.Value.(int32)
		if !ok {
			return false
		}
		present[v] = true
	}
	for i := range k {
		if !present[i] {
			return false
		}
	}
	return true
}

// useful reports whether the pattern vector vec matches some value
// that no row of matrix matches — Maranget's usefulness relation
// U(matrix, vec). Exhaustiveness and redundancy both reduce to it.
func (c *coverageChecker) useful(matrix [][]core.Pat, vec []core.Pat,
) bool {
	if len(vec) == 0 {
		return len(matrix) == 0
	}
	head, rest := vec[0], vec[1:]
	if key, subs, wild := patInfo(head); !wild {
		// A constructor: specialize by it and recurse.
		return c.useful(specialize(matrix, key, len(subs)),
			append(append([]core.Pat{}, subs...), rest...))
	}
	// A wildcard. Gather the constructors present in the first
	// column; if they form a complete signature, the wildcard is
	// useful only where some constructor's specialization is. The
	// column's type comes from a real pattern (an injected wildcard
	// has none), falling back to the wildcard's own type.
	present := map[string]int{}
	colType := head.Type()
	for _, row := range matrix {
		if key, subs, wild := patInfo(row[0]); !wild {
			present[key] = len(subs)
			if t := row[0].Type(); t != nil {
				colType = t
			}
		}
	}
	if len(present) > 0 && c.complete(colType, present) {
		for key, arity := range present {
			if c.useful(specialize(matrix, key, arity),
				append(wildcards(arity), rest...)) {
				return true
			}
		}
		return false
	}
	// Otherwise the wildcard is useful iff it is useful against the
	// default matrix (rows whose first pattern is a wildcard).
	return c.useful(defaultMatrix(matrix), rest)
}

// complete reports whether the constructors present cover every
// constructor of type t, so no wildcard is needed.
func (c *coverageChecker) complete(t types.Type,
	present map[string]int,
) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.List:
		_, nilOK := present[nilKey]
		_, consOK := present[consKey]
		return nilOK && consOK
	case *types.Named:
		return len(present) == c.sys.NumConstructors(t.Name)
	case *types.Primitive:
		switch t.String() {
		case "bool":
			_, hasT := present["lit:bool:true"]
			_, hasF := present["lit:bool:false"]
			return hasT && hasF
		case "unit":
			_, ok := present["lit:unit:()"]
			return ok
		default:
			return false // int, real, string, char, word: infinite
		}
	case *types.Record, *types.Tuple:
		_, ok := present[tupleKey]
		return ok
	default:
		return false
	}
}

const (
	nilKey   = "nil"
	consKey  = "cons"
	tupleKey = "tuple"
)

// patInfo classifies a pattern for the usefulness algorithm: its
// constructor key, its sub-patterns (in match order), and whether
// it is a wildcard (a variable or "_", matching any value).
func patInfo(p core.Pat) (string, []core.Pat, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := p.(type) {
	case *core.Con0Pat:
		return fmt.Sprintf("con:%s:%d", p.Datatype, p.Ordinal), nil, false
	case *core.ConPat:
		return fmt.Sprintf("con:%s:%d", p.Datatype, p.Ordinal),
			[]core.Pat{p.Arg}, false
	case *core.ConsPat:
		return consKey, []core.Pat{p.Head, p.Tail}, false
	case *core.IDPat:
		return "", nil, true
	case *core.ListPat:
		if len(p.Args) == 0 {
			return nilKey, nil, false
		}
		// "[a, b, ...]" is "a :: [b, ...]".
		tail := &core.ListPat{T: p.T, Args: p.Args[1:]}
		return consKey, []core.Pat{p.Args[0], tail}, false
	case *core.LiteralPat:
		switch p.Kind {
		case ast.BoolLiteralOp:
			return fmt.Sprintf("lit:bool:%v", p.Value), nil, false
		case ast.UnitLiteralOp:
			return "lit:unit:()", nil, false
		default:
			return fmt.Sprintf("lit:%s:%v", p.Kind, p.Value), nil, false
		}
	case *core.TuplePat:
		return tupleKey, p.Args, false
	case *core.WildcardPat:
		return "", nil, true
	default:
		return "", nil, true
	}
}

func wildcards(n int) []core.Pat {
	out := make([]core.Pat, n)
	for i := range out {
		out[i] = &core.WildcardPat{}
	}
	return out
}

// specialize returns S(key, matrix): for each row whose first
// pattern is the constructor key, the constructor's sub-patterns
// replace it; a wildcard row contributes arity wildcards; other
// rows are dropped.
func specialize(matrix [][]core.Pat, key string, arity int,
) [][]core.Pat {
	var out [][]core.Pat
	for _, row := range matrix {
		k, subs, wild := patInfo(row[0])
		switch {
		case wild:
			out = append(out, append(wildcards(arity), row[1:]...))
		case k == key:
			out = append(out, append(append([]core.Pat{}, subs...),
				row[1:]...))
		}
	}
	return out
}

// defaultMatrix returns D(matrix): the rows whose first pattern is
// a wildcard, with that first column removed.
func defaultMatrix(matrix [][]core.Pat) [][]core.Pat {
	var out [][]core.Pat
	for _, row := range matrix {
		if _, _, wild := patInfo(row[0]); wild {
			out = append(out, row[1:])
		}
	}
	return out
}

// walkDecl visits every "case" node in a declaration.
func (c *coverageChecker) walkDecl(decl core.Decl) {
	switch d := decl.(type) {
	case *core.NonRecValDecl:
		c.walkExp(d.Exp)
	case *core.RecValDecl:
		for _, b := range d.Binds {
			c.walkExp(b.Exp)
		}
	}
}

func (c *coverageChecker) walkExp(exp core.Exp) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *core.Apply:
		c.walkExp(e.Fn)
		c.walkExp(e.Arg)
	case *core.Case:
		c.walkExp(e.Exp)
		for _, m := range e.Matches {
			c.walkExp(m.Exp)
		}
		if len(e.Matches) > 0 {
			c.checkCase(e)
		}
	case *core.Fn:
		c.walkExp(e.Exp)
	case *core.From:
		c.walkFrom(e)
	case *core.Let:
		c.walkDecl(e.Decl)
		c.walkExp(e.Exp)
	case *core.List:
		for _, a := range e.Args {
			c.walkExp(a)
		}
	case *core.Tuple:
		for _, a := range e.Args {
			c.walkExp(a)
		}
	}
}

func (c *coverageChecker) walkFrom(f *core.From) {
	for _, step := range f.Steps {
		for _, e := range stepExps(step) {
			c.walkExp(e)
		}
	}
}

// stepExps returns the expressions a query step evaluates.
func stepExps(step core.FromStep) []core.Exp {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := step.(type) {
	case *core.Group:
		var out []core.Exp
		for _, k := range s.Keys {
			out = append(out, k.Exp)
		}
		for _, a := range s.Aggs {
			out = append(out, a.Fn)
			if a.Arg != nil {
				out = append(out, a.Arg)
			}
		}
		return out
	case *core.Into:
		return []core.Exp{s.Fn}
	case *core.Order:
		return []core.Exp{s.Exp}
	case *core.Scan:
		return []core.Exp{s.Exp}
	case *core.SetOp:
		return s.Args
	case *core.Skip:
		return []core.Exp{s.Exp}
	case *core.Take:
		return []core.Exp{s.Exp}
	case *core.Through:
		return []core.Exp{s.Fn}
	case *core.Where:
		return []core.Exp{s.Exp}
	case *core.Yield:
		if s.Exp != nil {
			return []core.Exp{s.Exp}
		}
		var out []core.Exp
		for _, f := range s.Fields {
			out = append(out, f.Exp)
		}
		return out
	default:
		return nil
	}
}
