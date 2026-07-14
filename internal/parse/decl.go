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

func isDeclStart(kind token.Kind) bool {
	// lint: sort until '^\t}' where '^\tcase '
	switch kind {
	case token.Datatype:
		return true
	case token.Fun:
		return true
	case token.Type:
		return true
	case token.Val:
		return true
	default:
		return false
	}
}

func (p *Parser) decl() (ast.Decl, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p.tok.Kind {
	case token.Datatype:
		return p.datatypeDecl()
	case token.Fun:
		return p.funDecl()
	case token.Type:
		return p.typeDecl()
	case token.Val:
		return p.valDecl()
	default:
		return nil, p.errorf("expected declaration, found " +
			p.tok.Kind.String())
	}
}

// datatypeDecl parses "datatype bind [and bind ...]" where each
// bind is "[tyvars] name = con [| con ...]" and each con is
// "Name [of type]".
func (p *Parser) datatypeDecl() (ast.Decl, error) {
	start := p.tok.Span.Start
	end := p.tok.Span.End
	err := p.next()
	if err != nil {
		return nil, err
	}
	var binds []ast.DatatypeBind
	for {
		var bind ast.DatatypeBind
		bind, err = p.datatypeBind()
		if err != nil {
			return nil, err
		}
		binds = append(binds, bind)
		if p.tok.Kind != token.And {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	span := token.Span{Start: start, End: end}
	return ast.NewDatatypeDecl(span, binds), nil
}

func (p *Parser) datatypeBind() (ast.DatatypeBind, error) {
	tyVars, err := p.tyVarSeq()
	if err != nil {
		return ast.DatatypeBind{}, err
	}
	err = p.expect(token.Ident)
	if err != nil {
		return ast.DatatypeBind{}, err
	}
	name := p.tok.Text
	err = p.next()
	if err != nil {
		return ast.DatatypeBind{}, err
	}
	err = p.expect(token.Eq)
	if err != nil {
		return ast.DatatypeBind{}, err
	}
	err = p.next()
	if err != nil {
		return ast.DatatypeBind{}, err
	}
	var cons []ast.ConBind
	for {
		var con ast.ConBind
		con, err = p.conBind()
		if err != nil {
			return ast.DatatypeBind{}, err
		}
		cons = append(cons, con)
		if p.tok.Kind != token.Bar {
			break
		}
		err = p.next()
		if err != nil {
			return ast.DatatypeBind{}, err
		}
	}
	return ast.DatatypeBind{
		TyVars: tyVars, Name: name,
		Cons: cons,
	}, nil
}

func (p *Parser) conBind() (ast.ConBind, error) {
	err := p.expect(token.Ident)
	if err != nil {
		return ast.ConBind{}, err
	}
	name := p.tok.Text
	err = p.next()
	if err != nil {
		return ast.ConBind{}, err
	}
	if p.tok.Kind != token.Of {
		return ast.ConBind{Name: name}, nil
	}
	err = p.next()
	if err != nil {
		return ast.ConBind{}, err
	}
	t, err := p.typeExpr()
	if err != nil {
		return ast.ConBind{}, err
	}
	return ast.ConBind{Name: name, Of: t}, nil
}

// typeDecl parses "type bind [and bind ...]" where each bind is
// "[tyvars] name = type".
func (p *Parser) typeDecl() (ast.Decl, error) {
	start := p.tok.Span.Start
	end := p.tok.Span.End
	err := p.next()
	if err != nil {
		return nil, err
	}
	var binds []ast.TypeBind
	for {
		var bind ast.TypeBind
		bind, err = p.typeBind()
		if err != nil {
			return nil, err
		}
		binds = append(binds, bind)
		if p.tok.Kind != token.And {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	span := token.Span{Start: start, End: end}
	return ast.NewTypeDecl(span, binds), nil
}

func (p *Parser) typeBind() (ast.TypeBind, error) {
	tyVars, err := p.tyVarSeq()
	if err != nil {
		return ast.TypeBind{}, err
	}
	err = p.expect(token.Ident)
	if err != nil {
		return ast.TypeBind{}, err
	}
	name := p.tok.Text
	err = p.next()
	if err != nil {
		return ast.TypeBind{}, err
	}
	err = p.expect(token.Eq)
	if err != nil {
		return ast.TypeBind{}, err
	}
	err = p.next()
	if err != nil {
		return ast.TypeBind{}, err
	}
	t, err := p.typeExpr()
	if err != nil {
		return ast.TypeBind{}, err
	}
	return ast.TypeBind{TyVars: tyVars, Name: name, Type: t}, nil
}

// valDecl parses "val [rec] pat = exp [and pat = exp ...]".
func (p *Parser) valDecl() (ast.Decl, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	rec := false
	if p.tok.Kind == token.Rec {
		rec = true
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	var binds []*ast.ValBind
	for {
		var bind *ast.ValBind
		bind, err = p.valBind()
		if err != nil {
			return nil, err
		}
		binds = append(binds, bind)
		if p.tok.Kind != token.And {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	last := binds[len(binds)-1]
	span := token.Span{Start: start, End: last.Span().End}
	return ast.NewValDecl(span, rec, binds), nil
}

func (p *Parser) valBind() (*ast.ValBind, error) {
	pat, err := p.pat()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.Eq)
	if err != nil {
		return nil, err
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	exp, err := p.expr()
	if err != nil {
		return nil, err
	}
	span := token.Span{
		Start: pat.Span().Start,
		End:   exp.Span().End,
	}
	return ast.NewValBind(span, pat, exp), nil
}

// funDecl parses "fun bind [and bind ...]" where each bind is
// "clause [| clause ...]" and each clause is "name pat... = exp".
func (p *Parser) funDecl() (ast.Decl, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	var binds []*ast.FunBind
	for {
		var bind *ast.FunBind
		bind, err = p.funBind()
		if err != nil {
			return nil, err
		}
		binds = append(binds, bind)
		if p.tok.Kind != token.And {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	last := binds[len(binds)-1]
	span := token.Span{Start: start, End: last.Span().End}
	return ast.NewFunDecl(span, binds), nil
}

func (p *Parser) funBind() (*ast.FunBind, error) {
	var matches []*ast.FunMatch
	for {
		m, err := p.funMatch()
		if err != nil {
			return nil, err
		}
		matches = append(matches, m)
		if p.tok.Kind != token.Bar {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	last := matches[len(matches)-1]
	span := token.Span{
		Start: matches[0].Span().Start,
		End:   last.Span().End,
	}
	return ast.NewFunBind(span, matches), nil
}

func (p *Parser) funMatch() (*ast.FunMatch, error) {
	if p.tok.Kind != token.Ident &&
		p.tok.Kind != token.QuotedIdent {
		return nil, p.errorf("expected identifier, found " +
			p.tok.Kind.String())
	}
	name := p.tok
	fnName := name.Text
	if name.Kind == token.QuotedIdent {
		fnName = unquoteIdent(name.Text)
	}
	err := p.next()
	if err != nil {
		return nil, err
	}
	var pats []ast.Pat
	for p.tok.Kind != token.Eq && p.tok.Kind != token.Colon {
		var pat ast.Pat
		pat, err = p.atomicPat()
		if err != nil {
			return nil, err
		}
		pats = append(pats, pat)
	}
	var returnType ast.Type
	if p.tok.Kind == token.Colon {
		err = p.next()
		if err != nil {
			return nil, err
		}
		returnType, err = p.typeExpr()
		if err != nil {
			return nil, err
		}
	}
	err = p.expect(token.Eq)
	if err != nil {
		return nil, err
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	exp, err := p.expr()
	if err != nil {
		return nil, err
	}
	span := token.Span{
		Start: name.Span.Start,
		End:   exp.Span().End,
	}
	return ast.NewFunMatch(span, fnName, pats, returnType,
		exp), nil
}

// letExpr parses "let decl ... in exp end"; declarations may be
// separated by ";".
func (p *Parser) letExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	var decls []ast.Decl
	for p.tok.Kind != token.In {
		if p.tok.Kind == token.Semi {
			err = p.next()
			if err != nil {
				return nil, err
			}
			continue
		}
		var d ast.Decl
		d, err = p.decl()
		if err != nil {
			return nil, err
		}
		decls = append(decls, d)
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	exp, err := p.expr()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.End)
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: p.tok.Span.End}
	err = p.next()
	if err != nil {
		return nil, err
	}
	return ast.NewLet(span, decls, exp), nil
}
