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
	"strings"
	"unicode/utf8"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/internal/unify"
)

// Type constructors used in unification terms.
const (
	fnTyCon     = "fn"
	listTyCon   = "list"
	bagTyCon    = "bag"
	recordTyCon = "record"
	tupleTyCon  = "tuple"

	// collectionTyCon is the internal term operator for a
	// collection: collection(elem, orderedness), where orderedness
	// is the atom "ordered" (a list), "unordered" (a bag), or a
	// variable. The "$" prefix keeps it distinct from any
	// user-facing type name. A list and a bag are the same term but
	// for the orderedness argument, so they unify as far as their
	// elements and clash only on orderedness.
	collectionTyCon = "$collection"
	// argTyCon bundles the two orderedness arguments of a meet into
	// one term, so a single constraint can prune on the pair.
	argTyCon      = "$arg"
	orderedName   = "ordered"
	unorderedName = "unordered"
)

// Resolved is the outcome of type deduction: the declaration and
// a type for every node in it.
type Resolved struct {
	Decl    ast.Decl
	TypeMap *TypeMap
}

// Deduce infers a type for every node of a declaration, in an
// environment given by bindings.
func Deduce(sys *types.System, bindings []Binding,
	decl ast.Decl,
) (*Resolved, error) {
	r := &typeResolver{
		sys:      sys,
		u:        unify.New(),
		nodeTerm: map[ast.Node]unify.Term{},
	}
	var env typeEnv = emptyTypeEnv{}
	if len(bindings) > 0 {
		byName := make(map[string]*Binding, len(bindings))
		for i := range bindings {
			byName[bindings[i].Name] = &bindings[i]
		}
		env = &bindingTypeEnv{parent: env, bindings: byName}
		r.bindings = byName
	}
	var termMap []patTerm
	decl2, err := r.deduceDecl(env, decl, &termMap)
	if err != nil {
		return nil, err
	}
	// Unify; if an operator's type is undetermined and it has a
	// preferred type (e.g. int for "+"), apply the preference and
	// unify again.
	for {
		subst, err := r.u.Unify(r.pairs, r.actions,
			r.constraints)
		if err != nil {
			return nil, &Error{
				Span: decl.Span(),
				Msg:  "Cannot deduce type: " + err.Error(),
			}
		}
		again := false
		// A safe selector whose receiver resolved layer by layer
		// may not have deduced its result during unification;
		// retry against the full substitution.
		for len(r.pendingSafe) > 0 {
			ps := r.pendingSafe[0]
			r.pendingSafe = r.pendingSafe[1:]
			if _, isVar := subst.Resolve(ps.vResult).(*unify.Var); !isVar {
				continue
			}
			t := subst.Resolve(ps.vArg)
			if res := safeFieldTerm(t, ps.name, subst); res != nil {
				r.equiv(ps.vResult, res)
				again = true
				break
			}
		}
		if again {
			continue
		}
		for len(r.preferred) > 0 {
			pt := r.preferred[0]
			r.preferred = r.preferred[1:]
			if _, isVar := subst.Resolve(pt.v).(*unify.Var); isVar {
				r.equiv(pt.v, r.primTerm(pt.prim))
				again = true
				break
			}
		}
		if again {
			continue
		}
		typeMap := &TypeMap{
			sys:      sys,
			nodeTerm: r.nodeTerm,
			subst:    subst,
		}
		err = r.checkFieldRefs(typeMap)
		if err != nil {
			return nil, err
		}
		err = r.checkNumericOperators(typeMap)
		if err != nil {
			return nil, err
		}
		err = r.checkOrdinalOrder(subst)
		if err != nil {
			return nil, err
		}
		return &Resolved{Decl: decl2, TypeMap: typeMap}, nil
	}
}

// numericCall is an application of an overloaded numeric
// operator, to be checked against the operator's overload class
// once unification has resolved its type.
type numericCall struct {
	name  string
	apply *ast.Apply
}

// pendingSafeCall is a safe selector avaiting a post-unification
// retry.
type pendingSafeCall struct {
	name    string
	vArg    *unify.Var
	vResult *unify.Var
}

// selectorCall is a field reference "#field arg" (or its "arg
// .field" sugar), to be checked against the resolved type of its
// argument once unification is complete.
type selectorCall struct {
	sel   *ast.RecordSelector
	apply *ast.Apply
}

// checkFieldRefs checks every field reference "#field arg" now
// that unification has resolved its argument's type. An
// argument whose
// type is still a variable is a flex record we cannot resolve; a
// non-record argument cannot have fields; and a record or tuple
// argument must actually contain the field. The three cases carry
// distinct messages and spans (the first two point at the
// argument, the last at the selector).
func (r *typeResolver) checkFieldRefs(m *TypeMap) error {
	for _, call := range r.selectorCalls {
		argType, err := m.TypeOf(call.apply.Arg)
		if err != nil {
			return err
		}
		if call.sel.Safe {
			err := checkSafeFieldRef(call, argType)
			if err != nil {
				return err
			}
			continue
		}
		if _, isVar := argType.(*types.Var); isVar {
			return &Error{
				Span: call.apply.Arg.Span(),
				Msg: "unresolved flex record (can't tell what " +
					"fields there are besides #" + call.sel.Name + ")",
			}
		}
		names := fieldNames(argType)
		if names == nil {
			return &Error{
				Span: call.apply.Arg.Span(),
				Msg: "reference to field " + call.sel.Name +
					" of non-record type " + argType.String(),
			}
		}
		if !slices.Contains(names, call.sel.Name) {
			return &Error{
				Span: call.sel.Span(),
				Msg: "no field '" + call.sel.Name + "' in type '" +
					argType.String() + "'",
			}
		}
	}
	return nil
}

// checkSafeFieldRef checks a safe selector "arg?.field": the
// receiver must have at least one functor layer (option, list,
// bag, vector), the innermost type must be a record, and the
// record must contain the field.
func checkSafeFieldRef(call selectorCall, argType types.Type,
) error {
	inner, layers := peelSafeFunctors(argType)
	if layers == 0 {
		return &Error{
			Span: call.apply.Arg.Span(),
			Msg: "'?.' applied to non-functor type " +
				argType.String() + " (expected option or list)",
		}
	}
	names := fieldNames(inner)
	if names == nil {
		return &Error{
			Span: call.apply.Arg.Span(),
			Msg: "reference to field " + call.sel.Name +
				" of non-record type " + inner.String(),
		}
	}
	if !slices.Contains(names, call.sel.Name) {
		return &Error{
			Span: call.sel.Span(),
			Msg: "no field '" + call.sel.Name + "' in type '" +
				inner.String() + "'",
		}
	}
	return nil
}

// peelSafeFunctors strips the functor layers a safe selector
// tunnels through, returning the innermost type and how many
// layers were stripped.
func peelSafeFunctors(t types.Type) (types.Type, int) {
	n := 0
	for {
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch tt := t.(type) {
		case *types.Collection:
			t = tt.Elem
		case *types.List:
			t = tt.Elem
		case *types.Named:
			if len(tt.Args) == 1 && (tt.Name == typeOption ||
				tt.Name == bagTyCon || tt.Name == typeVector) {
				t = tt.Args[0]
				break
			}
			return t, n
		default:
			return t, n
		}
		n++
	}
}

// fieldNames returns the field labels of a record or tuple type
// (a tuple's fields are named "1", "2", ...), or nil if the type
// is not record-like. unit, though the empty record, is a
// primitive here and so counts as non-record.
func fieldNames(t types.Type) []string {
	switch t := t.(type) {
	case *types.Record:
		names := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			names[i] = f.Label
		}
		return names
	case *types.Tuple:
		names := make([]string, len(t.Args))
		for i := range t.Args {
			names[i] = strconv.Itoa(i + 1)
		}
		return names
	default:
		return nil
	}
}

// numericOpDomain gives the types for which each overloaded
// numeric operator is defined (its SML overload class). 'div' and
// 'mod' are integer-and-word; '/' is absent because it is real-only,
// so a bad operand is a unification conflict, not an excluded class
// member. 'abs' is int and real only, as a word is unsigned.
var numericOpDomain = map[string]map[string]bool{
	absName:  {intName: true, realName: true},
	opTimes:  {intName: true, realName: true, wordName: true},
	opPlus:   {intName: true, realName: true, wordName: true},
	opMinus:  {intName: true, realName: true, wordName: true},
	opDiv:    {intName: true, wordName: true},
	opMod:    {intName: true, wordName: true},
	opNegate: {intName: true, realName: true, wordName: true},
}

// checkNumericOperators checks that every application of an
// overloaded numeric operator has a type in the operator's
// overload class, after resolving: the check is by name (a
// rebinding is still checked), the
// outermost bad application reports first, '~' reports its
// operand's span, and a type that is still a variable passes.
func (r *typeResolver) checkNumericOperators(m *TypeMap) error {
	for _, call := range r.numericCalls {
		t, err := m.TypeOf(call.apply)
		if err != nil {
			return err
		}
		if _, isVar := t.(*types.Var); isVar {
			continue
		}
		if numericOpDomain[call.name][t.String()] {
			continue
		}
		span := call.apply.Span()
		if call.name == opNegate {
			span = call.apply.Arg.Span()
		}
		return &Error{
			Span: span,
			Msg: "operator '" +
				strings.TrimPrefix(call.name, "op ") +
				"' is not defined for type '" +
				t.String() + "'",
		}
	}
	return nil
}

// checkOrdinalOrder checks every use of "ordinal" now that
// unification has resolved the orderedness of its enclosing step.
// "ordinal" numbers rows by their position, so it is meaningful
// only in an ordered step (a list); using it where the step is
// unordered (a bag) is an error.
func (r *typeResolver) checkOrdinalOrder(
	subst *unify.Substitution,
) error {
	for _, use := range r.ordinalUses {
		if !isOrderedAtom(subst.Resolve(use.ord)) {
			return &Error{
				Span: use.span,
				Msg:  "cannot use 'ordinal' in unordered query",
			}
		}
	}
	return nil
}

// patTerm records that a declaration binds a name to a term; the
// caller adds the name to the environment.
type patTerm struct {
	name string
	term unify.Term
	kind ptKind
}

// ptKind distinguishes an ordinary binding from an "over"
// declaration and a "val inst" instance.
type ptKind int

const (
	ptVal ptKind = iota
	ptOver
	ptInst
)

// typeResolver assigns a unification variable to every AST node,
// generates term equivalences from the structure of the tree,
// and hands them to the unifier.
type typeResolver struct {
	sys           *types.System
	u             *unify.Unifier
	bindings      map[string]*Binding
	pairs         []unify.TermPair
	nodeTerm      map[ast.Node]unify.Term
	actions       []unify.VarAction
	constraints   []unify.Constraint
	preferred     []preferredType
	numericCalls  []numericCall
	selectorCalls []selectorCall
	tyVarScopes   []map[string]*unify.Var
	// pendingSafe holds safe selectors whose result could not be
	// deduced when their receiver's variable first resolved (an
	// inner layer was still open); after unification they are
	// retried against the full substitution.
	pendingSafe []pendingSafeCall
	// computeFrames tracks the enclosing compute clauses: "over"
	// aggregates and "elements" are valid only inside one, and
	// aggregate over the innermost frame's pre-group rows.
	computeFrames []*computeFrame
	// stepOrd is the orderedness of the query step currently being
	// typed, captured when an "ordinal" is used so its legality can
	// be checked once unification has resolved that orderedness.
	stepOrd unify.Term
	// ordinalUses records each "ordinal" reference and the
	// orderedness of the step it appears in; "ordinal" is positional,
	// so it is only valid where that orderedness is a list.
	ordinalUses []ordinalUse
}

// ordinalUse is one reference to "ordinal": the orderedness of the
// enclosing query step and the position of the keyword, checked
// after unification.
type ordinalUse struct {
	ord  unify.Term
	span token.Span
}

// computeFrame is one enclosing compute clause: the pre-group
// element type and orderedness its aggregates range over, and
// whether an "over" argument is being typed (over nests no
// further).
type computeFrame struct {
	elem       unify.Term
	ord        unify.Term
	argEnv     typeEnv
	overActive bool
}

// deduceOver types an aggregate "fn over arg": the argument is
// typed per pre-group row (keys are not in scope inside it), and
// the function maps a collection of argument values to the
// result, the collection's kind linked to the input's orderedness
// by the function's kind.
func (r *typeResolver) deduceOver(env typeEnv, call *ast.InfixCall,
	v *unify.Var,
) error {
	if len(r.computeFrames) == 0 {
		return &Error{
			Span: call.Span(),
			Msg:  "'over' is only valid in 'compute'",
		}
	}
	frame := r.computeFrames[len(r.computeFrames)-1]
	if frame.overActive {
		return &Error{
			Span: call.Span(),
			Msg:  "'over' is not valid in 'over'",
		}
	}
	frame.overActive = true
	defer func() { frame.overActive = false }()
	vArg := r.u.Variable()
	err := r.deduceExp(frame.argEnv, call.A1, vArg)
	if err != nil {
		return err
	}
	vAgg := r.u.Variable()
	err = r.deduceExp(env, call.A0, vAgg)
	if err != nil {
		return err
	}
	cArg := r.u.Variable()
	r.equiv(vAgg, r.fnTerm(cArg, v))
	r.linkAggOrderedness(call.A0, cArg, vArg, frame.ord)
	r.reg(call, v)
	return nil
}

// preferredType records that, if unification leaves v
// undetermined, it should be unified with a primitive type and
// unification retried; this resolves "1 + 2" to int.
type preferredType struct {
	v    *unify.Var
	prim string
}

// equiv declares that a term is equivalent to a variable.
func (r *typeResolver) equiv(v *unify.Var, t unify.Term) {
	if unify.Term(v) != t {
		r.pairs = append(r.pairs,
			unify.TermPair{Left: t, Right: v})
	}
}

// reg registers that a node's type is a variable.
func (r *typeResolver) reg(node ast.Node, v *unify.Var) {
	r.nodeTerm[node] = v
}

// reg2 registers that a node's type is a term.
func (r *typeResolver) reg2(node ast.Node, t unify.Term) {
	r.nodeTerm[node] = t
}

// regEquiv registers that a node's type is a term, equivalent to
// the variable.
func (r *typeResolver) regEquiv(node ast.Node, v *unify.Var,
	t unify.Term,
) {
	r.equiv(v, t)
	r.nodeTerm[node] = t
}

func (r *typeResolver) fnTerm(param, result unify.Term) unify.Term {
	return unify.Apply(fnTyCon, param, result)
}

func (r *typeResolver) primTerm(name string) unify.Term {
	return r.u.Atom(name)
}

// collectionTerm is "collection(elem, orderedness)".
func (r *typeResolver) collectionTerm(elem, ord unify.Term,
) unify.Term {
	return unify.Apply(collectionTyCon, elem, ord)
}

// listTerm is a collection of elem that is ordered (a list).
func (r *typeResolver) listTerm(elem unify.Term) unify.Term {
	return r.collectionTerm(elem, r.u.Atom(orderedName))
}

// bagTerm is a collection of elem that is unordered (a bag).
func (r *typeResolver) bagTerm(elem unify.Term) unify.Term {
	return r.collectionTerm(elem, r.u.Atom(unorderedName))
}

// isOrderedAtom reports whether a term is the concrete "ordered"
// atom — the orderedness of a list. A free variable or the
// "unordered" atom is not, and reads back as a bag.
func isOrderedAtom(t unify.Term) bool {
	s, ok := t.(*unify.Sequence)
	return ok && s.Op == orderedName && len(s.Terms) == 0
}

// meetOrderedness constrains o to be the meet of o0 and o1:
// ordered if both are ordered, otherwise unordered. It is a
// constraint whose four candidates prune as o0 and o1 resolve;
// when one remains, it equates o with the result.
func (r *typeResolver) meetOrderedness(o, o0, o1 *unify.Var) {
	ordered := r.u.Atom(orderedName)
	unordered := r.u.Atom(unorderedName)
	arg := r.u.Variable()
	r.equiv(arg, unify.Apply(argTyCon, o0, o1))
	candidate := func(a0, a1, result unify.Term) unify.Candidate {
		return unify.Candidate{
			Term:   unify.Apply(argTyCon, a0, a1),
			Action: unify.Equiv(o, result),
		}
	}
	r.constraints = append(r.constraints, unify.Constraint{
		Arg: arg,
		Candidates: []unify.Candidate{
			candidate(ordered, ordered, ordered),
			candidate(ordered, unordered, unordered),
			candidate(unordered, ordered, unordered),
			candidate(unordered, unordered, unordered),
		},
	})
}

// deduceRaise types "raise e": the argument is an exn; the
// result is free, since a raise never returns.
func (r *typeResolver) deduceRaise(env typeEnv, e *ast.Raise,
	v *unify.Var,
) error {
	vExn := r.u.Variable()
	err := r.deduceExp(env, e.E, vExn)
	if err != nil {
		return err
	}
	r.equiv(vExn, unify.Apply(exnTypeName))
	r.reg(e, v)
	return nil
}

// deduceElements types the "elements" keyword: the enclosing
// compute clause's pre-group rows as a collection.
func (r *typeResolver) deduceElements(e *ast.Elements, v *unify.Var,
) error {
	if len(r.computeFrames) == 0 {
		return &Error{
			Span: e.Span(),
			Msg:  "'elements' is only valid in a 'compute' clause",
		}
	}
	frame := r.computeFrames[len(r.computeFrames)-1]
	r.regEquiv(e, v, r.collectionTerm(frame.elem, frame.ord))
	return nil
}

// ordSubstKey is the subst slot (below any real type-variable
// ordinal) holding an instantiation's shared collection
// orderedness.
const ordSubstKey = -1

// typeTerm converts a type to a term. Type variables become
// unification variables via subst, fresh at their first
// occurrence, so each conversion instantiates a polymorphic type.
func (r *typeResolver) typeTerm(t types.Type,
	subst map[int]*unify.Var,
) unify.Term {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Collection:
		// A collection has free orderedness, so it unifies with a
		// list or a bag; fresh per instantiation, but shared by
		// every collection in one type, so a member such as
		// Relational.iterate is a list function on lists and a bag
		// function on bags.
		ord, ok := subst[ordSubstKey]
		if !ok {
			ord = r.u.Variable()
			subst[ordSubstKey] = ord
		}
		return r.collectionTerm(r.typeTerm(t.Elem, subst), ord)
	case *types.Fn:
		return r.fnTerm(r.typeTerm(t.Param, subst),
			r.typeTerm(t.Result, subst))
	case *types.List:
		return r.listTerm(r.typeTerm(t.Elem, subst))
	case *types.Named:
		if t.Name == bagTyCon && len(t.Args) == 1 {
			return r.bagTerm(r.typeTerm(t.Args[0], subst))
		}
		terms := make([]unify.Term, len(t.Args))
		for i, arg := range t.Args {
			terms[i] = r.typeTerm(arg, subst)
		}
		return unify.Apply(t.Name, terms...)
	case *types.Primitive:
		return r.u.Atom(t.String())
	case *types.Record:
		labels := make([]string, len(t.Fields))
		terms := make([]unify.Term, len(t.Fields))
		for i, f := range t.Fields {
			labels[i] = f.Label
			terms[i] = r.typeTerm(f.Type, subst)
		}
		return unify.Apply(recordLabel(labels), terms...)
	case *types.Tuple:
		terms := make([]unify.Term, len(t.Args))
		for i, arg := range t.Args {
			terms[i] = r.typeTerm(arg, subst)
		}
		return unify.Apply(tupleTyCon, terms...)
	case *types.Var:
		v, ok := subst[t.Ordinal]
		if !ok {
			v = r.u.Variable()
			subst[t.Ordinal] = v
		}
		return v
	default:
		panic("cannot convert type " + t.String())
	}
}

// astTypeTerm converts a type annotation to a term. A type
// variable resolves in the innermost annotation scope, created
// if absent, so annotations within one declaration share their
// type variables.
func (r *typeResolver) astTypeTerm(t ast.Type) (unify.Term,
	error,
) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *ast.ExpressionType:
		// "typeof exp": the annotation's type is the deduced type
		// of exp, resolved against the top-level bindings.
		var env typeEnv = emptyTypeEnv{}
		if r.bindings != nil {
			env = &bindingTypeEnv{parent: env, bindings: r.bindings}
		}
		v := r.u.Variable()
		err := r.deduceExp(env, t.Exp, v)
		if err != nil {
			return nil, err
		}
		return v, nil
	case *ast.FnType:
		param, err := r.astTypeTerm(t.Param)
		if err != nil {
			return nil, err
		}
		result, err := r.astTypeTerm(t.Result)
		if err != nil {
			return nil, err
		}
		return r.fnTerm(param, result), nil
	case *ast.NamedType:
		return r.astNamedTerm(t)
	case *ast.RecordType:
		fields := make([]labelTerm, len(t.Fields))
		for i, f := range t.Fields {
			term, err := r.astTypeTerm(f.Type)
			if err != nil {
				return nil, err
			}
			fields[i] = labelTerm{label: f.Label, term: term}
		}
		sortFields(fields)
		return r.recordTerm(fields), nil
	case *ast.TupleType:
		terms := make([]unify.Term, len(t.Args))
		for i, arg := range t.Args {
			term, err := r.astTypeTerm(arg)
			if err != nil {
				return nil, err
			}
			terms[i] = term
		}
		return r.tupleTerm(terms), nil
	case *ast.TyVar:
		if len(r.tyVarScopes) == 0 {
			return r.u.Variable(), nil
		}
		scope := r.tyVarScopes[len(r.tyVarScopes)-1]
		tv, ok := scope[t.Name]
		if !ok {
			tv = r.u.Variable()
			scope[t.Name] = tv
		}
		return tv, nil
	default:
		return nil, &Error{
			Span: t.Span(),
			Msg: "cannot deduce type for annotation " +
				t.Op().String(),
		}
	}
}

// astNamedTerm converts a named type annotation: a primitive, a
// list, or an instance of a datatype.
func (r *typeResolver) astNamedTerm(t *ast.NamedType) (unify.Term,
	error,
) {
	if alias, ok := r.sys.LookupAlias(t.Name); ok &&
		len(alias.TyVars) == len(t.Args) {
		subst := make(map[string]ast.Type, len(alias.TyVars))
		for i, tv := range alias.TyVars {
			subst[tv] = t.Args[i]
		}
		return r.astTypeTerm(ast.SubstituteType(alias.Body, subst))
	}
	terms := make([]unify.Term, len(t.Args))
	for i, arg := range t.Args {
		term, err := r.astTypeTerm(arg)
		if err != nil {
			return nil, err
		}
		terms[i] = term
	}
	if t.Name == listTyCon && len(terms) == 1 {
		return r.listTerm(terms[0]), nil
	}
	if t.Name == bagTyCon && len(terms) == 1 {
		return r.bagTerm(terms[0]), nil
	}
	if internal, arity, ok := r.sys.DatatypeInternal(t.Name); ok &&
		arity == len(terms) {
		return unify.Apply(internal, terms...), nil
	}
	if len(terms) == 0 && r.sys.Lookup(t.Name) != nil {
		return r.u.Atom(t.Name), nil
	}
	// A known type constructor given the wrong number of arguments
	// (e.g. "int pair" for a two-parameter alias) is an arity error,
	// not an unbound constructor.
	if expected, ok := r.typeConstructorArity(t.Name); ok {
		return nil, &Error{
			Span: t.Span(),
			Msg: "type constructor " + t.Name + " given " +
				strconv.Itoa(len(terms)) + " argument" +
				plural(len(terms)) + ", wants " +
				strconv.Itoa(expected),
		}
	}
	return nil, &Error{
		Span: t.Span(),
		Msg:  "unbound type constructor: " + t.Name,
	}
}

// typeConstructorArity returns the number of type arguments a named
// type constructor expects, and whether the name is a known
// constructor (an alias, datatype, the list or bag, or a nullary
// type).
func (r *typeResolver) typeConstructorArity(name string) (int, bool) {
	if alias, ok := r.sys.LookupAlias(name); ok {
		return len(alias.TyVars), true
	}
	if arity, ok := r.sys.DatatypeArity(name); ok {
		return arity, true
	}
	if name == listTyCon || name == bagTyCon {
		return 1, true
	}
	if r.sys.Lookup(name) != nil {
		return 0, true
	}
	return 0, false
}

// plural is "s" unless n is 1, for pluralizing a count in a message.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (r *typeResolver) deduceDecl(env typeEnv, decl ast.Decl,
	termMap *[]patTerm,
) (ast.Decl, error) {
	// lint: sort until '^	}' where '^	case '
	switch d := decl.(type) {
	case *ast.DatatypeDecl:
		return r.deduceDatatypeDecl(d, termMap)
	case *ast.FunDecl:
		return r.deduceValDecl(env, funToVal(d), termMap)
	case *ast.OverDecl:
		*termMap = append(*termMap,
			patTerm{name: d.Pat.Name, kind: ptOver})
		r.nodeTerm[decl] = r.primTerm("unit")
		return decl, nil
	case *ast.TypeDecl:
		return r.deduceTypeDecl(d)
	case *ast.ValDecl:
		return r.deduceValDecl(env, d, termMap)
	default:
		return nil, &Error{
			Span: decl.Span(),
			Msg: "cannot deduce type for " +
				decl.Op().String(),
		}
	}
}

func (r *typeResolver) deduceValDecl(env typeEnv,
	decl *ast.ValDecl, termMap *[]patTerm,
) (ast.Decl, error) {
	// "val inst foo = e" records e as an instance of the overloaded
	// name foo, rather than binding foo as an ordinary value.
	if decl.Inst {
		for _, b := range decl.Binds {
			idPat, ok := b.Pat.(*ast.IDPat)
			if !ok {
				return nil, &Error{
					Span: b.Span(),
					Msg:  "cannot convert to core: val inst",
				}
			}
			vPat := r.u.Variable()
			err := r.deduceValBind(env, b, &[]patTerm{}, vPat)
			if err != nil {
				return nil, err
			}
			*termMap = append(*termMap, patTerm{
				name: idPat.Name, term: vPat, kind: ptInst,
			})
		}
		r.nodeTerm[decl] = r.primTerm("unit")
		return decl, nil
	}
	// If recursive, bind each name (presumably a function) to
	// its type variable before deducing the expressions' types.
	env2 := env
	vPats := make([]*unify.Var, len(decl.Binds))
	for i, b := range decl.Binds {
		vPats[i] = r.u.Variable()
		if decl.Rec {
			if idPat, ok := b.Pat.(*ast.IDPat); ok {
				env2 = bind(env2, idPat.Name, vPats[i])
			}
		}
	}
	for i, b := range decl.Binds {
		err := r.deduceValBind(env2, b, termMap, vPats[i])
		if err != nil {
			return nil, err
		}
	}
	r.nodeTerm[decl] = r.primTerm("unit")
	return decl, nil
}

func (r *typeResolver) deduceValBind(env typeEnv,
	bind *ast.ValBind, termMap *[]patTerm, vPat *unify.Var,
) error {
	// Type variables in this binding's annotations share one
	// scope, so in "fun f (x: 'a) (y: 'a) = ..." both 'a are the
	// same type.
	r.tyVarScopes = append(r.tyVarScopes,
		map[string]*unify.Var{})
	defer func() {
		r.tyVarScopes = r.tyVarScopes[:len(r.tyVarScopes)-1]
	}()
	err := r.deducePat(bind.Pat, termMap, nil, vPat)
	if err != nil {
		return err
	}
	err = r.deduceExp(env, bind.Exp, vPat)
	if err != nil {
		return err
	}
	r.nodeTerm[bind] = r.primTerm("unit")
	return nil
}

// deduceConPat handles the application of a constructor to a
// pattern, e.g. "SOME x". The constructor's argument and result
// types share one instantiation, so "SOME x" has type
// "'a option" where "x" has type "'a".
func (r *typeResolver) deduceConPat(pat *ast.ConPat,
	termMap *[]patTerm, v *unify.Var,
) error {
	tc, ok := r.sys.LookupTyCon(pat.Name)
	if !ok || tc.Arg == nil {
		return &Error{
			Span: pat.Span(),
			Msg: "unbound constructor: " +
				pat.Name,
		}
	}
	vArg := r.u.Variable()
	err := r.deducePat(pat.Arg, termMap, nil, vArg)
	if err != nil {
		return err
	}
	subst := map[int]*unify.Var{}
	r.equiv(vArg, r.typeTerm(tc.Arg, subst))
	r.regEquiv(pat, v, r.typeTerm(tc.Result, subst))
	return nil
}

// deduceDatatypeDecl registers the declared datatypes and their
// constructors, and binds each constructor as a value. The
// datatypes are registered before any constructor argument type
// is converted, so constructors may refer to their own datatype
// (or a sibling's) recursively.
func (r *typeResolver) deduceDatatypeDecl(decl *ast.DatatypeDecl,
	termMap *[]patTerm,
) (ast.Decl, error) {
	internals := make([]string, len(decl.Binds))
	for i, b := range decl.Binds {
		internals[i] = r.sys.DeclareDatatype(b.Name, len(b.TyVars))
	}
	for bi, b := range decl.Binds {
		args := make([]types.Type, len(b.TyVars))
		tyVars := map[string]int{}
		for i, tv := range b.TyVars {
			args[i] = r.sys.Var(i)
			tyVars[tv] = i
		}
		result := r.sys.Named(internals[bi], args...)
		for _, c := range b.Cons {
			var argType types.Type
			if c.Of != nil {
				// A type variable in a constructor's argument must be
				// bound by the datatype's head.
				if tv := unboundTyVar(c.Of, tyVars); tv != nil {
					return nil, &Error{
						Span: tv.Span(),
						Msg: "unbound type variable in type " +
							"declaration: " + tv.Name,
					}
				}
				t, err := r.sys.FromAST(c.Of, tyVars)
				if err != nil {
					return nil, &Error{
						Span: decl.Span(),
						Msg:  err.Error(),
					}
				}
				argType = t
			}
			r.sys.DeclareTyCon(c.Name, argType, result)
			conType := result
			if argType != nil {
				conType = r.sys.Fn(argType, result)
			}
			*termMap = append(*termMap, patTerm{
				name: c.Name,
				term: r.typeTerm(conType, map[int]*unify.Var{}),
			})
		}
	}
	r.nodeTerm[decl] = r.primTerm("unit")
	return decl, nil
}

// deduceTypeDecl registers each type alias so later types can use
// it. An alias is transparent: it is expanded wherever it appears.
//
// A "type" declaration is not recursive, and the bindings of a
// "type ... and ..." group are simultaneous, so every body is
// resolved against the environment in effect before the
// declaration; only then are the new aliases registered. A name in
// a body therefore means an existing definition -- a prior alias is
// expanded, and a name that is not already a type (including the one
// being declared) is an "unbound type constructor" error, as in
// Standard ML. The resolved body replaces the written one, so the
// shell echoes the expanded form.
func (r *typeResolver) deduceTypeDecl(decl *ast.TypeDecl,
) (ast.Decl, error) {
	resolved := make([]ast.Type, len(decl.Binds))
	for i, b := range decl.Binds {
		body, err := r.resolveTypeBody(b.Type)
		if err != nil {
			return nil, err
		}
		resolved[i] = body
	}
	for i := range decl.Binds {
		decl.Binds[i].Type = resolved[i]
		r.sys.DeclareAlias(decl.Binds[i].Name, decl.Binds[i].TyVars,
			resolved[i])
	}
	r.nodeTerm[decl] = r.primTerm("unit")
	return decl, nil
}

// resolveTypeBody resolves a type-alias body against the current
// environment (the definitions before the declaration), expanding
// prior aliases and rejecting any name that is not already a type.
// The alias's own type variables pass through unchanged.
func (r *typeResolver) resolveTypeBody(t ast.Type) (ast.Type, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *ast.FnType:
		param, err := r.resolveTypeBody(n.Param)
		if err != nil {
			return nil, err
		}
		result, err := r.resolveTypeBody(n.Result)
		if err != nil {
			return nil, err
		}
		return ast.NewFnType(n.Span(), param, result), nil
	case *ast.NamedType:
		return r.resolveNamedType(n)
	case *ast.RecordType:
		fields := make([]ast.TypeField, len(n.Fields))
		for i, f := range n.Fields {
			ft, err := r.resolveTypeBody(f.Type)
			if err != nil {
				return nil, err
			}
			fields[i] = ast.TypeField{Label: f.Label, Type: ft}
		}
		return ast.NewRecordType(n.Span(), fields), nil
	case *ast.TupleType:
		args, err := r.resolveTypeArgs(n.Args)
		if err != nil {
			return nil, err
		}
		return ast.NewTupleType(n.Span(), args), nil
	default:
		return t, nil
	}
}

// resolveNamedType resolves a named type in an alias body: it
// expands a prior alias, keeps a datatype, list, bag, or nullary
// type (with resolved arguments), and rejects an unknown or
// wrong-arity constructor.
func (r *typeResolver) resolveNamedType(n *ast.NamedType) (ast.Type,
	error,
) {
	args, err := r.resolveTypeArgs(n.Args)
	if err != nil {
		return nil, err
	}
	if alias, ok := r.sys.LookupAlias(n.Name); ok &&
		len(alias.TyVars) == len(args) {
		subst := make(map[string]ast.Type, len(alias.TyVars))
		for i, tv := range alias.TyVars {
			subst[tv] = args[i]
		}
		return ast.SubstituteType(alias.Body, subst), nil
	}
	known := false
	if arity, ok := r.sys.DatatypeArity(n.Name); ok && arity == len(args) {
		known = true
	} else if (n.Name == listTyCon || n.Name == bagTyCon) &&
		len(args) == 1 {
		known = true
	} else if len(args) == 0 && r.sys.Lookup(n.Name) != nil {
		known = true
	}
	if known {
		return ast.NewNamedType(n.Span(), n.Name, args), nil
	}
	if expected, ok := r.typeConstructorArity(n.Name); ok {
		return nil, &Error{
			Span: n.Span(),
			Msg: "type constructor " + n.Name + " given " +
				strconv.Itoa(len(args)) + " argument" +
				plural(len(args)) + ", wants " +
				strconv.Itoa(expected),
		}
	}
	return nil, &Error{
		Span: n.Span(),
		Msg:  "unbound type constructor: " + n.Name,
	}
}

// resolveTypeArgs resolves each type argument of a named or
// composite type against the environment before the declaration.
func (r *typeResolver) resolveTypeArgs(args []ast.Type) ([]ast.Type,
	error,
) {
	out := make([]ast.Type, len(args))
	for i, a := range args {
		ra, err := r.resolveTypeBody(a)
		if err != nil {
			return nil, err
		}
		out[i] = ra
	}
	return out, nil
}

// unboundTyVar returns the first type variable in t that is not in
// the bound set, or nil if every variable is bound. It is used to
// reject a datatype constructor whose argument mentions a type
// variable the datatype's head does not declare.
func unboundTyVar(t ast.Type, bound map[string]int) *ast.TyVar {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *ast.FnType:
		if tv := unboundTyVar(n.Param, bound); tv != nil {
			return tv
		}
		return unboundTyVar(n.Result, bound)
	case *ast.NamedType:
		for _, a := range n.Args {
			if tv := unboundTyVar(a, bound); tv != nil {
				return tv
			}
		}
	case *ast.RecordType:
		for _, f := range n.Fields {
			if tv := unboundTyVar(f.Type, bound); tv != nil {
				return tv
			}
		}
	case *ast.TupleType:
		for _, a := range n.Args {
			if tv := unboundTyVar(a, bound); tv != nil {
				return tv
			}
		}
	case *ast.TyVar:
		if _, ok := bound[n.Name]; !ok {
			return n
		}
	}
	return nil
}

func (r *typeResolver) deducePat(pat ast.Pat,
	termMap *[]patTerm, labelNames []string, v *unify.Var,
) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch p := pat.(type) {
	case *ast.AnnotatedPat:
		term, err := r.astTypeTerm(p.Type)
		if err != nil {
			return err
		}
		r.equiv(v, term)
		err = r.deducePat(p.Pat, termMap, nil, v)
		if err != nil {
			return err
		}
		r.reg(pat, v)
		return nil
	case *ast.AsPat:
		// "name as p": the name and the sub-pattern both have the
		// matched value's type.
		*termMap = append(*termMap, patTerm{name: p.Name, term: v})
		err := r.deducePat(p.Pat, termMap, nil, v)
		if err != nil {
			return err
		}
		r.reg(pat, v)
		return nil
	case *ast.ConPat:
		return r.deduceConPat(p, termMap, v)
	case *ast.ConsPat:
		vElem := r.u.Variable()
		vList := r.u.Variable()
		err := r.deducePat(p.A0, termMap, nil, vElem)
		if err != nil {
			return err
		}
		err = r.deducePat(p.A1, termMap, nil, vList)
		if err != nil {
			return err
		}
		lt := r.listTerm(vElem)
		r.equiv(vList, lt)
		r.regEquiv(pat, v, lt)
		return nil
	case *ast.IDPat:
		if tc, ok := r.sys.LookupTyCon(p.Name); ok {
			// A constant constructor, e.g. NONE; it binds
			// nothing.
			r.regEquiv(pat, v,
				r.typeTerm(tc.Result, map[int]*unify.Var{}))
			return nil
		}
		*termMap = append(*termMap,
			patTerm{name: p.Name, term: v})
		r.reg(pat, v)
		return nil
	case *ast.ListPat:
		vElem := r.u.Variable()
		for _, arg := range p.Args {
			err := r.deducePat(arg, termMap, nil, vElem)
			if err != nil {
				return err
			}
		}
		r.regEquiv(pat, v, r.listTerm(vElem))
		return nil
	case *ast.LiteralPat:
		return r.deduceLiteral(pat, p.Kind, p.Value, v)
	case *ast.RecordPat:
		return r.deduceRecordPat(p, termMap, labelNames, v)
	case *ast.TuplePat:
		terms := make([]unify.Term, len(p.Args))
		for i, arg := range p.Args {
			vArg := r.u.Variable()
			err := r.deducePat(arg, termMap, nil, vArg)
			if err != nil {
				return err
			}
			terms[i] = vArg
		}
		r.regEquiv(pat, v, r.tupleTerm(terms))
		return nil
	case *ast.WildcardPat:
		r.reg(pat, v)
		return nil
	default:
		return &Error{
			Span: pat.Span(),
			Msg: "cannot deduce type for pattern " +
				pat.Op().String(),
		}
	}
}

func bindAll(env typeEnv, termMap []patTerm) typeEnv {
	for _, pt := range termMap {
		switch pt.kind {
		case ptOver:
			env = &overTypeEnv{parent: env, name: pt.name}
		case ptInst:
			//nolint:forcetypeassert // an instance term is a variable
			env = &instTypeEnv{
				parent: env, name: pt.name, v: pt.term.(*unify.Var),
			}
		default:
			env = bind(env, pt.name, pt.term)
		}
	}
	return env
}

func (r *typeResolver) deduceExp(env typeEnv, exp ast.Expr,
	v *unify.Var,
) error {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := exp.(type) {
	case *ast.AnnotatedExp:
		term, err := r.astTypeTerm(e.Type)
		if err != nil {
			return err
		}
		r.equiv(v, term)
		err = r.deduceExp(env, e.Exp, v)
		if err != nil {
			return err
		}
		r.reg(exp, v)
		return nil
	case *ast.Apply:
		return r.deduceApply(env, e, v)
	case *ast.Case:
		return r.deduceCase(env, e, v)
	case *ast.Elements:
		return r.deduceElements(e, v)
	case *ast.Fn:
		vResult := r.u.Variable()
		for _, m := range e.Matches {
			err := r.deduceMatch(env, m, v, vResult)
			if err != nil {
				return err
			}
		}
		r.reg(exp, v)
		return nil
	case *ast.From:
		return r.deduceFrom(env, e, v)
	case *ast.ID:
		return r.deduceID(env, e, v)
	case *ast.If:
		return r.deduceIf(env, e, v)
	case *ast.InfixCall:
		return r.deduceInfix(env, e, v)
	case *ast.Let:
		env2 := env
		for i, d := range e.Decls {
			var termMap []patTerm
			d2, err := r.deduceDecl(env2, d, &termMap)
			if err != nil {
				return err
			}
			e.Decls[i] = d2
			env2 = bindAll(env2, termMap)
		}
		err := r.deduceExp(env2, e.Exp, v)
		if err != nil {
			return err
		}
		r.reg(exp, v)
		return nil
	case *ast.ListExp:
		vElem := r.u.Variable()
		for _, arg := range e.Args {
			err := r.deduceExp(env, arg, vElem)
			if err != nil {
				return err
			}
		}
		r.regEquiv(exp, v, r.listTerm(vElem))
		return nil
	case *ast.Literal:
		return r.deduceLiteral(exp, e.Kind, e.Value, v)
	case *ast.PrefixCall:
		return r.deduceOpCall(env, opNegate, e,
			[]ast.Expr{e.A}, v)
	case *ast.Raise:
		return r.deduceRaise(env, e, v)
	case *ast.RangeList:
		return r.deduceRangeList(env, e, v)
	case *ast.Record:
		return r.deduceRecord(env, e, v)
	case *ast.RecordSelector:
		return &Error{
			Span: e.Span(),
			Msg: "unresolved flex record (can't tell what " +
				"fields there are besides #" + e.Name + ")",
		}
	case *ast.Tuple:
		terms := make([]unify.Term, len(e.Args))
		for i, arg := range e.Args {
			vArg := r.u.Variable()
			err := r.deduceExp(env, arg, vArg)
			if err != nil {
				return err
			}
			terms[i] = vArg
		}
		r.regEquiv(exp, v, r.tupleTerm(terms))
		return nil
	case *ast.TypeStringExp:
		// "type_string e" is a string; its operand is typed so that
		// the resolver can render the operand's type.
		vOperand := r.u.Variable()
		err := r.deduceExp(env, e.Exp, vOperand)
		if err != nil {
			return err
		}
		r.regEquiv(exp, v, r.primTerm(stringName))
		return nil
	default:
		return &Error{
			Span: exp.Span(),
			Msg: "cannot deduce type for " +
				exp.Op().String(),
		}
	}
}

// deduceID types a variable reference. A datatype constructor is
// usable as a value even if nothing bound it (e.g. it was
// declared by an earlier statement's datatype declaration).
func (r *typeResolver) deduceID(env typeEnv, id *ast.ID,
	v *unify.Var,
) error {
	term, ok := env.get(r, id.Name)
	if ok && id.Name == ordinalName && r.stepOrd != nil {
		// "ordinal" is positional, so it is only valid in an ordered
		// step. The step's orderedness is not yet resolved, so defer
		// the check until unification has run.
		r.ordinalUses = append(r.ordinalUses,
			ordinalUse{ord: r.stepOrd, span: id.Span()})
	}
	if !ok {
		tc, isCon := r.sys.LookupTyCon(id.Name)
		if !isCon {
			if id.Name == currentName || id.Name == ordinalName {
				return &Error{
					Span: id.Span(),
					Msg: "'" + id.Name +
						"' is only valid in a query",
				}
			}
			return &Error{
				Span: id.Span(),
				Msg: "unbound variable or constructor: " +
					id.Name,
			}
		}
		conType := tc.Result
		if tc.Arg != nil {
			conType = r.sys.Fn(tc.Arg, tc.Result)
		}
		term = r.typeTerm(conType, map[int]*unify.Var{})
	}
	// An operator used as a bare value ("op +", or passed to a
	// fold) is never applied here, so nothing forces its numeric
	// type variable; register its preferred default (int for "+",
	// real for "/") so an otherwise-undetermined "op +" prints as
	// "int * int -> int".
	if b, isBuiltin := topBuiltins[id.Name]; isBuiltin &&
		b.preferred != "" {
		for _, pv := range termVars(term) {
			r.preferred = append(r.preferred,
				preferredType{v: pv, prim: b.preferred})
		}
	}
	r.regEquiv(id, v, term)
	return nil
}

// termVars returns the distinct variables in a term, in the order
// they first appear.
func termVars(t unify.Term) []*unify.Var {
	var out []*unify.Var
	seen := map[*unify.Var]bool{}
	var walk func(unify.Term)
	walk = func(t unify.Term) {
		switch t := t.(type) {
		case *unify.Var:
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		case *unify.Sequence:
			for _, s := range t.Terms {
				walk(s)
			}
		}
	}
	walk(t)
	return out
}

// deduceLiteral handles literal expressions and literal patterns,
// whose types depend only on the literal kind.
func (r *typeResolver) deduceLiteral(node ast.Node, kind ast.Op,
	value string, v *unify.Var,
) error {
	var name string
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ast.BoolLiteralOp:
		name = boolName
	case ast.CharLiteralOp, ast.CharLiteralPatOp:
		err := checkCharLiteral(node, value)
		if err != nil {
			return err
		}
		name = "char"
	case ast.IntLiteralOp, ast.IntLiteralPatOp:
		err := checkIntRange(node, value)
		if err != nil {
			return err
		}
		name = intName
	case ast.RealLiteralOp, ast.RealLiteralPatOp:
		name = realName
	case ast.StringLiteralOp, ast.StringLiteralPatOp:
		name = "string"
	case ast.UnitLiteralOp:
		name = "unit"
	case ast.WordLiteralOp, ast.WordLiteralPatOp:
		name = "word"
	default:
		return &Error{
			Span: node.Span(),
			Msg: "cannot deduce type for literal " +
				kind.String(),
		}
	}
	r.regEquiv(node, v, r.primTerm(name))
	return nil
}

// checkCharLiteral rejects a character constant that does not
// contain exactly one character. The parser stores the constant's
// unquoted content (empty for #"", "ab" for #"ab"); a valid
// constant is exactly one character. The message is Standard ML's
// wording. The check runs here, at type resolution, so an invalid
// constant is reported as a positioned error before core building
// would try to read its first rune, whether the constant is used
// as an expression or a pattern.
func checkCharLiteral(node ast.Node, value string) error {
	if utf8.RuneCountInString(value) != 1 {
		return &Error{
			Span: node.Span(),
			Msg:  "character constant not length one",
		}
	}
	return nil
}

// checkIntRange rejects an int literal that does not fit in a
// signed 32-bit integer.
func checkIntRange(node ast.Node, value string) error {
	text := strings.ReplaceAll(value, "~", "-")
	_, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		return &Error{
			Span: node.Span(),
			Msg: "literal '" + value +
				"' is too large for type " + intName,
		}
	}
	return nil
}

func (r *typeResolver) deduceApply(env typeEnv, apply *ast.Apply,
	v *unify.Var,
) error {
	if id, ok := apply.Fn.(*ast.ID); ok {
		if insts := env.overloads(id.Name); insts != nil {
			return r.deduceOverloadApply(env, apply, insts, v)
		}
		if _, isNumeric := numericOpDomain[id.Name]; isNumeric {
			r.numericCalls = append(r.numericCalls,
				numericCall{name: id.Name, apply: apply})
		}
	}
	vFn := r.u.Variable()
	vArg := r.u.Variable()
	r.equiv(vFn, r.fnTerm(vArg, v))
	if sel, ok := apply.Arg.(*ast.RecordSelector); ok {
		// "apply" is "f #field": "#field" has type "vArg" and
		// also "vRec -> vField"; when vRec resolves we can
		// deduce vField.
		vRec := r.u.Variable()
		vField := r.u.Variable()
		r.selectorAction(sel, vRec, vField)
		r.regEquiv(apply.Arg, vArg, r.fnTerm(vRec, vField))
	} else {
		err := r.deduceExp(env, apply.Arg, vArg)
		if err != nil {
			return err
		}
	}
	if sel, ok := apply.Fn.(*ast.RecordSelector); ok {
		// "apply" is "#field arg": when vArg (the argument
		// type) resolves to a record, we can deduce v. A safe
		// selector ("arg?.field") instead tunnels through the
		// receiver's functor layers.
		if sel.Safe {
			r.safeSelectorAction(sel, vArg, v)
		} else {
			r.selectorAction(sel, vArg, v)
		}
		r.reg2(sel, r.fnTerm(vArg, v))
		r.selectorCalls = append(r.selectorCalls,
			selectorCall{sel: sel, apply: apply})
	} else {
		err := r.deduceExp(env, apply.Fn, vFn)
		if err != nil {
			return err
		}
	}
	if id, ok := apply.Fn.(*ast.ID); ok {
		if b, isBuiltin := topBuiltins[id.Name]; isBuiltin &&
			b.preferred != "" {
			r.preferred = append(r.preferred,
				preferredType{v: v, prim: b.preferred})
		}
	}
	r.reg(apply, v)
	return nil
}

// safeSelectorAction registers an action for a safe selector
// "arg?.field": when the receiver's type resolves, peel its
// functor layers, project the field, and re-wrap.
func (r *typeResolver) safeSelectorAction(sel *ast.RecordSelector,
	vArg, vResult *unify.Var,
) {
	fieldName := sel.Name
	r.pendingSafe = append(r.pendingSafe, pendingSafeCall{
		name: fieldName, vArg: vArg, vResult: vResult,
	})
	r.actions = append(r.actions, unify.VarAction{
		Var: vArg,
		Action: func(_ *unify.Var, t unify.Term,
			s *unify.Substitution, add func(l, r unify.Term),
		) {
			if res := safeFieldTerm(t, fieldName, s); res != nil {
				add(s.Resolve(vResult), res)
			}
		},
	})
}

// selectorAction registers an action for the record selector
// "#field": when the record type vArg becomes known, the
// selector's result type vResult is the field's type.
func (r *typeResolver) selectorAction(sel *ast.RecordSelector,
	vArg, vResult *unify.Var,
) {
	fieldName := sel.Name
	r.actions = append(r.actions, unify.VarAction{
		Var: vArg,
		Action: func(_ *unify.Var, t unify.Term,
			s *unify.Substitution, add func(l, r unify.Term),
		) {
			if fieldType := lookupField(t, fieldName, s); fieldType != nil {
				add(s.Resolve(vResult), fieldType)
			}
		},
	})
}

// deduceRecord handles a record expression, e.g. "{a=1, b=2}" or
// "{e with a=1}".
// deduceRecordFields types the fields of a record expression,
// returning them sorted by label with their deduced terms. It
// does not handle the "with" clause; the caller does.
// deduceRangeList types a range list: every bound unifies to one
// element type, and the result is a list of it.
func (r *typeResolver) deduceRangeList(env typeEnv,
	e *ast.RangeList, v *unify.Var,
) error {
	vElem := r.u.Variable()
	for _, item := range e.Items {
		for _, bound := range []ast.Expr{item.Lo, item.Hi} {
			if bound == nil {
				continue
			}
			err := r.deduceExp(env, bound, vElem)
			if err != nil {
				return err
			}
		}
	}
	r.regEquiv(e, v, r.listTerm(vElem))
	return nil
}

func (r *typeResolver) deduceRecordFields(env typeEnv,
	record *ast.Record,
) ([]labelTerm, error) {
	fields := make([]labelTerm, 0, len(record.Fields))
	byLabel := map[string]ast.Expr{}
	for _, f := range record.Fields {
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
		if _, dup := byLabel[label]; dup {
			span := f.LabelSpan
			if span == (token.Span{}) {
				span = f.Exp.Span()
			}
			return nil, &Error{
				Span: span,
				Msg: "duplicate field '" + label +
					"' in record",
			}
		}
		byLabel[label] = f.Exp
		fields = append(fields, labelTerm{label: label})
	}
	sortFields(fields)
	for i := range fields {
		vArg := r.u.Variable()
		err := r.deduceExp(env, byLabel[fields[i].label], vArg)
		if err != nil {
			return nil, err
		}
		fields[i].term = vArg
	}
	return fields, nil
}

func (r *typeResolver) deduceRecord(env typeEnv,
	record *ast.Record, v *unify.Var,
) error {
	fields, err := r.deduceRecordFields(env, record)
	if err != nil {
		return err
	}
	labelTypes := map[string]unify.Term{}
	for _, f := range fields {
		labelTypes[f.label] = f.term
	}
	if record.With == nil {
		r.regEquiv(record, v, r.recordTerm(fields))
		return nil
	}
	v2 := r.u.Variable()
	err = r.deduceExp(env, record.With, v2)
	if err != nil {
		return err
	}
	// When we know the type of the expression before 'with', we
	// can unify the types of the fields it has in common with
	// the explicit fields.
	r.actions = append(r.actions, unify.VarAction{
		Var: v2,
		Action: func(_ *unify.Var, t unify.Term,
			s *unify.Substitution, add func(l, r unify.Term),
		) {
			seq, ok := t.(*unify.Sequence)
			if !ok {
				return
			}
			for i, fieldName := range fieldList(seq) {
				if labelType, common := labelTypes[fieldName]; common {
					add(s.Resolve(labelType),
						s.Resolve(seq.Terms[i]))
				}
			}
		},
	})
	r.equiv(v, v2)
	r.reg(record, v)
	return nil
}

// deduceRecordPat handles a record pattern, e.g. "{a, b = p}" or
// "{a, ...}".
func (r *typeResolver) deduceRecordPat(pat *ast.RecordPat,
	termMap *[]patTerm, labelNames []string, v *unify.Var,
) error {
	byLabel := map[string]ast.Pat{}
	for _, f := range pat.Fields {
		byLabel[f.Label] = f.Pat
	}
	// The field set is the pattern's own labels or, in a match
	// list, the union of the labels of the sibling patterns.
	if labelNames == nil {
		for _, f := range pat.Fields {
			labelNames = append(labelNames, f.Label)
		}
	}
	fields := make([]labelTerm, len(labelNames))
	for i, label := range labelNames {
		fields[i] = labelTerm{label: label}
	}
	sortFields(fields)
	for i := range fields {
		vArg := r.u.Variable()
		if fieldPat, ok := byLabel[fields[i].label]; ok {
			err := r.deducePat(fieldPat, termMap, nil, vArg)
			if err != nil {
				return err
			}
		}
		fields[i].term = vArg
	}
	term := r.recordTerm(fields)
	if !pat.Ellipsis {
		r.regEquiv(pat, v, term)
		return nil
	}
	// The pattern has an ellipsis, so it matches any record with
	// at least these fields. When the source record's type
	// becomes known, unify the named fields' types.
	labelTypes := map[string]bool{}
	for _, f := range fields {
		labelTypes[f.label] = true
	}
	v2 := r.u.Variable()
	r.equiv(v2, term)
	r.actions = append(r.actions, unify.VarAction{
		Var: v,
		Action: func(_ *unify.Var, t unify.Term,
			s *unify.Substitution, add func(l, r unify.Term),
		) {
			seq, ok := t.(*unify.Sequence)
			if !ok {
				return
			}
			var fields2 []labelTerm
			for i, fieldName := range fieldList(seq) {
				if labelTypes[fieldName] {
					fields2 = append(fields2, labelTerm{
						label: fieldName,
						term:  seq.Terms[i],
					})
				}
			}
			add(s.Resolve(v2), r.recordTerm(fields2))
		},
	})
	r.reg(pat, v)
	return nil
}

// deduceInfix handles an infix operator application. The logical
// connectives type as bool directly; any other operator desugars
// to the application of its top-level binding, "a + b" becoming
// "(op +) (a, b)".
func (r *typeResolver) deduceInfix(env typeEnv, call *ast.InfixCall,
	v *unify.Var,
) error {
	switch call.Kind {
	case ast.OverOp:
		return r.deduceOver(env, call, v)
	case ast.AndalsoOp, ast.ImpliesOp, ast.OrelseOp:
		err := r.deduceExp(env, call.A0, v)
		if err != nil {
			return err
		}
		err = r.deduceExp(env, call.A1, v)
		if err != nil {
			return err
		}
		r.regEquiv(call, v, r.primTerm(boolName))
		return nil
	default:
		name, ok := infixOpNames[call.Kind]
		if !ok {
			return &Error{
				Span: call.Span(),
				Msg: "cannot deduce type for " +
					call.Kind.String(),
			}
		}
		return r.deduceOpCall(env, name, call,
			[]ast.Expr{call.A0, call.A1}, v)
	}
}

// deduceOpCall types an operator call as the application of the
// operator's top-level binding to its operands.
func (r *typeResolver) deduceOpCall(env typeEnv, name string,
	call ast.Expr, args []ast.Expr, v *unify.Var,
) error {
	span := call.Span()
	var arg ast.Expr
	if len(args) == 1 {
		arg = args[0]
	} else {
		arg = ast.NewTuple(span, args)
	}
	apply := ast.NewApply(span, ast.NewID(span, name), arg)
	err := r.deduceApply(env, apply, v)
	if err != nil {
		return err
	}
	r.reg(call, v)
	return nil
}

// deduceOverloadApply types the application of an overloaded name.
// Each instance is a function; a constraint requires the argument
// to match one instance's parameter type, and equates the result
// with that instance's result type. As unification resolves the
// argument type, candidates that cannot match are pruned; when one
// remains, the result type is fixed.
func (r *typeResolver) deduceOverloadApply(env typeEnv,
	apply *ast.Apply, insts []*unify.Var, v *unify.Var,
) error {
	vArg := r.u.Variable()
	err := r.deduceExp(env, apply.Arg, vArg)
	if err != nil {
		return err
	}
	argResults := make([]unify.TermPair, len(insts))
	for i, iv := range insts {
		vP := r.u.Variable()
		vR := r.u.Variable()
		r.equiv(iv, r.fnTerm(vP, vR))
		argResults[i] = unify.TermPair{Left: vP, Right: vR}
	}
	r.constraints = append(r.constraints,
		unify.Overload(vArg, v, argResults))
	r.reg(apply, v)
	return nil
}

func (r *typeResolver) deduceIf(env typeEnv, ifExp *ast.If,
	v *unify.Var,
) error {
	vCond := r.u.Variable()
	err := r.deduceExp(env, ifExp.Cond, vCond)
	if err != nil {
		return err
	}
	r.equiv(vCond, r.primTerm(boolName))
	err = r.deduceExp(env, ifExp.IfTrue, v)
	if err != nil {
		return err
	}
	err = r.deduceExp(env, ifExp.IfFalse, v)
	if err != nil {
		return err
	}
	r.reg(ifExp, v)
	return nil
}

// deduceCase handles "case exp of pat => exp | ...". Every rule's
// pattern unifies with the scrutinee's type. If any rule has a
// record pattern, all the rules' record patterns share the union
// of their field names, which lets a rule mention only the fields
// it needs.
func (r *typeResolver) deduceCase(env typeEnv, caseExp *ast.Case,
	v *unify.Var,
) error {
	v2 := r.u.Variable()
	err := r.deduceExp(env, caseExp.Exp, v2)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var labelNames []string
	if seq, ok := r.nodeTerm[caseExp.Exp].(*unify.Sequence); ok {
		for _, label := range fieldList(seq) {
			seen[label] = true
			labelNames = append(labelNames, label)
		}
	}
	for _, m := range caseExp.Matches {
		if recordPat, ok := m.Pat.(*ast.RecordPat); ok {
			for _, f := range recordPat.Fields {
				if !seen[f.Label] {
					seen[f.Label] = true
					labelNames = append(labelNames, f.Label)
				}
			}
		}
	}
	err = r.deduceMatchList(env, caseExp.Matches, labelNames,
		v2, v)
	if err != nil {
		return err
	}
	r.reg(caseExp, v)
	return nil
}

// deduceMatchList handles the rules of a case: each rule's
// pattern has the scrutinee's type, and each rule's expression
// has the result type.
func (r *typeResolver) deduceMatchList(env typeEnv,
	matches []*ast.Match, labelNames []string,
	argVariable, resultVariable *unify.Var,
) error {
	for _, m := range matches {
		var termMap []patTerm
		err := r.deducePat(m.Pat, &termMap, labelNames,
			argVariable)
		if err != nil {
			return err
		}
		env2 := bindAll(env, termMap)
		err = r.deduceExp(env2, m.Exp, resultVariable)
		if err != nil {
			return err
		}
	}
	return nil
}

// deduceMatch handles one match rule "pat => exp" of a fn: the
// rule's type is "typeof(pat) -> typeof(exp)".
func (r *typeResolver) deduceMatch(env typeEnv, match *ast.Match,
	argVariable, resultVariable *unify.Var,
) error {
	vPat := r.u.Variable()
	var termMap []patTerm
	err := r.deducePat(match.Pat, &termMap, nil, vPat)
	if err != nil {
		return err
	}
	env2 := bindAll(env, termMap)
	err = r.deduceExp(env2, match.Exp, resultVariable)
	if err != nil {
		return err
	}
	r.regEquiv(match, argVariable,
		r.fnTerm(vPat, resultVariable))
	return nil
}
