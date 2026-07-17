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

// Parser builds an AST from tokens, with up to two tokens of
// lookahead.
type Parser struct {
	lexer *Lexer
	ahead *token.Token
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

// Stmt parses src as a statement: a declaration or an expression,
// followed by ";".
func Stmt(name, src string) (ast.Node, error) {
	p, err := NewParser(name, src)
	if err != nil {
		return nil, err
	}
	n, err := p.declOrExpr()
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
	return n, nil
}

// TypeString parses src as a type expression.
func TypeString(name, src string) (ast.Type, error) {
	p, err := NewParser(name, src)
	if err != nil {
		return nil, err
	}
	t, err := p.typeExpr()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.EOF)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// DeclOrExpr parses src as a declaration or an expression.
func DeclOrExpr(name, src string) (ast.Node, error) {
	p, err := NewParser(name, src)
	if err != nil {
		return nil, err
	}
	n, err := p.declOrExpr()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.EOF)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (p *Parser) declOrExpr() (ast.Node, error) {
	if isDeclStart(p.tok.Kind) {
		return p.decl()
	}
	return p.expr()
}

func (p *Parser) next() error {
	if p.ahead != nil {
		p.tok = *p.ahead
		p.ahead = nil
		return nil
	}
	tok, err := p.lexer.Next()
	if err != nil {
		return err
	}
	p.tok = tok
	return nil
}

// peek returns the kind of the token after the current one.
func (p *Parser) peek() (token.Kind, error) {
	if p.ahead == nil {
		tok, err := p.lexer.Next()
		if err != nil {
			return token.EOF, err
		}
		p.ahead = &tok
	}
	return p.ahead.Kind, nil
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

// Operator tables by precedence level. Level 5 (cons, at) is
// right-associative and level 6.5
// (negate) is prefix; both are special-cased below.
var (
	level0Ops = map[token.Kind]ast.Op{
		token.Implies: ast.ImpliesOp,
	}
	level1Ops = map[token.Kind]ast.Op{
		token.Orelse: ast.OrelseOp,
	}
	level2Ops = map[token.Kind]ast.Op{
		token.Andalso: ast.AndalsoOp,
	}
	level3Ops = map[token.Kind]ast.Op{
		token.O: ast.ComposeOp,
	}
	level4Ops = map[token.Kind]ast.Op{
		token.Le:      ast.LeOp,
		token.Lt:      ast.LtOp,
		token.Ge:      ast.GeOp,
		token.Gt:      ast.GtOp,
		token.Eq:      ast.EqOp,
		token.Ne:      ast.NeOp,
		token.Elem:    ast.ElemOp,
		token.Notelem: ast.NotElemOp,
	}
	level5Ops = map[token.Kind]ast.Op{
		token.Cons: ast.ConsOp,
		token.At:   ast.AtOp,
	}
	level6Ops = map[token.Kind]ast.Op{
		token.Plus:  ast.PlusOp,
		token.Minus: ast.MinusOp,
		token.Caret: ast.CaretOp,
	}
	level7Ops = map[token.Kind]ast.Op{
		token.Star:  ast.TimesOp,
		token.Slash: ast.DivideOp,
		token.Div:   ast.DivOp,
		token.Mod:   ast.ModOp,
	}
	overOps = map[token.Kind]ast.Op{
		token.Over: ast.OverOp,
	}
	// opNames maps an operator token to the name of the binding it
	// denotes when written as a value: "op +" is the curried form
	// of the "+" operator, "List.foldl (op +) 0 xs".
	opNames = map[token.Kind]string{
		token.At:    "op @",
		token.Caret: "op ^",
		token.Cons:  "op ::",
		token.Div:   "op div",
		token.Eq:    "op =",
		token.Ge:    "op >=",
		token.Gt:    "op >",
		token.Le:    "op <=",
		token.Lt:    "op <",
		token.Minus: "op -",
		token.Mod:   "op mod",
		token.Ne:    "op <>",
		token.O:     "op o",
		token.Plus:  "op +",
		token.Slash: "op /",
		token.Star:  "op *",
		token.Tilde: "op ~",
	}
)

func (p *Parser) expr() (ast.Expr, error) {
	e, err := p.expr0()
	if err != nil {
		return nil, err
	}
	// A type annotation, "e : t", binds loosest of all.
	for p.tok.Kind == token.Colon {
		err = p.next()
		if err != nil {
			return nil, err
		}
		t, err := p.typeExpr()
		if err != nil {
			return nil, err
		}
		span := token.Span{
			Start: e.Span().Start,
			End:   t.Span().End,
		}
		e = ast.NewAnnotatedExp(span, e, t)
	}
	return e, nil
}

func (p *Parser) expr0() (ast.Expr, error) {
	return p.leftChain(level0Ops, func() (ast.Expr, error) {
		return p.leftChain(level1Ops, func() (ast.Expr, error) {
			return p.leftChain(level2Ops,
				func() (ast.Expr, error) {
					return p.leftChain(level3Ops, p.expr4)
				})
		})
	})
}

func (p *Parser) expr4() (ast.Expr, error) {
	return p.leftChain(level4Ops, p.expr5)
}

// expr5 parses the right-associative cons/append level.
func (p *Parser) expr5() (ast.Expr, error) {
	e, err := p.expr6()
	if err != nil {
		return nil, err
	}
	op, ok := level5Ops[p.tok.Kind]
	if !ok {
		return e, nil
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	rhs, err := p.expr5()
	if err != nil {
		return nil, err
	}
	return ast.NewInfixCall(spanOver(e, rhs), op, e, rhs), nil
}

func (p *Parser) expr6() (ast.Expr, error) {
	return p.leftChain(level6Ops, p.negate7)
}

// negate7 parses an optional prefix "~" whose operand is a whole
// multiplicative chain: "~x * 2" is the negation of "x * 2", but
// "~a + b" negates only "a".
func (p *Parser) negate7() (ast.Expr, error) {
	if p.tok.Kind != token.Tilde {
		return p.expr7()
	}
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	e, err := p.expr7()
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: e.Span().End}
	return ast.NewPrefixCall(span, ast.NegateOp, e), nil
}

func (p *Parser) expr7() (ast.Expr, error) {
	return p.leftChain(level7Ops, p.overChain)
}

// overChain parses the "over" level (between the multiplicative
// level and application), used by aggregate expressions such as
// "count over e".
func (p *Parser) overChain() (ast.Expr, error) {
	return p.leftChain(overOps, p.applyChain)
}

// leftChain parses "next {op next}" for a left-associative
// operator level.
func (p *Parser) leftChain(ops map[token.Kind]ast.Op,
	next func() (ast.Expr, error),
) (ast.Expr, error) {
	e, err := next()
	if err != nil {
		return nil, err
	}
	for {
		op, ok := ops[p.tok.Kind]
		if !ok {
			return e, nil
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
		rhs, err := next()
		if err != nil {
			return nil, err
		}
		e = ast.NewInfixCall(spanOver(e, rhs), op, e, rhs)
	}
}

// applyChain parses one or more atoms (each with postfix field
// selections); juxtaposition is left-associative application.
func (p *Parser) applyChain() (ast.Expr, error) {
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
		if p.tok.Kind != token.Ident &&
			p.tok.Kind != token.IntLit &&
			p.tok.Kind != token.QuotedIdent {
			return nil, p.errorf("expected identifier, found " +
				p.tok.Kind.String())
		}
		field := p.tok
		fieldName := field.Text
		if field.Kind == token.QuotedIdent {
			fieldName = unquoteIdent(field.Text)
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
		s := ast.NewRecordSelector(field.Span, fieldName)
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
	token.WordLit:       ast.WordLiteralOp,
	token.StringLit:     ast.StringLiteralOp,
	token.CharLit:       ast.CharLiteralOp,
}

func isAtomStart(kind token.Kind) bool {
	if _, lit := literalOps[kind]; lit {
		return true
	}
	switch kind {
	case token.Case, token.Current, token.Elements,
		token.Exists, token.Fn, token.Forall, token.From,
		token.Ident, token.If, token.Label, token.LParen,
		token.LBracket, token.LBrace, token.Let, token.Ordinal,
		token.QuotedIdent:
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
	case token.Case:
		return p.caseExpr()
	case token.Current, token.Elements, token.Ordinal:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewID(tok.Span, tok.Text), nil
	case token.Exists:
		return p.fromExpr(ast.ExistsOp)
	case token.Fn:
		return p.fnExpr()
	case token.Forall:
		return p.fromExpr(ast.ForallOp)
	case token.From:
		return p.fromExpr(ast.FromOp)
	case token.Ident:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewID(tok.Span, tok.Text), nil
	case token.If:
		return p.ifExpr()
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
	case token.Let:
		return p.letExpr()
	case token.Op:
		return p.opExpr()
	case token.QuotedIdent:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewID(tok.Span,
			unquoteIdent(tok.Text)), nil
	default:
		return p.literal()
	}
}

// opExpr parses "op <operator>", an operator used as a first-class
// value. "op +" denotes the binding "op +", the curried form of the
// "+" operator, so it can be passed as an argument. Any other name
// keeps its "op " prefix too, so "op foo" resolves against — and,
// when unbound, reports — the name "op foo".
func (p *Parser) opExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	tok := p.tok
	span := token.Span{Start: start, End: tok.Span.End}
	err = p.next()
	if err != nil {
		return nil, err
	}
	if tok.Kind == token.Ident {
		return ast.NewID(span, "op "+tok.Text), nil
	}
	name, ok := opNames[tok.Kind]
	if !ok {
		return nil, p.errorf("expected operator after 'op'")
	}
	return ast.NewID(span, name), nil
}

// ifExpr parses "if e1 then e2 else e3".
func (p *Parser) ifExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	cond, err := p.expr()
	if err != nil {
		return nil, err
	}
	ifTrue, err := p.keywordExpr(token.Then)
	if err != nil {
		return nil, err
	}
	ifFalse, err := p.keywordExpr(token.Else)
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: ifFalse.Span().End}
	return ast.NewIf(span, cond, ifTrue, ifFalse), nil
}

// keywordExpr consumes the given keyword and parses the
// expression that follows it.
func (p *Parser) keywordExpr(kw token.Kind) (ast.Expr, error) {
	err := p.expect(kw)
	if err != nil {
		return nil, err
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	return p.expr()
}

// fnExpr parses "fn match | match ...".
func (p *Parser) fnExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	matches, err := p.matchList()
	if err != nil {
		return nil, err
	}
	last := matches[len(matches)-1]
	span := token.Span{Start: start, End: last.Span().End}
	return ast.NewFn(span, matches), nil
}

// caseExpr parses "case e of match | match ...".
func (p *Parser) caseExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	exp, err := p.expr()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.Of)
	if err != nil {
		return nil, err
	}
	err = p.next()
	if err != nil {
		return nil, err
	}
	matches, err := p.matchList()
	if err != nil {
		return nil, err
	}
	last := matches[len(matches)-1]
	span := token.Span{Start: start, End: last.Span().End}
	return ast.NewCase(span, exp, matches), nil
}

// matchList parses "match | match | ..." where each match is
// "pat => exp".
func (p *Parser) matchList() ([]*ast.Match, error) {
	var matches []*ast.Match
	for {
		m, err := p.match()
		if err != nil {
			return nil, err
		}
		matches = append(matches, m)
		if p.tok.Kind != token.Bar {
			return matches, nil
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
}

func (p *Parser) match() (*ast.Match, error) {
	pat, err := p.pat()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.RArrow)
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
	return ast.NewMatch(span, pat, exp), nil
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
	var with ast.Expr
	var fields []ast.Field
	first := true
	for p.tok.Kind != token.RBrace {
		var f ast.Field
		f, err = p.recordField()
		if err != nil {
			return nil, err
		}
		if first && f.Label == "" &&
			p.tok.Kind == token.With {
			with = f.Exp
			err = p.next()
			if err != nil {
				return nil, err
			}
			first = false
			continue
		}
		first = false
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
	r := ast.NewRecord(span, fields)
	r.With = with
	return r, nil
}

func (p *Parser) recordField() (ast.Field, error) {
	if p.labelStart() {
		kind, err := p.peek()
		if err != nil {
			return ast.Field{}, err
		}
		if kind == token.Eq {
			return p.labeledField()
		}
	}
	e, err := p.expr()
	if err != nil {
		return ast.Field{}, err
	}
	return ast.Field{Exp: e}, nil
}

// labelStart reports whether the current token can begin a record
// field's label: a name, a backtick-quoted identifier, or a
// canonical positive integer ("1", "2", ...). A numeric label
// with a leading zero ("0", "007") is not a label, so "{0 = 0}"
// parses as a record over the expression "0 = 0".
func (p *Parser) labelStart() bool {
	// lint: sort until '^	}' where '^	case '
	switch p.tok.Kind {
	case token.Ident, token.QuotedIdent:
		return true
	case token.IntLit:
		return p.tok.Text[0] != '0'
	default:
		return false
	}
}

func (p *Parser) labeledField() (ast.Field, error) {
	label := p.tok.Text
	if p.tok.Kind == token.QuotedIdent {
		label = unquoteIdent(label)
	}
	labelSpan := p.tok.Span
	err := p.next()
	if err != nil {
		return ast.Field{}, err
	}
	err = p.next()
	if err != nil {
		return ast.Field{}, err
	}
	exp, err := p.expr()
	if err != nil {
		return ast.Field{}, err
	}
	return ast.Field{
		Label:     label,
		LabelSpan: labelSpan,
		Exp:       exp,
	}, nil
}
