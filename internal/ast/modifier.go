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

package ast

import "github.com/hydromatic/morel-go/internal/token"

// Exists says what a modifier does to a label the record has.
type Exists int

// The Exists cases.
const (
	// ExistsReplace assigns the value the modifier gives.
	ExistsReplace Exists = iota
	// ExistsRemove removes the field.
	ExistsRemove
	// ExistsSkip leaves the field as it was.
	ExistsSkip
	// ExistsError reports that the field exists.
	ExistsError
)

// Absent says what a modifier does to a label the record does
// not have.
type Absent int

// The Absent cases.
const (
	// AbsentAdd adds a field with the value the modifier gives.
	AbsentAdd Absent = iota
	// AbsentSkip does nothing.
	AbsentSkip
	// AbsentError reports that the field does not exist.
	AbsentError
)

// ModifierVerb says what a record modifier does to a label, in
// each of the two cases: the record has the label already, or it
// does not.
//
// A verb names one case, and makes the other an error; a pair
// joined by "or" names both, and since each verb names its own
// case the pair is unordered. "skip" does nothing, and takes
// whichever case the other verb does not.
type ModifierVerb int

// The record-modifier verbs.
const (
	// ExtendVerb, "extend", adds a label, and rejects one that
	// exists.
	ExtendVerb ModifierVerb = iota
	// ExtendOrSkipVerb, "extend or skip", adds a label, and keeps
	// one that exists.
	ExtendOrSkipVerb
	// ExtendOrReplaceVerb, "extend or replace", adds a label, or
	// assigns to it.
	ExtendOrReplaceVerb
	// ReplaceVerb, "replace", assigns to a label, and rejects one
	// that is absent.
	ReplaceVerb
	// ReplaceOrSkipVerb, "replace or skip", assigns to a label,
	// and ignores one that is absent.
	ReplaceOrSkipVerb
	// RemoveVerb, "remove", removes a label, and rejects one that
	// is absent.
	RemoveVerb
	// RemoveOrSkipVerb, "remove or skip", removes a label, and
	// ignores one that is absent.
	RemoveOrSkipVerb
)

// verbInfo describes one verb: the keywords before and after an
// intervening "lenient", and the two cases it names.
type verbInfo struct {
	before string
	after  string
	exists Exists
	absent Absent
}

var verbInfos = map[ModifierVerb]verbInfo{
	ExtendVerb: {"extend", "", ExistsError, AbsentAdd},
	ExtendOrSkipVerb: {
		"extend or skip", "", ExistsSkip, AbsentAdd,
	},
	ExtendOrReplaceVerb: {
		"extend or replace", "", ExistsReplace, AbsentAdd,
	},
	ReplaceVerb: {"replace", "", ExistsReplace, AbsentError},
	ReplaceOrSkipVerb: {
		"replace", " or skip", ExistsReplace, AbsentSkip,
	},
	RemoveVerb: {"remove", "", ExistsRemove, AbsentError},
	RemoveOrSkipVerb: {
		"remove or skip", "", ExistsRemove, AbsentSkip,
	},
}

// Verbs returns the keywords of this verb, such as
// "extend or replace", with "lenient" in its place if lenient.
func (v ModifierVerb) Verbs(lenient bool) string {
	info := verbInfos[v]
	if lenient {
		return info.before + " lenient" + info.after
	}
	return info.before + info.after
}

// Exists returns what this verb does to a label the record has.
func (v ModifierVerb) Exists() Exists { return verbInfos[v].exists }

// Absent returns what this verb does to a label the record does
// not have.
func (v ModifierVerb) Absent() Absent { return verbInfos[v].absent }

// AllowsLenient returns whether "lenient" is allowed after this
// verb; it is allowed only where a field can be assigned,
// because it relaxes the rule that assignment preserves the
// field's type.
func (v ModifierVerb) AllowsLenient() bool {
	return v.Exists() == ExistsReplace
}

func (v ModifierVerb) String() string { return v.Verbs(false) }

// Modifier is an operator applied to a record value inside
// braces. Modifiers are applied left to right, and each sees the
// result of the previous one.
type Modifier interface {
	// Span is the source range of the modifier, from its first
	// verb to the end of its operand.
	Span() token.Span
	// Verbs returns the keywords that introduce this modifier,
	// such as "replace lenient" or "extend or skip all".
	Verbs() string
	// ForEachExp calls action for each expression this modifier
	// contains.
	ForEachExp(action func(Expr))
	// ForEachLabel calls action for each label this modifier names.
	// An "all" modifier names none; the labels of its argument are
	// not known until it has a type.
	ForEachLabel(action func(string))
	modifier()
}

// modifierBase carries the span common to all modifiers.
type modifierBase struct {
	span token.Span
}

func (m *modifierBase) Span() token.Span { return m.span }
func (*modifierBase) modifier()          {}

// AssignModifier assigns to the labels of an expression row:
// "extend", "replace" and their "or" pairs.
type AssignModifier struct {
	modifierBase

	Fields  []Field
	Verb    ModifierVerb
	Lenient bool
}

// NewAssignModifier returns an "extend" or "replace" modifier.
func NewAssignModifier(span token.Span, verb ModifierVerb,
	lenient bool, fields []Field,
) *AssignModifier {
	return &AssignModifier{
		modifierBase: modifierBase{span}, Fields: fields,
		Verb: verb, Lenient: lenient,
	}
}

// Verbs implements Modifier.
func (m *AssignModifier) Verbs() string {
	return m.Verb.Verbs(m.Lenient)
}

// ForEachExp implements Modifier.
func (m *AssignModifier) ForEachExp(action func(Expr)) {
	for _, f := range m.Fields {
		action(f.Exp)
	}
}

// ForEachLabel implements Modifier.
func (m *AssignModifier) ForEachLabel(action func(string)) {
	for _, f := range m.Fields {
		if f.Label != "" {
			action(f.Label)
		}
	}
}

// AllModifier applies its verb to every field of a
// record-valued expression: "extend all", "replace all" and
// their "or" pairs.
type AllModifier struct {
	modifierBase

	Exp     Expr
	Verb    ModifierVerb
	Lenient bool
}

// NewAllModifier returns an "extend all" or "replace all"
// modifier.
func NewAllModifier(span token.Span, verb ModifierVerb,
	lenient bool, exp Expr,
) *AllModifier {
	return &AllModifier{
		modifierBase: modifierBase{span}, Exp: exp,
		Verb: verb, Lenient: lenient,
	}
}

// Verbs implements Modifier.
func (m *AllModifier) Verbs() string {
	return m.Verb.Verbs(m.Lenient) + " all"
}

// ForEachExp implements Modifier.
func (m *AllModifier) ForEachExp(action func(Expr)) {
	action(m.Exp)
}

// ForEachLabel implements Modifier.
func (*AllModifier) ForEachLabel(func(string)) {}

// RemoveModifier removes labels: "remove" and "remove or skip".
type RemoveModifier struct {
	modifierBase

	Labels []Label
	Verb   ModifierVerb
}

// NewRemoveModifier returns a "remove" modifier.
func NewRemoveModifier(span token.Span, verb ModifierVerb,
	labels []Label,
) *RemoveModifier {
	return &RemoveModifier{
		modifierBase: modifierBase{span}, Labels: labels,
		Verb: verb,
	}
}

// Verbs implements Modifier.
func (m *RemoveModifier) Verbs() string {
	return m.Verb.Verbs(false)
}

// ForEachExp implements Modifier.
func (*RemoveModifier) ForEachExp(func(Expr)) {}

// ForEachLabel implements Modifier.
func (m *RemoveModifier) ForEachLabel(action func(string)) {
	for _, label := range m.Labels {
		action(label.Name)
	}
}

// RenameModifier relabels fields. Each pair gives the value of
// the label on the right to the label on the left, and all pairs
// apply simultaneously.
type RenameModifier struct {
	modifierBase

	Pairs []RenamePair
}

// NewRenameModifier returns a "rename" modifier.
func NewRenameModifier(span token.Span,
	pairs []RenamePair,
) *RenameModifier {
	return &RenameModifier{
		modifierBase: modifierBase{span}, Pairs: pairs,
	}
}

// Verbs implements Modifier.
func (*RenameModifier) Verbs() string { return "rename" }

// ForEachExp implements Modifier.
func (*RenameModifier) ForEachExp(func(Expr)) {}

// ForEachLabel implements Modifier.
func (m *RenameModifier) ForEachLabel(action func(string)) {
	for _, pair := range m.Pairs {
		action(pair.To.Name)
	}
}

// RenamePair is one "to = from" pair of a "rename" modifier.
type RenamePair struct {
	To   Label
	From Label
}

// Label is a record label, with the source range it was written
// at, so that errors can anchor to it.
type Label struct {
	Name string
	Span token.Span
}
