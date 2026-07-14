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

package shell

import (
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/types"
)

// formatValue renders a value as the shell prints it, directed
// by the value's static type. (Line wrapping and exact real
// formatting arrive with the pretty-printing engine.)
func formatValue(v eval.Val, t types.Type) string {
	var b strings.Builder
	writeValue(&b, v, t)
	return b.String()
}

func writeValue(b *strings.Builder, v eval.Val, t types.Type) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Fn:
		b.WriteString("fn")
	case *types.List:
		b.WriteString("[")
		for i, elem := range asVals(v) {
			if i > 0 {
				b.WriteString(",")
			}
			writeValue(b, elem, t.Elem)
		}
		b.WriteString("]")
	case *types.Named:
		writeCon(b, v, t)
	case *types.Primitive:
		writePrimitive(b, v, t)
	case *types.Record:
		b.WriteString("{")
		for i, field := range t.Fields {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(field.Label + "=")
			writeValue(b, asVals(v)[i], field.Type)
		}
		b.WriteString("}")
	case *types.Tuple:
		b.WriteString("(")
		for i, arg := range t.Args {
			if i > 0 {
				b.WriteString(",")
			}
			writeValue(b, asVals(v)[i], arg)
		}
		b.WriteString(")")
	case *types.Var:
		b.WriteString("fn")
	}
}

func writePrimitive(b *strings.Builder, v eval.Val,
	t *types.Primitive,
) {
	// lint: sort until '^\t}' where '^\tcase '
	switch t.String() {
	case "bool":
		v2, _ := v.(bool)
		b.WriteString(strconv.FormatBool(v2))
	case "char":
		v2, _ := v.(rune)
		b.WriteString(`#"` + escapeString(string(v2)) + `"`)
	case "int":
		v2, _ := v.(int32)
		b.WriteString(formatInt(v2))
	case "real":
		v2, _ := v.(float32)
		b.WriteString(formatReal(v2))
	case "string":
		v2, _ := v.(string)
		b.WriteString(`"` + escapeString(v2) + `"`)
	case "unit":
		b.WriteString("()")
	}
}

// writeCon renders a datatype value such as "SOME 4"; a
// constructor argument that is itself a constructor application
// is parenthesized, "SOME (SOME 4)".
func writeCon(b *strings.Builder, v eval.Val, t *types.Named) {
	con, ok := v.(eval.Con)
	if !ok {
		b.WriteString("fn")
		return
	}
	b.WriteString(con.Name)
	if con.Arg == nil {
		return
	}
	b.WriteString(" ")
	argType := conArgType(t)
	arg := formatValue(con.Arg, argType)
	if _, isCon := con.Arg.(eval.Con); isCon &&
		strings.Contains(arg, " ") {
		arg = "(" + arg + ")"
	}
	b.WriteString(arg)
}

// conArgType is a placeholder until datatype values arrive: the
// argument type of a unary datatype such as option is its type
// argument.
func conArgType(t *types.Named) types.Type {
	if len(t.Args) == 1 {
		return t.Args[0]
	}
	return t
}

// formatInt renders an int with Morel's negation sign: ~3.
func formatInt(i int32) string {
	return strings.ReplaceAll(strconv.FormatInt(int64(i), 10),
		"-", "~")
}

// formatReal renders a real provisionally; exact java formatting
// arrives with the pretty-printing engine.
func formatReal(f float32) string {
	s := strconv.FormatFloat(float64(f), 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return strings.ReplaceAll(s, "-", "~")
}

func asVals(v eval.Val) []eval.Val {
	vals, ok := v.([]eval.Val)
	if !ok {
		return nil
	}
	return vals
}
