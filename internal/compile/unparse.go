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
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/types"
)

// resolveBuiltins rewrites an identifier that names a structure
// member -- "op ^", or the alias "size" -- into the member form
// itself, "String.^" and "String.size".
//
// morel-java does this in its inliner, which replaces such an
// identifier with the built-in's function literal, and the
// distinction is what a plan's rendering turns on: an operator
// still written as an identifier renders infix, `"one:" ^ s`,
// while a resolved member renders `#^ String ("one:", s)`. That
// is why the initial plan and an optimized one spell the same
// application differently.
//
// morel-go's inliner leaves the identifier alone, so the
// substitution happens here instead, on the tree a plan is
// rendered from and never on the tree that is evaluated.
func resolveBuiltins(d core.Decl) core.Decl {
	r := &rewriter{}
	r.exp = func(e core.Exp) (core.Exp, bool) {
		id, ok := e.(*core.ID)
		if !ok || strings.Contains(id.Pat.Name, ".") {
			return nil, false
		}
		qualified := planFnName(id.Pat.Name, id.Pat.T)
		structName, member, ok := strings.Cut(qualified, ".")
		if !ok {
			return nil, false
		}
		return &core.Apply{
			T:   id.Pat.T,
			Fn:  &core.Selector{T: id.Pat.T, Name: member},
			Arg: &core.ID{Pat: &core.IDPat{Name: structName}},
		}, true
	}
	return r.rewriteDecl(d)
}

// UnparseDecl renders a Core declaration as one line of source
// text — the form Sys.planEx returns. Distinct variables sharing
// a name are renumbered in first-appearance order: the first
// keeps the name, later ones append "_1", "_2", and so on.
func UnparseDecl(sys *types.System, decl core.Decl) string {
	u := &unparser{sys: sys, seen: map[string][]*core.IDPat{}}
	u.decl(decl)
	return u.sb.String()
}

// Operator contexts: an operator's left and right binding powers.
// A sub-expression is parenthesized when the context binds
// tighter than the expression's own operator.
const (
	// precQuery is the binding of a query, and of "fn", "let" and
	// "case": each has an open-ended tail, so each parenthesizes
	// where a step keyword could otherwise be read as its own.
	precQuery   = 1
	precOrelse  = 1 // orelse: left 2, right 3
	precAndalso = 2
	precCompare = 4 // = <> < <= > >= elem: non-associative
	precCons    = 5 // :: @: right-associative
	precPlus    = 6 // + - ^
	precTimes   = 7
	precApply   = 8
	precAtom    = 99
)

// binding is an operator's left and right binding powers,
// derived from precedence and associativity: each power is twice
// the precedence, the far side of the associativity one tighter.
func binding(prec int, assoc rune) (int, int) {
	lo := prec + prec
	hi := lo + 1
	switch assoc {
	case 'l':
		return lo, hi
	case 'r':
		return hi, lo
	default:
		return hi, hi
	}
}

// unparser accumulates the rendering.
type unparser struct {
	sys  *types.System
	sb   strings.Builder
	seen map[string][]*core.IDPat
}

func (u *unparser) put(s string) { u.sb.WriteString(s) }

// name renders a variable, renumbering repeats of its name.
func (u *unparser) name(pat *core.IDPat) {
	list := u.seen[pat.Name]
	for i, p := range list {
		if p == pat {
			u.put(suffixed(pat.Name, i))
			return
		}
	}
	u.seen[pat.Name] = append(list, pat)
	u.put(suffixed(pat.Name, len(list)))
}

// suffixed quotes a name that has to be quoted to be read back --
// a reserved word such as "left" -- and appends the renumbering
// suffix. The suffix goes outside the quotes, so that what is
// marked as the reserved word is the name the user wrote.
func suffixed(name string, i int) string {
	quoted := parse.QuoteIdent(name)
	if i == 0 {
		return quoted
	}
	return quoted + "_" + strconv.Itoa(i)
}

// decl renders a declaration.
func (u *unparser) decl(d core.Decl) {
	switch d := d.(type) {
	case *core.NonRecValDecl:
		u.put("val ")
		u.pat(d.Pat)
		u.put(" = ")
		u.exp(d.Exp, 0, 0)
	case *core.RecValDecl:
		u.put("val rec ")
		for i, b := range d.Binds {
			if i > 0 {
				u.put(" and ")
			}
			u.pat(b.Pat)
			u.put(" = ")
			u.exp(b.Exp, 0, 0)
		}
	}
}

// exp renders an expression in an operator context.
func (u *unparser) exp(e core.Exp, left, right int) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := e.(type) {
	case *core.Apply:
		u.apply(e, left, right)
	case *core.Case:
		u.caseExp(e, left, right)
	case *core.Con:
		u.put(e.Name)
	case *core.Fn:
		u.wrap(left, right, 1, 1, func() {
			u.put("fn ")
			u.name(e.IDPat)
			u.put(" => ")
			u.exp(e.Exp, 0, 0)
		})
	case *core.From:
		u.wrap(left, right, 1, 1, func() { u.from(e) })
	case *core.ID:
		u.name(e.Pat)
	case *core.Let:
		u.wrap(left, right, 1, 1, func() {
			u.put("let ")
			u.decl(e.Decl)
			u.put(" in ")
			u.exp(e.Exp, 0, 0)
			u.put(" end")
		})
	case *core.List:
		// A list literal is an application underneath, so as an
		// argument it parenthesizes.
		l, r := binding(precApply, 'l')
		u.wrap(left, right, l, r, func() {
			u.put("[")
			u.exps(e.Args)
			u.put("]")
		})
	case *core.Literal:
		u.literal(e)
	case *core.RangeList:
		u.rangeList(e)
	case *core.Selector:
		u.put("#" + e.Name)
	case *core.Tuple:
		u.tuple(e)
	default:
		u.put("?")
	}
}

// wrap parenthesizes body when the context binds tighter than the
// node.
func (u *unparser) wrap(left, right, nodeLeft, nodeRight int,
	body func(),
) {
	if left > nodeLeft || nodeRight < right {
		u.put("(")
		body()
		u.put(")")
		return
	}
	body()
}

// exps renders a comma-separated list.
func (u *unparser) exps(args []core.Exp) {
	for i, a := range args {
		if i > 0 {
			u.put(", ")
		}
		u.exp(a, 0, 0)
	}
}

// tuple renders a tuple, as a record when its type is one.
func (u *unparser) tuple(e *core.Tuple) {
	if rec, ok := e.T.(*types.Record); ok {
		u.put("{")
		for i, f := range rec.Fields {
			if i > 0 {
				u.put(", ")
			}
			u.put(f.Label + " = ")
			u.exp(e.Args[i], 0, 0)
		}
		u.put("}")
		return
	}
	if len(e.Args) == 0 {
		u.put("()")
		return
	}
	u.put("(")
	u.exps(e.Args)
	u.put(")")
}

// literal renders a constant.
func (u *unparser) literal(e *core.Literal) {
	// lint: sort until '^\t}' where '^\tcase '
	switch v := e.Value.(type) {
	case *eval.RangeExtent:
		u.put(strconv.Quote(v.T.String()))
	case bool:
		u.put(strconv.FormatBool(v))
	case core.Unit:
		u.put("()")
	case float32:
		u.put(negText(strconv.FormatFloat(float64(v), 'g', -1,
			32)))
	case int32:
		if e.Kind == ast.CharLiteralOp {
			u.put(charText(v))
			return
		}
		u.put(negText(strconv.FormatInt(int64(v), 10)))
	case string:
		u.put(strconv.Quote(v))
	case uint64:
		u.put(fmt.Sprintf("0wx%X", v))
	default:
		u.put("?")
	}
}

// negText renders a numeric text with Morel's "~" negation.
func negText(s string) string {
	if strings.HasPrefix(s, "-") {
		return "~" + s[1:]
	}
	return s
}

// charText renders a character literal.
func charText(c rune) string {
	switch c {
	case '"':
		return `#"\""`
	case '\\':
		return `#"\\"`
	default:
		return `#"` + string(c) + `"`
	}
}

// rangeList renders "[lo .. hi]" forms.
func (u *unparser) rangeList(e *core.RangeList) {
	u.put("[")
	for i, item := range e.Items {
		if i > 0 {
			u.put(", ")
		}
		if item.Lo != nil {
			u.exp(item.Lo, 0, 0)
		}
		if item.Kind != ast.RangePoint {
			u.put(" .. ")
			if item.Hi != nil {
				u.exp(item.Hi, 0, 0)
			}
		}
	}
	u.put("]")
}

// infixName maps a core operator name to its infix spelling, for
// the operators that render infix. It covers every infix operator
// of the grammar, as morel-java's Op.BY_OP_NAME does: an operator
// written as an identifier -- "op ^" -- has not been resolved to
// the structure member that implements it, and so still renders
// as the operator it was written as. See infixOf.
func infixName(name string) (string, int, rune) {
	// lint: sort until '^\t}' where '^\tcase '
	switch name {
	case eqOpName, opGe, opGt, opLe, opLt, opNe:
		return strings.TrimPrefix(name, "op "), precCompare, 'n'
	case opAt, opCons:
		return strings.TrimPrefix(name, "op "), precCons, 'r'
	case opCaret, opMinus, opPlus:
		return strings.TrimPrefix(name, "op "), precPlus, 'l'
	case opDiv, opMod, opTimes:
		return strings.TrimPrefix(name, "op "), precTimes, 'l'
	default:
		return "", 0, 0
	}
}

// infixOf returns the infix spelling of an application's
// function, and whether it has one.
//
// An unresolved operator -- a bare "op X" identifier -- always
// renders infix. A resolved structure member renders as
// "#member Structure", with the sole exception of "Int.+" and
// "Real.+", which morel-java keeps infix (Resolver.toOp); that is
// why a plan can read "#* Int (x, y + 3)", mixing the two forms
// in one expression.
func infixOf(fn core.Exp) (string, int, rune) {
	name := builtinName(fn)
	if op, prec, assoc := infixName(name); op != "" {
		if _, isID := fn.(*core.ID); isID {
			return op, prec, assoc
		}
		return "", 0, 0
	}
	switch name {
	case "Int.+", "Real.+":
		return "+", precPlus, 'l'
	default:
		return "", 0, 0
	}
}

// apply renders an application: infix for the comparison and plus
// family, "#member Structure" for other qualified builtins, the
// extent form for internal extents, and juxtaposition otherwise.
func (u *unparser) apply(e *core.Apply, left, right int) {
	name := builtinName(e.Fn)
	if name == ExtentName {
		lit, ok := e.Arg.(*core.Literal)
		if ok {
			l, r := binding(precApply, 'l')
			u.wrap(left, right, l, r, func() {
				u.put("extent ")
				u.literal(lit)
			})
			return
		}
	}
	if op, prec, assoc := infixOf(e.Fn); op != "" {
		if tuple, ok := e.Arg.(*core.Tuple); ok &&
			len(tuple.Args) == 2 {
			l, r := binding(prec, assoc)
			u.wrap(left, right, l, r, func() {
				u.exp(tuple.Args[0], left, l)
				u.put(" " + op + " ")
				u.exp(tuple.Args[1], r, right)
			})
			return
		}
	}
	fnText, ok := u.applyFnText(e)
	l, r := binding(precApply, 'l')
	u.wrap(left, right, l, r, func() {
		if ok {
			u.put(fnText)
		} else {
			u.exp(e.Fn, left, l)
		}
		u.put(" ")
		u.exp(e.Arg, r, right)
	})
}

// applyFnText renders an application's function position as text:
// "not", "op elem", "#member Structure", or a record selection.
func (u *unparser) applyFnText(e *core.Apply) (string, bool) {
	// lint: sort until '^\t}' where '^\tcase '
	switch fn := e.Fn.(type) {
	case *core.Apply:
		// A qualified builtin: Structure.member.
		if name := builtinName(e.Fn); name != "" &&
			strings.Contains(name, ".") {
			return sharpName(name), true
		}
		return "", false
	case *core.ID:
		name := fn.Pat.Name
		if name == notName {
			return notName, true
		}
		qualified := planFnName(name, fn.Pat.T)
		if strings.Contains(qualified, ".") {
			return sharpName(qualified), true
		}
		if strings.HasPrefix(name, "op ") ||
			strings.Contains(name, ".") {
			if strings.Contains(name, ".") {
				return sharpName(name), true
			}
			return name, true
		}
		return "", false
	case *core.Selector:
		return "#" + fn.Name, true
	default:
		return "", false
	}
}

// sharpName renders "Structure.member" as "#member Structure".
func sharpName(qualified string) string {
	dot := strings.Index(qualified, ".")
	return "#" + qualified[dot+1:] + " " + qualified[:dot]
}

// caseExp renders a case, spelling the boolean-connective
// encodings back as operators.
func (u *unparser) caseExp(e *core.Case, left, right int) {
	if cond, ifTrue, ifFalse, ok := asBoolCase(e); ok {
		if isBoolLiteral(ifFalse, false) &&
			!isBoolLiteral(ifTrue, true) {
			l, r := binding(precAndalso, 'l')
			u.wrap(left, right, l, r, func() {
				u.exp(cond, left, l)
				u.put(" andalso ")
				u.exp(ifTrue, r, right)
			})
			return
		}
		if isBoolLiteral(ifTrue, true) &&
			!isBoolLiteral(ifFalse, false) {
			l, r := binding(precOrelse, 'l')
			u.wrap(left, right, l, r, func() {
				u.exp(cond, left, l)
				u.put(" orelse ")
				u.exp(ifFalse, r, right)
			})
			return
		}
	}
	u.wrap(left, right, 1, 1, func() {
		u.put("case ")
		u.exp(e.Exp, 0, 0)
		u.put(" of ")
		for i, m := range e.Matches {
			if i > 0 {
				u.put(" | ")
			}
			u.pat(m.Pat)
			u.put(" => ")
			u.exp(m.Exp, 0, 0)
		}
	})
}

// from renders a query.
func (u *unparser) from(e *core.From) {
	switch e.Kind {
	case ast.ExistsOp:
		u.put("exists")
	case ast.ForallOp:
		u.put("forall")
	default:
		u.put("from")
	}
	scans := 0
	var rowVar *core.IDPat
	rowVars := 0
	for _, step := range e.Steps {
		u.step(step, &scans, &rowVar, &rowVars)
	}
}

// step renders one query step.
func (u *unparser) step(step core.FromStep, scans *int,
	rowVar **core.IDPat, rowVars *int,
) {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := step.(type) {
	case *core.Distinct:
		// A distinct over a single variable is spelled as an
		// atom group.
		if *rowVars == 1 {
			u.put(" group ")
			u.name(*rowVar)
		} else {
			u.put(" distinct")
		}
	case *core.Group:
		u.group(s)
	case *core.Into:
		u.put(" into ")
		u.exp(s.Fn, 0, 0)
	case *core.Order:
		u.put(" order ")
		u.exp(s.Exp, 0, 0)
	case *core.Scan:
		if *scans == 0 {
			u.put(" ")
		} else {
			u.put(" join ")
		}
		*scans++
		u.scanPat(s.Pat)
		if id, ok := s.Pat.(*core.IDPat); ok {
			*rowVar = id
		}
		*rowVars += len(core.PatIDs(s.Pat))
		if isInfiniteExtent(s.Exp) {
			u.put(" : " + collectionElem(s.Exp.Type()).String())
			return
		}
		u.put(" in ")
		u.exp(s.Exp, precQuery+1, precQuery+1)
	case *core.Skip:
		u.put(" skip ")
		u.exp(s.Exp, 0, 0)
	case *core.Take:
		u.put(" take ")
		u.exp(s.Exp, 0, 0)
	case *core.Through:
		u.put(" through ")
		u.scanPat(s.Pat)
		u.put(" in ")
		u.exp(s.Fn, 0, 0)
	case *core.Where:
		u.put(" where ")
		u.exp(s.Exp, 0, 0)
	case *core.Yield:
		u.yield(s, rowVar, rowVars)
	}
}

// yield renders a yield step.
func (u *unparser) yield(s *core.Yield, rowVar **core.IDPat,
	rowVars *int,
) {
	if s.Fields != nil {
		u.put(" yield {")
		for i, f := range s.Fields {
			if i > 0 {
				u.put(", ")
			}
			u.put(f.Pat.Name + " = ")
			u.exp(f.Exp, 0, 0)
		}
		u.put("}")
		*rowVars = len(s.Fields)
		if len(s.Fields) == 1 {
			*rowVar = s.Fields[0].Pat
		}
		return
	}
	u.put(" yield ")
	u.exp(s.Exp, 0, 0)
	if id, ok := s.Exp.(*core.ID); ok {
		*rowVar = id.Pat
		*rowVars = 1
	}
}

// group renders a group step.
func (u *unparser) group(s *core.Group) {
	u.put(" group ")
	if len(s.Keys) == 1 && len(s.Aggs) == 0 {
		u.exp(s.Keys[0].Exp, 0, 0)
		return
	}
	u.put("{")
	for i, k := range s.Keys {
		if i > 0 {
			u.put(", ")
		}
		u.put(k.Pat.Name + " = ")
		u.exp(k.Exp, 0, 0)
	}
	u.put("}")
	if len(s.Aggs) > 0 {
		u.put(" compute {")
		for i, a := range s.Aggs {
			if i > 0 {
				u.put(", ")
			}
			u.put(a.Pat.Name + " = ")
			u.exp(a.Fn, 0, 0)
			if a.Arg != nil {
				u.put(" over ")
				u.exp(a.Arg, 0, 0)
			}
		}
		u.put("}")
	}
}

// scanPat renders a scan's pattern, parenthesizing record
// patterns.
func (u *unparser) scanPat(p core.Pat) {
	if tp, ok := p.(*core.TuplePat); ok {
		if _, isRec := tp.T.(*types.Record); isRec {
			u.put("(")
			u.pat(p)
			u.put(")")
			return
		}
	}
	u.pat(p)
}

// pat renders a pattern.
func (u *unparser) pat(p core.Pat) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := p.(type) {
	case *core.AsPat:
		u.name(p.Pat)
		u.put(" as ")
		u.pat(p.Body)
	case *core.Con0Pat:
		u.put(p.Name)
	case *core.ConPat:
		u.put(p.Name + "(")
		u.pat(p.Arg)
		u.put(")")
	case *core.ConsPat:
		u.pat(p.Head)
		u.put(" :: ")
		u.pat(p.Tail)
	case *core.IDPat:
		u.name(p)
	case *core.ListPat:
		u.put("[")
		for i, a := range p.Args {
			if i > 0 {
				u.put(", ")
			}
			u.pat(a)
		}
		u.put("]")
	case *core.LiteralPat:
		u.literal(&core.Literal{
			T: p.T, Kind: p.Kind,
			Value: p.Value,
		})
	case *core.TuplePat:
		if rec, ok := p.T.(*types.Record); ok {
			u.put("{")
			for i, f := range rec.Fields {
				if i > 0 {
					u.put(", ")
				}
				u.put(f.Label + " = ")
				u.pat(p.Args[i])
			}
			u.put("}")
			return
		}
		u.put("(")
		for i, a := range p.Args {
			if i > 0 {
				u.put(", ")
			}
			u.pat(a)
		}
		u.put(")")
	case *core.WildcardPat:
		u.put("_")
	default:
		u.put("?")
	}
}
