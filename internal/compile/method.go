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
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/internal/unify"
)

// Postfix method calls. A member marked "[@@method]" in a signature
// can be called "receiver.member arg" as sugar for the structure
// call. The parser reads "x.f a" as "Apply(Apply(#f, x), a)", and
// the type resolver, on reaching such an apply, desugars it to the
// structure call, which the core resolver then follows. Dispatch is
// by the receiver's head type constructor, so "compare" reaches
// Date.compare on a date and Time.compare on a time.
//
// It happens during type deduction, not before it, because the head
// of a receiver is often something only inference knows -- the
// element type of a query's source, the type a "let" gave a name --
// and because the environment there is the scoped one, so a local
// binding shadows a top-level binding of the same name.

// MethodInfo describes one "[@@method]" member from a signature.
type MethodInfo struct {
	Type      types.Type
	Structure string
	Name      string
	// Target, when set, is the qualified binding the call
	// rewrites to instead of "Structure.Name" — used by an
	// overload (a second method of the same name dispatching on
	// a different receiver type, e.g. Range.contains on a
	// continuous_set) whose implementation hides behind its own
	// binding.
	Target string
}

// methodCandidate is a registered method: the structure whose member
// implements it, the head type constructor it dispatches on, and
// whether its parameter is a tuple (so the receiver splices in).
type methodCandidate struct {
	structure    string
	receiverHead string
	target       string
	paramIsTuple bool
	// resultHead is the head type constructor of what the method
	// returns, which is the receiver's head when the call is itself
	// the receiver of another: "xs.drop(2).drop 1". It is "" for a
	// method that returns a type variable, such as "Option.getOpt".
	resultHead string
}

// MethodRegistry maps a method name to its candidates, and holds
// each structure's member types for receiver-type inference.
// globalType reports the session's binding for a name, which types
// a receiver that names one and is not shadowed.
type MethodRegistry struct {
	byName      map[string][]methodCandidate
	memberTypes map[string]map[string]types.Type
	globalType  func(string) types.Type
}

// NewMethodRegistry builds the registry from the signature's method
// list and its structure bindings.
func NewMethodRegistry(sys *types.System,
	methods []MethodInfo, bindings []Binding,
	globalType func(string) types.Type,
) *MethodRegistry {
	reg := &MethodRegistry{
		byName:      map[string][]methodCandidate{},
		memberTypes: map[string]map[string]types.Type{},
	}
	for _, b := range bindings {
		rec, ok := b.Type.(*types.Record)
		if !ok {
			continue
		}
		m := make(map[string]types.Type, len(rec.Fields))
		for _, f := range rec.Fields {
			m[f.Label] = f.Type
		}
		reg.memberTypes[b.Name] = m
	}
	for _, mi := range methods {
		t := mi.Type
		if ct := CollectionAggType(sys, mi.Name, t); ct != nil &&
			mi.Structure == relationalStructure {
			// An aggregate over a collection takes a list receiver
			// and a bag receiver alike, as its binding does; the
			// signature can only write "bag".
			t = ct
		}
		recv, isTuple := receiverPart(t)
		reg.byName[mi.Name] = append(reg.byName[mi.Name], methodCandidate{
			structure:    mi.Structure,
			receiverHead: typeHead(recv),
			target:       mi.Target,
			paramIsTuple: isTuple,
			resultHead:   resultHead(t),
		})
	}
	reg.globalType = globalType
	return reg
}

// receiverPart returns a method's receiver type — its parameter, or
// the first element when the parameter is a tuple — and whether the
// parameter was a tuple.
func receiverPart(t types.Type) (types.Type, bool) {
	fn, ok := t.(*types.Fn)
	if !ok {
		return t, false
	}
	if tup, ok := fn.Param.(*types.Tuple); ok && len(tup.Args) > 0 {
		return tup.Args[0], true
	}
	return fn.Param, false
}

// relationalStructure is the structure whose aggregates work on a
// list and on a bag alike.
const relationalStructure = "Relational"

// collectionHead is the head of a collection type, whose
// orderedness is not settled: a method declared over one takes a
// list receiver and a bag receiver alike.
const collectionHead = "$collection"

// typeHead is a type's head constructor: a datatype or primitive
// name, "list", or a collection.
func typeHead(t types.Type) string {
	// lint: sort until '^	}' where '^	case '
	switch tt := t.(type) {
	case *types.Collection:
		return collectionHead
	case *types.List:
		return listTyCon
	case *types.Named:
		return tt.Name
	case *types.Primitive:
		return tt.String()
	default:
		return ""
	}
}

// resultHead is the head constructor of a type, or of a function
// type's result.
func resultHead(t types.Type) string {
	if fn, ok := t.(*types.Fn); ok {
		return typeHead(fn.Result)
	}
	return typeHead(t)
}

// desugarHead is desugar for a call whose receiver's head type
// constructor is already known — as it is once type deduction has
// reached the call, for a receiver whose type only inference gives.
// It reports false if no method of that name dispatches on that
// head.
func (reg *MethodRegistry) desugarHead(apply *ast.Apply, name string,
	receiver ast.Expr, head string,
) (ast.Expr, bool) {
	cand := reg.candidate(name, head)
	if cand == nil {
		return nil, false
	}
	span := apply.Span()
	var member ast.Expr = ast.NewApply(span,
		ast.NewRecordSelector(span, name),
		ast.NewID(span, cand.structure))
	if cand.target != "" {
		// An overload rewrites to its own hidden binding.
		member = ast.NewID(span, cand.target)
	}
	arg := apply.Arg
	if lit, ok := arg.(*ast.Literal); ok && lit.Kind == ast.UnitLiteralOp {
		// receiver.f () → Structure.f receiver
		return ast.NewApply(span, member, receiver), true
	}
	if cand.paramIsTuple {
		// receiver.f arg → Structure.f (receiver, arg)
		elems := []ast.Expr{receiver}
		if tup, ok := arg.(*ast.Tuple); ok {
			elems = append(elems, tup.Args...)
		} else {
			elems = append(elems, arg)
		}
		return ast.NewApply(span, member, ast.NewTuple(span, elems)), true
	}
	// receiver.f arg → Structure.f receiver arg
	return ast.NewApply(span, ast.NewApply(span, member, receiver), arg), true
}

// headMatches reports whether a method whose receiver is of head
// "param" takes a receiver of head "recv". A method over a
// collection, such as "Relational.max", takes a list and a bag
// alike.
func headMatches(param, recv string) bool {
	if param == collectionHead {
		return recv == listTyCon || recv == bagTyCon
	}
	return param == recv
}

// literalHead is the head constructor of a literal's type.
func literalHead(kind ast.Op) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case ast.BoolLiteralOp:
		return "bool"
	case ast.CharLiteralOp:
		return "char"
	case ast.IntLiteralOp:
		return "int"
	case ast.RealLiteralOp:
		return realName
	case ast.StringLiteralOp:
		return "string"
	case ast.WordLiteralOp:
		return wordName
	default:
		return ""
	}
}

// structureMember matches "Structure.member" — "Apply(#member,
// ID(Structure))" — and returns the structure and member names.
func structureMember(a *ast.Apply) (string, string, bool) {
	sel, ok := a.Fn.(*ast.RecordSelector)
	if !ok {
		return "", "", false
	}
	id, ok := a.Arg.(*ast.ID)
	if !ok {
		return "", "", false
	}
	return id.Name, sel.Name, true
}

func (reg *MethodRegistry) memberType(structure, member string) types.Type {
	if m, ok := reg.memberTypes[structure]; ok {
		return m[member]
	}
	return nil
}

// methodCall matches "receiver.method arg" — parsed as
// "Apply(Apply(#method, receiver), arg)" — where the name is a
// registered method, and returns the name and the receiver.
func (r *typeResolver) methodCall(apply *ast.Apply) (string, ast.Expr,
	bool,
) {
	if r.methods == nil {
		return "", nil, false
	}
	inner, isApply := apply.Fn.(*ast.Apply)
	if !isApply {
		return "", nil, false
	}
	sel, isSel := inner.Fn.(*ast.RecordSelector)
	if !isSel || sel.Safe {
		return "", nil, false
	}
	if _, known := r.methods.byName[sel.Name]; !known {
		return "", nil, false
	}
	// "Structure.member arg" is a member call, not a postfix one --
	// and it is what a postfix call desugars to, so without this the
	// desugared call would be desugared again.
	if s, m, isMember := structureMember(inner); isMember &&
		r.methods.memberType(s, m) != nil {
		return "", nil, false
	}
	return sel.Name, inner.Arg, true
}

// methodApply reports the structure call that an apply desugars to,
// if the apply is "receiver.method arg" and the receiver's type
// picks out a method of that name.
func (r *typeResolver) methodApply(env typeEnv, apply *ast.Apply,
) (ast.Expr, bool) {
	name, receiver, isCall := r.methodCall(apply)
	if !isCall {
		return nil, false
	}
	head := r.receiverHead(env, receiver)
	if head == "" {
		// Nothing says what the receiver is. The argument may: a
		// method whose receiver and argument are of one type, such
		// as "compare", is pinned by either.
		head = r.argHead(env, name, apply.Arg)
	}
	if head == "" {
		return nil, false
	}
	return r.methods.desugarHead(apply, name, receiver, head)
}

// receiverHead is the head type constructor of a postfix method
// call's receiver, worked out without elaborating it — elaboration
// has side effects, and the receiver is elaborated once, as part of
// the structure call the caller builds.
func (r *typeResolver) receiverHead(env typeEnv, recv ast.Expr) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch e := recv.(type) {
	case *ast.AnnotatedExp:
		// The annotation and the expression agree, so either will
		// do; the expression is asked first because it is typed the
		// same way every other receiver is.
		if head := r.receiverHead(env, e.Exp); head != "" {
			return head
		}
		return annotationHead(e.Type)
	case *ast.Apply:
		return r.applyHead(env, e)
	case *ast.ID:
		return r.nameHead(env, e.Name)
	case *ast.ListExp:
		return listTyCon
	case *ast.Literal:
		return literalHead(e.Kind)
	}
	return ""
}

// annotationHead is the head type constructor a type annotation
// names, before the annotation has been resolved to a type.
func annotationHead(t ast.Type) string {
	if named, isNamed := t.(*ast.NamedType); isNamed {
		return named.Name
	}
	return ""
}

// nameHead is the head type constructor of a name. A name in scope
// is typed by inference; one that is not is a top-level binding, and
// the session's environment types it. Taking them in that order is
// what makes a "let" binding or a query variable shadow a top-level
// binding of the same name.
func (r *typeResolver) nameHead(env typeEnv, name string) string {
	if t, inScope := env.peek(name); inScope {
		if head := r.headOfTerm(t); head != "" {
			return head
		}
		return r.solvedHead(t)
	}
	if t := r.methods.globalType(name); t != nil {
		return typeHead(t)
	}
	return ""
}

// applyHead is the head type constructor of an application: the
// result of whatever it applies.
func (r *typeResolver) applyHead(env typeEnv, e *ast.Apply) string {
	reg := r.methods
	// "Structure.member" and "Structure.member arg", whose member
	// types the signature gives.
	if s, m, isMember := structureMember(e); isMember {
		if mt := reg.memberType(s, m); mt != nil {
			return typeHead(mt)
		}
	}
	if inner, isApply := e.Fn.(*ast.Apply); isApply {
		if s, m, isMember := structureMember(inner); isMember {
			if mt := reg.memberType(s, m); mt != nil {
				return resultHead(mt)
			}
		}
	}
	// A method call as a receiver — "xs.drop(2).drop 1" — has the
	// head of what the method returns.
	if name, receiver, isCall := r.methodCall(e); isCall {
		if cand := reg.candidate(name,
			r.receiverHead(env, receiver)); cand != nil {
			return cand.resultHead
		}
	}
	// A field selection as a receiver — "r.i.compare (r.n)" — has
	// the head of the field's type.
	if sel, isSel := e.Fn.(*ast.RecordSelector); isSel && !sel.Safe {
		return r.fieldHead(env, e.Arg, sel.Name)
	}
	// A named function or constructor applied to an argument has the
	// head of its result: "(bag [1, 2]).max ()" is a bag, and
	// "(CLOSED (3, 7)).contains 5" a range.
	if id, isID := e.Fn.(*ast.ID); isID {
		if _, inScope := env.peek(id.Name); !inScope {
			if t := reg.globalType(id.Name); t != nil {
				return resultHead(t)
			}
		}
	}
	return ""
}

// fieldHead is the head type constructor of a field of a record
// expression, or "" if the expression is not known to be a record
// with that field.
func (r *typeResolver) fieldHead(env typeEnv, recv ast.Expr,
	field string,
) string {
	id, isID := recv.(*ast.ID)
	if !isID {
		return ""
	}
	if t, inScope := env.peek(id.Name); inScope {
		// The record's fields are known only once its term is
		// resolved: a query variable's row type comes of decomposing
		// its source.
		subst := r.headSubstitution()
		if subst == nil {
			return ""
		}
		s, isSeq := subst.Resolve(t).(*unify.Sequence)
		if !isSeq {
			return ""
		}
		for i, label := range fieldList(s) {
			if label == field && i < len(s.Terms) {
				return resolvedHead(s.Terms[i])
			}
		}
		return ""
	}
	// A top-level binding's type is declared, not inferred.
	rec, isRec := r.methods.globalType(id.Name).(*types.Record)
	if !isRec {
		return ""
	}
	for _, f := range rec.Fields {
		if f.Label == field {
			return typeHead(f.Type)
		}
	}
	return ""
}

// argHead is the head type constructor the argument of a call gives
// its receiver. It applies to a method whose receiver and argument
// are of one type — "compare : 'a * 'a -> order", "max", "min" — for
// which the argument pins the candidate just as the receiver would.
// It is used only when exactly one candidate matches, so that a
// method whose argument is unrelated to its receiver cannot be
// dispatched by it.
func (r *typeResolver) argHead(env typeEnv, name string,
	arg ast.Expr,
) string {
	head := r.receiverHead(env, arg)
	if head == "" {
		return ""
	}
	matches := 0
	for _, cand := range r.methods.byName[name] {
		if headMatches(cand.receiverHead, head) {
			matches++
		}
	}
	if matches != 1 {
		return ""
	}
	return head
}

// candidate is the method of this name that takes a receiver of
// this head, or nil if there is none.
func (reg *MethodRegistry) candidate(name, head string,
) *methodCandidate {
	if head == "" {
		return nil
	}
	for i, cand := range reg.byName[name] {
		if headMatches(cand.receiverHead, head) {
			return &reg.byName[name][i]
		}
	}
	return nil
}

// solvedHead is the head type constructor of a term according to a
// solve of the constraints gathered so far. It answers where
// headOfTerm cannot: a query variable is tied to its source's
// element type only by decomposing the source's collection term,
// which unification does and a scan of the constraints does not. The
// solve is side-effect free — its actions append to a throwaway work
// list — and it is made once per state of the constraints, since a
// receiver is typed the same as long as none have been added.
func (r *typeResolver) solvedHead(t unify.Term) string {
	subst := r.headSubstitution()
	if subst == nil {
		return ""
	}
	return resolvedHead(subst.Resolve(t))
}

// headSubstitution is that solve, or nil if the constraints
// gathered so far have no solution — in which case nothing is
// dispatched, and unification reports the conflict when the pass
// ends.
func (r *typeResolver) headSubstitution() *unify.Substitution {
	if r.headSubstAt != len(r.pairs) {
		r.headSubstAt = len(r.pairs)
		subst, err := r.u.Unify(r.pairs, r.actions, r.constraints)
		if err != nil {
			subst = nil
		}
		r.headSubst = subst
	}
	return r.headSubst
}

// resolvedHead is the head type constructor of a term that a
// substitution has resolved, or "" if it is a variable or a
// structural term.
func resolvedHead(t unify.Term) string {
	s, isSeq := t.(*unify.Sequence)
	if !isSeq || structuralOp(s.Op) {
		return ""
	}
	if s.Op != collectionTyCon {
		return s.Op
	}
	if len(s.Terms) != collectionArity {
		return ""
	}
	ord, isSeq := s.Terms[1].(*unify.Sequence)
	if !isSeq {
		return ""
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch ord.Op {
	case orderedName:
		return listTyCon
	case unorderedName:
		return bagTyCon
	default:
		return ""
	}
}

// structuralOp reports whether a term operator builds a type from
// others — function, tuple, record — rather than being a type
// constructor a method can dispatch on.
func structuralOp(op string) bool {
	return op == fnTyCon || op == tupleTyCon ||
		strings.HasPrefix(op, recordTyCon)
}

// headOfTerm is the head type constructor of a term — "int", "list",
// a datatype name — as far as the constraints gathered so far
// determine it, or "" if they do not yet. It scans them backwards
// for the terms bound to a variable, following variable-to-variable
// links, which is how a name whose type was inferred earlier in the
// declaration yields its head.
func (r *typeResolver) headOfTerm(t unify.Term) string {
	return r.termHead(t, map[*unify.Var]bool{})
}

func (r *typeResolver) termHead(t unify.Term,
	seen map[*unify.Var]bool,
) string {
	switch t := t.(type) {
	case *unify.Sequence:
		return r.seqHead(t, seen)
	case *unify.Var:
		if seen[t] {
			return ""
		}
		seen[t] = true
		for _, pair := range slices.Backward(r.pairs) {
			if v, isVar := pair.Right.(*unify.Var); !isVar || v != t {
				continue
			}
			if head := r.termHead(pair.Left, seen); head != "" {
				return head
			}
		}
	}
	return ""
}

// seqHead is the head type constructor of a sequence. The structural
// operators — function, tuple, record — are not type constructors,
// and a term built from one has no head.
func (r *typeResolver) seqHead(s *unify.Sequence,
	seen map[*unify.Var]bool,
) string {
	switch {
	case s.Op == collectionTyCon:
		return r.collectionHead(s, seen)
	case structuralOp(s.Op):
		return ""
	default:
		return s.Op
	}
}

// collectionArity is the number of arguments of a collection term:
// its element type and its orderedness.
const collectionArity = 2

// collectionHead resolves a collection term to "list" or "bag" by
// its orderedness, so that dispatch tells the two apart. It is ""
// while the orderedness is undetermined.
func (r *typeResolver) collectionHead(s *unify.Sequence,
	seen map[*unify.Var]bool,
) string {
	if len(s.Terms) != collectionArity {
		return ""
	}
	ord := s.Terms[1]
	for {
		v, isVar := ord.(*unify.Var)
		if !isVar || seen[v] {
			break
		}
		seen[v] = true
		next := r.boundTerm(v)
		if next == nil {
			break
		}
		ord = next
	}
	if atom, isSeq := ord.(*unify.Sequence); isSeq &&
		len(atom.Terms) == 0 {
		switch atom.Op {
		case orderedName:
			return listTyCon
		case unorderedName:
			return bagTyCon
		}
	}
	return ""
}

// boundTerm is the term most recently equated with a variable, or
// nil if the constraints gathered so far bind it to none.
func (r *typeResolver) boundTerm(v *unify.Var) unify.Term {
	for _, pair := range slices.Backward(r.pairs) {
		if v2, isVar := pair.Right.(*unify.Var); isVar && v2 == v {
			return pair.Left
		}
	}
	return nil
}
