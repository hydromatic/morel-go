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
	"sort"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/token"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/types"
)

// Resolve converts a type-resolved declaration to Core. The
// overload registry (which may be nil) is read to select the
// winning instance at an overloaded use, and updated when a
// "val inst" declaration registers a new instance.
func Resolve(resolved *Resolved, overloads *OverloadEnv) (core.Decl,
	error,
) {
	r := &resolver{
		typeMap:          resolved.TypeMap,
		aggSubst:         map[ast.Node]*core.IDPat{},
		overloads:        overloads,
		dictionaryParams: map[string]*core.IDPat{},
	}
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
	// ordinalScopes is the counter of each query being converted,
	// innermost last.
	ordinalScopes []*ordinalScope
	// ordinalOwner is the 1-based index in ordinalScopes of the
	// query whose rows "ordinal" counts here, and 0 where "ordinal"
	// has no query to count. It is the query being converted,
	// except in the expressions that a query evaluates before its
	// first row (see beforeFirstRow), where it is the enclosing
	// query's.
	ordinalOwner int
	// enclosingRow and enclosingOwner are currentRow and
	// ordinalOwner as they were outside the query being converted;
	// beforeFirstRow restores them.
	enclosingRow   core.Exp
	enclosingOwner int
	// aggSubst maps an "over" aggregate or "elements" node,
	// hoisted out of a compute field into a hidden group
	// aggregate, to the variable holding its per-group result.
	aggSubst map[ast.Node]*core.IDPat
	// overloads is the registry of overloaded names and their
	// instances. A "val inst" declaration adds to it; an overloaded
	// use reads it to select the instance whose parameter type
	// accepts the argument. It may be nil.
	overloads *OverloadEnv
	// dictionaryParams maps an overloaded name to the dictionary
	// parameter that supplies its instance while the body of a
	// qualified (overload-constrained) value is being compiled. Inside
	// that body an overloaded name used at an abstract type compiles to
	// a reference to this parameter rather than the milestone-1
	// placeholder (hydromatic/morel#426, dictionary passing). It is
	// populated by toCoreWithDictionaries for the duration of the body.
	dictionaryParams map[string]*core.IDPat
	// dictCount names dictionary parameters uniquely ("dict$0", ...).
	dictCount int
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
	if od, ok := decl.(*ast.OverDecl); ok {
		// An "over name" declaration introduces an overloaded name. It
		// lowers to core and echoes but binds nothing usable yet, so
		// the environment is unchanged. Register the name in the
		// overload scope (which, inside a "let", is a local child), so
		// that a "val inst" that follows and a use in the body resolve
		// against it. At top level the kernel also declares it, but
		// Declare is idempotent.
		if r.overloads != nil {
			r.overloads.Declare(od.Pat.Name)
		}
		return &core.OverDecl{Name: od.Pat.Name}, env, nil
	}
	d, ok := decl.(*ast.ValDecl)
	if !ok {
		return nil, nil, &Error{
			Span: decl.Span(),
			Msg: "cannot convert to core: " +
				decl.Op().String(),
		}
	}
	if d.Inst {
		return r.toInstDecl(env, d)
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
	// If the type resolver left overload constraints unresolved (the
	// bound value is used at an abstract type), attach a qualified
	// type to the bound name for display, e.g. "val demo = fn :
	// (second : 'a -> 'b, first : 'a -> 'c) => 'a -> 'b * 'c".
	var exp core.Exp
	if idPat, ok := pat.(*core.IDPat); ok {
		q, qerr := r.typeMap.Qualify(bind.Pat)
		if qerr != nil {
			return nil, nil, qerr
		}
		if qual, ok := q.(*types.Qualified); ok {
			// The bound value uses overloaded names at an abstract type,
			// so it has a qualified type. Compile it with one dictionary
			// parameter per predicate (Wadler-Blott dictionary passing):
			// inside the body an overloaded name at an abstract type
			// refers to its dictionary parameter, and the value becomes a
			// curried function that each use site supplies the selected
			// instances to.
			idPat.SurfaceT = qual
			exp, err = r.toCoreWithDictionaries(env, qual.Predicates,
				bind.Exp)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if exp == nil {
		exp, err = r.toExp(env, bind.Exp)
		if err != nil {
			return nil, nil, err
		}
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

// toInstDecl lowers a "val inst name = e" instance declaration. It
// lowers e to core, binds it to a generated hidden name unique to
// the (name, instance) pair (e.g. "first$0"), registers the
// instance's type against name in the overload registry, and marks
// the declaration with the overloaded name so the shell echoes it
// as "val name = ...". A later use of name selects this instance
// (via toOverloadApply) when its argument type matches e's
// parameter type.
func (r *resolver) toInstDecl(env *coreEnv, d *ast.ValDecl) (
	core.Decl, *coreEnv, error,
) {
	if r.overloads == nil || len(d.Binds) != 1 {
		return nil, nil, &Error{
			Span: d.Span(),
			Msg:  cannotConvertValInst,
		}
	}
	bind := d.Binds[0]
	idPat, ok := bind.Pat.(*ast.IDPat)
	if !ok {
		return nil, nil, &Error{
			Span: bind.Span(),
			Msg:  cannotConvertValInst,
		}
	}
	exp, err := r.toExp(env, bind.Exp)
	if err != nil {
		return nil, nil, err
	}
	pat := r.overloads.Add(idPat.Name, exp.Type())
	return &core.NonRecValDecl{
		Pat:      pat,
		Exp:      exp,
		Span:     bind.Span(),
		Overload: idPat.Name,
	}, env.bind(pat), nil
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
		// core pattern is the pattern it wraps. Keep an aliased
		// annotation ("myInt" rather than "int") unexpanded as the
		// surface type, for display: the surface and expanded forms
		// are the same interned type unless an alias makes them
		// differ.
		idPat, err := r.toIDPat(p.Pat)
		if err != nil {
			return nil, err
		}
		surface, e1 := r.typeMap.sys.SurfaceFromAST(p.Type,
			map[string]int{})
		expanded, e2 := r.typeMap.sys.FromAST(p.Type, map[string]int{})
		if e1 == nil && e2 == nil && surface != expanded {
			idPat.SurfaceT = surface
		}
		return idPat, nil
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
	case *ast.Elements:
		return r.toElements(e)
	case *ast.Fn:
		return r.toFn(env, e, t)
	case *ast.From:
		return r.toFrom(env, e, t)
	case *ast.ID:
		if e.Keyword && e.Name == currentName &&
			r.currentRow != nil {
			return r.currentRow, nil
		}
		if e.Keyword && e.Name == ordinalName && r.ordinalOwner > 0 {
			// The occurrence reads the counter of the query whose
			// rows it counts, which that query then maintains.
			sc := r.ordinalScopes[r.ordinalOwner-1]
			sc.used = true
			return &core.Ordinal{T: r.typeMap.sys.Int, Pat: sc.pat},
				nil
		}
		if pat := env.get(e.Name); pat != nil {
			return &core.ID{Pat: pat, Span: e.Span()}, nil
		}
		if con, ok := r.toCon(e.Name, t); ok {
			return con, nil
		}
		// The name is not declared in this compilation unit
		// (e.g. a built-in value), so make a declaration site
		// for it.
		return &core.ID{
			Pat:  &core.IDPat{T: t, Name: e.Name},
			Span: e.Span(),
		}, nil
	case *ast.If:
		return r.toIf(env, e, t)
	case *ast.InfixCall:
		if e.Kind == ast.OverOp {
			return r.toOverExp(e)
		}
		return r.toInfix(env, e, t)
	case *ast.Let:
		return r.toLetExp(env, e.Decls, e.Exp)
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
	if exp, isMethod := r.typeMap.methodCalls[apply]; isMethod {
		// A postfix method call: convert the structure call the type
		// resolver dispatched it to, never the call as parsed.
		return r.toExp(env, exp)
	}
	if sel, ok := apply.Fn.(*ast.RecordSelector); ok && sel.Safe {
		return r.toSafeNav(env, apply, sel, t)
	}
	if id, ok := apply.Fn.(*ast.ID); ok &&
		r.overloads.IsOverloaded(id.Name) && env.get(id.Name) == nil {
		return r.toOverloadApply(env, apply, id, t)
	}
	fn, err := r.toExp(env, apply.Fn)
	if err != nil {
		return nil, err
	}
	// If the function is a qualified-typed binding, supply its
	// dictionaries (the instances selected from the now-concrete use
	// type) as curried arguments ahead of the real argument
	// (hydromatic/morel#426, dictionary passing).
	if id, ok := apply.Fn.(*ast.ID); ok {
		dicts, useType, derr := r.dictionaryArgsForUse(env, id, apply.Fn)
		if derr != nil {
			return nil, derr
		}
		fn = applyDicts(fn, dicts, useType, apply.Span(), r.typeMap.sys)
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

// applyDicts wraps fn in one curried application per dictionary
// argument, giving each intermediate application the curried
// function type it produces (the innermost result is useType, the
// value's real function type at this use site). Returns fn unchanged
// when there are no dictionaries.
func applyDicts(fn core.Exp, dicts []core.Exp, useType types.Type,
	span token.Span, sys *types.System,
) core.Exp {
	if len(dicts) == 0 {
		return fn
	}
	// after[i] is the type of fn once dicts[0..i] have been applied:
	// the last dictionary leaves useType, each earlier one a curried
	// function returning the next.
	after := make([]types.Type, len(dicts))
	acc := useType
	for i, dict := range slices.Backward(dicts) {
		after[i] = acc
		acc = sys.Fn(dict.Type(), acc)
	}
	for i, dict := range dicts {
		fn = &core.Apply{T: after[i], Fn: fn, Arg: dict, Span: span}
	}
	return fn
}

// toOverloadApply lowers a call of an overloaded name. It selects
// the instance whose parameter type accepts the argument's resolved
// type and emits a call of that instance's hidden binding. Type
// resolution has already narrowed to one instance when it succeeds;
// here we recover which one by matching the argument type against
// each instance's parameter type. For Milestone 2a only a single
// unambiguous match is supported; zero or several matches are a
// positioned error (deferred ambiguity handling).
func (r *resolver) toOverloadApply(env *coreEnv, apply *ast.Apply,
	id *ast.ID, t types.Type,
) (core.Exp, error) {
	argType, err := r.typeMap.TypeOf(apply.Arg)
	if err != nil {
		return nil, err
	}
	var matches []OverloadInstance
	for _, inst := range r.overloads.Instances(id.Name) {
		fn, ok := inst.Type.(*types.Fn)
		if ok && specializes(argType, fn.Param) {
			matches = append(matches, inst)
		}
	}
	if len(matches) != 1 {
		// If the argument type is not concrete, no instance can be
		// selected statically: this is an overloaded use inside a
		// qualified-typed value. If a dictionary parameter for this name
		// is in scope (we are compiling the body of such a value), the
		// use compiles to an application of that parameter, which at run
		// time holds the instance the caller selected (dictionary
		// passing). Otherwise emit the milestone-1 placeholder.
		if containsVar(argType) {
			if dictPat, ok := r.dictionaryParams[id.Name]; ok {
				return r.dictApply(env, apply, dictPat, t)
			}
			return r.unresolvedOverload(env, apply, id, t)
		}
		return nil, &Error{
			Span: apply.Span(),
			Msg: "no unique instance of overloaded '" + id.Name +
				"' for argument type " + argType.String(),
		}
	}
	inst := matches[0]
	arg, err := r.toExp(env, apply.Arg)
	if err != nil {
		return nil, err
	}
	return &core.Apply{
		T:    t,
		Fn:   &core.ID{Pat: inst.HiddenPat},
		Arg:  arg,
		Span: apply.Span(),
	}, nil
}

// toCoreWithDictionaries compiles the value of a qualified binding,
// introducing one dictionary parameter per predicate. Inside exp, an
// overloaded name used at an abstract type compiles to a reference to
// its dictionary parameter (see toOverloadApply); the compiled value
// is wrapped in one curried lambda per predicate, so at a use site
// the caller supplies the instances as ordinary arguments (see
// dictionaryArgsForUse). The first predicate is the outermost
// parameter, matching the argument order at the use site.
func (r *resolver) toCoreWithDictionaries(env *coreEnv,
	predicates []types.Predicate, exp ast.Expr,
) (core.Exp, error) {
	dictPats := make([]*core.IDPat, len(predicates))
	for i, p := range predicates {
		dictPat := &core.IDPat{T: p.Type, Name: r.freshDictName()}
		dictPats[i] = dictPat
		r.dictionaryParams[p.Name] = dictPat
	}
	body, err := r.toExp(env, exp)
	for _, p := range predicates {
		delete(r.dictionaryParams, p.Name)
	}
	if err != nil {
		return nil, err
	}
	for _, dictPat := range slices.Backward(dictPats) {
		fnType, ok := r.typeMap.sys.Fn(dictPat.T, body.Type()).(*types.Fn)
		if !ok {
			return nil, &Error{
				Span: exp.Span(),
				Msg:  "dictionary parameter is not a function type",
			}
		}
		body = &core.Fn{T: fnType, IDPat: dictPat, Exp: body}
	}
	return body, nil
}

// dictApply lowers an overloaded application inside the body of a
// qualified value to an application of the dictionary parameter that
// carries the instance the caller selected (dictionary passing).
func (r *resolver) dictApply(env *coreEnv, apply *ast.Apply,
	dictPat *core.IDPat, t types.Type,
) (core.Exp, error) {
	arg, err := r.toExp(env, apply.Arg)
	if err != nil {
		return nil, err
	}
	return &core.Apply{
		T:    t,
		Fn:   &core.ID{Pat: dictPat},
		Arg:  arg,
		Span: apply.Span(),
	}, nil
}

// freshDictName returns a unique dictionary-parameter name; the "$"
// keeps it distinct from any user-written name.
func (r *resolver) freshDictName() string {
	name := "dict$" + itoa(r.dictCount)
	r.dictCount++
	return name
}

// dictionaryArgsForUse returns, for a use of a qualified-typed
// binding named by id, the dictionary arguments to pass at this use
// site — one per predicate, each the instance selected by the (now
// concrete) use type — in predicate order, together with the use
// type (the binding's real function type here). It returns nil, nil
// when id is not a qualified-typed binding.
func (r *resolver) dictionaryArgsForUse(env *coreEnv, id *ast.ID,
	fnNode ast.Expr,
) ([]core.Exp, types.Type, error) {
	// The binding may be local to this compilation unit (its IdPat
	// carries the qualified surface type) or declared in an earlier
	// statement (its qualified type is in the top-level bindings).
	var qual *types.Qualified
	if pat := env.get(id.Name); pat != nil {
		qual, _ = pat.SurfaceT.(*types.Qualified)
	} else if b, ok := r.typeMap.bindings[id.Name]; ok {
		qual, _ = b.Type.(*types.Qualified)
	}
	if qual == nil {
		return nil, nil, nil
	}
	useType, err := r.typeMap.TypeOf(fnNode)
	if err != nil {
		return nil, nil, err
	}
	// Recover the substitution from the scheme's type variables to the
	// concrete types at this use site by matching the scheme's base
	// type against the use type.
	subst := map[int]types.Type{}
	matchType(qual.Type, useType, subst)
	dicts := make([]core.Exp, 0, len(qual.Predicates))
	for _, p := range qual.Predicates {
		pfn, ok := p.Type.(*types.Fn)
		if !ok {
			return nil, nil, &Error{
				Span: id.Span(),
				Msg:  "overload predicate is not a function type",
			}
		}
		predArgType := substType(r.typeMap.sys, pfn.Param, subst)
		inst, ok := r.selectInstance(p.Name, predArgType)
		if !ok {
			return nil, nil, &Error{
				Span: id.Span(),
				Msg: "no unique instance of overloaded '" + p.Name +
					"' for argument type " + predArgType.String(),
			}
		}
		dicts = append(dicts, &core.ID{Pat: inst.HiddenPat})
	}
	return dicts, useType, nil
}

// selectInstance returns the unique overload instance of name
// callable with an argument of argType, or false if not exactly one
// matches.
func (r *resolver) selectInstance(name string, argType types.Type) (
	OverloadInstance, bool,
) {
	var matches []OverloadInstance
	for _, inst := range r.overloads.Instances(name) {
		if fn, ok := inst.Type.(*types.Fn); ok &&
			specializes(argType, fn.Param) {
			matches = append(matches, inst)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return OverloadInstance{}, false
}

// matchType records, for each type variable in pattern, the concrete
// type at the corresponding position in concrete. It is a one-way
// match (pattern has variables where concrete is concrete), enough to
// recover a qualified scheme's use-site substitution.
func matchType(pattern, concrete types.Type, subst map[int]types.Type) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pattern.(type) {
	case *types.Collection:
		if c, ok := concrete.(*types.Collection); ok {
			matchType(p.Elem, c.Elem, subst)
		}
	case *types.Fn:
		if c, ok := concrete.(*types.Fn); ok {
			matchType(p.Param, c.Param, subst)
			matchType(p.Result, c.Result, subst)
		}
	case *types.List:
		if c, ok := concrete.(*types.List); ok {
			matchType(p.Elem, c.Elem, subst)
		}
	case *types.Named:
		if c, ok := concrete.(*types.Named); ok && p.Name == c.Name {
			matchTypes(p.Args, c.Args, subst)
		}
	case *types.Record:
		if c, ok := concrete.(*types.Record); ok {
			matchFields(p.Fields, c.Fields, subst)
		}
	case *types.Tuple:
		if c, ok := concrete.(*types.Tuple); ok {
			matchTypes(p.Args, c.Args, subst)
		}
	case *types.Var:
		if _, seen := subst[p.Ordinal]; !seen {
			subst[p.Ordinal] = concrete
		}
	}
}

// matchTypes matches two equal-length type lists position by
// position (doing nothing if the lengths differ).
func matchTypes(ps, cs []types.Type, subst map[int]types.Type) {
	if len(ps) != len(cs) {
		return
	}
	for i := range ps {
		matchType(ps[i], cs[i], subst)
	}
}

// matchFields is matchTypes for record fields.
func matchFields(ps, cs []types.Field, subst map[int]types.Type) {
	if len(ps) != len(cs) {
		return
	}
	for i := range ps {
		matchType(ps[i].Type, cs[i].Type, subst)
	}
}

// substType applies a type-variable substitution (by ordinal),
// leaving unmapped variables unchanged.
func substType(sys *types.System, t types.Type,
	subst map[int]types.Type,
) types.Type {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Collection:
		return sys.Collection(substType(sys, t.Elem, subst))
	case *types.Fn:
		return sys.Fn(substType(sys, t.Param, subst),
			substType(sys, t.Result, subst))
	case *types.List:
		return sys.List(substType(sys, t.Elem, subst))
	case *types.Named:
		args := make([]types.Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = substType(sys, a, subst)
		}
		return sys.Named(t.Name, args...)
	case *types.Record:
		fields := make([]types.Field, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = types.Field{
				Label: f.Label, Type: substType(sys, f.Type, subst),
			}
		}
		return sys.Record(fields)
	case *types.Tuple:
		args := make([]types.Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = substType(sys, a, subst)
		}
		return sys.Tuple(args...)
	case *types.Var:
		if r, ok := subst[t.Ordinal]; ok {
			return r
		}
		return t
	}
	return t
}

// unresolvedOverload builds a placeholder for an overloaded
// application whose argument type is not concrete (so no instance
// can be selected statically). The placeholder has the result type
// of the application but raises Fail if it is ever evaluated; the
// enclosing value type-checks (with a qualified type) but cannot yet
// be applied. A later milestone will replace this with dictionary
// passing.
func (r *resolver) unresolvedOverload(env *coreEnv,
	apply *ast.Apply, id *ast.ID, t types.Type,
) (core.Exp, error) {
	// Evaluate the argument for its effects and type, even though the
	// result is discarded by the raise.
	_, err := r.toExp(env, apply.Arg)
	if err != nil {
		return nil, err
	}
	sys := r.typeMap.sys
	tc, ok := sys.LookupTyCon("Fail")
	if !ok {
		return nil, &Error{
			Span: apply.Span(),
			Msg: "no unique instance of overloaded '" + id.Name +
				"' (cannot build placeholder)",
		}
	}
	exnType := tc.Result
	failCon, _ := r.toCon("Fail", sys.Fn(sys.String, exnType))
	msg := &core.Literal{
		T:    sys.String,
		Kind: ast.StringLiteralOp,
		Value: "overloaded '" + id.Name +
			"' cannot yet be applied at an abstract type",
	}
	failExn := &core.Apply{
		T: exnType, Fn: failCon, Arg: msg,
		Span: apply.Span(),
	}
	return &core.Apply{
		T: t,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(exnType, t),
			Name: RaiseName,
		}},
		Arg:  failExn,
		Span: apply.Span(),
	}, nil
}

// containsVar reports whether a type contains a type variable.
func containsVar(t types.Type) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Collection:
		return containsVar(t.Elem)
	case *types.Fn:
		return containsVar(t.Param) || containsVar(t.Result)
	case *types.List:
		return containsVar(t.Elem)
	case *types.Named:
		return slices.ContainsFunc(t.Args, containsVar)
	case *types.Record:
		for _, f := range t.Fields {
			if containsVar(f.Type) {
				return true
			}
		}
		return false
	case *types.Tuple:
		return slices.ContainsFunc(t.Args, containsVar)
	case *types.Var:
		return true
	}
	return false
}

// toFrom converts a query to Core. Only the subset that evaluates
// so far is supported: a single "in" scan, followed by "where"
// filters and an optional trailing "yield". Anything else — a
// scalar scan, a join, a group, an order, or a step after a yield —
// is not yet convertible, so the statement produces no output.
func (r *resolver) toFrom(env *coreEnv, from *ast.From,
	t types.Type,
) (core.Exp, error) {
	// "current" rewrites to the row entering each step, and
	// "ordinal" counts this query's rows; save the outer state for a
	// nested query, restore it on the way out. The enclosing state
	// stays reachable: the expressions this query evaluates before
	// its first row read it (see beforeFirstRow).
	savedCurrent, savedInQuery := r.currentRow, r.inQuery
	savedOwner, savedScopes := r.ordinalOwner, r.ordinalScopes
	savedEnclosingRow := r.enclosingRow
	savedEnclosingOwner := r.enclosingOwner
	defer func() {
		r.currentRow, r.inQuery = savedCurrent, savedInQuery
		r.ordinalOwner, r.ordinalScopes = savedOwner, savedScopes
		r.enclosingRow = savedEnclosingRow
		r.enclosingOwner = savedEnclosingOwner
	}()
	r.enclosingRow, r.enclosingOwner = r.currentRow, r.ordinalOwner
	r.inQuery = true
	scope := &ordinalScope{
		pat: &core.IDPat{T: r.typeMap.sys.Int, Name: ordinalName},
	}
	r.ordinalScopes = append(r.ordinalScopes, scope)
	r.ordinalOwner = len(r.ordinalScopes)
	steps := make([]core.FromStep, 0, len(from.Steps))
	cur := env
	computeScalar := false
	var rowVars []*core.IDPat
	for i := 0; i < len(from.Steps); i++ {
		step := from.Steps[i]
		r.currentRow = r.buildRow(rowVars)
		// A "yield" that is not the last step rebinds the row's
		// variables to the fields the yielded value exposes.
		if y, ok := step.(*ast.YieldStep); ok &&
			i < len(from.Steps)-1 {
			yieldSteps, newCur, newVars, err := r.toYieldStep(cur, y)
			if err != nil {
				return nil, err
			}
			steps = append(steps, yieldSteps...)
			cur = newCur
			rowVars = newVars
			continue
		}
		// A "group" absorbs the "compute" that follows it, so the
		// aggregates are typed over the pre-group rows.
		if g, ok := step.(*ast.GroupStep); ok {
			gs, gVars, newCur, ni, err := r.toGroupAbsorb(
				cur, rowVars, from, g, i)
			if err != nil {
				return nil, err
			}
			steps = append(steps, gs...)
			cur = newCur
			rowVars = gVars
			i = ni
			continue
		}
		// A "yieldAll" flattens: scan the collection per row, the
		// element becoming the whole row, named by the binder or
		// "current".
		if ya, ok := step.(*ast.YieldAllStep); ok {
			yaSteps, pat, err := r.toYieldAllStep(cur, ya)
			if err != nil {
				return nil, err
			}
			steps = append(steps, yaSteps...)
			cur = cur.bind(pat)
			rowVars = []*core.IDPat{pat}
			continue
		}
		// A standalone "compute" is a group with no keys whose
		// single row is unwrapped to a scalar by a wrapping
		// "only".
		if c, ok := step.(*ast.ComputeStep); ok {
			groupSteps, groupRowVars, newCur, err := r.toGroupStep(
				cur, rowVars, nil, c,
			)
			if err != nil {
				return nil, err
			}
			steps = append(steps, groupSteps...)
			cur = newCur
			rowVars = groupRowVars
			computeScalar = true
			continue
		}
		newSteps, newCur, _, err := r.toQueryStep(env, cur,
			rowVars, step, i == 0)
		if err != nil {
			return nil, err
		}
		steps = append(steps, newSteps...)
		cur = newCur
		rowVars = updateRowVars(rowVars, newSteps)
	}
	if len(steps) == 0 || !isScanStep(steps[0]) {
		// A query with no scans iterates one row of no variables:
		// seed it with a scan over the unit singleton, whose
		// variable stays out of the row.
		sys := r.typeMap.sys
		seed := &core.Scan{
			Pat: &core.IDPat{T: sys.Unit, Name: "$empty"},
			Exp: &core.List{
				T: sys.List(sys.Unit),
				Args: []core.Exp{&core.Literal{
					T:     sys.Unit,
					Kind:  ast.UnitLiteralOp,
					Value: core.Unit{},
				}},
			},
		}
		steps = append([]core.FromStep{seed}, steps...)
	}
	// The query maintains a counter only if something reads it.
	var ordinal *core.IDPat
	if scope.used {
		ordinal = scope.pat
	}
	if computeScalar {
		return r.onlyWrap(t, steps, from, ordinal), nil
	}
	return &core.From{
		T:       t,
		Steps:   steps,
		Kind:    from.Kind,
		Ordinal: ordinal,
	}, nil
}

// ordinalScope is one query's row counter: the hidden variable
// that holds it, and whether any "ordinal" reads it — a query that
// nothing counts does not maintain one.
type ordinalScope struct {
	pat  *core.IDPat
	used bool
}

// beforeFirstRow converts an expression that the query evaluates
// before its first row — the collection its first step scans, a
// "take" or "skip" count, an operand of "union", "except" or
// "intersect", and the function of a "through" or an "into". Such
// an expression is evaluated once per execution of the query, hence
// once per row of the step containing it, so "current" and
// "ordinal" in it read that enclosing row, not one of this query's.
// A query in one of those positions is evaluated before its
// enclosing query's first row too, so the reading skips that query
// as well.
func (r *resolver) beforeFirstRow(
	f func() (core.Exp, error),
) (core.Exp, error) {
	savedRow, savedOwner := r.currentRow, r.ordinalOwner
	r.currentRow, r.ordinalOwner = r.enclosingRow, r.enclosingOwner
	defer func() { r.currentRow, r.ordinalOwner = savedRow, savedOwner }()
	return f()
}

// toGroupAbsorb converts a "group" step at index i, absorbing the
// "compute" that follows it (so the aggregates are typed over the
// pre-group rows); it returns the index of the last step consumed.
func (r *resolver) toGroupAbsorb(cur *coreEnv,
	rowVars []*core.IDPat, from *ast.From, g *ast.GroupStep, i int,
) ([]core.FromStep, []*core.IDPat, *coreEnv, int, error) {
	var compute *ast.ComputeStep
	if i+1 < len(from.Steps) {
		if c, ok := from.Steps[i+1].(*ast.ComputeStep); ok {
			compute = c
			i++
		}
	}
	groupSteps, groupRowVars, newCur, err := r.toGroupStep(
		cur, rowVars, g, compute)
	return groupSteps, groupRowVars, newCur, i, err
}

// isScanStep reports whether a step is a scan.
func isScanStep(s core.FromStep) bool {
	_, ok := s.(*core.Scan)
	return ok
}

// onlyWrap wraps a standalone-compute query in "only": the inner
// from collects the empty-key group's single row, and "only"
// unwraps it to the scalar.
func (r *resolver) onlyWrap(t types.Type, steps []core.FromStep,
	from *ast.From, ordinal *core.IDPat,
) core.Exp {
	sys := r.typeMap.sys
	bagT := sys.Named("bag", t)
	return &core.Apply{
		T: t,
		Fn: &core.ID{Pat: &core.IDPat{
			T:    sys.Fn(bagT, t),
			Name: onlyName,
		}},
		Arg: &core.From{
			T:       bagT,
			Steps:   steps,
			Kind:    from.Kind,
			Ordinal: ordinal,
		},
		Span: from.Span(),
	}
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
func (r *resolver) toQueryStep(env, cur *coreEnv,
	rowVars []*core.IDPat, step ast.FromStep, first bool,
) ([]core.FromStep, *coreEnv, bool, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch s := step.(type) {
	case *ast.DistinctStep:
		return []core.FromStep{&core.Distinct{}}, cur, false, nil
	case *ast.IntoStep:
		// "into f" applies f to the whole query result, so f is an
		// expression of the root scope, not the query row's -- the
		// query variables are out of scope inside it.
		fn, err := r.beforeFirstRow(func() (core.Exp, error) {
			return r.toExp(env, s.Exp)
		})
		return []core.FromStep{&core.Into{Fn: fn}}, cur, true, err
	case *ast.OrderStep:
		exp, err := r.toExp(cur, s.Exp)
		return []core.FromStep{
			&core.Order{Exp: exp, Span: s.Exp.Span()},
		}, cur, false, err
	case *ast.RequireStep:
		// "require p" in a "forall" reduces to yielding p, whose
		// truth for every row the quantifier then checks.
		exp, err := r.toExp(cur, s.Exp)
		return []core.FromStep{&core.Yield{Exp: exp}}, cur, true, err
	case *ast.Scan:
		scanSteps, newCur, err := r.toScanStep(cur, rowVars, s, first)
		return scanSteps, newCur, false, err
	case *ast.SetOpStep:
		setOp, err := r.toSetOpStep(env, s)
		return []core.FromStep{setOp}, cur, false, err
	case *ast.SkipStep:
		exp, err := r.beforeFirstRow(func() (core.Exp, error) {
			return r.toExp(env, s.Exp)
		})
		return []core.FromStep{&core.Skip{Exp: exp}}, cur, false, err
	case *ast.TakeStep:
		exp, err := r.beforeFirstRow(func() (core.Exp, error) {
			return r.toExp(env, s.Exp)
		})
		return []core.FromStep{&core.Take{Exp: exp}}, cur, false, err
	case *ast.ThroughStep:
		return r.toThroughStep(env, cur, s)
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
func (r *resolver) toGroupStep(cur *coreEnv,
	rowVars []*core.IDPat, group *ast.GroupStep,
	compute *ast.ComputeStep,
) ([]core.FromStep, []*core.IDPat, *coreEnv, error) {
	var keys []core.GroupKey
	if group != nil {
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
	}
	// Keys are in scope in aggregate functions and residual
	// expressions, but not in "over" arguments, which range over
	// the pre-group rows.
	keyEnv := cur
	for _, k := range keys {
		keyEnv = keyEnv.bind(k.Pat)
	}
	var aggs []core.GroupAgg
	var residuals []core.YieldField
	fieldPats := make([]*core.IDPat, 0, len(keys))
	for _, k := range keys {
		fieldPats = append(fieldPats, k.Pat)
	}
	if compute != nil {
		var err error
		aggs, residuals, fieldPats, err = r.toComputeFields(
			cur, keyEnv, rowVars, compute, fieldPats)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	groupStep := &core.Group{Keys: keys, Aggs: aggs}
	groupSteps := []core.FromStep{groupStep}
	if len(residuals) > 0 {
		// Re-yield the visible row: keys and direct aggregates
		// pass through; residual fields are computed from the
		// hidden aggregates.
		var yf []core.YieldField
		for _, k := range keys {
			yf = append(yf, core.YieldField{
				Pat: k.Pat, Exp: &core.ID{Pat: k.Pat},
			})
		}
		for _, a := range aggs {
			if !strings.HasPrefix(a.Pat.Name, "$agg_") {
				yf = append(yf, core.YieldField{
					Pat: a.Pat, Exp: &core.ID{Pat: a.Pat},
				})
			}
		}
		yf = append(yf, residuals...)
		groupSteps = append(groupSteps, &core.Yield{Fields: yf})
	}
	binder := ""
	if group != nil {
		binder = group.Binder
	}
	if binder == "" {
		newCur := cur
		for _, p := range fieldPats {
			newCur = newCur.bind(p)
		}
		return groupSteps, fieldPats, newCur, nil
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
	binderPat := &core.IDPat{T: yieldExp.Type(), Name: binder}
	yieldStep := &core.Yield{
		Fields: []core.YieldField{{Pat: binderPat, Exp: yieldExp}},
	}
	return append(groupSteps, yieldStep),
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
	return ok && rec.Base == nil && len(rec.Fields) == 1
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
	[]core.FromStep, *coreEnv, []*core.IDPat, error,
) {
	if s.Binder != "" {
		exp, err := r.toExp(cur, s.Exp)
		if err != nil {
			return nil, nil, nil, err
		}
		pat := &core.IDPat{T: exp.Type(), Name: s.Binder}
		field := core.YieldField{Pat: pat, Exp: exp}
		step := &core.Yield{Fields: []core.YieldField{field}}
		return []core.FromStep{step}, cur.bind(pat),
			[]*core.IDPat{pat}, nil
	}
	// A record wrapped in "let"s — the record is not the yielded
	// expression itself — cannot be split into its fields here; it
	// becomes a step of its own.
	rec := yieldRecord(s.Exp, r.typeMap.desugared)
	if rec != nil && ast.Expr(rec) != s.Exp {
		return r.toWrappedYieldStep(cur, s.Exp)
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
	step := &core.Yield{Fields: fields}
	return []core.FromStep{step}, newCur, rowVars, nil
}

// rowName is the variable that holds a yielded row while its
// fields are projected out of it. No source label can collide
// with it: "$" cannot appear in a Morel label.
const rowName = "$row"

// toWrappedYieldStep converts a "yield" whose value is a record
// wrapped in "let"s — those the user wrote, and those a record's
// modifiers desugar to. It becomes two steps: one that binds the
// whole row, and one that projects its fields, because the rest of
// the pipeline reads a step's fields off a tuple. Splitting also
// keeps the expression to one evaluation per row.
func (r *resolver) toWrappedYieldStep(cur *coreEnv, e ast.Expr) (
	[]core.FromStep, *coreEnv, []*core.IDPat, error,
) {
	exp, err := r.toExp(cur, e)
	if err != nil {
		return nil, nil, nil, err
	}
	rowPat := &core.IDPat{T: exp.Type(), Name: rowName}
	rowID := &core.ID{Pat: rowPat}
	bind := &core.Yield{
		Fields: []core.YieldField{{Pat: rowPat, Exp: exp}},
	}
	var fields []core.YieldField
	newCur := cur
	var rowVars []*core.IDPat
	for i, f := range recordLikeFields(exp.Type()) {
		pat := &core.IDPat{T: f.Type, Name: f.Label}
		fields = append(fields, core.YieldField{
			Pat: pat,
			Exp: &core.Apply{
				T: f.Type,
				Fn: &core.Selector{
					T:     r.typeMap.sys.Fn(exp.Type(), f.Type),
					Name:  f.Label,
					Index: i,
				},
				Arg: rowID,
			},
		})
		newCur = newCur.bind(pat)
		rowVars = append(rowVars, pat)
	}
	project := &core.Yield{Fields: fields}
	return []core.FromStep{bind, project}, newCur, rowVars, nil
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
	if rec, ok := exp.(*ast.Record); ok && rec.Base == nil {
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
		a, err := r.beforeFirstRow(func() (core.Exp, error) {
			return r.toExp(env, arg)
		})
		if err != nil {
			return nil, err
		}
		args[i] = a
	}
	return &core.SetOp{Kind: s.Kind, Args: args, Distinct: s.Distinct}, nil
}

// toThroughStep converts a "through pat in f" step: f maps the
// collection to a new one whose elements pat binds. The pattern's
// variables become the query's variables downstream. Like "into",
// f applies to the whole collection, so it is an expression of the
// root scope, evaluated before the query's first row.
func (r *resolver) toThroughStep(env, cur *coreEnv,
	s *ast.ThroughStep,
) ([]core.FromStep, *coreEnv, bool, error) {
	pat, err := r.toPat(s.Pat)
	if err != nil {
		return nil, nil, false, err
	}
	fn, err := r.beforeFirstRow(func() (core.Exp, error) {
		return r.toExp(env, s.Exp)
	})
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
// ("=") scan is not yet supported. first says whether the scan is
// the query's first step, whose source is evaluated before the
// query has a row of its own.
func (r *resolver) toScanStep(cur *coreEnv,
	rowVars []*core.IDPat, s *ast.Scan, first bool,
) ([]core.FromStep, *coreEnv, error) {
	pat, err := r.toPat(s.Pat)
	if err != nil {
		return nil, nil, err
	}
	source := func(f func() (core.Exp, error)) (core.Exp, error) {
		return f()
	}
	if first {
		source = r.beforeFirstRow
	}
	var exp core.Exp
	// lint: sort until '^\t}' where '^\tcase '
	switch s.Kind {
	case ast.ScanEq:
		// "pat = exp" binds the pattern to the value of exp; it
		// lowers to a scan of the singleton list of that value.
		val, valErr := source(func() (core.Exp, error) {
			return r.toExp(cur, s.Exp)
		})
		if valErr != nil {
			return nil, nil, valErr
		}
		exp = &core.List{
			T:    r.typeMap.sys.List(val.Type()),
			Args: []core.Exp{val},
		}
	case ast.ScanIn:
		// The source is in the current scope, so a later scan may
		// depend on an earlier scan's variables.
		exp, err = source(func() (core.Exp, error) {
			return r.toExp(cur, s.Exp)
		})
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
	steps := r.extentScans(s, pat, exp)
	for _, id := range core.PatIDs(pat) {
		cur = cur.bind(id)
	}
	// A "join ... on" condition filters over the scan's variables,
	// so it lowers to a where after the scan — except for an outer
	// join, which evaluates it over the unwrapped values inside
	// the join itself.
	if s.On != nil {
		on, err := r.toExp(cur, s.On)
		if err != nil {
			return nil, nil, err
		}
		scan, isScan := steps[0].(*core.Scan)
		if isScan && (s.Join == ast.LeftJoinOp ||
			s.Join == ast.RightJoinOp || s.Join == ast.FullJoinOp) {
			// An outer join has one scan; extentScans only splits a
			// sourceless scan, which has no "on".
			scan.On = on
		} else {
			steps = append(steps, &core.Where{Exp: on})
		}
	}
	// An outer join's nullable variables are option-typed
	// downstream: the runtime wraps their slot values, and here
	// their patterns' types wrap to match (after the on
	// condition, which sees the raw types).
	sys := r.typeMap.sys
	if optionalizesRight(s.Join) {
		for _, id := range core.PatIDs(pat) {
			id.T = sys.Named(typeOption, id.T)
		}
	}
	if optionalizesLeft(s.Join) {
		for _, id := range rowVars {
			id.T = sys.Named(typeOption, id.T)
		}
	}
	return steps, cur, nil
}

// extentScans is the scan, or scans, a scan step lowers to.
// Usually it is the one scan, over the source the caller built.
//
// A sourceless scan whose pattern is composite is different. Its
// rows are the combinations of the variables the pattern binds;
// the pattern's other parts -- literals, constructors, wildcards
// -- bind nothing, and so say nothing about the rows. So a
// pattern that binds no variables leaves no scans, and the step
// contributes the one row that binds nothing.
//
// A pattern that does bind variables is left as one scan over the
// extent of its whole type while that extent has an end. Where it
// has none -- a record with an int field, say -- that one scan
// grounds nothing, because a single unbounded component makes the
// whole of it unbounded, and a variable of a bounded type loses
// the bound its own type gives it; so the pattern becomes one
// scan per variable, each over the extent of that variable's own
// type, and a variable of an unbounded type is left for a
// predicate to ground, as a lone unbounded variable is.
//
// An "as" pattern is never split. Its variable stands for the
// whole of what the components match, which separate scans would
// not keep in step.
func (r *resolver) extentScans(s *ast.Scan, pat core.Pat,
	exp core.Exp,
) []core.FromStep {
	_, isID := pat.(*core.IDPat)
	ids := core.PatIDs(pat)
	split := len(ids) == 0 || isInfiniteExtent(exp)
	if s.Kind != ast.ScanUnbounded || isID || s.On != nil ||
		!split || containsAsPat(pat) {
		return []core.FromStep{
			&core.Scan{Pat: pat, Exp: exp, Join: s.Join},
		}
	}
	var steps []core.FromStep
	for i, id := range ids {
		// The first scan keeps the step's join; those that follow
		// are joined to it by comma.
		join := ast.ScanOp
		if i == 0 {
			join = s.Join
		}
		steps = append(steps, &core.Scan{
			Pat:  id,
			Exp:  r.extentExp(id.T, s.Span()),
			Join: join,
		})
	}
	return steps
}

// containsAsPat reports whether a pattern names the whole of what
// some part of it matches, "p as (b, c)".
func containsAsPat(pat core.Pat) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *core.AsPat:
		return true
	case *core.ConPat:
		return containsAsPat(p.Arg)
	case *core.ConsPat:
		return containsAsPat(p.Head) || containsAsPat(p.Tail)
	case *core.ListPat:
		return slices.ContainsFunc(p.Args, containsAsPat)
	case *core.TuplePat:
		return slices.ContainsFunc(p.Args, containsAsPat)
	}
	return false
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
// elements are the fields in canonical order. A record with
// modifiers converts as the nested "let"s that the type resolver
// replaced it with.
func (r *resolver) toRecord(env *coreEnv, record *ast.Record,
	t types.Type,
) (core.Exp, error) {
	if record.Base != nil {
		exp, ok := r.typeMap.desugared[record]
		if !ok {
			// checkRecordModifiers rejects a record the type
			// resolver could not desugar, so this cannot happen.
			return nil, &Error{
				Span: record.Span(),
				Msg:  "record modifiers were not resolved",
			}
		}
		return r.toExp(env, exp)
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
		// core pattern is the pattern it wraps. Keep an aliased
		// annotation ("myInt" not "int") as the binding's surface
		// type, for display.
		inner, err := r.toPat(p.Pat)
		if err != nil {
			return nil, err
		}
		if idPat, ok := inner.(*core.IDPat); ok {
			surface, e1 := r.typeMap.sys.SurfaceFromAST(p.Type,
				map[string]int{})
			expanded, e2 := r.typeMap.sys.FromAST(p.Type,
				map[string]int{})
			if e1 == nil && e2 == nil && surface != expanded {
				idPat.SurfaceT = surface
			}
		}
		return inner, nil
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
				Span: p.NameSpan,
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
	// The target may be a tuple type; a tuple is a record whose
	// labels are the ordinals, so "{1 = x, 2 = y}" matches it.
	fields := recordLikeFields(t)
	if fields == nil {
		return &core.WildcardPat{T: t}, nil
	}
	byLabel := make(map[string]ast.Pat, len(p.Fields))
	for _, f := range p.Fields {
		byLabel[f.Label] = f.Pat
	}
	args := make([]core.Pat, len(fields))
	for i, f := range fields {
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

// recordLikeFields is the labelled fields of a record or tuple
// type, in the order of the values of a tuple of that type, or
// nil if the type is neither.
func recordLikeFields(t types.Type) []types.Field {
	switch t := t.(type) {
	case *types.Record:
		return t.Fields
	case *types.Tuple:
		fields := make([]types.Field, len(t.Args))
		for i, arg := range t.Args {
			fields[i] = types.Field{
				Label: strconv.Itoa(i + 1), Type: arg,
			}
		}
		return fields
	default:
		return nil
	}
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

// toLetExp lowers a "let ... in e end" expression. The let gets its
// own overload scope, so "over"/"val inst" declared inside it
// register locally — visible to the body but discarded afterwards —
// and never leak to the enclosing (session) scope.
func (r *resolver) toLetExp(env *coreEnv, decls []ast.Decl,
	exp ast.Expr,
) (core.Exp, error) {
	saved := r.overloads
	if saved != nil {
		r.overloads = NewChildOverloadEnv(saved)
	}
	body, err := r.flattenLet(env, decls, exp)
	r.overloads = saved
	return body, err
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

// charValue is the value of a character constant, from its
// unquoted text.
//
// A morel character is a byte, and a morel string is a string of
// bytes, so a "\ddd" escape decodes to the one byte ddd -- which
// above 127 is not a character of UTF-8 on its own. Reading the
// text as UTF-8 would make such a constant the replacement
// character, 65533, so a text of one byte is that byte. A
// character written as itself is read as UTF-8, since that is how
// the source is; it is a character only if it is one a byte can
// hold.
func charValue(text string) (int32, bool) {
	if len(text) == 1 {
		return int32(text[0]), true
	}
	runes := []rune(text)
	const charCount = 256
	if len(runes) == 1 && runes[0] < charCount {
		return runes[0], true
	}
	return 0, false
}

// literalValue converts a literal's text to its runtime value.
func literalValue(kind ast.Op, text string) (any, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ast.CharLiteralOp, ast.CharLiteralPatOp:
		// A constant that is not exactly one character is rejected
		// earlier, at type resolution (see checkCharLiteral); guard
		// here too so a stray bad literal cannot index out of range.
		c, ok := charValue(text)
		if !ok {
			return nil, &Error{
				Msg: "character constant not length one",
			}
		}
		return c, nil
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
