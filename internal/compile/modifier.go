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

package compile

import (
	"slices"
	"sort"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// modArg is one field of the record a modifier produces: its
// label and the expression that gives its value.
type modArg struct {
	label string
	exp   ast.Expr
}

// desugarModifiers converts a record with modifiers into an
// expression: one "let" per modifier, each destructuring the
// record that the modifier before it produced.
//
// Destructuring is what makes the fields visible to the
// assignments, and makes them shadow the enclosing environment;
// nesting is what makes a modifier see the result of the one
// before it; and the bindings of one "let" being simultaneous is
// what makes the assignments of one modifier simultaneous. For
// example, "{r replace i = j, j = i remove j}" becomes
//
//	let val {i = i, j = j} = r in
//	  (let val {i = i, j = j} = {i = j, j = i} in
//	    {i = i}
//	  end)
//	end
//
// fieldNames gives the field names of the base, and of the
// argument of each "all" modifier. Each modifier also checks the
// labels it mentions against the fields it is applied to.
func desugarModifiers(record *ast.Record,
	fieldNames map[ast.Expr][]string,
) (ast.Expr, error) {
	span := record.Span()
	exp := record.Base
	fields := fieldNames[record.Base]
	for _, m := range record.Modifiers {
		valBinds := []*ast.ValBind{
			ast.NewValBind(span, fieldsPat(span, fields), exp),
		}
		var args []modArg
		var err error
		// lint: sort until '^		}' where '^		case '
		switch m := m.(type) {
		case *ast.AllModifier:
			name := freeName(fields)
			valBinds = append(valBinds,
				ast.NewValBind(span, ast.NewIDPat(span, name), m.Exp))
			args, err = assignAllFields(span, m, fields,
				fieldNames[m.Exp], name)
		case *ast.AssignModifier:
			args, err = assignFields(span, m, fields)
		case *ast.RemoveModifier:
			args, err = removeFields(span, m, fields)
		case *ast.RenameModifier:
			args, err = renameFields(span, m, fields)
		}
		if err != nil {
			return nil, err
		}
		recFields := make([]ast.Field, len(args))
		fields = make([]string, len(args))
		for i, a := range args {
			recFields[i] = ast.Field{Label: a.label, Exp: a.exp}
			fields[i] = a.label
		}
		sort.Slice(fields, func(i, j int) bool {
			return types.LabelLess(fields[i], fields[j])
		})
		exp = ast.NewLet(span,
			[]ast.Decl{
				ast.NewValDecl(span, false, false, valBinds),
			},
			ast.NewRecord(span, recFields))
	}
	return exp, nil
}

// fieldsPat returns a pattern that destructures a record into its
// fields, "{a = a, b = b}".
func fieldsPat(span token.Span, fields []string) ast.Pat {
	patFields := make([]ast.PatField, len(fields))
	for i, field := range fields {
		patFields[i] = ast.PatField{
			Label: field, Pat: ast.NewIDPat(span, field),
		}
	}
	return ast.NewRecordPat(span, patFields, false)
}

// freeName returns a name that is not one of fields.
func freeName(fields []string) string {
	name := "$all"
	for slices.Contains(fields, name) {
		name += "_"
	}
	return name
}

// assignFields applies an "extend" or "replace" modifier, in
// either case taking each label to whichever of the verb's two
// cases it falls in: the record has the label already, or it does
// not.
func assignFields(span token.Span, m *ast.AssignModifier,
	fields []string,
) ([]modArg, error) {
	labelled, err := labelFields(m.Fields)
	if err != nil {
		return nil, err
	}
	assigned := map[string]ast.Expr{}
	for _, f := range labelled {
		err = checkAssign(m.Verb, f.Label,
			slices.Contains(fields, f.Label), f.LabelSpan)
		if err != nil {
			return nil, err
		}
		assigned[f.Label] = f.Exp
	}

	// Fields the record has: assigned, or kept as they were.
	var args []modArg
	for _, field := range fields {
		exp, isAssigned := assigned[field]
		if !isAssigned || m.Verb.Exists() == ast.ExistsSkip {
			args = append(args, modArg{field, ast.NewID(span, field)})
			continue
		}
		if !m.Lenient {
			exp = sameType(exp, field)
		}
		args = append(args, modArg{field, exp})
	}

	// Labels the record does not have: added, or ignored.
	if m.Verb.Absent() == ast.AbsentAdd {
		for _, f := range labelled {
			if !slices.Contains(fields, f.Label) {
				args = append(args, modArg{f.Label, f.Exp})
			}
		}
	}
	return args, nil
}

// labelFields makes a modifier's implicit labels explicit, so
// that "{r replace a}" assigns the value of "a" to field "a", and
// gives every label a source range to blame: its own if it was
// written, else its expression's.
func labelFields(fields []ast.Field) ([]ast.Field, error) {
	labelled := make([]ast.Field, len(fields))
	for i, f := range fields {
		if f.Label == "" {
			f.Label = implicitLabel(f.Exp)
			if f.Label == "" {
				return nil, &Error{
					Span: f.Exp.Span(),
					Msg: cannotDeriveLabel + " " +
						ast.UnparseExpr(f.Exp),
				}
			}
		}
		if f.LabelSpan == (token.Span{}) {
			f.LabelSpan = f.Exp.Span()
		}
		if slices.ContainsFunc(labelled[:i], func(g ast.Field) bool {
			return g.Label == f.Label
		}) {
			return nil, duplicateField(f.Label, f.LabelSpan)
		}
		labelled[i] = f
	}
	return labelled, nil
}

// assignAllFields applies an "extend all" or "replace all"
// modifier: the same rules as assignFields, for every field of
// the modifier's record-valued argument, which name binds.
func assignAllFields(span token.Span, m *ast.AllModifier,
	fields, allFields []string, name string,
) ([]modArg, error) {
	for _, field := range allFields {
		err := checkAssign(m.Verb, field,
			slices.Contains(fields, field), m.Exp.Span())
		if err != nil {
			return nil, err
		}
	}

	var args []modArg
	for _, field := range fields {
		if !slices.Contains(allFields, field) ||
			m.Verb.Exists() == ast.ExistsSkip {
			args = append(args, modArg{field, ast.NewID(span, field)})
			continue
		}
		exp := selectField(span, name, field)
		if !m.Lenient {
			exp = sameType(exp, field)
		}
		args = append(args, modArg{field, exp})
	}

	if m.Verb.Absent() == ast.AbsentAdd {
		for _, field := range allFields {
			if !slices.Contains(fields, field) {
				args = append(args,
					modArg{field, selectField(span, name, field)})
			}
		}
	}
	return args, nil
}

// checkAssign reports a label that falls in the case its verb
// makes an error: one that exists where the verb adds, or one
// that is absent where the verb assigns.
func checkAssign(verb ast.ModifierVerb, label string, exists bool,
	span token.Span,
) error {
	switch {
	case exists && verb.Exists() == ast.ExistsError:
		return fieldExists(label, span)
	case !exists && verb.Absent() == ast.AbsentError:
		return fieldNotFound(label, span)
	default:
		return nil
	}
}

// renameFields applies a "rename" modifier. It takes the value of
// each label on the right, which must exist, and gives it to the
// label on the left, which must not survive the renaming.
func renameFields(span token.Span, m *ast.RenameModifier,
	fields []string,
) ([]modArg, error) {
	var sources []string
	for _, pair := range m.Pairs {
		if !slices.Contains(fields, pair.From.Name) {
			return nil, fieldNotFound(pair.From.Name, pair.From.Span)
		}
		if slices.Contains(sources, pair.From.Name) {
			return nil, duplicateField(pair.From.Name, pair.From.Span)
		}
		sources = append(sources, pair.From.Name)
	}
	var args []modArg
	for _, field := range fields {
		if !slices.Contains(sources, field) {
			args = append(args, modArg{field, ast.NewID(span, field)})
		}
	}
	for _, pair := range m.Pairs {
		if slices.ContainsFunc(args, func(a modArg) bool {
			return a.label == pair.To.Name
		}) {
			return nil, fieldExists(pair.To.Name, pair.To.Span)
		}
		args = append(args, modArg{
			pair.To.Name, ast.NewID(span, pair.From.Name),
		})
	}
	return args, nil
}

// removeFields applies a "remove" modifier.
func removeFields(span token.Span, m *ast.RemoveModifier,
	fields []string,
) ([]modArg, error) {
	var removed []string
	for _, label := range m.Labels {
		if !slices.Contains(fields, label.Name) &&
			m.Verb.Absent() == ast.AbsentError {
			return nil, fieldNotFound(label.Name, label.Span)
		}
		if slices.Contains(removed, label.Name) {
			return nil, duplicateField(label.Name, label.Span)
		}
		removed = append(removed, label.Name)
	}
	var args []modArg
	for _, field := range fields {
		if !slices.Contains(removed, field) {
			args = append(args, modArg{field, ast.NewID(span, field)})
		}
	}
	return args, nil
}

// sameType returns "exp : typeof field", which gives an assigned
// value the type of the field it replaces. Assignment does not
// change a field's type, unless the modifier is "lenient".
func sameType(exp ast.Expr, field string) ast.Expr {
	span := exp.Span()
	return ast.NewAnnotatedExp(span, exp,
		ast.NewExpressionType(span, ast.NewID(span, field)))
}

// selectField returns the expression "#field name".
func selectField(span token.Span, name, field string) ast.Expr {
	return ast.NewApply(span,
		ast.NewRecordSelector(span, field), ast.NewID(span, name))
}

// modifierNeedsBase is the error when modifiers follow anything
// but a single unlabeled field.
const modifierNeedsBase = "a record modifier applies to a base " +
	"expression; enclose the expression and its modifiers in braces"

func fieldNotFound(field string, span token.Span) error {
	return &Error{
		Span: span, Msg: "field '" + field + "' does not exist",
	}
}

func fieldExists(field string, span token.Span) error {
	return &Error{
		Span: span, Msg: "field '" + field + "' already exists",
	}
}

func duplicateField(field string, span token.Span) error {
	return &Error{
		Span: span,
		Msg:  "duplicate field '" + field + "' in record",
	}
}

// yieldRecord returns the record whose fields a "yield" step
// binds: the expression itself, the body of a chain of "let"s, or
// the record that a record with modifiers desugars to (also a
// chain of "let"s); nil if the step yields anything else.
//
// The type resolver and the core resolver apply the same test to
// the same tree, so that the bindings the one deduces are the ones
// the other creates.
func yieldRecord(exp ast.Expr,
	desugared map[*ast.Record]ast.Expr,
) *ast.Record {
	for {
		switch e := exp.(type) {
		case *ast.Let:
			exp = e.Exp
		case *ast.Record:
			if e.Base == nil {
				return e
			}
			d, ok := desugared[e]
			if !ok {
				return nil
			}
			exp = d
		default:
			return nil
		}
	}
}
