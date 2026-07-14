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

// fromExpr parses a query of the given kind (from, exists, or
// forall): comma-separated scans, then steps in any order.
func (p *Parser) fromExpr(kind ast.Op) (ast.Expr, error) {
	start := p.tok.Span.Start
	err := p.next()
	if err != nil {
		return nil, err
	}
	steps, err := p.scanList()
	if err != nil {
		return nil, err
	}
	for {
		step, err := p.fromStep()
		if err != nil {
			return nil, err
		}
		if step == nil {
			break
		}
		steps = append(steps, step...)
	}
	last := steps[len(steps)-1]
	span := token.Span{Start: start, End: last.Span().End}
	return ast.NewFrom(span, kind, steps), nil
}

// exprStepKinds maps a step keyword to its op, for steps of the
// form "keyword exp".
var exprStepKinds = map[token.Kind]ast.Op{
	token.Compute: ast.ComputeOp,
	token.Group:   ast.GroupOp,
	token.Into:    ast.IntoOp,
	token.Order:   ast.OrderOp,
	token.Require: ast.RequireOp,
	token.Skip:    ast.SkipOp,
	token.Take:    ast.TakeOp,
	token.Where:   ast.WhereOp,
	token.Yield:   ast.YieldOp,
}

var setOpKinds = map[token.Kind]ast.Op{
	token.Except:    ast.ExceptOp,
	token.Intersect: ast.IntersectOp,
	token.Union:     ast.UnionOp,
}

// fromStep parses one pipeline step, or returns nil at the end
// of the query.
func (p *Parser) fromStep() ([]ast.FromStep, error) {
	if kind, ok := exprStepKinds[p.tok.Kind]; ok {
		return p.exprStep(kind)
	}
	if kind, ok := setOpKinds[p.tok.Kind]; ok {
		return p.setOpStep(kind)
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch p.tok.Kind {
	case token.Distinct:
		span := p.tok.Span
		err := p.next()
		if err != nil {
			return nil, err
		}
		return []ast.FromStep{ast.NewBareStep(span,
			ast.DistinctOp)}, nil
	case token.Join:
		err := p.next()
		if err != nil {
			return nil, err
		}
		return p.scanList()
	case token.Through:
		return p.throughStep()
	case token.Unorder:
		span := p.tok.Span
		err := p.next()
		if err != nil {
			return nil, err
		}
		return []ast.FromStep{ast.NewBareStep(span,
			ast.UnorderOp)}, nil
	default:
		return nil, nil
	}
}

// exprStep parses "keyword [binder =] exp"; only group and
// compute may have a binder.
func (p *Parser) exprStep(kind ast.Op) ([]ast.FromStep, error) {
	err := p.next()
	if err != nil {
		return nil, err
	}
	binder := ""
	if kind == ast.GroupOp || kind == ast.ComputeOp {
		binder, err = p.stepBinder()
		if err != nil {
			return nil, err
		}
	}
	exp, err := p.expr()
	if err != nil {
		return nil, err
	}
	step := ast.NewStep(exp.Span(), kind, exp)
	// lint: sort until '^\t}' where '^\tcase '
	switch st := step.(type) {
	case *ast.ComputeStep:
		st.Binder = binder
	case *ast.GroupStep:
		st.Binder = binder
	default:
	}
	return []ast.FromStep{step}, nil
}

// stepBinder recognizes "name =" before a group or compute
// expression.
func (p *Parser) stepBinder() (string, error) {
	if p.tok.Kind != token.Ident {
		return "", nil
	}
	kind, err := p.peek()
	if err != nil {
		return "", err
	}
	if kind != token.Eq {
		return "", nil
	}
	binder := p.tok.Text
	err = p.next()
	if err != nil {
		return "", err
	}
	err = p.next()
	if err != nil {
		return "", err
	}
	return binder, nil
}

// setOpStep parses "union|intersect|except [distinct] exp".
func (p *Parser) setOpStep(kind ast.Op) ([]ast.FromStep, error) {
	err := p.next()
	if err != nil {
		return nil, err
	}
	distinct := false
	if p.tok.Kind == token.Distinct {
		distinct = true
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
	exp, err := p.expr()
	if err != nil {
		return nil, err
	}
	return []ast.FromStep{ast.NewSetOpStep(exp.Span(), kind,
		distinct, exp)}, nil
}

// throughStep parses "through pat in exp".
func (p *Parser) throughStep() ([]ast.FromStep, error) {
	err := p.next()
	if err != nil {
		return nil, err
	}
	pat, err := p.pat()
	if err != nil {
		return nil, err
	}
	err = p.expect(token.In)
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
	return []ast.FromStep{ast.NewThroughStep(span, pat,
		exp)}, nil
}

// scanList parses "scan [, scan ...]".
func (p *Parser) scanList() ([]ast.FromStep, error) {
	var steps []ast.FromStep
	for {
		scan, err := p.scan()
		if err != nil {
			return nil, err
		}
		steps = append(steps, scan)
		if p.tok.Kind != token.Comma {
			return steps, nil
		}
		err = p.next()
		if err != nil {
			return nil, err
		}
	}
}

// scan parses "pat [in exp | = exp] [on exp]".
func (p *Parser) scan() (*ast.Scan, error) {
	pat, err := p.pat()
	if err != nil {
		return nil, err
	}
	kind := ast.ScanUnbounded
	var exp ast.Expr
	switch p.tok.Kind {
	case token.Eq:
		kind = ast.ScanEq
	case token.In:
		kind = ast.ScanIn
	default:
	}
	end := pat.Span().End
	if kind != ast.ScanUnbounded {
		err = p.next()
		if err != nil {
			return nil, err
		}
		exp, err = p.expr()
		if err != nil {
			return nil, err
		}
		end = exp.Span().End
	}
	var on ast.Expr
	if p.tok.Kind == token.On {
		err = p.next()
		if err != nil {
			return nil, err
		}
		on, err = p.expr()
		if err != nil {
			return nil, err
		}
		end = on.Span().End
	}
	span := token.Span{Start: pat.Span().Start, End: end}
	return ast.NewScan(span, kind, pat, exp, on), nil
}
