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

// Parser builds an AST from tokens, with one token of lookahead.
type Parser struct {
	lexer *Lexer
	tok   token.Token
	name  string
}

// NewParser returns a parser over src.
func NewParser(name, src string) (*Parser, error) {
	p := &Parser{lexer: NewLexer(name, src), name: name}
	err := p.next()
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Expr parses src as a single expression.
func Expr(name, src string) (ast.Expr, error) {
	p, err := NewParser(name, src)
	if err != nil {
		return nil, err
	}
	e, err := p.expr()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.EOF)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// Stmt parses src as an expression statement: an expression
// followed by ";".
func Stmt(name, src string) (ast.Expr, error) {
	p, err := NewParser(name, src)
	if err != nil {
		return nil, err
	}
	e, err := p.expr()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.Semi)
	if err != nil {
		return nil, err
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.EOF)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (p *Parser) next() error {
	tok, err := p.lexer.Next()
	if err != nil {
		return err
	}
	p.tok = tok
	return nil
}

func (p *Parser) errorf(msg string) error {
	return &Error{Name: p.name, Span: p.tok.Span, Msg: msg}
}

func (p *Parser) expect(kind token.Kind) error {
	if p.tok.Kind != kind {
		return p.errorf("expected " + kind.String() +
			", found " + p.tok.Kind.String())
	}
	return nil
}

// expr parses an expression: for now, one or more atoms (each
// with postfix field selections); juxtaposition is
// left-associative function application.
func (p *Parser) expr() (ast.Expr, error) {
	e, err := p.atomSuffixed()
	if err != nil {
		return nil, err
	}
	for isAtomStart(p.tok.Kind) {
		arg, err := p.atomSuffixed()
		if err != nil {
			return nil, err
		}
		e = ast.NewApply(spanOver(e, arg), e, arg)
	}
	return e, nil
}

func spanOver(a, b ast.Expr) token.Span {
	return token.Span{Start: a.Span().Start, End: b.Span().End}
}

// atomSuffixed parses an atom followed by field selections:
// "e.f" is the application of the selector "#f" to "e".
func (p *Parser) atomSuffixed() (ast.Expr, error) {
	e, err := p.atom()
	if err != nil {
		return nil, err
	}
	for p.tok.Kind == token.Dot {
		err := p.next()
		if err != nil {
			return nil, err
		}
		err = p.expect(token.Ident)
		if err != nil {
			return nil, err
		}
		field := p.tok
		err = p.next()
		if err != nil {
			return nil, err
		}
		s := ast.NewRecordSelector(field.Span, field.Text)
		span := token.Span{
			Start: e.Span().Start,
			End:   field.Span.End,
		}
		e = ast.NewApply(span, s, e)
	}
	return e, nil
}

var literalOps = map[token.Kind]ast.Op{
	token.IntLit:        ast.IntLiteralOp,
	token.RealLit:       ast.RealLiteralOp,
	token.ScientificLit: ast.RealLiteralOp,
	token.StringLit:     ast.StringLiteralOp,
	token.CharLit:       ast.CharLiteralOp,
}

func isAtomStart(kind token.Kind) bool {
	if _, lit := literalOps[kind]; lit {
		return true
	}
	switch kind {
	case token.Ident, token.Label, token.LParen,
		token.LBracket, token.LBrace:
		return true
	default:
		return false
	}
}

// atom parses an atomic expression: a literal, an identifier, a
// record selector, or a bracketed form.
func (p *Parser) atom() (ast.Expr, error) {
	tok := p.tok
	// lint: sort until '^\t}' where '^\tcase '
	switch tok.Kind {
	case token.Ident:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewID(tok.Span, tok.Text), nil
	case token.LBrace:
		return p.recordExpr()
	case token.LBracket:
		return p.listExpr()
	case token.LParen:
		return p.parenExpr()
	case token.Label:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewRecordSelector(tok.Span, tok.Text[1:]), nil
	default:
		return p.literal()
	}
}

func (p *Parser) literal() (ast.Expr, error) {
	tok := p.tok
	op, ok := literalOps[tok.Kind]
	if !ok {
		return nil, p.errorf("expected expression, found " +
			p.tok.Kind.String())
	}
	err := p.next()
	if err != nil {
		return nil, err
	}
	value := tok.Text
	switch tok.Kind {
	case token.StringLit, token.CharLit:
		value = Unquote(tok.Text)
	default:
	}
	return ast.NewLiteral(tok.Span, op, value), nil
}

// parenExpr parses "()" (unit), "(e)" (grouping), or
// "(e1, e2, ...)" (a tuple).
func (p *Parser) parenExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind == token.RParen {
		span := token.Span{Start: start, End: p.tok.Span.End}
		err = p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewLiteral(span, ast.UnitLiteralOp, "()"), nil
	}
	args, end, err := p.exprList(token.RParen)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return args[0], nil
	}
	span := token.Span{Start: start, End: end}
	return ast.NewTuple(span, args), nil
}

// listExpr parses "[e1, e2, ...]".
func (p *Parser) listExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind == token.RBracket {
		span := token.Span{Start: start, End: p.tok.Span.End}
		err = p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewListExp(span, nil), nil
	}
	args, end, err := p.exprList(token.RBracket)
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: end}
	return ast.NewListExp(span, args), nil
}

// exprList parses "e1, e2, ..." up to and including the closing
// token, returning the expressions and the closer's end position.
func (p *Parser) exprList(closer token.Kind) ([]ast.Expr,
	token.Pos, error,
) {
	var args []ast.Expr
	for {
		e, err := p.expr()
		if err != nil {
			return nil, token.Pos{}, err
		}
		args = append(args, e)
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, token.Pos{}, err
		}
	}
	err := p.expect(closer)
	if err != nil {
		return nil, token.Pos{}, err
	}
	end := p.tok.Span.End
	err = p.next()
	if err != nil {
		return nil, token.Pos{}, err
	}
	return args, end, nil
}

// recordExpr parses "{a = e1, b = e2, ...}"; a field without
// "label =" has an implicit label, filled in during resolution.
func (p *Parser) recordExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	var fields []ast.Field
	for p.tok.Kind != token.RBrace {
		var f ast.Field
		f, err = p.recordField()
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	err = p.expect(token.RBrace)
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: p.tok.Span.End}
	err = p.next()
	if err != nil {
		return nil, err
	}
	return ast.NewRecord(span, fields), nil
}

func (p *Parser) recordField() (ast.Field, error) {
	e, err := p.expr()
	if err != nil {
		return ast.Field{}, err
	}
	if p.tok.Kind != token.Eq {
		return ast.Field{Exp: e}, nil
	}
	id, ok := e.(*ast.ID)
	if !ok {
		return ast.Field{},
			p.errorf("expected label before =")
	}
	err = p.next()
	if err != nil {
		return ast.Field{}, err
	}
	exp, err := p.expr()
	if err != nil {
		return ast.Field{}, err
	}
	return ast.Field{Label: id.Name, Exp: exp}, nil
}
