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

package datalog

import (
	"slices"
	"strconv"
	"strings"
)

// Parse parses a Datalog program. A parse error's message
// reproduces javacc's ParseException format — the "Encountered
// ... Was expecting" text — which the corpus pins.
func Parse(src string) (*Program, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	var stmts []Statement
	for p.cur().kind != tkEOF {
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
	}
	return NewProgram(stmts), nil
}

// parser is a recursive-descent parser over the token stream.
type parser struct {
	toks []token
	i    int
}

func (p *parser) cur() token { return p.toks[p.i] }

func (p *parser) peek() token {
	if p.i+1 < len(p.toks) {
		return p.toks[p.i+1]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) advance() token {
	tok := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return tok
}

// expect consumes a token of the given kind or fails.
func (p *parser) expect(kind tokKind) (token, error) {
	if p.cur().kind != kind {
		return token{}, p.parseError(kind)
	}
	return p.advance(), nil
}

// parseError reports the current token against the expected
// kinds, in javacc's ParseException format. Expected kinds are
// listed in grammar token order, as javacc does.
func (p *parser) parseError(expected ...tokKind) error {
	slices.Sort(expected)
	expected = slices.Compact(expected)
	tok := p.cur()
	img := "<EOF>"
	if tok.kind != tkEOF {
		img = escapeImage(tok.text)
	}
	header := "Was expecting one of:"
	if len(expected) == 1 {
		header = "Was expecting:"
	}
	var b strings.Builder
	b.WriteString("Encountered \"" + img + "\" at line " +
		itoa(tok.line) + ", column " + itoa(tok.col) + ".\n")
	b.WriteString(header + "\n    ")
	for _, k := range expected {
		b.WriteString(images[k] + " ...\n    ")
	}
	return &Error{Msg: b.String()}
}

// statement parses one declaration, directive, fact, or rule.
func (p *parser) statement() (Statement, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p.cur().kind {
	case tkDecl:
		return p.declaration()
	case tkIdent:
		return p.factOrRule()
	case tkInput:
		return p.input()
	case tkOutput:
		return p.output()
	default:
		return nil, p.parseError(tkEOF, tkDecl, tkInput,
			tkOutput, tkIdent)
	}
}

// declaration parses ".decl name(param:type, ...)".
func (p *parser) declaration() (Statement, error) {
	p.advance()
	name, err := p.expect(tkIdent)
	if err != nil {
		return nil, err
	}
	_, err = p.expect(tkLparen)
	if err != nil {
		return nil, err
	}
	d := &Declaration{Name: name.text}
	if p.cur().kind != tkRparen {
		for {
			param, err := p.param()
			if err != nil {
				return nil, err
			}
			d.Params = append(d.Params, param)
			if p.cur().kind != tkComma {
				break
			}
			p.advance()
		}
		if p.cur().kind != tkRparen {
			return nil, p.parseError(tkRparen, tkComma)
		}
	}
	p.advance()
	return d, nil
}

// param parses "name : (int|string)".
func (p *parser) param() (Param, error) {
	name, err := p.expect(tkIdent)
	if err != nil {
		return Param{}, err
	}
	_, err = p.expect(tkColon)
	if err != nil {
		return Param{}, err
	}
	switch p.cur().kind {
	case tkInt:
		p.advance()
		return Param{Name: name.text, Type: typeInt}, nil
	case tkString:
		p.advance()
		return Param{Name: name.text, Type: typeString}, nil
	default:
		return Param{}, p.parseError(tkInt, tkString)
	}
}

// input parses ".input relation [file]".
func (p *parser) input() (Statement, error) {
	p.advance()
	name, err := p.expect(tkIdent)
	if err != nil {
		return nil, err
	}
	in := &Input{Relation: name.text}
	if p.cur().kind == tkStrLit {
		in.FileName = p.advance().val
	}
	return in, nil
}

// output parses ".output relation".
func (p *parser) output() (Statement, error) {
	p.advance()
	name, err := p.expect(tkIdent)
	if err != nil {
		return nil, err
	}
	return &Output{Relation: name.text}, nil
}

// factOrRule parses "atom." (a fact) or "atom :- body." (a rule).
func (p *parser) factOrRule() (Statement, error) {
	head, err := p.atom()
	if err != nil {
		return nil, err
	}
	switch p.cur().kind {
	case tkDot:
		p.advance()
		return &Fact{Atom: head}, nil
	case tkImplies:
		p.advance()
		body, err := p.ruleBody()
		if err != nil {
			return nil, err
		}
		_, err = p.expect(tkDot)
		if err != nil {
			return nil, err
		}
		return &Rule{Head: head, Body: body}, nil
	default:
		return nil, p.parseError(tkDot, tkImplies)
	}
}

// ruleBody parses "bodyAtom (, bodyAtom)*".
func (p *parser) ruleBody() ([]BodyItem, error) {
	var items []BodyItem
	for {
		item, err := p.bodyItem()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if p.cur().kind != tkComma {
			return items, nil
		}
		p.advance()
	}
}

// bodyItem parses a comparison, or an atom with optional "!"
// negation; an identifier followed by "(" is an atom, anything
// else that can start a term is a comparison.
func (p *parser) bodyItem() (BodyItem, error) {
	if p.cur().kind == tkBang {
		p.advance()
		atom, err := p.atom()
		if err != nil {
			return nil, err
		}
		return &BodyAtom{Atom: atom, Negated: true}, nil
	}
	if p.cur().kind == tkIdent && p.peek().kind == tkLparen {
		atom, err := p.atom()
		if err != nil {
			return nil, err
		}
		return &BodyAtom{Atom: atom}, nil
	}
	left, err := p.term()
	if err != nil {
		return nil, err
	}
	op, err := p.compOp()
	if err != nil {
		return nil, err
	}
	right, err := p.term()
	if err != nil {
		return nil, err
	}
	return &Comparison{Left: left, Op: op, Right: right}, nil
}

// compOps maps comparison token kinds to their spellings.
var compOps = map[tokKind]string{
	tkLe: "<=",
	tkGe: ">=",
	tkNe: "!=",
	tkLt: "<",
	tkGt: ">",
	tkEq: "=",
}

// compOp parses one comparison operator.
func (p *parser) compOp() (string, error) {
	if op, ok := compOps[p.cur().kind]; ok {
		p.advance()
		return op, nil
	}
	return "", p.parseError(tkLe, tkGe, tkNe, tkLt, tkGt, tkEq)
}

// atom parses "name(term, ...)".
func (p *parser) atom() (*Atom, error) {
	name, err := p.expect(tkIdent)
	if err != nil {
		return nil, err
	}
	_, err = p.expect(tkLparen)
	if err != nil {
		return nil, err
	}
	a := &Atom{Name: name.text}
	if p.cur().kind != tkRparen {
		for {
			t, err := p.term()
			if err != nil {
				return nil, err
			}
			a.Args = append(a.Args, t)
			if p.cur().kind != tkComma {
				break
			}
			p.advance()
		}
		if p.cur().kind != tkRparen {
			return nil, p.parseError(tkRparen, tkComma)
		}
	}
	p.advance()
	return a, nil
}

// term parses additive precedence: mult (("+"|"-") mult)*.
func (p *parser) term() (Term, error) {
	left, err := p.multiplicative()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().kind {
		case tkPlus:
			op = "+"
		case tkMinus:
			op = "-"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.multiplicative()
		if err != nil {
			return nil, err
		}
		left = &Arith{Left: left, Op: op, Right: right}
	}
}

// multiplicative parses primary (("*"|"/") primary)*.
func (p *parser) multiplicative() (Term, error) {
	left, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().kind {
		case tkStar:
			op = "*"
		case tkSlash:
			op = "/"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.primary()
		if err != nil {
			return nil, err
		}
		left = &Arith{Left: left, Op: op, Right: right}
	}
}

// primary parses a variable, a literal, or a parenthesized term.
func (p *parser) primary() (Term, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch p.cur().kind {
	case tkIdent:
		return &Variable{Name: p.advance().text}, nil
	case tkIntLit:
		tok := p.advance()
		n, err := strconv.Atoi(tok.text)
		if err != nil {
			return nil, &Error{Msg: err.Error()}
		}
		return &Constant{Type: typeInt, Int: n}, nil
	case tkLparen:
		p.advance()
		t, err := p.term()
		if err != nil {
			return nil, err
		}
		_, err = p.expect(tkRparen)
		if err != nil {
			return nil, err
		}
		return t, nil
	case tkStrLit:
		return &Constant{
			Type: typeString, Str: p.advance().val,
		}, nil
	default:
		return nil, p.parseError(tkLparen, tkIntLit, tkStrLit,
			tkIdent)
	}
}
