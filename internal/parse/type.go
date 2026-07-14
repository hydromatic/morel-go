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

package parse

import (
	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/token"
)

// typeExpr parses a type: "->" is right-associative and binds
// loosest; "*" is non-associative; a type-constructor application
// ("int list") binds tightest.
func (p *Parser) typeExpr() (ast.Type, error) {
	t, err := p.tupleType()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind != token.RThinArrow {
		return t, nil
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	result, err := p.typeExpr()
	if err != nil {
		return nil, err
	}
	span := token.Span{
		Start: t.Span().Start,
		End:   result.Span().End,
	}
	return ast.NewFnType(span, t, result), nil
}

func (p *Parser) tupleType() (ast.Type, error) {
	t, err := p.postfixType()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind != token.Star {
		return t, nil
	}
	args := []ast.Type{t}
	for p.tok.Kind == token.Star {
		err = p.next()
		if err != nil {
			return nil, err
		}
		var arg ast.Type
		arg, err = p.postfixType()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	last := args[len(args)-1]
	span := token.Span{
		Start: t.Span().Start,
		End:   last.Span().End,
	}
	return ast.NewTupleType(span, args), nil
}

// postfixType parses an atomic type followed by type-constructor
// names: "int list list".
func (p *Parser) postfixType() (ast.Type, error) {
	t, err := p.atomicType()
	if err != nil {
		return nil, err
	}
	for p.tok.Kind == token.Ident {
		name := p.tok
		err = p.next()
		if err != nil {
			return nil, err
		}
		span := token.Span{
			Start: t.Span().Start,
			End:   name.Span.End,
		}
		t = ast.NewNamedType(span, name.Text, []ast.Type{t})
	}
	return t, nil
}

func (p *Parser) atomicType() (ast.Type, error) {
	tok := p.tok
	// lint: sort until '^\t}' where '^\tcase '
	switch tok.Kind {
	case token.Ident:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewNamedType(tok.Span, tok.Text, nil), nil
	case token.LBrace:
		return p.recordType()
	case token.LParen:
		return p.parenType()
	case token.TyVar:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewTyVar(tok.Span, tok.Text), nil
	default:
		return nil, p.errorf("expected type, found " +
			tok.Kind.String())
	}
}

// parenType parses "(t)" (grouping) or "(t1, t2, ...) name" (the
// argument list of a type constructor).
func (p *Parser) parenType() (ast.Type, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	var args []ast.Type
	for {
		var t ast.Type
		t, err = p.typeExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, t)
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	err = p.expect(token.RParen)
	if err != nil {
		return nil, err
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	if len(args) == 1 && p.tok.Kind != token.Ident {
		return args[0], nil
	}
	err = p.expect(token.Ident)
	if err != nil {
		return nil, err
	}
	name := p.tok
	err = p.next()
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: name.Span.End}
	return ast.NewNamedType(span, name.Text, args), nil
}

// recordType parses "{a: t1, b: t2, ...}".
func (p *Parser) recordType() (ast.Type, error) {
	fields, span, err := braceFields(p, p.recordTypeField)
	if err != nil {
		return nil, err
	}
	return ast.NewRecordType(span, fields), nil
}

// braceFields parses "{f1, f2, ...}" using field for each
// element, returning the fields and the span of the braces.
func braceFields[T any](p *Parser, field func() (T, error)) ([]T,
	token.Span, error,
) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, token.Span{}, err
	}
	var fields []T
	for p.tok.Kind != token.RBrace {
		var f T
		f, err = field()
		if err != nil {
			return nil, token.Span{}, err
		}
		fields = append(fields, f)
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, token.Span{}, err
		}
	}
	err = p.expect(token.RBrace)
	if err != nil {
		return nil, token.Span{}, err
	}
	span := token.Span{Start: start, End: p.tok.Span.End}
	err = p.next()
	if err != nil {
		return nil, token.Span{}, err
	}
	return fields, span, nil
}

func (p *Parser) recordTypeField() (ast.TypeField, error) {
	err := p.expect(token.Ident)
	if err != nil {
		return ast.TypeField{}, err
	}
	label := p.tok.Text
	err = p.next()
	if err != nil {
		return ast.TypeField{}, err
	}
	err = p.expect(token.Colon)
	if err != nil {
		return ast.TypeField{}, err
	}
	err = p.next()
	if err != nil {
		return ast.TypeField{}, err
	}
	t, err := p.typeExpr()
	if err != nil {
		return ast.TypeField{}, err
	}
	return ast.TypeField{Label: label, Type: t}, nil
}

// tyVarSeq parses the type parameters of a datatype or type
// binding: nothing, "'a", or "('a, 'b)".
func (p *Parser) tyVarSeq() ([]string, error) {
	if p.tok.Kind == token.TyVar {
		name := p.tok.Text
		err := p.next()
		if err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	if p.tok.Kind != token.LParen {
		return nil, nil
	}
	kind, err := p.peek()
	if err != nil {
		return nil, err
	}
	if kind != token.TyVar {
		return nil, nil
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	var names []string
	for {
		err = p.expect(token.TyVar)
		if err != nil {
			return nil, err
		}
		names = append(names, p.tok.Text)
		err = p.next()
		if err != nil {
			return nil, err
		}
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	err = p.expect(token.RParen)
	if err != nil {
		return nil, err
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	return names, nil
}
