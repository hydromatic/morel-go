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

// MicroCall recognizes a statement of the exact shape
// `A.b "str";` — the application of a dotted name to one string
// literal. It returns the dotted name, the decoded string, and
// whether the statement matched.
func MicroCall(name, src string) (string, string, bool) {
	l := NewLexer(name, src)
	var toks []token.Token
	for {
		tok, err := l.Next()
		if err != nil {
			return "", "", false
		}
		if tok.Kind == token.EOF {
			break
		}
		toks = append(toks, tok)
	}
	kinds := []token.Kind{
		token.Ident, token.Dot, token.Ident,
		token.StringLit, token.Semi,
	}
	if len(toks) != len(kinds) {
		return "", "", false
	}
	for i, k := range kinds {
		if toks[i].Kind != k {
			return "", "", false
		}
	}
	return toks[0].Text + "." + toks[2].Text,
		Unquote(toks[3].Text), true
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

// expr parses an expression: for now, one or more atoms;
// juxtaposition is left-associative function application.
func (p *Parser) expr() (ast.Expr, error) {
	e, err := p.atom()
	if err != nil {
		return nil, err
	}
	for isAtomStart(p.tok.Kind) {
		arg, err := p.atom()
		if err != nil {
			return nil, err
		}
		span := token.Span{
			Start: e.Span().Start,
			End:   arg.Span().End,
		}
		e = ast.NewApply(span, e, arg)
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
	_, lit := literalOps[kind]
	return lit || kind == token.Ident
}

// atom parses an atomic expression: a literal or an identifier.
func (p *Parser) atom() (ast.Expr, error) {
	tok := p.tok
	if op, ok := literalOps[tok.Kind]; ok {
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
	if tok.Kind == token.Ident {
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewID(tok.Span, tok.Text), nil
	}
	return nil, p.errorf("expected expression, found " +
		p.tok.Kind.String())
}
