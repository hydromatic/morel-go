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
	"math"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/core"
)

// PlanString renders a value as the compiled-plan output shows it,
// inside a "constant(...)". This is the value's raw textual form —
// integers and reals with a leading "-" (not "~"), strings and
// characters without quotes, unit as the empty string, lists and
// tuples as "[a, b, c]", and a datatype value as "[Name]" or
// "[Name, arg]" — matching the runtime's own value rendering.
func PlanString(v Val) string {
	// lint: sort until '^	}' where '^	case '
	switch v := v.(type) {
	case Con:
		if v.Arg != nil {
			return "[" + v.Name + ", " + PlanString(v.Arg) + "]"
		}
		return "[" + v.Name + "]"
	case []Val:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = PlanString(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case bool:
		return strconv.FormatBool(v)
	case core.Unit:
		return ""
	case float32:
		return planReal(float64(v))
	case float64:
		return planReal(v)
	case int32:
		// int32 backs both int and char; a char constant in a plan
		// is rare, so this renders the integer form.
		return strconv.FormatInt(int64(v), 10)
	case nil:
		return ""
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	default:
		return "?"
	}
}

// IsBuiltinFn reports whether code is a constant holding a
// built-in function value — a bare Go function rather than a
// user-defined closure or constructor. The compiled plan names
// such a function ("fnValue Int.+") instead of describing its
// code.
func IsBuiltinFn(code Code) bool {
	c, ok := code.(*constantCode)
	if !ok {
		return false
	}
	switch c.v.(type) {
	case Fn, func(Val) (Val, error):
		return true
	default:
		return false
	}
}

// planReal renders a real as the plan shows it, in the runtime's
// textual form: "Infinity", "-Infinity", "NaN", and otherwise a
// decimal that always carries a point ("1.0", not "1").
func planReal(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	s := strconv.FormatFloat(f, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
