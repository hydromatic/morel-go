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
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/token"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// Resolve converts a type-resolved declaration to Core.
func Resolve(resolved *Resolved) (core.Decl, error) {
	r := &resolver{typeMap: resolved.TypeMap}
	decl, _, err := r.toDecl(nil, resolved.Decl)
	return decl, err
}

// resolver converts AST nodes to Core, attaching the types that
// the TypeResolver deduced.
type resolver struct {
	typeMap *TypeMap
	// currentRow is the value that "current" rewrites to inside a
	// query step: the current row, a record of the query variables
	// (or the sole variable). It is nil outside a query.
	currentRow core.Exp
	// inQuery is true while resolving a query's steps, where
	// "ordinal" is a valid keyword.
	inQuery bool
}

// buildRow is the value of a query row: the sole variable, or a
// record (a sorted tuple) of the variables. It is what "current"
// refers to.
func (r *resolver) buildRow(rowVars []*core.IDPat) core.Exp {
	if len(rowVars) == 0 {
		return nil
	}
	if len(rowVars) == 1 {
		return &core.ID{Pat: rowVars[0]}
	}
	sorted := append([]*core.IDPat(nil), rowVars...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	fields := make([]types.Field, len(sorted))
	args := make([]core.Exp, len(sorted))
	for i, v := range sorted {
		fields[i] = types.Field{Label: v.Name, Type: v.T}
		args[i] = &core.ID{Pat: v}
	}
	return &core.Tuple{T: r.typeMap.sys.Record(fields), Args: args}
}

// coreEnv maps a name in scope to the IDPat that declared it, so
// every reference resolves to its declaration.
type coreEnv struct {
	parent *coreEnv
	pat    *core.IDPat
}

func (e *coreEnv) get(name string) *core.IDPat {
	for env := e; env != nil; env = env.parent {
		if env.pat.Name == name {
			return env.pat
		}
	}
	return nil
}

func (e *coreEnv) bind(pat *core.IDPat) *coreEnv {
	return &coreEnv{parent: e, pat: pat}
}

// toDecl converts a declaration, returning also the environment
// for whatever follows the declaration.
func (r *resolver) toDecl(env *coreEnv, decl ast.Decl) (core.Decl,
	*coreEnv, error,
) {
	d, ok := decl.(*ast.ValDecl)
	if !ok {
		return nil, nil, &Error{
			Span: decl.Span(),
			Msg: "cannot convert to core: " +
				decl.Op().String(),
		}
	}
	if d.Inst {
		// A "val inst" instance is typed (so ":t" works within a
		// let) but not yet compiled or evaluated.
		return nil, nil, &Error{
			Span: decl.Span(),
			Msg:  "cannot convert to core: val inst",
		}
	}
	if d.Rec {
		return r.toRecDecl(env, d)
	}
	if len(d.Binds) != 1 {
		return r.toParallelDecl(env, d)
	}
	bind := d.Binds[0]
	pat, err := r.toPat(bind.Pat)
	if err != nil {
		return nil, nil, err
	}
	exp, err := r.toExp(env, bind.Exp)
	if err != nil {
		return nil, nil, err
	}
	env2 := env
	for _, id := range core.PatIDs(pat) {
		env2 = env2.bind(id)
	}
	valDecl := &core.NonRecValDecl{
		Pat:  pat,
		Exp:  exp,
		Span: bind.Span(),
	}
	return valDecl, env2, nil
}

// toParallelDecl converts a non-recursive "and" group ("val x =
// e1 and y = e2"), which binds its patterns in parallel: each
// expression sees only the outer environment, not its siblings.
// It is modelled as a single tuple binding, "val (p1, p2) = (e1,
// e2)", which the compiler already destructures into the
// individual names.
func (r *resolver) toParallelDecl(env *coreEnv, d *ast.ValDecl) (
	core.Decl, *coreEnv, error,
) {
	pats := make([]core.Pat, len(d.Binds))
	exps := make([]core.Exp, len(d.Binds))
	patTypes := make([]types.Type, len(d.Binds))
	expTypes := make([]types.Type, len(d.Binds))
	for i, bind := range d.Binds {
		pat, err := r.toPat(bind.Pat)
		if err != nil {
			return nil, nil, err
		}
		exp, err := r.toExp(env, bind.Exp)
		if err != nil {
			return nil, nil, err
		}
		pats[i], exps[i] = pat, exp
		patTypes[i], expTypes[i] = pat.Type(), exp.Type()
	}
	patTuple := &core.TuplePat{
		T:    r.typeMap.sys.Tuple(patTypes...),
		Args: pats,
	}
	expTuple := &core.Tuple{
		T:    r.typeMap.sys.Tuple(expTypes...),
		Args: exps,
	}
	env2 := env
	for _, id := range core.PatIDs(patTuple) {
		env2 = env2.bind(id)
	}
	return &core.NonRecValDecl{
		Pat:  patTuple,
		Exp:  expTuple,
		Span: d.Span(),
	}, env2, nil
}

// toRecDecl converts a recursive declaration; its names are in
// scope in all of its own expressions.
func (r *resolver) toRecDecl(env *coreEnv, d *ast.ValDecl) (
	core.Decl, *coreEnv, error,
) {
	idPats := make([]*core.IDPat, len(d.Binds))
	env2 := env
	for i, bind := range d.Binds {
		idPat, err := r.toIDPat(bind.Pat)
		if err != nil {
			return nil, nil, err
		}
		idPats[i] = idPat
		env2 = env2.bind(idPat)
	}
	binds := make([]*core.NonRecValDecl, len(d.Binds))
	for i, bind := range d.Binds {
		exp, err := r.toExp(env2, bind.Exp)
		if err != nil {
			return nil, nil, err
		}
		binds[i] = &core.NonRecValDecl{
			Pat:  idPats[i],
			Exp:  exp,
			Span: bind.Span(),
		}
	}
	// A "rec" declaration whose expression never uses the bound
	// name is not really recursive; treating it as non-recursive
	// lets the inliner consider it.
	if len(binds) == 1 && !referencesAny(binds[0].Exp, idPats) {
		return binds[0], env2, nil
	}
	return &core.RecValDecl{Binds: binds}, env2, nil
}

// toIDPat converts a pattern that binds (at most) one name; a
// wildcard becomes a pattern whose name is never referenced.
func (r *resolver) toIDPat(pat ast.Pat) (*core.IDPat, error) {
	t, err := r.typeMap.TypeOf(pat)
	if err != nil {
		return nil, err
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.AnnotatedPat:
		// The annotation constrained the type during inference; the
		// core pattern is just the pattern it wraps.
		return r.toIDPat(p.Pat)
	case *ast.IDPat:
		return &core.IDPat{T: t, Name: p.Name}, nil
	case *ast.WildcardPat:
		return &core.IDPat{T: t, Name: "_"}, nil
	}
	return nil, &Error{
		Span: pat.Span(),
		Msg: "cannot convert to core: pattern " +
			pat.Op().String(),
	}
}

func (r *resolver) toExp(env *coreEnv, exp ast.Expr) (core.Exp,
	error,
) {
	t, err := r.typeMap.TypeOf(exp)
	if err != nil {
		return nil, err
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.AnnotatedExp:
		return r.toExp(env, e.Exp)
	case *ast.Apply:
		return r.toApply(env, e, t)
	case *ast.Case:
		return r.toCase(env, e, t)
	case *ast.Fn:
		return r.toFn(env, e, t)
	case *ast.From:
		return r.toFrom(env, e, t)
	case *ast.ID:
		if e.Name == currentName && r.currentRow != nil {
			return r.currentRow, nil
		}
		if e.Name == ordinalName && r.inQuery {
			return &core.Ordinal{T: r.typeMap.sys.Int}, nil
		}
		if pat := env.get(e.Name); pat != nil {
			return &core.ID{Pat: pat}, nil
		}
		if con, ok := r.toCon(e.Name, t); ok {
			return con, nil
		}
		// The name is not declared in this compilation unit
		// (e.g. a built-in value), so make a declaration site
		// for it.
		return &core.ID{
			Pat: &core.IDPat{T: t, Name: e.Name},
		}, nil
	case *ast.If:
		return r.toIf(env, e, t)
	case *ast.InfixCall:
		return r.toInfix(env, e, t)
	case *ast.Let:
		return r.flattenLet(env, e.Decls, e.Exp)
	case *ast.ListExp:
		args := make([]core.Exp, len(e.Args))
		for i, arg := range e.Args {
			a, err := r.toExp(env, arg)
			if err != nil {
				return nil, err
			}
			args[i] = a
		}
		return &core.List{T: t, Args: args}, nil
	case *ast.Literal:
		return r.toLiteral(e, t)
	case *ast.PrefixCall:
		if e.Kind != ast.NegateOp {
			return nil, &Error{
				Span: e.Span(),
				Msg: "cannot convert to core: " +
					e.Kind.String(),
			}
		}
		arg, err := r.toExp(env, e.A)
		if err != nil {
			return nil, err
		}
		fnPat := &core.IDPat{
			T:    r.typeMap.sys.Fn(t, t),
			Name: "op ~",
		}
		return &core.Apply{
			T:    t,
			Fn:   &core.ID{Pat: fnPat},
			Arg:  arg,
			Span: e.Span(),
		}, nil
	case *ast.Raise:
		return r.toRaise(env, e, t)
	case *ast.RangeList:
		return r.toRangeList(env, e, t)
	case *ast.Record:
		return r.toRecord(env, e, t)
	case *ast.RecordSelector:
		return r.toSelector(e, t)
	case *ast.Tuple:
		args := make([]core.Exp, len(e.Args))
		for i, arg := range e.Args {
			a, err := r.toExp(env, arg)
			if err != nil {
				return nil, err
			}
			args[i] = a
		}
		return &core.Tuple{T: t, Args: args}, nil
	case *ast.TypeStringExp:
		// The operand's type is known after inference; render it as
		// a string literal.
		operandType, err := r.typeMap.TypeOf(e.Exp)
		if err != nil {
			return nil, err
		}
		return &core.Literal{
			T: t, Kind: ast.StringLiteralOp, Value: operandType.String(),
		}, nil
	default:
		return nil, &Error{
			Span: exp.Span(),
			Msg: "cannot convert to core: " +
				exp.Op().String(),
		}
	}
}

func (r *resolver) toLiteral(literal *ast.Literal,
	t types.Type,
) (core.Exp, error) {
	value, err := literalValue(literal.Kind, literal.Value)
	if err != nil {
		return nil, &Error{
			Span: literal.Span(),
			Msg:  err.Error(),
		}
	}
	exp := &core.Literal{T: t, Kind: literal.Kind, Value: value}
	return exp, nil
}

func (r *resolver) toApply(env *coreEnv, apply *ast.Apply,
	t types.Type,
) (core.Exp, error) {
	fn, err := r.toExp(env, apply.Fn)
	if err != nil {
		return nil, err
	}
	arg, err := r.toExp(env, apply.Arg)
	if err != nil {
		return nil, err
	}
	apply2 := &core.Apply{
		T:    t,
		Fn:   fn,
		Arg:  arg,
		Span: apply.Span(),
	}
	return apply2, nil
}

// toFrom converts a query to Core. Only the subset that evaluates
// so far is supported: a single "in" scan, followed by "where"
// filters and an optional trailing "yield". Anything else — a
// scalar scan, a join, a group, an order, or a step after a yield —
// is not yet convertible, so the statement produces no output.
func (r *resolver) toFrom(env *coreEnv, from *ast.From,
	t types.Type,
) (core.Exp, error) {
	unsupported := &Error{
		Span: from.Span(),
		Msg:  "cannot convert to core: " + from.Op().String(),
	}
	if len(from.Steps) == 0 {
		return nil, unsupported
	}
	// "current" rewrites to the row entering each step, and
	// "ordinal" is valid; save the outer state for a nested query,
	// restore it on the way out.
	savedCurrent, savedInQuery := r.currentRow, r.inQuery
	defer func() { r.currentRow, r.inQuery = savedCurrent, savedInQuery }()
	r.inQuery = true
	steps := make([]core.FromStep, 0, len(from.Steps))
	cur := env
	var rowVars []*core.IDPat
	for i := 0; i < len(from.Steps); i++ {
		step := from.Steps[i]
		r.currentRow = r.buildRow(rowVars)
		// A "yield" that is not the last step rebinds the row's
		// variables to the fields the yielded value exposes.
		if y, ok := step.(*ast.YieldStep); ok &&
			i < len(from.Steps)-1 {
			yieldStep, newCur, newVars, err := r.toYieldStep(cur, y)
			if err != nil {
				return nil, err
			}
			steps = append(steps, yieldStep)
			cur = newCur
			rowVars = newVars
			continue
		}
		// A "group" absorbs the "compute" that follows it, so the
		// aggregates are typed over the pre-group rows.
		if g, ok := step.(*ast.GroupStep); ok {
			var compute *ast.ComputeStep
			if i+1 < len(from.Steps) {
				if c, ok := from.Steps[i+1].(*ast.ComputeStep); ok {
					compute = c
					i++
				}
			}
			groupSteps, groupRowVars, newCur, err := r.toGroupStep(
				cur, g, compute,
			)
			if err != nil {
				return nil, err
			}
			steps = append(steps, groupSteps...)
			cur = newCur
			rowVars = groupRowVars
			continue
		}
		newSteps, newCur, _, err := r.toQueryStep(env, cur, step)
		if err != nil {
			return nil, err
		}
		steps = append(steps, newSteps...)
		cur = newCur
		rowVars = updateRowVars(rowVars, newSteps)
	}
	if _, ok := steps[0].(*core.Scan); !ok {
		return nil, unsupported
	}
	return &core.From{T: t, Steps: steps, Kind: from.Kind}, nil
}

// updateRowVars adjusts the current row's variables after a step: a
// scan adds its pattern's variables, a through replaces them.
func updateRowVars(rowVars []*core.IDPat,
	newSteps []core.FromStep,
) []*core.IDPat {
	for _, s := range newSteps {
		switch s := s.(type) {
		case *core.Scan:
			rowVars = append(rowVars, core.PatIDs(s.Pat)...)
		case *core.Through:
			rowVars = core.PatIDs(s.Pat)
		}
	}
	return rowVars
}

// toQueryStep converts a query step other than a group, returning
// the Core steps it produces, the environment for what follows, and
// whether it was a yield (after which no step may follow). Scan
// sources, order keys, where and yield expressions see the query
// variables; skip/take counts, set-op arguments, and an "into"
// function see only the root scope.
func (r *resolver) toQueryStep(env, cur *coreEnv, step ast.FromStep,
) ([]core.FromStep, *coreEnv, bool, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := step.(type) {
	case *ast.DistinctStep:
		return []core.FromStep{&core.Distinct{}}, cur, false, nil
	case *ast.IntoStep:
		// "into f" applies f to the whole query result, so f is an
		// expression of the root scope, not the query row's -- the
		// query variables are out of scope inside it.
		fn, err := r.toExp(env, s.Exp)
		return []core.FromStep{&core.Into{Fn: fn}}, cur, true, err
	case *ast.OrderStep:
		exp, err := r.toExp(cur, s.Exp)
		return []core.FromStep{&core.Order{Exp: exp}}, cur, false, err
	case *ast.RequireStep:
		// "require p" in a "forall" reduces to yielding p, whose
		// truth for every row the quantifier then checks.
		exp, err := r.toExp(cur, s.Exp)
		return []core.FromStep{&core.Yield{Exp: exp}}, cur, true, err
	case *ast.Scan:
		scanSteps, newCur, err := r.toScanStep(cur, s)
		return scanSteps, newCur, false, err
	case *ast.SetOpStep:
		setOp, err := r.toSetOpStep(env, s)
		return []core.FromStep{setOp}, cur, false, err
	case *ast.SkipStep:
		exp, err := r.toExp(env, s.Exp)
		return []core.FromStep{&core.Skip{Exp: exp}}, cur, false, err
	case *ast.TakeStep:
		exp, err := r.toExp(env, s.Exp)
		return []core.FromStep{&core.Take{Exp: exp}}, cur, false, err
	case *ast.ThroughStep:
		return r.toThroughStep(cur, s)
	case *ast.UnorderStep:
		// "unorder" only changes orderedness (a bag), which the type
		// records; the collection value is unchanged, so it produces
		// no step.
		return nil, cur, false, nil
	case *ast.WhereStep:
		exp, err := r.toExp(cur, s.Exp)
		return []core.FromStep{&core.Where{Exp: exp}}, cur, false, err
	case *ast.YieldStep:
		exp, err := r.toExp(cur, s.Exp)
		return []core.FromStep{&core.Yield{Exp: exp}}, cur, true, err
	default:
		return nil, nil, false, &Error{
			Span: step.Span(),
			Msg:  "cannot convert to core: " + step.Op().String(),
		}
	}
}

// toGroupStep converts a "group" step and the "compute" that
// follows it. The keys and aggregate arguments are computed over an
// input row (the current scope); the key and aggregate fields
// become the query's variables downstream.
func (r *resolver) toGroupStep(cur *coreEnv, group *ast.GroupStep,
	compute *ast.ComputeStep,
) ([]core.FromStep, []*core.IDPat, *coreEnv, error) {
	var keys []core.GroupKey
	for _, f := range r.stepFields(group.Exp) {
		exp, err := r.toExp(cur, f.exp)
		if err != nil {
			return nil, nil, nil, err
		}
		keys = append(keys, core.GroupKey{
			Pat: &core.IDPat{T: exp.Type(), Name: f.label},
			Exp: exp,
		})
	}
	var aggs []core.GroupAgg
	if compute != nil {
		for _, f := range r.stepFields(compute.Exp) {
			agg, err := r.toGroupAgg(cur, f)
			if err != nil {
				return nil, nil, nil, err
			}
			aggs = append(aggs, agg)
		}
	}
	groupStep := &core.Group{Keys: keys, Aggs: aggs}
	fieldPats := make([]*core.IDPat, 0, len(keys)+len(aggs))
	for _, k := range keys {
		fieldPats = append(fieldPats, k.Pat)
	}
	for _, a := range aggs {
		fieldPats = append(fieldPats, a.Pat)
	}
	if group.Binder == "" {
		newCur := cur
		for _, p := range fieldPats {
			newCur = newCur.bind(p)
		}
		return []core.FromStep{groupStep}, fieldPats, newCur, nil
	}
	// A binder names the whole group row. Emit a following "yield"
	// that assembles the key and aggregate fields into that row -- a
	// record, or the bare value when the group is an atom (a single
	// field from a non-record key or aggregate) -- and binds it to
	// the binder, the only variable visible downstream.
	var yieldExp core.Exp
	if groupRowIsAtom(group, compute, len(fieldPats)) {
		yieldExp = &core.ID{Pat: fieldPats[0]}
	} else {
		yieldExp = r.recordExp(fieldPats)
	}
	binderPat := &core.IDPat{T: yieldExp.Type(), Name: group.Binder}
	yieldStep := &core.Yield{
		Fields: []core.YieldField{{Pat: binderPat, Exp: yieldExp}},
	}
	return []core.FromStep{groupStep, yieldStep},
		[]*core.IDPat{binderPat}, cur.bind(binderPat), nil
}

// groupRowIsAtom reports whether a group's row is a single bare
// value rather than a record: exactly one field (nFields == 1)
// that comes from a non-record expression, so neither the key nor
// the compute clause is a singleton record literal.
func groupRowIsAtom(group *ast.GroupStep, compute *ast.ComputeStep,
	nFields int,
) bool {
	return nFields == 1 &&
		!isSingletonRecord(group.Exp) &&
		(compute == nil || !isSingletonRecord(compute.Exp))
}

// isSingletonRecord reports whether an expression is a record
// literal with exactly one field.
func isSingletonRecord(exp ast.Expr) bool {
	rec, ok := exp.(*ast.Record)
	return ok && rec.With == nil && len(rec.Fields) == 1
}

// recordExp builds a record of the given variables, sorted by name
// (the canonical field order), each field bound to its variable.
func (r *resolver) recordExp(pats []*core.IDPat) core.Exp {
	sorted := append([]*core.IDPat(nil), pats...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	fields := make([]types.Field, len(sorted))
	args := make([]core.Exp, len(sorted))
	for i, v := range sorted {
		fields[i] = types.Field{Label: v.Name, Type: v.T}
		args[i] = &core.ID{Pat: v}
	}
	return &core.Tuple{T: r.typeMap.sys.Record(fields), Args: args}
}

// toYieldStep converts a "yield" that is not the last step: each
// field the yielded value exposes becomes an output variable that
// later steps see, computed from the input row. A binder exposes a
// single variable of that name, bound to the whole yielded value.
func (r *resolver) toYieldStep(cur *coreEnv, s *ast.YieldStep) (
	core.FromStep, *coreEnv, []*core.IDPat, error,
) {
	if s.Binder != "" {
		exp, err := r.toExp(cur, s.Exp)
		if err != nil {
			return nil, nil, nil, err
		}
		pat := &core.IDPat{T: exp.Type(), Name: s.Binder}
		field := core.YieldField{Pat: pat, Exp: exp}
		return &core.Yield{Fields: []core.YieldField{field}},
			cur.bind(pat), []*core.IDPat{pat}, nil
	}
	var fields []core.YieldField
	newCur := cur
	var rowVars []*core.IDPat
	for _, f := range r.stepFields(s.Exp) {
		exp, err := r.toExp(cur, f.exp)
		if err != nil {
			return nil, nil, nil, err
		}
		pat := &core.IDPat{T: exp.Type(), Name: f.label}
		fields = append(fields, core.YieldField{Pat: pat, Exp: exp})
		newCur = newCur.bind(pat)
		rowVars = append(rowVars, pat)
	}
	return &core.Yield{Fields: fields}, newCur, rowVars, nil
}

// toGroupAgg converts one aggregate field "label = fn over arg" (or
// a bare "fn"), reading its result type from the type map.
func (r *resolver) toGroupAgg(cur *coreEnv, f stepField) (
	core.GroupAgg, error,
) {
	fnExp, argExp := f.exp, ast.Expr(nil)
	if ic, ok := f.exp.(*ast.InfixCall); ok && ic.Kind == ast.OverOp {
		fnExp, argExp = ic.A0, ic.A1
	}
	fn, err := r.toExp(cur, fnExp)
	if err != nil {
		return core.GroupAgg{}, err
	}
	var arg core.Exp
	if argExp != nil {
		arg, err = r.toExp(cur, argExp)
		if err != nil {
			return core.GroupAgg{}, err
		}
	}
	t, err := r.typeMap.TypeOf(f.exp)
	if err != nil {
		return core.GroupAgg{}, err
	}
	return core.GroupAgg{
		Pat: &core.IDPat{T: t, Name: f.label},
		Fn:  fn,
		Arg: arg,
	}, nil
}

// stepField is a labelled expression in a group's keys or a
// compute's aggregates.
type stepField struct {
	label string
	exp   ast.Expr
}

// stepFields splits a group-key or compute expression into its
// labelled fields: a record's fields (by their labels or implicit
// labels), or a single field labelled by its implicit label.
func (r *resolver) stepFields(exp ast.Expr) []stepField {
	if rec, ok := exp.(*ast.Record); ok && rec.With == nil {
		fields := make([]stepField, len(rec.Fields))
		for i, f := range rec.Fields {
			label := f.Label
			if label == "" {
				label = implicitLabel(f.Exp)
			}
			fields[i] = stepField{label: label, exp: f.Exp}
		}
		return fields
	}
	label := implicitLabel(exp)
	if label == "" {
		label = currentName
	}
	return []stepField{{label: label, exp: exp}}
}

// toSetOpStep converts a union/intersect/except step. Its argument
// collections are in the root scope, so they cannot reference the
// query's variables.
func (r *resolver) toSetOpStep(env *coreEnv, s *ast.SetOpStep) (
	core.FromStep, error,
) {
	args := make([]core.Exp, len(s.Exps))
	for i, arg := range s.Exps {
		a, err := r.toExp(env, arg)
		if err != nil {
			return nil, err
		}
		args[i] = a
	}
	return &core.SetOp{Kind: s.Kind, Args: args, Distinct: s.Distinct}, nil
}

// toThroughStep converts a "through pat in f" step: f maps the
// collection to a new one whose elements pat binds. The pattern's
// variables become the query's variables downstream.
func (r *resolver) toThroughStep(cur *coreEnv, s *ast.ThroughStep,
) ([]core.FromStep, *coreEnv, bool, error) {
	pat, err := r.toPat(s.Pat)
	if err != nil {
		return nil, nil, false, err
	}
	fn, err := r.toExp(cur, s.Exp)
	if err != nil {
		return nil, nil, false, err
	}
	newCur := cur
	for _, id := range core.PatIDs(pat) {
		newCur = newCur.bind(id)
	}
	return []core.FromStep{&core.Through{Pat: pat, Fn: fn}}, newCur,
		false, nil
}

// toScanStep converts an "in" scan, returning the Core steps it
// produces (a scan, plus a where for a "join ... on" condition) and
// the environment extended with the scan's variables. A scalar
// ("=") scan is not yet supported.
func (r *resolver) toScanStep(cur *coreEnv, s *ast.Scan) (
	[]core.FromStep, *coreEnv, error,
) {
	pat, err := r.toPat(s.Pat)
	if err != nil {
		return nil, nil, err
	}
	var exp core.Exp
	// lint: sort until '^\t}' where '^\tcase '
	switch s.Kind {
	case ast.ScanIn:
		// The source is in the current scope, so a later scan may
		// depend on an earlier scan's variables.
		exp, err = r.toExp(cur, s.Exp)
		if err != nil {
			return nil, nil, err
		}
	case ast.ScanUnbounded:
		// A sourceless scan iterates the extent of the pattern's
		// type: all its values, restricted by nothing yet.
		exp = r.extentExp(pat.Type(), s.Span())
	default:
		return nil, nil, &Error{
			Span: s.Span(),
			Msg:  "cannot convert to core: " + s.Op().String(),
		}
	}
	steps := []core.FromStep{&core.Scan{Pat: pat, Exp: exp}}
	for _, id := range core.PatIDs(pat) {
		cur = cur.bind(id)
	}
	// A "join ... on" condition filters over the scan's variables,
	// so it lowers to a where after the scan.
	if s.On != nil {
		on, err := r.toExp(cur, s.On)
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, &core.Where{Exp: on})
	}
	return steps, cur, nil
}

// ExtentName is the name of the internal builtin that returns an
// extent's values.
const ExtentName = "$.extent"

// RaiseName is the name of the internal builtin behind the
// "raise" expression; the kernel binds it to eval.RaiseFn.
const RaiseName = "$.raise"

// exnTypeName names the exception datatype.
const exnTypeName = "exn"

// toRaise converts "raise e" to a call of the internal raise
// builtin. The application's span is the whole raise expression,
// which is where the report says the exception was raised.
func (r *resolver) toRaise(env *coreEnv, raise *ast.Raise,
	t types.Type,
) (core.Exp, error) {
	arg, err := r.toExp(env, raise.E)
	if err != nil {
		return nil, err
	}
	return &core.Apply{
		T: t,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    r.typeMap.sys.Fn(arg.Type(), t),
			Name: RaiseName,
		}},
		Arg:  arg,
		Span: raise.Span(),
	}, nil
}

// extentExp builds the source of a sourceless scan: a call of the
// internal extent builtin on the (materialized, if finite) extent
// of the element type.
func (r *resolver) extentExp(t types.Type, span token.Span,
) core.Exp {
	return extentScanExp(r.typeMap.sys, t, span)
}

// toFn converts a function. A single rule that binds one name
// becomes a Fn directly; otherwise the parameter is a fresh
// variable and the match list becomes a case over it.
func (r *resolver) toFn(env *coreEnv, fn *ast.Fn,
	t types.Type,
) (core.Exp, error) {
	fnType, ok := t.(*types.Fn)
	if !ok {
		return nil, &Error{
			Span: fn.Span(),
			Msg:  "function does not have function type",
		}
	}
	if len(fn.Matches) == 1 && r.irrefutable(fn.Matches[0].Pat) {
		match := fn.Matches[0]
		switch match.Pat.(type) {
		case *ast.IDPat, *ast.WildcardPat:
			idPat, err := r.toIDPat(match.Pat)
			if err != nil {
				return nil, err
			}
			exp, err := r.toExp(env.bind(idPat), match.Exp)
			if err != nil {
				return nil, err
			}
			fnExp := &core.Fn{
				T: fnType, IDPat: idPat,
				Exp: exp,
			}
			return fnExp, nil
		}
	}
	param := &core.IDPat{T: fnType.Param, Name: "v"}
	matches, err := r.toMatches(env, fn.Matches)
	if err != nil {
		return nil, err
	}
	body := &core.Case{
		T:       fnType.Result,
		Exp:     &core.ID{Pat: param},
		Matches: matches,
		Span:    fn.Span(),
	}
	return &core.Fn{T: fnType, IDPat: param, Exp: body}, nil
}

// irrefutable reports whether a pattern always matches — a
// wildcard, or a name that binds a variable rather than a
// constructor (so "true", "nil", and other constructors are
// refutable, even though they parse as identifiers).
func (r *resolver) irrefutable(pat ast.Pat) bool {
	switch p := pat.(type) {
	case *ast.WildcardPat:
		return true
	case *ast.IDPat:
		_, isCon := r.typeMap.sys.LookupTyCon(p.Name)
		return !isCon
	default:
		return false
	}
}

// toIf translates "if c then a else b" as if the user had written
// "case c of true => a | _ => b".
func (r *resolver) toIf(env *coreEnv, ifExp *ast.If,
	t types.Type,
) (core.Exp, error) {
	cond, err := r.toExp(env, ifExp.Cond)
	if err != nil {
		return nil, err
	}
	ifTrue, err := r.toExp(env, ifExp.IfTrue)
	if err != nil {
		return nil, err
	}
	ifFalse, err := r.toExp(env, ifExp.IfFalse)
	if err != nil {
		return nil, err
	}
	boolType := cond.Type()
	caseExp := &core.Case{
		T:   t,
		Exp: cond,
		Matches: []core.Match{
			{
				Pat: &core.LiteralPat{
					T:     boolType,
					Kind:  ast.BoolLiteralOp,
					Value: true,
				},
				Exp: ifTrue,
			},
			{
				Pat: &core.WildcardPat{T: boolType},
				Exp: ifFalse,
			},
		},
	}
	return caseExp, nil
}

// toInfix converts an infix operator application. The logical
// connectives become cases ("a andalso b" runs b only if a is
// true); any other operator becomes the application of its
// top-level binding to the operand pair.
func (r *resolver) toInfix(env *coreEnv, call *ast.InfixCall,
	t types.Type,
) (core.Exp, error) {
	a0, err := r.toExp(env, call.A0)
	if err != nil {
		return nil, err
	}
	a1, err := r.toExp(env, call.A1)
	if err != nil {
		return nil, err
	}
	sys := r.typeMap.sys
	// lint: sort until '^\t}' where '^\tcase '
	switch call.Kind {
	case ast.AndalsoOp:
		return boolCase(sys, a0, a1,
			&core.Literal{
				T: sys.Bool, Kind: ast.BoolLiteralOp,
				Value: false,
			}), nil
	case ast.ImpliesOp:
		return boolCase(sys, a0, a1,
			&core.Literal{
				T: sys.Bool, Kind: ast.BoolLiteralOp,
				Value: true,
			}), nil
	case ast.OrelseOp:
		return boolCase(sys, a0,
			&core.Literal{
				T: sys.Bool, Kind: ast.BoolLiteralOp,
				Value: true,
			}, a1), nil
	default:
	}
	name, ok := infixOpNames[call.Kind]
	if !ok {
		return nil, &Error{
			Span: call.Span(),
			Msg: "cannot convert to core: " +
				call.Kind.String(),
		}
	}
	argType := sys.Tuple(a0.Type(), a1.Type())
	fnPat := &core.IDPat{
		T:    sys.Fn(argType, t),
		Name: name,
	}
	arg := &core.Tuple{
		T:    argType,
		Args: []core.Exp{a0, a1},
	}
	return &core.Apply{
		T:    t,
		Fn:   &core.ID{Pat: fnPat},
		Arg:  arg,
		Span: call.Span(),
	}, nil
}

// boolCase builds "case cond of true => ifTrue | _ => ifFalse".
func boolCase(sys *types.System, cond, ifTrue,
	ifFalse core.Exp,
) core.Exp {
	return &core.Case{
		T:   ifTrue.Type(),
		Exp: cond,
		Matches: []core.Match{
			{
				Pat: &core.LiteralPat{
					T:     sys.Bool,
					Kind:  ast.BoolLiteralOp,
					Value: true,
				},
				Exp: ifTrue,
			},
			{
				Pat: &core.WildcardPat{T: sys.Bool},
				Exp: ifFalse,
			},
		},
	}
}

// cannotDeriveLabel is the error when a record field has no
// explicit label and its expression is not an identifier.
const cannotDeriveLabel = "cannot derive label for expression"

// toRecord converts a record expression to a tuple whose
// elements are the fields in canonical order.
func (r *resolver) toRecord(env *coreEnv, record *ast.Record,
	t types.Type,
) (core.Exp, error) {
	if record.With != nil {
		return r.toRecordUpdate(env, record, t)
	}
	// The empty record is unit, the same value as "()".
	if len(record.Fields) == 0 {
		return &core.Literal{
			T: t, Kind: ast.UnitLiteralOp, Value: core.Unit{},
		}, nil
	}
	type fieldExp struct {
		label string
		exp   ast.Expr
	}
	fields := make([]fieldExp, len(record.Fields))
	for i, f := range record.Fields {
		label := f.Label
		if label == "" {
			label = implicitLabel(f.Exp)
			if label == "" {
				return nil, &Error{
					Span: f.Exp.Span(),
					Msg:  cannotDeriveLabel + " " + ast.UnparseExpr(f.Exp),
				}
			}
		}
		fields[i] = fieldExp{label: label, exp: f.Exp}
	}
	sort.Slice(fields, func(i, j int) bool {
		return types.LabelLess(fields[i].label, fields[j].label)
	})
	args := make([]core.Exp, len(fields))
	for i, f := range fields {
		arg, err := r.toExp(env, f.exp)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}
	return &core.Tuple{T: t, Args: args}, nil
}

// toRangeList converts a range-list expression to core, converting
// each item's bound expressions.
func (r *resolver) toRangeList(env *coreEnv, rl *ast.RangeList,
	t types.Type,
) (core.Exp, error) {
	items := make([]core.RangeItem, len(rl.Items))
	for i, item := range rl.Items {
		ci := core.RangeItem{Kind: item.Kind}
		if item.Lo != nil {
			lo, err := r.toExp(env, item.Lo)
			if err != nil {
				return nil, err
			}
			ci.Lo = lo
		}
		if item.Hi != nil {
			hi, err := r.toExp(env, item.Hi)
			if err != nil {
				return nil, err
			}
			ci.Hi = hi
		}
		items[i] = ci
	}
	return &core.RangeList{T: t, Items: items}, nil
}

// toRecordUpdate converts "{base with lab = e, ...}" to a let that
// binds the base record once and builds a new record, taking the
// named fields from the update expressions and every other field
// from the base by selection. The result has the base's field set
// (typing has already unified each update with its base field).
func (r *resolver) toRecordUpdate(env *coreEnv, record *ast.Record,
	t types.Type,
) (core.Exp, error) {
	rec, ok := t.(*types.Record)
	if !ok {
		return nil, &Error{
			Span: record.Span(),
			Msg:  "record update requires a record type",
		}
	}
	updates := map[string]ast.Expr{}
	for _, f := range record.Fields {
		label := f.Label
		if label == "" {
			id, isID := f.Exp.(*ast.ID)
			if !isID {
				return nil, &Error{
					Span: record.Span(),
					Msg:  cannotDeriveLabel,
				}
			}
			label = id.Name
		}
		updates[label] = f.Exp
	}
	baseExp, err := r.toExp(env, record.With)
	if err != nil {
		return nil, err
	}
	// Bind the base to a fresh name so unchanged fields can be
	// selected from it without re-evaluating the base expression.
	// The name uses a character no source identifier can, so it
	// cannot capture a user variable.
	basePat := &core.IDPat{T: baseExp.Type(), Name: "$with"}
	baseID := &core.ID{Pat: basePat}
	args := make([]core.Exp, len(rec.Fields))
	for i, field := range rec.Fields {
		if upd, isUpd := updates[field.Label]; isUpd {
			arg, err := r.toExp(env, upd)
			if err != nil {
				return nil, err
			}
			args[i] = arg
			continue
		}
		args[i] = &core.Apply{
			T: field.Type,
			Fn: &core.Selector{
				T:     r.typeMap.sys.Fn(rec, field.Type),
				Name:  field.Label,
				Index: i,
			},
			Arg: baseID,
		}
	}
	return &core.Let{
		Decl: &core.NonRecValDecl{
			Pat: basePat, Exp: baseExp, Span: record.Span(),
		},
		Exp: &core.Tuple{T: t, Args: args},
	}, nil
}

// toCon converts a reference to a datatype constructor. The
// bool constructors are Go bools and "nil" is the empty list;
// every other constructor is a Con value (or, applied, a Con
// wrapper).
func (r *resolver) toCon(name string, t types.Type) (core.Exp,
	bool,
) {
	tc, ok := r.typeMap.sys.LookupTyCon(name)
	if !ok {
		return nil, false
	}
	switch datatypeOf(tc) {
	case boolName:
		literal := &core.Literal{
			T:     t,
			Kind:  ast.BoolLiteralOp,
			Value: name == "true",
		}
		return literal, true
	case listTyCon:
		return &core.List{T: t}, true
	default:
		return &core.Con{
			T:        t,
			Datatype: datatypeOf(tc),
			Name:     name,
			Ordinal:  tc.Ordinal,
			HasArg:   tc.Arg != nil,
		}, true
	}
}

// datatypeOf is the name of the datatype a constructor belongs
// to.
func datatypeOf(tc types.TyCon) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch result := tc.Result.(type) {
	case *types.List:
		return listTyCon
	case *types.Named:
		return result.Name
	default:
		return result.String()
	}
}

// toSelector converts a field selector to a function that
// extracts the field's element, using the record or tuple type
// that inference gave the selector.
func (r *resolver) toSelector(sel *ast.RecordSelector,
	t types.Type,
) (core.Exp, error) {
	fnType, ok := t.(*types.Fn)
	if !ok {
		return nil, &Error{
			Span: sel.Span(),
			Msg: "unresolved flex record (can't tell what " +
				"fields there are besides #" + sel.Name + ")",
		}
	}
	index := -1
	switch param := fnType.Param.(type) {
	case *types.Record:
		for i, f := range param.Fields {
			if f.Label == sel.Name {
				index = i
			}
		}
	case *types.Tuple:
		i, err := strconv.Atoi(sel.Name)
		if err == nil && i >= 1 && i <= len(param.Args) {
			index = i - 1
		}
	}
	if index < 0 {
		return nil, &Error{
			Span: sel.Span(),
			Msg:  "no field " + sel.Name,
		}
	}
	selector := &core.Selector{
		T:     t,
		Name:  sel.Name,
		Index: index,
	}
	return selector, nil
}

// toCase converts "case e of pat => exp | ...". Each rule's
// pattern variables are in scope in its body.
func (r *resolver) toCase(env *coreEnv, caseExp *ast.Case,
	t types.Type,
) (core.Exp, error) {
	scrutinee, err := r.toExp(env, caseExp.Exp)
	if err != nil {
		return nil, err
	}
	matches, err := r.toMatches(env, caseExp.Matches)
	if err != nil {
		return nil, err
	}
	caseExp2 := &core.Case{
		T:       t,
		Exp:     scrutinee,
		Matches: matches,
		Span:    matchesSpan(caseExp.Matches),
	}
	return caseExp2, nil
}

// matchesSpan is the position of a match list, from the first
// rule's start to the last rule's end; a Bind failure is
// reported there.
func matchesSpan(matches []*ast.Match) token.Span {
	return token.Span{
		Start: matches[0].Span().Start,
		End:   matches[len(matches)-1].Span().End,
	}
}

func (r *resolver) toMatches(env *coreEnv,
	matches []*ast.Match,
) ([]core.Match, error) {
	result := make([]core.Match, len(matches))
	for i, m := range matches {
		pat, err := r.toPat(m.Pat)
		if err != nil {
			return nil, err
		}
		env2 := env
		for _, id := range core.PatIDs(pat) {
			env2 = env2.bind(id)
		}
		body, err := r.toExp(env2, m.Exp)
		if err != nil {
			return nil, err
		}
		result[i] = core.Match{Pat: pat, Exp: body, Span: m.Span()}
	}
	return result, nil
}

// toPat converts a pattern.
func (r *resolver) toPat(pat ast.Pat) (core.Pat, error) {
	t, err := r.typeMap.TypeOf(pat)
	if err != nil {
		return nil, err
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.AnnotatedPat:
		// The annotation constrained the type during inference; the
		// core pattern is just the pattern it wraps.
		return r.toPat(p.Pat)
	case *ast.AsPat:
		inner, err := r.toPat(p.Pat)
		if err != nil {
			return nil, err
		}
		return &core.AsPat{
			T:    t,
			Pat:  &core.IDPat{T: t, Name: p.Name},
			Body: inner,
		}, nil
	case *ast.ConPat:
		tc, ok := r.typeMap.sys.LookupTyCon(p.Name)
		if !ok || tc.Arg == nil {
			return nil, &Error{
				Span: pat.Span(),
				Msg:  "unbound constructor: " + p.Name,
			}
		}
		arg, err := r.toPat(p.Arg)
		if err != nil {
			return nil, err
		}
		conPat := &core.ConPat{
			T:        t,
			Datatype: datatypeOf(tc),
			Name:     p.Name,
			Ordinal:  tc.Ordinal,
			Arg:      arg,
		}
		return conPat, nil
	case *ast.ConsPat:
		return r.toConsPat(p, t)
	case *ast.IDPat:
		if tc, isCon := r.typeMap.sys.LookupTyCon(p.Name); isCon {
			return r.toCon0Pat(p.Name, tc, t), nil
		}
		return &core.IDPat{T: t, Name: p.Name}, nil
	case *ast.ListPat:
		return r.toListPat(p, t)
	case *ast.LiteralPat:
		value, err := literalValue(p.Kind, p.Value)
		if err != nil {
			return nil, &Error{
				Span: p.Span(),
				Msg:  err.Error(),
			}
		}
		literalPat := &core.LiteralPat{
			T:     t,
			Kind:  p.Kind,
			Value: value,
		}
		return literalPat, nil
	case *ast.RecordPat:
		return r.toRecordPat(p, t)
	case *ast.TuplePat:
		if len(p.Args) == 0 {
			return &core.WildcardPat{T: t}, nil
		}
		args := make([]core.Pat, len(p.Args))
		for i, argPat := range p.Args {
			arg, err := r.toPat(argPat)
			if err != nil {
				return nil, err
			}
			args[i] = arg
		}
		return &core.TuplePat{T: t, Args: args}, nil
	case *ast.WildcardPat:
		return &core.WildcardPat{T: t}, nil
	default:
		return nil, &Error{
			Span: pat.Span(),
			Msg: "cannot convert to core: pattern " +
				pat.Op().String(),
		}
	}
}

// toCon0Pat converts a constant constructor in a pattern: the
// bool constructors match as literals, "nil" as the empty list,
// and any other as its (datatype, ordinal).
func (r *resolver) toCon0Pat(name string, tc types.TyCon,
	t types.Type,
) core.Pat {
	switch datatypeOf(tc) {
	case boolName:
		return &core.LiteralPat{
			T:     t,
			Kind:  ast.BoolLiteralOp,
			Value: name == "true",
		}
	case listTyCon:
		return &core.ListPat{T: t}
	default:
		return &core.Con0Pat{
			T:        t,
			Datatype: datatypeOf(tc),
			Name:     name,
			Ordinal:  tc.Ordinal,
		}
	}
}

func (r *resolver) toConsPat(p *ast.ConsPat,
	t types.Type,
) (core.Pat, error) {
	head, err := r.toPat(p.A0)
	if err != nil {
		return nil, err
	}
	tail, err := r.toPat(p.A1)
	if err != nil {
		return nil, err
	}
	return &core.ConsPat{T: t, Head: head, Tail: tail}, nil
}

// toRecordPat converts a record pattern to a tuple pattern over the
// record type's fields in canonical order: each named field uses its
// sub-pattern, and any field the pattern omits (only possible under
// "...") matches a wildcard. A non-record type is unit ("{}").
func (r *resolver) toRecordPat(p *ast.RecordPat,
	t types.Type,
) (core.Pat, error) {
	rec, ok := t.(*types.Record)
	if !ok {
		return &core.WildcardPat{T: t}, nil
	}
	byLabel := make(map[string]ast.Pat, len(p.Fields))
	for _, f := range p.Fields {
		byLabel[f.Label] = f.Pat
	}
	args := make([]core.Pat, len(rec.Fields))
	for i, f := range rec.Fields {
		fp, named := byLabel[f.Label]
		if !named {
			args[i] = &core.WildcardPat{T: f.Type}
			continue
		}
		arg, err := r.toPat(fp)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}
	return &core.TuplePat{T: t, Args: args}, nil
}

func (r *resolver) toListPat(p *ast.ListPat,
	t types.Type,
) (core.Pat, error) {
	args := make([]core.Pat, len(p.Args))
	for i, argPat := range p.Args {
		arg, err := r.toPat(argPat)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}
	return &core.ListPat{T: t, Args: args}, nil
}

// flattenLet converts "let d1 d2 ... in e end" to nested Lets,
// one declaration each.
func (r *resolver) flattenLet(env *coreEnv, decls []ast.Decl,
	exp ast.Expr,
) (core.Exp, error) {
	if len(decls) == 0 {
		return r.toExp(env, exp)
	}
	if _, isDatatype := decls[0].(*ast.DatatypeDecl); isDatatype {
		return r.flattenLet(env, decls[1:], exp)
	}
	decl, env2, err := r.toDecl(env, decls[0])
	if err != nil {
		return nil, err
	}
	body, err := r.flattenLet(env2, decls[1:], exp)
	if err != nil {
		return nil, err
	}
	return &core.Let{Decl: decl, Exp: body}, nil
}

// literalValue converts a literal's text to its runtime value.
func literalValue(kind ast.Op, text string) (any, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ast.CharLiteralOp, ast.CharLiteralPatOp:
		return []rune(text)[0], nil
	case ast.IntLiteralOp, ast.IntLiteralPatOp:
		i, err := strconv.ParseInt(
			strings.ReplaceAll(text, "~", "-"), 10, 32,
		)
		if err != nil {
			return nil, &Error{Msg: "invalid literal: " + text}
		}
		return int32(i), nil
	case ast.RealLiteralOp, ast.RealLiteralPatOp:
		f, err := strconv.ParseFloat(
			strings.ReplaceAll(text, "~", "-"), 32,
		)
		if err != nil {
			return nil, &Error{Msg: "invalid literal: " + text}
		}
		return float32(f), nil
	case ast.StringLiteralOp, ast.StringLiteralPatOp:
		return text, nil
	case ast.UnitLiteralOp:
		return core.Unit{}, nil
	case ast.WordLiteralOp, ast.WordLiteralPatOp:
		s := text[2:] // after "0w"
		base := 10
		if len(s) > 0 && (s[0] == 'x' || s[0] == 'X') {
			s, base = s[1:], 16
		}
		u, err := strconv.ParseUint(s, base, 64)
		if err != nil {
			return nil, &Error{Msg: "invalid literal: " + text}
		}
		return u, nil
	default:
		return nil, &Error{
			Msg: "cannot convert literal " + kind.String(),
		}
	}
}
