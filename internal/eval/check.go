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

package eval

import (
	"errors"
	"strings"

	"github.com/hydromatic/morel-go/internal/token"
	"github.com/hydromatic/morel-go/internal/types"
)

// ValueString renders a value as Morel writes it, for a message
// that quotes it: flat, and at the default print length and
// depth rather than the session's. The shell installs the printer
// it uses for results, so that a message and a result write a
// value the same way; until it does, a value renders opaquely.
var ValueString = func(types.Type, Val) string { return "value" }

// CheckCode returns code that enforces a checked type's
// conditions on a value.
//
// raises says what happens when the condition is false: a claim
// raises Constraint, whereas "asOpt" asks rather than claims, so
// a value that does not have the type is an answer. giveValue
// says what the code gives back when the condition holds -- the
// value for a claim, and true for a component of a composite,
// whose checks conjoin into one condition.
func CheckCode(value, cond Code, t types.Type, name, blame string,
	raises, giveValue bool, span token.Span,
) Code {
	return &checkCode{
		value: value, cond: cond, t: t, name: name, blame: blame,
		raises: raises, giveValue: giveValue, span: span,
	}
}

type checkCode struct {
	value     Code
	cond      Code
	t         types.Type
	name      string
	blame     string
	span      token.Span
	raises    bool
	giveValue bool
}

func (c *checkCode) Eval(f *Frame) (Val, error) {
	v, err := c.value.Eval(f)
	if err != nil {
		return nil, err
	}
	holds, err := c.cond.Eval(f)
	if err != nil {
		return nil, c.undecided(v, err)
	}
	if holds == true {
		if c.giveValue {
			return v, nil
		}
		return true, nil
	}
	if !c.raises {
		return false, nil
	}
	return nil, &MorelError{
		Exn:  ExnConstraint,
		Desc: c.describe(v, " is not a valid "),
		Span: c.span,
	}
}

func (c *checkCode) Describe() string {
	return "check(condition " + c.cond.Describe() +
		", value " + c.value.Describe() + ", " + c.name + ")"
}

// undecided reports a condition that could not be evaluated.
// Whether the value has the type is then not false but unknown;
// Constraint is raised either way -- the value has not been shown
// to have the type -- but the message says which happened, and
// what went wrong.
func (c *checkCode) undecided(v Val, err error) error {
	var me *MorelError
	if !errors.As(err, &me) {
		// Not a Morel-level failure, so not something a condition
		// can be said to have decided; let it out.
		return err
	}
	if me.Exn == ExnConstraint {
		// A check on a component has already reported, and said
		// precisely which component and why. Wrapping it again
		// would bury that.
		return err
	}
	return &MorelError{
		Exn: ExnConstraint,
		Desc: "cannot tell whether " + c.describe(v, " is a valid ") +
			"; " + me.Summary(),
		Span: c.span,
	}
}

// describe renders "<value><verb><type>", with the blame path
// after it when the value is a component of something.
func (c *checkCode) describe(v Val, verb string) string {
	var b strings.Builder
	b.WriteString(ValueString(c.t, v))
	b.WriteString(verb)
	b.WriteString(c.name)
	if c.blame != "" {
		b.WriteString(": ")
		b.WriteString(c.blame)
	}
	return b.String()
}
