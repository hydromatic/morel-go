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

package ast

import "github.com/hydromatic/morel-go/internal/token"

// Type is a type-expression node.
type Type interface {
	Node
	typ()
}

type typeBase struct{ base }

func (typeBase) typ() {}

func tb(span token.Span) typeBase {
	return typeBase{base{span}}
}

// TyVar is a type variable, "'a".
type TyVar struct {
	typeBase

	Name string
}

// NewTyVar returns a type variable; name includes the leading
// "'".
func NewTyVar(span token.Span, name string) *TyVar {
	return &TyVar{typeBase: tb(span), Name: name}
}

// Op implements Node.
func (*TyVar) Op() Op { return TyVarOp }

// SubstituteType returns a copy of t with each type variable
// replaced by the type subst maps its name to; a variable absent
// from subst is left unchanged. It expands a type alias by
// substituting its arguments for its parameters.
func SubstituteType(t Type, subst map[string]Type) Type {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *FnType:
		return &FnType{
			typeBase: n.typeBase,
			Param:    SubstituteType(n.Param, subst),
			Result:   SubstituteType(n.Result, subst),
		}
	case *NamedType:
		args := make([]Type, len(n.Args))
		for i, a := range n.Args {
			args[i] = SubstituteType(a, subst)
		}
		return &NamedType{typeBase: n.typeBase, Name: n.Name, Args: args}
	case *RecordType:
		fields := make([]TypeField, len(n.Fields))
		for i, f := range n.Fields {
			fields[i] = TypeField{
				Label: f.Label, Type: SubstituteType(f.Type, subst),
			}
		}
		return &RecordType{typeBase: n.typeBase, Fields: fields}
	case *TupleType:
		args := make([]Type, len(n.Args))
		for i, a := range n.Args {
			args[i] = SubstituteType(a, subst)
		}
		return &TupleType{typeBase: n.typeBase, Args: args}
	case *TyVar:
		if r, ok := subst[n.Name]; ok {
			return r
		}
		return n
	default:
		return t
	}
}

// MapTypeofs replaces each "typeof e" in a type with what f
// returns for e, and leaves the rest of the type as it was. A
// type declaration echoes the type it means -- "type t = int",
// not "type t = typeof mi".
func MapTypeofs(t Type, f func(Expr) (Type, error)) (Type, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *ExpressionType:
		return f(n.Exp)
	case *FnType:
		param, err := MapTypeofs(n.Param, f)
		if err != nil {
			return nil, err
		}
		result, err := MapTypeofs(n.Result, f)
		if err != nil {
			return nil, err
		}
		return NewFnType(n.Span(), param, result), nil
	case *NamedType:
		args, err := mapTypeofList(n.Args, f)
		if err != nil {
			return nil, err
		}
		return NewNamedType(n.Span(), n.Name, args), nil
	case *RecordType:
		fields := make([]TypeField, len(n.Fields))
		for i, fld := range n.Fields {
			ft, err := MapTypeofs(fld.Type, f)
			if err != nil {
				return nil, err
			}
			fields[i] = TypeField{Label: fld.Label, Type: ft}
		}
		return NewRecordType(n.Span(), fields), nil
	case *TupleType:
		args, err := mapTypeofList(n.Args, f)
		if err != nil {
			return nil, err
		}
		return NewTupleType(n.Span(), args), nil
	default:
		return t, nil
	}
}

// mapTypeofList maps MapTypeofs over a list of types.
func mapTypeofList(ts []Type, f func(Expr) (Type, error)) (
	[]Type, error,
) {
	out := make([]Type, len(ts))
	for i, t := range ts {
		u, err := MapTypeofs(t, f)
		if err != nil {
			return nil, err
		}
		out[i] = u
	}
	return out, nil
}

// MapNamedTypeNames returns a copy of t with every named type's name
// replaced by f(name); f is applied to each NamedType (including
// "list") and should return the name unchanged when it does not want
// to remap. Structure and type variables are preserved.
func MapNamedTypeNames(t Type, f func(string) string) Type {
	// lint: sort until '^\t}' where '^\tcase '
	switch n := t.(type) {
	case *FnType:
		return NewFnType(n.Span(), MapNamedTypeNames(n.Param, f),
			MapNamedTypeNames(n.Result, f))
	case *NamedType:
		args := make([]Type, len(n.Args))
		for i, a := range n.Args {
			args[i] = MapNamedTypeNames(a, f)
		}
		return NewNamedType(n.Span(), f(n.Name), args)
	case *RecordType:
		fields := make([]TypeField, len(n.Fields))
		for i, fld := range n.Fields {
			fields[i] = TypeField{
				Label: fld.Label, Type: MapNamedTypeNames(fld.Type, f),
			}
		}
		return NewRecordType(n.Span(), fields)
	case *TupleType:
		args := make([]Type, len(n.Args))
		for i, a := range n.Args {
			args[i] = MapNamedTypeNames(a, f)
		}
		return NewTupleType(n.Span(), args)
	default:
		return t
	}
}

// NamedType is a type constructor applied to zero or more
// arguments: "int", "int list", "('a, 'b) pair".
type NamedType struct {
	typeBase

	Name string
	Args []Type
}

// NewNamedType returns a named type.
func NewNamedType(span token.Span, name string,
	args []Type,
) *NamedType {
	return &NamedType{typeBase: tb(span), Name: name, Args: args}
}

// Op implements Node.
func (*NamedType) Op() Op { return NamedTypeOp }

// TupleType is "t1 * t2 * ..."; "*" is non-associative, so
// nested tuple types are distinct.
type TupleType struct {
	typeBase

	Args []Type
}

// NewTupleType returns a tuple type.
func NewTupleType(span token.Span, args []Type) *TupleType {
	return &TupleType{typeBase: tb(span), Args: args}
}

// Op implements Node.
func (*TupleType) Op() Op { return TupleTypeOp }

// FnType is "t1 -> t2"; "->" is right-associative.
type FnType struct {
	typeBase

	Param  Type
	Result Type
}

// NewFnType returns a function type.
func NewFnType(span token.Span, param, result Type) *FnType {
	return &FnType{
		typeBase: tb(span), Param: param,
		Result: result,
	}
}

// Op implements Node.
func (*FnType) Op() Op { return FunctionTypeOp }

// TypeField is one field of a record type, "label: type".
type TypeField struct {
	Label string
	Type  Type
}

// RecordType is "{a: t1, b: t2, ...}".
type RecordType struct {
	typeBase

	Fields []TypeField
}

// NewRecordType returns a record type.
func NewRecordType(span token.Span,
	fields []TypeField,
) *RecordType {
	return &RecordType{typeBase: tb(span), Fields: fields}
}

// Op implements Node.
func (*RecordType) Op() Op { return RecordTypeOp }

// AnnotatedExp is "exp : type".
type AnnotatedExp struct {
	exprBase

	Exp  Expr
	Type Type
}

// NewAnnotatedExp returns a type-annotated expression.
func NewAnnotatedExp(span token.Span, exp Expr,
	typ Type,
) *AnnotatedExp {
	return &AnnotatedExp{
		exprBase: exprBase{base{span}},
		Exp:      exp,
		Type:     typ,
	}
}

// Op implements Node.
func (*AnnotatedExp) Op() Op { return AnnotatedExpOp }

// AnnotatedPat is "pat : type".
type AnnotatedPat struct {
	patBase

	Pat  Pat
	Type Type
}

// NewAnnotatedPat returns a type-annotated pattern.
func NewAnnotatedPat(span token.Span, pat Pat,
	typ Type,
) *AnnotatedPat {
	return &AnnotatedPat{patBase: pb(span), Pat: pat, Type: typ}
}

// Op implements Node.
func (*AnnotatedPat) Op() Op { return AnnotatedPatOp }

// ConBind is one constructor of a datatype binding,
// "Name [of type]"; Of is nil for a constant constructor.
type ConBind struct {
	Name string
	Of   Type
}

// DatatypeBind is one binding of a datatype declaration:
// "['a | ('a, 'b)] name = con | con ...".
type DatatypeBind struct {
	TyVars []string
	Name   string
	Cons   []ConBind
}

// DatatypeDecl is "datatype bind [and bind ...]".
type DatatypeDecl struct {
	declBase

	Binds []DatatypeBind
}

// NewDatatypeDecl returns a datatype declaration.
func NewDatatypeDecl(span token.Span,
	binds []DatatypeBind,
) *DatatypeDecl {
	return &DatatypeDecl{
		declBase: declBase{base{span}},
		Binds:    binds,
	}
}

// Op implements Node.
func (*DatatypeDecl) Op() Op { return DatatypeDeclOp }

// TypeBind is one binding of a type-alias declaration.
type TypeBind struct {
	TyVars []string
	Name   string
	Type   Type
}

// TypeDecl is "type bind [and bind ...]".
type TypeDecl struct {
	declBase

	Binds []TypeBind
}

// NewTypeDecl returns a type-alias declaration.
func NewTypeDecl(span token.Span, binds []TypeBind) *TypeDecl {
	return &TypeDecl{declBase: declBase{base{span}}, Binds: binds}
}

// Op implements Node.
func (*TypeDecl) Op() Op { return TypeDeclOp }

// SigSpec is one specification in a signature body: a type
// (optionally aliased), a value, an exception, or a datatype.
type SigSpec struct {
	Kind   Op
	TyVars []string
	Name   string
	Type   Type          // val's type, type's alias, exception's of
	Bind   *DatatypeBind // datatype spec
	// Attrs are the specification's attributes, "[@@a]", with a
	// doc comment before it desugared to "[@@doc]" and first. For
	// a floating attribute, which is a specification of its own,
	// Kind is FloatingAttrDeclOp and Attrs holds the one.
	Attrs []*Attribute
}

// SigBind is one "NAME = sig spec... end" of a signature
// declaration.
type SigBind struct {
	Name  string
	Specs []SigSpec
}

// SignatureDecl is "signature bind [and bind ...]".
type SignatureDecl struct {
	declBase

	Binds []SigBind
}

// NewSignatureDecl returns a signature declaration.
func NewSignatureDecl(span token.Span,
	binds []SigBind,
) *SignatureDecl {
	return &SignatureDecl{
		declBase: declBase{base{span}},
		Binds:    binds,
	}
}

// Op implements Node.
func (*SignatureDecl) Op() Op { return SignatureDeclOp }

// ExpressionType is "typeof exp".
type ExpressionType struct {
	typeBase

	Exp Expr
}

// NewExpressionType returns a typeof type.
func NewExpressionType(span token.Span,
	exp Expr,
) *ExpressionType {
	return &ExpressionType{typeBase: tb(span), Exp: exp}
}

// Op implements Node.
func (*ExpressionType) Op() Op { return ExpressionTypeOp }
