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
	"strings"

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
	if p.tok.Kind == token.LBracketAt3 {
		// A floating attribute stands alone as an item.
		start := p.tok.Span.Start
		a, err := p.attribute(ast.AttrFloating)
		if err != nil {
			return nil, err
		}
		span := token.Span{Start: start, End: a.Span().End}
		return ast.NewFloatingAttrDecl(span, a), nil
	}
	if isDeclStart(p.tok.Kind) || p.tok.Kind == token.LBracketAt2 {
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

// overChain parses the "over" level: an application-level
// aggregate function, optionally followed by "over" and an
// argument that extends down to level 5, so "sum over e.id +
// e.deptno" aggregates the per-row sums.
func (p *Parser) overChain() (ast.Expr, error) {
	e, err := p.applyChain()
	if err != nil {
		return nil, err
	}
	if p.tok.Kind != token.Over {
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
	return ast.NewInfixCall(spanOver(e, rhs), ast.OverOp, e,
		rhs), nil
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
	var e ast.Expr
	if p.tok.Kind == token.TypeString {
		start := p.tok.Span.Start
		err := p.next()
		if err != nil {
			return nil, err
		}
		// The operand is an atom with field selections (expression9),
		// not a full application, so "type_string f x" is
		// "(type_string f) x" -- x juxtaposes as an argument (a type
		// error).
		operand, err := p.atomSuffixed()
		if err != nil {
			return nil, err
		}
		span := token.Span{Start: start, End: operand.Span().End}
		e = ast.NewTypeStringExp(span, operand)
	} else {
		var err error
		e, err = p.atomSuffixed()
		if err != nil {
			return nil, err
		}
	}
	// Arguments and field selections interleave, so that a selection
	// that follows an argument applies to the application built so
	// far: "(CLOSED (3, 7)).contains 5" may be written without the
	// parentheses, and a postfix method call chains. A bare
	// identifier is the exception -- "f x.y" is "f (x.y)" -- and
	// argAtom keeps it.
	for {
		if isAtomStart(p.tok.Kind) {
			arg, err := p.argAtom()
			if err != nil {
				return nil, err
			}
			e = ast.NewApply(spanOver(e, arg), e, arg)
			continue
		}
		if p.tok.Kind != token.Dot && p.tok.Kind != token.QDot {
			break
		}
		if _, isID := e.(*ast.ID); isID {
			// "x.f" was already taken by the head atom; a name here
			// is a module reference, which the head handles.
			break
		}
		var err error
		e, err = p.selection(e)
		if err != nil {
			return nil, err
		}
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
	return p.suffixed(e)
}

// suffixed applies the field selections that follow an expression.
func (p *Parser) suffixed(e ast.Expr) (ast.Expr, error) {
	for p.tok.Kind == token.Dot || p.tok.Kind == token.QDot {
		var err error
		e, err = p.selection(e)
		if err != nil {
			return nil, err
		}
	}
	// An attribute binds at atom level, after any selection, so
	// "1 + 2 [@a]" attributes the 2 and "(1 + 2) [@a]" the sum.
	attrs, err := p.expAttributes()
	if err != nil {
		return nil, err
	}
	if len(attrs) > 0 {
		span := token.Span{
			Start: e.Span().Start,
			End:   attrs[len(attrs)-1].Span().End,
		}
		e = ast.NewAttributedExp(span, e, attrs)
	}
	return e, nil
}

// argAtom parses an atom in argument position. A bare identifier
// takes its field selections with it, so that "f x.y" is "f (x.y)";
// any other atom leaves them to the enclosing application, so that
// "xs.drop(2).drop 1" reads the second ".drop" as applying to
// "xs.drop(2)" rather than to the "(2)".
func (p *Parser) argAtom() (ast.Expr, error) {
	e, err := p.atom()
	if err != nil {
		return nil, err
	}
	if _, isID := e.(*ast.ID); !isID {
		return e, nil
	}
	return p.suffixed(e)
}

// selection parses one field selection ".f", or the
// safe-navigation form "?.f", applied to an expression.
func (p *Parser) selection(e ast.Expr) (ast.Expr, error) {
	safe := p.tok.Kind == token.QDot
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
	s.Safe = safe
	span := token.Span{
		Start: e.Span().Start,
		End:   field.Span.End,
	}
	return ast.NewApply(span, s, e), nil
}

// expAttributes parses the run of "[@a]" attributes that may
// follow an expression or type atom.
func (p *Parser) expAttributes() ([]*ast.Attribute, error) {
	var attrs []*ast.Attribute
	for p.tok.Kind == token.LBracketAt {
		a, err := p.attribute(ast.AttrExp)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, a)
	}
	return attrs, nil
}

// attribute parses one attribute, from its opening bracket: a
// dotted name, then an optional payload -- an expression, or a
// type after ":" -- then "]".
func (p *Parser) attribute(kind ast.AttributeKind) (
	*ast.Attribute, error,
) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	name, err := p.attributeName()
	if err != nil {
		return nil, err
	}
	var payload ast.Expr
	var typePayload ast.Type
	switch {
	case p.tok.Kind == token.Colon:
		err = p.next()
		if err != nil {
			return nil, err
		}
		typePayload, err = p.typeExpr()
	case p.tok.Kind != token.RBracket:
		payload, err = p.expr()
	}
	if err != nil {
		return nil, err
	}
	err = p.expect(token.RBracket)
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: p.tok.Span.End}
	err = p.next()
	if err != nil {
		return nil, err
	}
	if typePayload != nil {
		return ast.NewAttributeWithType(span, kind, name,
			typePayload), nil
	}
	return ast.NewAttribute(span, kind, name, payload), nil
}

// attributeName parses an attribute's name, which may be dotted:
// "deprecated", "foo.bar".
func (p *Parser) attributeName() (string, error) {
	var b strings.Builder
	for {
		if p.tok.Kind != token.Ident &&
			p.tok.Kind != token.QuotedIdent {
			return "", p.errorf("expected identifier, found " +
				p.tok.Kind.String())
		}
		name := p.tok.Text
		if p.tok.Kind == token.QuotedIdent {
			name = unquoteIdent(name)
		}
		b.WriteString(name)
		err := p.next()
		if err != nil {
			return "", err
		}
		if p.tok.Kind != token.Dot {
			return b.String(), nil
		}
		b.WriteString(".")
		err = p.next()
		if err != nil {
			return "", err
		}
	}
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
		token.QuotedIdent, token.Raise:
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
	case token.Current, token.Ordinal:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewKeywordID(tok.Span, tok.Text), nil
	case token.Elements:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return ast.NewElements(tok.Span), nil
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
	case token.Raise:
		return p.raiseExpr()
	default:
		return p.literal()
	}
}

// raiseExpr parses "raise e".
func (p *Parser) raiseExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	e, err := p.expr()
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: e.Span().End}
	return ast.NewRaise(span, e), nil
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
	span := token.Span{Start: start, End: end}
	if len(args) == 1 {
		ast.SetSpan(args[0], span)
		return args[0], nil
	}
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
	var items []ast.RangeItem
	for {
		var item ast.RangeItem
		item, err = p.rangeListItem()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	err = p.expect(token.RBracket)
	if err != nil {
		return nil, err
	}
	span := token.Span{Start: start, End: p.tok.Span.End}
	err = p.next()
	if err != nil {
		return nil, err
	}
	// A list whose items are all points is an ordinary list; only
	// a list with a range item is a RangeList.
	allPoints := true
	for _, it := range items {
		if it.Kind != ast.RangePoint {
			allPoints = false
			break
		}
	}
	if allPoints {
		args := make([]ast.Expr, len(items))
		for i, it := range items {
			args[i] = it.Lo
		}
		return ast.NewListExp(span, args), nil
	}
	return ast.NewRangeList(span, items), nil
}

// rangeListItem parses one item of a list: a plain expression
// (a point), a bounded range "e .. e2" (with "..", "..^", "^..",
// or "^..^"), or an unbounded form such as "e .." or "..^ e".
func (p *Parser) rangeListItem() (ast.RangeItem, error) {
	// Leading-".." forms have no lower bound.
	if p.tok.Kind == token.DotDot || p.tok.Kind == token.DotDotCaret {
		return p.leadingRangeItem()
	}
	lo, err := p.expr()
	if err != nil {
		return ast.RangeItem{}, err
	}
	return p.rangeItemAfterLo(lo)
}

// leadingRangeItem parses "[.. ]" (ALL), "[.. e]" (AT_MOST), or
// "[..^ e]" (LESS_THAN); the current token is "..' or "..^".
func (p *Parser) leadingRangeItem() (ast.RangeItem, error) {
	caret := p.tok.Kind == token.DotDotCaret
	err := p.next()
	if err != nil {
		return ast.RangeItem{}, err
	}
	if !caret && p.atRangeItemEnd() {
		return ast.RangeItem{Kind: ast.RangeAll}, nil
	}
	hi, err := p.expr()
	if err != nil {
		return ast.RangeItem{}, err
	}
	kind := ast.RangeAtMost
	if caret {
		kind = ast.RangeLessThan
	}
	return ast.RangeItem{Kind: kind, Hi: hi}, nil
}

// rangeItemAfterLo parses the rest of an expression-first item,
// given its lower bound: a point, a bounded interval, or an
// upper-unbounded form.
func (p *Parser) rangeItemAfterLo(lo ast.Expr) (ast.RangeItem,
	error,
) {
	var bounded, unbounded ast.RangeKind
	upperClosed := false
	// lint: sort until '^\t}' where '^\tcase '
	switch p.tok.Kind {
	case token.CaretDotDot:
		bounded = ast.RangeOpenClosed
		unbounded = ast.RangeGreaterThan
		upperClosed = true
	case token.CaretDotDotCaret:
		bounded = ast.RangeOpen
	case token.DotDot:
		bounded = ast.RangeClosed
		unbounded = ast.RangeAtLeast
		upperClosed = true
	case token.DotDotCaret:
		bounded = ast.RangeClosedOpen
	default:
		return ast.RangeItem{Kind: ast.RangePoint, Lo: lo}, nil
	}
	err := p.next()
	if err != nil {
		return ast.RangeItem{}, err
	}
	// ".." and "^.." may be unbounded above; "..^" and "^..^"
	// always take an upper bound.
	if upperClosed && p.atRangeItemEnd() {
		return ast.RangeItem{Kind: unbounded, Lo: lo}, nil
	}
	hi, err := p.expr()
	if err != nil {
		return ast.RangeItem{}, err
	}
	return ast.RangeItem{Kind: bounded, Lo: lo, Hi: hi}, nil
}

// atRangeItemEnd reports whether the current token ends a range
// item without an upper bound (a comma or the closing bracket).
func (p *Parser) atRangeItemEnd() bool {
	return p.tok.Kind == token.Comma || p.tok.Kind == token.RBracket
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

// recordExpr parses "{a = e1, b = e2, ...}", or a base
// expression followed by the modifiers applied to it,
// "{e replace a = e1 remove b}". A field without "label =" has
// an implicit label, filled in during resolution.
func (p *Parser) recordExpr() (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	var fields []ast.Field
	for p.tok.Kind != token.RBrace && !isModifierStart(p.tok.Kind) {
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
	var modifiers []ast.Modifier
	for isModifierStart(p.tok.Kind) {
		var m ast.Modifier
		m, err = p.recordModifier()
		if err != nil {
			return nil, err
		}
		modifiers = append(modifiers, m)
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
	if len(modifiers) == 0 {
		return ast.NewRecord(span, fields), nil
	}
	return ast.NewRecordModify(span, fields, modifiers), nil
}

// isModifierStart reports whether kind is the first verb of a
// record modifier.
func isModifierStart(kind token.Kind) bool {
	// lint: sort until '^	}' where '^	case '
	switch kind {
	case token.Extend, token.Remove, token.Rename,
		token.Replace:
		return true
	default:
		return false
	}
}

// atWord reports whether the current token is the given
// non-reserved keyword. Such a keyword is an ordinary identifier
// to the lexer, and is a keyword only here, after a modifier
// verb, where no identifier can occur.
func (p *Parser) atWord(word string) bool {
	return p.tok.Kind == token.Ident && p.tok.Text == word
}

// recordModifier parses one modifier of a record expression,
// such as "replace a = 1, b = 2", "extend or replace all r",
// "remove or skip a" or "rename b = a".
//
// A modifier is one or two verbs -- the second joined by "or",
// naming the case the first does not -- an optional "lenient",
// an optional "all", and an operand.
func (p *Parser) recordModifier() (ast.Modifier, error) {
	start := p.tok.Span.Start
	kind := p.tok.Kind
	err := p.next()
	if err != nil {
		return nil, err
	}
	// lint: sort until '^	}' where '^	case '
	switch kind {
	case token.Extend:
		return p.extendModifier(start)
	case token.Remove:
		return p.removeModifier(start)
	case token.Replace:
		return p.replaceModifier(start)
	default:
		return p.renameModifier(start)
	}
}

// extendModifier parses an "extend" modifier, after its verb:
// an optional "or skip" or "or replace [lenient]", then an
// operand.
func (p *Parser) extendModifier(start token.Pos) (ast.Modifier,
	error,
) {
	verb := ast.ExtendVerb
	lenient := false
	if p.atWord("or") {
		var err error
		verb, lenient, err = p.extendOr()
		if err != nil {
			return nil, err
		}
	}
	return p.modifierOperand(start, verb, lenient)
}

// extendOr parses the "or skip" or "or replace [lenient]" that
// may follow "extend", at the "or", and returns the verb the
// pair names.
func (p *Parser) extendOr() (ast.ModifierVerb, bool, error) {
	err := p.next()
	if err != nil {
		return 0, false, err
	}
	var verb ast.ModifierVerb
	// lint: sort until '^	}' where '^	case '
	switch p.tok.Kind {
	case token.Replace:
		verb = ast.ExtendOrReplaceVerb
	case token.Skip:
		verb = ast.ExtendOrSkipVerb
	default:
		return 0, false, p.errorf("expected skip or replace, " +
			"found " + p.tok.Kind.String())
	}
	err = p.next()
	if err != nil {
		return 0, false, err
	}
	if verb != ast.ExtendOrReplaceVerb || !p.atWord("lenient") {
		return verb, false, nil
	}
	err = p.next()
	if err != nil {
		return 0, false, err
	}
	return verb, true, nil
}

// orSkip parses the "or skip" that may follow "replace" or
// "remove", and reports whether it was there.
func (p *Parser) orSkip() (bool, error) {
	if !p.atWord("or") {
		return false, nil
	}
	err := p.next()
	if err != nil {
		return false, err
	}
	err = p.expect(token.Skip)
	if err != nil {
		return false, err
	}
	return true, p.next()
}

// replaceModifier parses a "replace" modifier, after its verb:
// an optional "lenient", an optional "or skip", then an operand.
func (p *Parser) replaceModifier(start token.Pos) (ast.Modifier,
	error,
) {
	verb := ast.ReplaceVerb
	lenient := p.atWord("lenient")
	if lenient {
		err := p.next()
		if err != nil {
			return nil, err
		}
	}
	skip, err := p.orSkip()
	if err != nil {
		return nil, err
	}
	if skip {
		verb = ast.ReplaceOrSkipVerb
	}
	return p.modifierOperand(start, verb, lenient)
}

// removeModifier parses a "remove" modifier, after its verb: an
// optional "or skip", then the labels to remove.
func (p *Parser) removeModifier(start token.Pos) (ast.Modifier,
	error,
) {
	verb := ast.RemoveVerb
	skip, err := p.orSkip()
	if err != nil {
		return nil, err
	}
	if skip {
		verb = ast.RemoveOrSkipVerb
	}
	var labels []ast.Label
	for {
		label, err := p.recordLabel()
		if err != nil {
			return nil, err
		}
		labels = append(labels, label)
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	span := token.Span{
		Start: start, End: labels[len(labels)-1].Span.End,
	}
	return ast.NewRemoveModifier(span, verb, labels), nil
}

// renameModifier parses a "rename" modifier, after its verb: a
// list of "newLabel = oldLabel" pairs.
func (p *Parser) renameModifier(start token.Pos) (ast.Modifier,
	error,
) {
	var pairs []ast.RenamePair
	for {
		to, err := p.recordLabel()
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
		from, err := p.recordLabel()
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, ast.RenamePair{To: to, From: from})
		if p.tok.Kind != token.Comma {
			break
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	span := token.Span{
		Start: start, End: pairs[len(pairs)-1].From.Span.End,
	}
	return ast.NewRenameModifier(span, pairs), nil
}

// atAll reports whether the current token is the "all" keyword
// of a modifier operand. "all" followed by "=" is a label.
func (p *Parser) atAll() (bool, error) {
	if !p.atWord("all") {
		return false, nil
	}
	kind, err := p.peek()
	if err != nil {
		return false, err
	}
	return kind != token.Eq, nil
}

// modifierOperand parses the operand of an "extend" or
// "replace" modifier: "all" and a record-valued expression, or
// a list of assignments.
func (p *Parser) modifierOperand(start token.Pos,
	verb ast.ModifierVerb, lenient bool,
) (ast.Modifier, error) {
	all, err := p.atAll()
	if err != nil {
		return nil, err
	}
	if all {
		err = p.next()
		if err != nil {
			return nil, err
		}
		var exp ast.Expr
		exp, err = p.expr()
		if err != nil {
			return nil, err
		}
		span := token.Span{Start: start, End: exp.Span().End}
		return ast.NewAllModifier(span, verb, lenient, exp), nil
	}
	var fields []ast.Field
	for {
		f, err := p.recordField()
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
	span := token.Span{
		Start: start,
		End:   fields[len(fields)-1].Exp.Span().End,
	}
	return ast.NewAssignModifier(span, verb, lenient, fields), nil
}

// recordLabel parses the label of a "remove" or "rename"
// modifier: a name, a backtick-quoted identifier, or a positive
// integer.
func (p *Parser) recordLabel() (ast.Label, error) {
	name := p.tok.Text
	switch p.tok.Kind {
	case token.Ident, token.IntLit:
	case token.QuotedIdent:
		name = unquoteIdent(name)
	default:
		return ast.Label{}, p.errorf("expected label, found " +
			p.tok.Kind.String())
	}
	label := ast.Label{Name: name, Span: p.tok.Span}
	err := p.next()
	if err != nil {
		return ast.Label{}, err
	}
	return label, nil
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
