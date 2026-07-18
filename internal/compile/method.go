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
	"github.com/hydromatic/morel-go/internal/types"
)

// Postfix method calls. A member marked "[@@method]" in a signature
// can be called "receiver.member arg" as sugar for the structure
// call. The parser reads "x.f a" as "Apply(Apply(#f, x), a)"; this
// pass rewrites that, when f is a method and x's type is known, into
// the equivalent structure call, which the ordinary resolver then
// handles. Dispatch is by the receiver's head type constructor, so
// "compare" reaches Date.compare on a date and Time.compare on a
// time.

// MethodInfo describes one "[@@method]" member from a signature.
type MethodInfo struct {
	Type      types.Type
	Structure string
	Name      string
}

// methodCandidate is a registered method: the structure whose member
// implements it, the head type constructor it dispatches on, and
// whether its parameter is a tuple (so the receiver splices in).
type methodCandidate struct {
	structure    string
	receiverHead string
	paramIsTuple bool
}

// MethodRegistry maps a method name to its candidates, and holds
// each structure's member types for receiver-type inference.
type MethodRegistry struct {
	byName      map[string][]methodCandidate
	memberTypes map[string]map[string]types.Type
}

// NewMethodRegistry builds the registry from the signature's method
// list and its structure bindings.
func NewMethodRegistry(
	methods []MethodInfo, bindings []Binding,
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
		recv, isTuple := receiverPart(mi.Type)
		reg.byName[mi.Name] = append(reg.byName[mi.Name], methodCandidate{
			structure:    mi.Structure,
			receiverHead: typeHead(recv),
			paramIsTuple: isTuple,
		})
	}
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

// typeHead is a type's head constructor: a datatype or primitive
// name, or "list".
func typeHead(t types.Type) string {
	// lint: sort until '^	}' where '^	case '
	switch tt := t.(type) {
	case *types.List:
		return "list"
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

// RewriteDecl rewrites postfix method calls throughout a
// declaration in place.
func (reg *MethodRegistry) RewriteDecl(decl ast.Decl) {
	if vd, ok := decl.(*ast.ValDecl); ok {
		for _, b := range vd.Binds {
			b.Exp = reg.rewriteExpr(b.Exp)
		}
	}
}

// rewriteExpr rewrites postfix method calls within an expression,
// bottom-up.
func (reg *MethodRegistry) rewriteExpr(e ast.Expr) ast.Expr {
	// lint: sort until '^	}' where '^	case '
	switch x := e.(type) {
	case *ast.AnnotatedExp:
		x.Exp = reg.rewriteExpr(x.Exp)
	case *ast.Apply:
		x.Fn = reg.rewriteExpr(x.Fn)
		x.Arg = reg.rewriteExpr(x.Arg)
		if d, ok := reg.desugar(x); ok {
			return d
		}
	case *ast.Case:
		x.Exp = reg.rewriteExpr(x.Exp)
		reg.rewriteMatches(x.Matches)
	case *ast.Fn:
		reg.rewriteMatches(x.Matches)
	case *ast.If:
		x.Cond = reg.rewriteExpr(x.Cond)
		x.IfTrue = reg.rewriteExpr(x.IfTrue)
		x.IfFalse = reg.rewriteExpr(x.IfFalse)
	case *ast.InfixCall:
		x.A0 = reg.rewriteExpr(x.A0)
		x.A1 = reg.rewriteExpr(x.A1)
	case *ast.Let:
		for _, d := range x.Decls {
			reg.RewriteDecl(d)
		}
		x.Exp = reg.rewriteExpr(x.Exp)
	case *ast.ListExp:
		for i := range x.Args {
			x.Args[i] = reg.rewriteExpr(x.Args[i])
		}
	case *ast.PrefixCall:
		x.A = reg.rewriteExpr(x.A)
	case *ast.Record:
		for i := range x.Fields {
			x.Fields[i].Exp = reg.rewriteExpr(x.Fields[i].Exp)
		}
	case *ast.Tuple:
		for i := range x.Args {
			x.Args[i] = reg.rewriteExpr(x.Args[i])
		}
	}
	return e
}

func (reg *MethodRegistry) rewriteMatches(matches []*ast.Match) {
	for _, m := range matches {
		m.Exp = reg.rewriteExpr(m.Exp)
	}
}

// desugar rewrites "receiver.method arg" — parsed as
// "Apply(Apply(#method, receiver), arg)" — into the structure call,
// or reports that the apply is not a dispatchable method call.
func (reg *MethodRegistry) desugar(apply *ast.Apply) (ast.Expr, bool) {
	inner, ok := apply.Fn.(*ast.Apply)
	if !ok {
		return nil, false
	}
	sel, ok := inner.Fn.(*ast.RecordSelector)
	if !ok {
		return nil, false
	}
	cands, ok := reg.byName[sel.Name]
	if !ok {
		return nil, false
	}
	receiver := inner.Arg
	head := reg.receiverHead(receiver)
	if head == "" {
		return nil, false
	}
	var cand *methodCandidate
	for i := range cands {
		if cands[i].receiverHead == head {
			cand = &cands[i]
			break
		}
	}
	if cand == nil {
		return nil, false
	}
	span := apply.Span()
	member := ast.NewApply(span, ast.NewRecordSelector(span, sel.Name),
		ast.NewID(span, cand.structure))
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

// receiverHead infers the head type constructor of a receiver
// expression structurally, for a "Structure.member" projection or a
// "Structure.member arg" call whose member types are known.
func (reg *MethodRegistry) receiverHead(recv ast.Expr) string {
	r, ok := recv.(*ast.Apply)
	if !ok {
		return ""
	}
	if inner, ok := r.Fn.(*ast.Apply); ok {
		if s, m, ok := structureMember(inner); ok {
			if mt := reg.memberType(s, m); mt != nil {
				return resultHead(mt)
			}
		}
	}
	if s, m, ok := structureMember(r); ok {
		if mt := reg.memberType(s, m); mt != nil {
			return typeHead(mt)
		}
	}
	return ""
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
