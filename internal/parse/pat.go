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

var literalPatOps = map[token.Kind]ast.Op{
	token.IntLit:        ast.IntLiteralPatOp,
	token.RealLit:       ast.RealLiteralPatOp,
	token.ScientificLit: ast.RealLiteralPatOp,
	token.StringLit:     ast.StringLiteralPatOp,
	token.CharLit:       ast.CharLiteralPatOp,
}

// pat parses a pattern: a cons pattern, optionally layered as
// "name as pat".
func (p *Parser) pat() (ast.Pat, error) {
	pat, err := p.consPat()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind != token.As {
		return pat, nil
	}
	id, ok := pat.(*ast.IDPat)
	if !ok {
		return nil, p.errorf("expected name before as")
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	rhs, err := p.pat()
	if err != nil {
		return nil, err
	}
	span := token.Span{
		Start: id.Span().Start,
		End:   rhs.Span().End,
	}
	return ast.NewAsPat(span, id.Name, rhs), nil
}

// consPat parses "atomicPat [:: consPat]"; "::" is
// right-associative.
func (p *Parser) consPat() (ast.Pat, error) {
	pat, err := p.atomicPat()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind != token.Cons {
		return pat, nil
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	rhs, err := p.consPat()
	if err != nil {
		return nil, err
	}
	span := token.Span{
		Start: pat.Span().Start,
		End:   rhs.Span().End,
	}
	return ast.NewConsPat(span, pat, rhs), nil
}

func (p *Parser) atomicPat() (ast.Pat, error) {
	tok := p.tok
	// lint: sort until '^\t}' where '^\tcase '
	switch tok.Kind {
	case token.Ident:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewIDPat(tok.Span, tok.Text), nil
	case token.LBrace:
		return p.recordPat()
	case token.LBracket:
		return p.listPat()
	case token.LParen:
		return p.tuplePat()
	case token.Underscore:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewWildcardPat(tok.Span), nil
	default:
		return p.literalPat()
	}
}

func (p *Parser) literalPat() (ast.Pat, error) {
	tok := p.tok
	op, ok := literalPatOps[tok.Kind]
	if !ok {
		return nil, p.errorf("expected pattern, found " +
			tok.Kind.String())
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
	return ast.NewLiteralPat(tok.Span, op, value), nil
}

// tuplePat parses "()" (the empty tuple pattern), "(p)"
// (grouping), or "(p1, p2, ...)".
func (p *Parser) tuplePat() (ast.Pat, error) {
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
		return ast.NewTuplePat(span, nil), nil
	}
	args, end, err := p.patList(token.RParen)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return args[0], nil
	}
	span := token.Span{Start: start, End: end}
	return ast.NewTuplePat(span, args), nil
}

func (p *Parser) listPat() (ast.Pat, error) {
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
		return ast.NewListPat(span, nil), nil
	}
	args, end, err := p.patList(token.RBracket)
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: end}
	return ast.NewListPat(span, args), nil
}

// patList parses "p1, p2, ..." up to and including the closing
// token.
func (p *Parser) patList(closer token.Kind) ([]ast.Pat,
	token.Pos, error,
) {
	var args []ast.Pat
	for {
		pat, err := p.pat()
		if err != nil {
			return nil, token.Pos{}, err
		}
		args = append(args, pat)
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

// recordPat parses "{f1, f2, ...}" where each field is
// "label = pat" or "label" (implicit: the label is also the
// pattern), and the last field may be "...".
func (p *Parser) recordPat() (ast.Pat, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	var fields []ast.PatField
	ellipsis := false
	for p.tok.Kind != token.RBrace {
		if p.tok.Kind == token.Ellipsis {
			ellipsis = true
			err = p.next()
			if err != nil {
				return nil, err
			}
			break
		}
		var f ast.PatField
		f, err = p.recordPatField()
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
	return ast.NewRecordPat(span, fields, ellipsis), nil
}

func (p *Parser) recordPatField() (ast.PatField, error) {
	err := p.expect(token.Ident)
	if err != nil {
		return ast.PatField{}, err
	}
	label := p.tok
	err = p.next()
	if err != nil {
		return ast.PatField{}, err
	}
	if p.tok.Kind != token.Eq {
		pat := ast.NewIDPat(label.Span, label.Text)
		return ast.PatField{Label: label.Text, Pat: pat}, nil
	}
	err = p.next()
	if err != nil {
		return ast.PatField{}, err
	}
	pat, err := p.pat()
	if err != nil {
		return ast.PatField{}, err
	}
	return ast.PatField{Label: label.Text, Pat: pat}, nil
}
