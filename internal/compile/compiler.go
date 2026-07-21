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
	"sort"
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/types"
)

// Bind is one name that a compiled statement binds, and the
// frame slot that holds its value after the statement runs.
type Bind struct {
	Pat  *core.IDPat
	Slot int
}

// Compiled is a statement ready to run: code that evaluates the
// statement's expressions and stores each bound name's value in
// its slot, the bindings to read back out (in pattern order),
// and the frame size.
type Compiled struct {
	Binds []Bind
	Code  eval.Code
	// Plan is the code of the bound expression — the code that
	// Sys.plan describes. For "val pat = exp" it is exp's code,
	// without the surrounding binding.
	Plan  eval.Code
	Slots int
}

// Statement compiles a declaration such as "val pat = exp".
// Values gives the runtime values of free names: built-ins and
// the results of earlier statements.
func Statement(decl core.Decl,
	values map[string]eval.Val, sys *types.System,
) (*Compiled, error) {
	c := &compiler{
		values:      values,
		slots:       map[*core.IDPat]int{},
		ordinalSlot: -1,
		sys:         sys,
	}
	var code, plan eval.Code
	var ids []*core.IDPat
	switch d := decl.(type) {
	case *core.NonRecValDecl:
		exp, err := c.compileExp(d.Exp)
		if err != nil {
			return nil, err
		}
		pat, err := c.compilePat(d.Pat)
		if err != nil {
			return nil, err
		}
		code = eval.Let(pat, exp, eval.Unit(), d.Span)
		plan = exp
		ids = core.PatIDs(d.Pat)
	case *core.RecValDecl:
		var err error
		code, err = c.compileRec(d, eval.Unit())
		if err != nil {
			return nil, err
		}
		for _, bind := range d.Binds {
			ids = append(ids, core.PatIDs(bind.Pat)...)
		}
	default:
		return nil, &Error{
			Msg: "cannot compile " + decl.Op().String(),
		}
	}
	binds := make([]Bind, len(ids))
	for i, id := range ids {
		binds[i] = Bind{Pat: id, Slot: c.slots[id]}
	}
	return &Compiled{
		Binds: binds,
		Code:  code,
		Plan:  plan,
		Slots: c.nSlots,
	}, nil
}

// compiler converts Core to Code, assigning each bound variable
// a frame slot. Each function body gets its own compiler (and
// frame layout); a reference to an enclosing function's variable
// becomes a capture.
type compiler struct {
	values   map[string]eval.Val
	slots    map[*core.IDPat]int
	parent   *compiler
	sys      *types.System
	captures []eval.Capture
	nSlots   int
	// ordinalSlot is the frame slot holding the current query row's
	// position, allocated on the first use of "ordinal" in a query,
	// or -1 when the query does not use it.
	ordinalSlot int
}

// resolveSlot returns the frame slot of a variable. A variable
// of an enclosing function is captured into a fresh slot of this
// frame — transitively, so each scope between the use and the
// declaration captures it in turn.
func (c *compiler) resolveSlot(pat *core.IDPat) (int, bool) {
	if slot, ok := c.slots[pat]; ok {
		return slot, true
	}
	if c.parent == nil {
		return 0, false
	}
	outer, ok := c.parent.resolveSlot(pat)
	if !ok {
		return 0, false
	}
	slot := c.nSlots
	c.nSlots++
	c.slots[pat] = slot
	c.captures = append(c.captures,
		eval.Capture{From: outer, To: slot})
	return slot, true
}

func (c *compiler) compileExp(exp core.Exp) (eval.Code, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *core.Apply:
		fn, err := c.compileExp(e.Fn)
		if err != nil {
			return nil, err
		}
		arg, err := c.compileExp(e.Arg)
		if err != nil {
			return nil, err
		}
		name, arity, curried := c.builtinFnInfo(e.Fn, fn)
		return eval.Apply(fn, arg, e.Span, name, arity, curried), nil
	case *core.Case:
		return c.compileCase(e)
	case *core.Con:
		if e.Datatype == variantTypeName {
			return c.compileVariantCon(e)
		}
		con := eval.Con{
			Datatype: e.Datatype,
			Name:     e.Name,
			Ordinal:  e.Ordinal,
		}
		if !e.HasArg {
			return eval.Constant(con), nil
		}
		return eval.Constant(eval.Fn(
			func(arg eval.Val) (eval.Val, error) {
				con2 := con
				con2.Arg = arg
				return con2, nil
			})), nil
	case *core.Fn:
		return c.compileFn(e)
	case *core.From:
		return c.compileFrom(e)
	case *core.ID:
		if slot, ok := c.resolveSlot(e.Pat); ok {
			return eval.GetSlot(slot, e.Pat.Name), nil
		}
		if v, ok := c.values[e.Pat.Name]; ok {
			return eval.Constant(v), nil
		}
		return nil, &Error{Msg: "not found: " + e.Pat.Name}
	case *core.Let:
		return c.compileLet(e)
	case *core.List:
		args := make([]eval.Code, len(e.Args))
		for i, arg := range e.Args {
			a, err := c.compileExp(arg)
			if err != nil {
				return nil, err
			}
			args[i] = a
		}
		return eval.Tuple(args), nil
	case *core.Literal:
		return eval.Constant(e.Value), nil
	case *core.Ordinal:
		if c.ordinalSlot < 0 {
			c.ordinalSlot = c.nSlots
			c.nSlots++
		}
		return eval.GetSlot(c.ordinalSlot, ordinalName), nil
	case *core.Selector:
		return eval.Constant(eval.Nth(e.Index)), nil
	case *core.Tuple:
		args := make([]eval.Code, len(e.Args))
		for i, arg := range e.Args {
			a, err := c.compileExp(arg)
			if err != nil {
				return nil, err
			}
			args[i] = a
		}
		return eval.Tuple(args), nil
	default:
		return nil, &Error{
			Msg: "cannot compile " + exp.Op().String(),
		}
	}
}

// builtinFnInfo returns the qualified name and argument arity of
// the function in an application, when it is a built-in. The
// compiled plan names such a function; for anything else it
// returns "", 0. A built-in is referenced either directly, as a
// named value whose code is a bare function (an operator such as
// "op +"), or as a structure member, "Structure.member", which
// resolves to a field selection over a structure value.
func (c *compiler) builtinFnInfo(fnExp core.Exp,
	fnCode eval.Code,
) (string, int, int) {
	// lint: sort until '^\t}' where '^\tcase '
	switch fn := fnExp.(type) {
	case *core.Apply:
		sel, isSel := fn.Fn.(*core.Selector)
		id, isID := fn.Arg.(*core.ID)
		if !isSel || !isID {
			return "", 0, 0
		}
		name := id.Pat.Name + "." + sel.Name
		if _, ok := c.values[name]; !ok {
			return "", 0, 0
		}
		return planFnName(name), builtinArity(fn.Type()),
			curriedArity(fn.Type())
	case *core.ID:
		if !eval.IsBuiltinFn(fnCode) {
			return "", 0, 0
		}
		return planFnName(fn.Pat.Name), builtinArity(fn.Pat.T),
			curriedArity(fn.Pat.T)
	default:
		return "", 0, 0
	}
}

// curriedArity is the number of curried arguments a built-in takes,
// used to collapse a fully applied curried built-in to apply2/apply3
// in the plan. It is the number of arrows in the type — except that
// a built-in whose parameter is a tuple takes it as a single
// argument (rendered via the tuple form) and its result, if a
// function, is applied separately, so its arity is 1. This keeps
// "(f o g) x" from folding the outer application into the compose.
func curriedArity(t types.Type) int {
	if fn, isFn := t.(*types.Fn); isFn {
		if _, isTuple := fn.Param.(*types.Tuple); isTuple {
			return 1
		}
	}
	n := 0
	for {
		fn, isFn := t.(*types.Fn)
		if !isFn {
			return n
		}
		n++
		t = fn.Result
	}
}

// planFnName is a built-in's name as the compiled plan shows it:
// an operator (whose name begins "op ") drops the prefix and is
// shown bare, as "+" or "@".
func planFnName(name string) string {
	if rest, ok := strings.CutPrefix(name, "op "); ok {
		return rest
	}
	return name
}

// builtinArity is the number of arguments a function type takes at
// once: the size of its parameter tuple, or 1 when the parameter
// is not a tuple.
func builtinArity(t types.Type) int {
	fn, isFn := t.(*types.Fn)
	if !isFn {
		return 1
	}
	if tup, isTuple := fn.Param.(*types.Tuple); isTuple {
		return len(tup.Args)
	}
	return 1
}

// compileFrom compiles a query into a pipeline of stages that
// scan collections and filter rows, then collect each surviving
// row. A scan's source is compiled in the current scope, so a
// later scan may depend on an earlier one's variables; its pattern
// gets frame slots, which the following stages and the collected
// value read. With a trailing yield, the collected value is the
// yield expression; otherwise it is the row itself — the sole
// bound variable, or a record of all the scan variables in label
// order.
func (c *compiler) compileFrom(from *core.From) (eval.Code, error) {
	// "ordinal" allocates a slot on first use within this query; a
	// nested query has its own, so save and restore the outer slot.
	savedOrdinal := c.ordinalSlot
	c.ordinalSlot = -1
	defer func() { c.ordinalSlot = savedOrdinal }()
	b := &fromBuilder{c: c}
	for _, step := range from.Steps {
		err := b.add(step)
		if err != nil {
			return nil, err
		}
	}
	collect := c.rowCode(b.rowPats, collectionElem(from.T))
	if b.yieldExp != nil {
		var err error
		collect, err = c.compileExp(b.yieldExp)
		if err != nil {
			return nil, err
		}
	}
	query := eval.From(b.allSlots, b.stages, collect, c.ordinalSlot)
	if b.intoFn != nil {
		fn, err := c.compileExp(b.intoFn)
		if err != nil {
			return nil, err
		}
		return eval.Into(query, fn), nil
	}
	if from.Kind == ast.ExistsOp {
		return eval.Exists(query), nil
	}
	if from.Kind == ast.ForallOp {
		return eval.Forall(query), nil
	}
	return query, nil
}

// fromBuilder accumulates a query's pipeline: its stages, the
// patterns making up the current row (rowPats, replaced by a group
// or through), every bound slot (allSlots), and a trailing yield or
// into.
type fromBuilder struct {
	c        *compiler
	stages   []eval.FromStage
	rowPats  []core.Pat
	allSlots []int
	yieldExp core.Exp
	intoFn   core.Exp
}

// add compiles one query step into the builder.
func (b *fromBuilder) add(step core.FromStep) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := step.(type) {
	case *core.Group:
		stage, outPats, err := b.c.compileGroup(s)
		if err != nil {
			return err
		}
		b.rebind(stage, outPats)
		return nil
	case *core.Into:
		b.intoFn = s.Fn
		return nil
	case *core.Through:
		stage, err := b.c.compileThrough(s, b.rowPats)
		if err != nil {
			return err
		}
		b.rebind(stage, []core.Pat{s.Pat})
		return nil
	case *core.Yield:
		if s.Fields != nil {
			stage, outPats, err := b.c.compileYield(s)
			if err != nil {
				return err
			}
			b.rebind(stage, outPats)
			return nil
		}
		b.yieldExp = s.Exp
		return nil
	default:
		stage, err := b.c.compileStep(step, &b.rowPats)
		if err != nil {
			return err
		}
		b.stages = append(b.stages, stage)
		if scan, ok := step.(*core.Scan); ok {
			b.allSlots = append(b.allSlots,
				b.c.patSlots([]core.Pat{scan.Pat})...)
		}
		return nil
	}
}

// rebind adds a stage that replaces the current row with new fields.
func (b *fromBuilder) rebind(stage eval.FromStage, outPats []core.Pat) {
	b.stages = append(b.stages, stage)
	b.rowPats = outPats
	b.allSlots = append(b.allSlots, b.c.patSlots(outPats)...)
}

// compileThrough compiles a through step: the input row's value
// feeds the function, and the pattern binds each element of the
// result to fresh slots.
func (c *compiler) compileThrough(t *core.Through, rowPats []core.Pat,
) (eval.FromStage, error) {
	row := c.rowCode(rowPats, nil)
	fn, err := c.compileExp(t.Fn)
	if err != nil {
		return nil, err
	}
	pat, err := c.compilePat(t.Pat)
	if err != nil {
		return nil, err
	}
	return &eval.ThroughStage{Row: row, Fn: fn, Pat: pat}, nil
}

// patSlots returns the frame slots of the patterns' variables.
func (c *compiler) patSlots(pats []core.Pat) []int {
	ids := sortedVarIDs(pats)
	slots := make([]int, len(ids))
	for i, id := range ids {
		slots[i] = c.slots[id]
	}
	return slots
}

// compileGroup compiles a group step, returning its stage and the
// output field patterns (the query's variables downstream). The key
// and aggregate-argument expressions are compiled over the input
// row's slots; each key and aggregate gets an output slot.
func (c *compiler) compileGroup(g *core.Group) (eval.FromStage,
	[]core.Pat, error,
) {
	keys := make([]eval.GroupKeyCode, len(g.Keys))
	outPats := make([]core.Pat, 0, len(g.Keys)+len(g.Aggs))
	for i, k := range g.Keys {
		code, err := c.compileExp(k.Exp)
		if err != nil {
			return nil, nil, err
		}
		keys[i] = eval.GroupKeyCode{Code: code, Slot: c.allocSlot(k.Pat)}
		outPats = append(outPats, k.Pat)
	}
	aggs := make([]eval.GroupAggCode, len(g.Aggs))
	for i, a := range g.Aggs {
		fn, err := c.compileExp(a.Fn)
		if err != nil {
			return nil, nil, err
		}
		var arg eval.Code
		if a.Arg != nil {
			arg, err = c.compileExp(a.Arg)
			if err != nil {
				return nil, nil, err
			}
		}
		aggs[i] = eval.GroupAggCode{
			Fn: fn, Arg: arg, Slot: c.allocSlot(a.Pat),
		}
		outPats = append(outPats, a.Pat)
	}
	return &eval.GroupStage{Keys: keys, Aggs: aggs}, outPats, nil
}

// compileYield compiles a mid-query "yield" into a stage that
// rebinds the row: each field's value is computed from the input
// row and written to a fresh output slot that later steps read.
func (c *compiler) compileYield(y *core.Yield) (eval.FromStage,
	[]core.Pat, error,
) {
	fields := make([]eval.YieldFieldCode, len(y.Fields))
	outPats := make([]core.Pat, len(y.Fields))
	for i, f := range y.Fields {
		code, err := c.compileExp(f.Exp)
		if err != nil {
			return nil, nil, err
		}
		fields[i] = eval.YieldFieldCode{Code: code, Slot: c.allocSlot(f.Pat)}
		outPats[i] = f.Pat
	}
	return &eval.YieldStage{Fields: fields}, outPats, nil
}

// compileStep compiles one query step (other than a trailing
// yield) to a pipeline stage, recording each scan's pattern in
// scanPats.
func (c *compiler) compileStep(step core.FromStep,
	scanPats *[]core.Pat,
) (eval.FromStage, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := step.(type) {
	case *core.Distinct:
		ids := sortedVarIDs(*scanPats)
		slots := make([]int, len(ids))
		for i, id := range ids {
			slots[i] = c.allocSlot(id)
		}
		return &eval.DistinctStage{Slots: slots}, nil
	case *core.Order:
		key, err := c.compileExp(s.Exp)
		if err != nil {
			return nil, err
		}
		return &eval.OrderStage{Key: key}, nil
	case *core.Scan:
		source, err := c.compileExp(s.Exp)
		if err != nil {
			return nil, err
		}
		pat, err := c.compilePat(s.Pat)
		if err != nil {
			return nil, err
		}
		*scanPats = append(*scanPats, s.Pat)
		return &eval.ScanStage{Source: source, Pat: pat}, nil
	case *core.SetOp:
		return c.compileSetOp(s, *scanPats)
	case *core.Skip:
		count, err := c.compileExp(s.Exp)
		if err != nil {
			return nil, err
		}
		return &eval.SkipStage{Count: count}, nil
	case *core.Take:
		count, err := c.compileExp(s.Exp)
		if err != nil {
			return nil, err
		}
		return &eval.TakeStage{Count: count}, nil
	case *core.Where:
		cond, err := c.compileExp(s.Exp)
		if err != nil {
			return nil, err
		}
		return &eval.WhereStage{Cond: cond}, nil
	default:
		return nil, &Error{Msg: "cannot compile " + s.Op().String()}
	}
}

// compileSetOp compiles a union/intersect/except step. Its
// arguments are collections of the row type, compiled in the
// enclosing scope. A row is a record when several scan variables
// are in scope so far, or a single value otherwise.
func (c *compiler) compileSetOp(s *core.SetOp, scanPats []core.Pat,
) (eval.FromStage, error) {
	args := make([]eval.Code, len(s.Args))
	for i, arg := range s.Args {
		a, err := c.compileExp(arg)
		if err != nil {
			return nil, err
		}
		args[i] = a
	}
	ids := sortedVarIDs(scanPats)
	slots := make([]int, len(ids))
	for i, id := range ids {
		slots[i] = c.allocSlot(id)
	}
	return &eval.SetOpStage{
		Args:     args,
		Kind:     setOpKinds[s.Kind],
		Distinct: s.Distinct,
		Slots:    slots,
	}, nil
}

// setOpKinds maps a set-operation Op to its evaluator kind.
var setOpKinds = map[ast.Op]eval.SetOpKind{
	ast.UnionOp:     eval.SetUnion,
	ast.IntersectOp: eval.SetIntersect,
	ast.ExceptOp:    eval.SetExcept,
}

// rowCode is the code for a query row that has no explicit yield:
// the sole bound variable's value, or a record (a []Val in
// label-sorted order) of all the row variables. A single variable
// is a bare value — unless it is the sole field of a one-field
// record row (from "yield {x = e}"), in which case its value is
// wrapped. That case is told apart from a lone variable that
// happens to hold a record (a scan of records) by elem being a
// one-field record whose label is the variable's name.
func (c *compiler) rowCode(pats []core.Pat, elem types.Type) eval.Code {
	ids := sortedVarIDs(pats)
	if len(ids) == 1 && !singletonRecord(elem, ids[0].Name) {
		return eval.GetSlot(c.slots[ids[0]], ids[0].Name)
	}
	args := make([]eval.Code, len(ids))
	for i, id := range ids {
		args[i] = eval.GetSlot(c.slots[id], id.Name)
	}
	return eval.Tuple(args)
}

// singletonRecord reports whether elem is a record with a single
// field of the given label.
func singletonRecord(elem types.Type, label string) bool {
	rec, ok := elem.(*types.Record)
	return ok && len(rec.Fields) == 1 && rec.Fields[0].Label == label
}

// collectionElem is the element type of a list or bag type.
func collectionElem(t types.Type) types.Type {
	switch t := t.(type) {
	case *types.List:
		return t.Elem
	case *types.Named:
		if len(t.Args) == 1 {
			return t.Args[0]
		}
	}
	return nil
}

// sortedVarIDs is the patterns' bound variables, sorted by name —
// the order of a row record's fields, and of the query's saved
// frame slots.
func sortedVarIDs(pats []core.Pat) []*core.IDPat {
	ids := make([]*core.IDPat, 0, len(pats))
	for _, pat := range pats {
		ids = append(ids, core.PatIDs(pat)...)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].Name < ids[j].Name
	})
	return ids
}

// compileFn compiles a function body in its own scope, then
// emits code that creates the closure, capturing whatever the
// body referenced from enclosing scopes.
func (c *compiler) compileFn(fn *core.Fn) (eval.Code, error) {
	inner := &compiler{
		values:      c.values,
		slots:       map[*core.IDPat]int{},
		ordinalSlot: -1,
		parent:      c,
		sys:         c.sys,
	}
	param, err := inner.compilePat(fn.IDPat)
	if err != nil {
		return nil, err
	}
	body, err := inner.compileExp(fn.Exp)
	if err != nil {
		return nil, err
	}
	return eval.MakeClosure(param, fn.IDPat.Name, body,
		inner.captures, inner.nSlots), nil
}

func (c *compiler) compileCase(caseExp *core.Case) (eval.Code,
	error,
) {
	scrutinee, err := c.compileExp(caseExp.Exp)
	if err != nil {
		return nil, err
	}
	clauses := make([]eval.MatchClause, len(caseExp.Matches))
	for i, m := range caseExp.Matches {
		pat, err := c.compilePat(m.Pat)
		if err != nil {
			return nil, err
		}
		body, err := c.compileExp(m.Exp)
		if err != nil {
			return nil, err
		}
		clauses[i] = eval.MatchClause{Pat: pat, Body: body}
	}
	return eval.Case(scrutinee, clauses, caseExp.Span), nil
}

// compilePat compiles a pattern, allocating a slot for each name
// it binds.
func (c *compiler) compilePat(pat core.Pat) (eval.Pat, error) {
	for _, id := range core.PatIDs(pat) {
		c.allocSlot(id)
	}
	return c.patCode(pat)
}

func (c *compiler) patCode(pat core.Pat) (eval.Pat, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *core.AsPat:
		body, err := c.patCode(p.Body)
		if err != nil {
			return nil, err
		}
		return eval.AsPat{Slot: c.slots[p.Pat], Body: body}, nil
	case *core.Con0Pat:
		con0Pat := eval.Con0Pat{
			Datatype: p.Datatype,
			Ordinal:  p.Ordinal,
		}
		return con0Pat, nil
	case *core.ConPat:
		arg, err := c.patCode(p.Arg)
		if err != nil {
			return nil, err
		}
		conPat := eval.ConAppPat{
			Arg:      arg,
			Datatype: p.Datatype,
			Ordinal:  p.Ordinal,
		}
		return conPat, nil
	case *core.ConsPat:
		head, err := c.patCode(p.Head)
		if err != nil {
			return nil, err
		}
		tail, err := c.patCode(p.Tail)
		if err != nil {
			return nil, err
		}
		return eval.ConsPat{Head: head, Tail: tail}, nil
	case *core.IDPat:
		return eval.SlotPat{Slot: c.slots[p]}, nil
	case *core.ListPat:
		pats := make([]eval.Pat, len(p.Args))
		for i, arg := range p.Args {
			argPat, err := c.patCode(arg)
			if err != nil {
				return nil, err
			}
			pats[i] = argPat
		}
		return eval.ListPat{Pats: pats}, nil
	case *core.LiteralPat:
		return eval.LiteralPat{V: p.Value}, nil
	case *core.TuplePat:
		pats := make([]eval.Pat, len(p.Args))
		for i, arg := range p.Args {
			argPat, err := c.patCode(arg)
			if err != nil {
				return nil, err
			}
			pats[i] = argPat
		}
		return eval.TuplePat{Pats: pats}, nil
	case *core.WildcardPat:
		return eval.WildcardPat{}, nil
	default:
		return nil, &Error{
			Msg: "cannot compile pattern " + pat.Op().String(),
		}
	}
}

func (c *compiler) compileLet(let *core.Let) (eval.Code, error) {
	switch d := let.Decl.(type) {
	case *core.NonRecValDecl:
		init, err := c.compileExp(d.Exp)
		if err != nil {
			return nil, err
		}
		pat, err := c.compilePat(d.Pat)
		if err != nil {
			return nil, err
		}
		body, err := c.compileExp(let.Exp)
		if err != nil {
			return nil, err
		}
		return eval.Let(pat, init, body, d.Span), nil
	case *core.RecValDecl:
		for _, bind := range d.Binds {
			if idPat, ok := bind.Pat.(*core.IDPat); ok {
				c.allocSlot(idPat)
			}
		}
		body, err := c.compileExp(let.Exp)
		if err != nil {
			return nil, err
		}
		return c.compileRec(d, body)
	default:
		return nil, &Error{
			Msg: "cannot compile " + let.Decl.Op().String(),
		}
	}
}

// compileRec compiles a recursive declaration, giving every
// binding's name its slot before compiling any expression, then
// wrapping the body in a LetRec that patches the closures'
// self-references. Recursive bindings are names (the type
// checker required that), so each pattern is one IDPat.
func (c *compiler) compileRec(d *core.RecValDecl,
	body eval.Code,
) (eval.Code, error) {
	slots := make([]int, len(d.Binds))
	for i, bind := range d.Binds {
		idPat, ok := bind.Pat.(*core.IDPat)
		if !ok {
			return nil, &Error{
				Msg: "cannot compile recursive pattern " +
					bind.Pat.Op().String(),
			}
		}
		slots[i] = c.allocSlot(idPat)
	}
	inits := make([]eval.Code, len(d.Binds))
	for i, bind := range d.Binds {
		init, err := c.compileExp(bind.Exp)
		if err != nil {
			return nil, err
		}
		inits[i] = init
	}
	return eval.LetRec(slots, inits, body), nil
}

// allocSlot returns the frame slot of a variable, allocating one
// on first use.
func (c *compiler) allocSlot(pat *core.IDPat) int {
	if slot, ok := c.slots[pat]; ok {
		return slot
	}
	slot := c.nSlots
	c.nSlots++
	c.slots[pat] = slot
	return slot
}
