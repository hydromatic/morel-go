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
	"math"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/pp"
	"github.com/hydromatic/morel-go/internal/types"
)

// prettyBinding renders "val name = value : type", choosing line
// breaks with the layout engine. The value stays on the "val
// ... =" line only if it fits there entirely flat; otherwise the
// whole value moves to its own line, indented by 2, where it is
// free to wrap. Likewise the type stays on the value's last line
// if it fits flat there. This matches how SML/NJ lays out a
// binding.
func (c *Config) prettyBinding(name string, v eval.Val,
	t types.Type,
) string {
	valueDoc := c.valueDoc(t, v, 1)
	typeDoc := pp.Text(t.String())
	const indent = 2
	valuePart := pp.Union(
		pp.Beside(pp.Text(" "), pp.Flatten(valueDoc)),
		pp.Nest(indent, pp.Beside(pp.HardLine(), valueDoc)))
	typePart := pp.Union(
		pp.Beside(pp.Text(" : "), typeDoc),
		pp.Nest(indent, pp.Concat(pp.HardLine(), pp.Text(": "),
			typeDoc)))
	doc := pp.Concat(pp.Text("val "+name+" ="), valuePart,
		typePart)
	return pp.Render(c.width(), doc)
}

// width is the page width passed to the renderer. Rendering at
// lineWidth - 1 matches SML/NJ's right margin: its printer
// allows one more column than ours before breaking.
func (c *Config) width() int {
	if c.LineWidth < 0 {
		return math.MaxInt32
	}
	return c.LineWidth - 1
}

// valueDoc builds the layout of a value, directed by the value's
// static type. Beyond printDepth, a nested value prints as "#".
func (c *Config) valueDoc(t types.Type, v eval.Val,
	depth int,
) pp.Doc {
	if c.PrintDepth >= 0 && depth > c.PrintDepth {
		return pp.Text("#")
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Fn:
		return pp.Text("fn")
	case *types.List:
		return c.seqDoc("[", "]", c.elementDocs(t.Elem, v, depth))
	case *types.Named:
		return c.conDoc(t, v, depth)
	case *types.Primitive:
		return pp.Text(c.primitiveString(t, v))
	case *types.Record:
		vals := asVals(v)
		docs := make([]pp.Doc, len(t.Fields))
		for i, field := range t.Fields {
			docs[i] = pp.Beside(pp.Text(field.Label+"="),
				c.valueDoc(field.Type, vals[i], depth+1))
		}
		return c.seqDoc("{", "}", docs)
	case *types.Tuple:
		vals := asVals(v)
		docs := make([]pp.Doc, len(t.Args))
		for i, arg := range t.Args {
			docs[i] = c.valueDoc(arg, vals[i], depth+1)
		}
		return c.seqDoc("(", ")", docs)
	case *types.Var:
		return pp.Text("fn")
	default:
		return pp.Text("fn")
	}
}

// elementDocs builds the layouts of a list's elements; beyond
// printLength, the remaining elements print as one "...".
func (c *Config) elementDocs(elemType types.Type, v eval.Val,
	depth int,
) []pp.Doc {
	var docs []pp.Doc
	for i, elem := range asVals(v) {
		if c.PrintLength >= 0 && i >= c.PrintLength {
			docs = append(docs, pp.Text("..."))
			break
		}
		docs = append(docs, c.valueDoc(elemType, elem, depth+1))
	}
	return docs
}

// seqDoc lays out a bracketed sequence (a list, record, or
// tuple). Elements fill across lines: as many as fit share a
// line, and each element is treated as a unit (a record in a
// list of records stays together, and the list wraps between
// records). There is no space after the comma, the way SML/NJ
// prints values. Continuation lines align under the first
// element.
func (c *Config) seqDoc(open, closing string,
	docs []pp.Doc,
) pp.Doc {
	if len(docs) == 0 {
		return pp.Text(open + closing)
	}
	items := make([]pp.Doc, len(docs))
	for i, d := range docs {
		if i < len(docs)-1 {
			items[i] = pp.Beside(d, pp.Text(","))
		} else {
			items[i] = d
		}
	}
	return pp.Concat(pp.Text(open),
		pp.Align(pp.Fill(pp.Empty(), items)), pp.Text(closing))
}

// conDoc lays out a datatype value such as "SOME 4"; an argument
// that is not atomic is parenthesized, "SOME (SOME 4)".
func (c *Config) conDoc(t *types.Named, v eval.Val,
	depth int,
) pp.Doc {
	con, ok := v.(eval.Con)
	if !ok {
		return pp.Text("fn")
	}
	if con.Arg == nil {
		return pp.Text(con.Name)
	}
	argType := conArgType(t)
	argDoc := c.valueDoc(argType, con.Arg, depth+1)
	if _, isPrim := argType.(*types.Primitive); !isPrim {
		argDoc = pp.Concat(pp.Text("("), argDoc, pp.Text(")"))
	}
	return pp.Concat(pp.Text(con.Name), pp.Text(" "), argDoc)
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

func (c *Config) primitiveString(t *types.Primitive,
	v eval.Val,
) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch t.String() {
	case "bool":
		v2, _ := v.(bool)
		return strconv.FormatBool(v2)
	case "char":
		v2, _ := v.(rune)
		return `#"` + escapeString(string(v2)) + `"`
	case "int":
		v2, _ := v.(int32)
		return formatInt(v2)
	case "real":
		v2, _ := v.(float32)
		return formatReal(v2)
	case "string":
		v2, _ := v.(string)
		if c.StringDepth >= 0 && len(v2) > c.StringDepth {
			return `"` + escapeString(v2[:c.StringDepth]) +
				`#"`
		}
		return `"` + escapeString(v2) + `"`
	default:
		return "()"
	}
}

// formatInt renders an int with Morel's negation sign: ~3.
func formatInt(i int32) string {
	return strings.ReplaceAll(strconv.FormatInt(int64(i), 10),
		"-", "~")
}

// formatReal renders a real the way java's Codes.floatToString
// does, matching Standard ML's Real.toString: the shortest
// decimal digits that round-trip the float32; plain decimal
// notation for magnitudes in [1e-3, 1e7) and scientific notation
// otherwise; a trailing ".0" dropped (1.0 prints as "1", 1.0e10
// as "1E10"); and "~" for minus, in exponents too.
func formatReal(f float32) string {
	f64 := float64(f)
	switch {
	case math.IsNaN(f64):
		return "nan"
	case math.IsInf(f64, 1):
		return "inf"
	case math.IsInf(f64, -1):
		return "~inf"
	}
	s := strconv.FormatFloat(f64, 'E', -1, 32)
	mantissa, expText, _ := strings.Cut(s, "E")
	exp, err := strconv.Atoi(expText)
	if err != nil {
		return s
	}
	neg := strings.HasPrefix(mantissa, "-")
	digits := strings.ReplaceAll(
		strings.TrimPrefix(mantissa, "-"), ".", "")
	var b strings.Builder
	if neg {
		b.WriteString("~")
	}
	const loExp, hiExp = -3, 7
	if exp >= loExp && exp < hiExp {
		writeDecimal(&b, digits, exp)
	} else {
		writeScientific(&b, digits, exp)
	}
	// Real.minPos: SML reports 1.4E~45, though "1E~45" denotes
	// the same float.
	result := b.String()
	if strings.HasSuffix(result, "1E~45") {
		result = strings.Replace(result, "1E~45", "1.4E~45", 1)
	}
	return result
}

// writeDecimal renders digits with the decimal point after the
// digit at position exp: digits "15" with exp 0 is "1.5", digits
// "1" with exp -3 is "0.001", digits "1" with exp 2 is "100".
func writeDecimal(b *strings.Builder, digits string, exp int) {
	if exp < 0 {
		b.WriteString("0.")
		b.WriteString(strings.Repeat("0", -exp-1))
		b.WriteString(digits)
		return
	}
	if len(digits) <= exp+1 {
		b.WriteString(digits)
		b.WriteString(strings.Repeat("0", exp+1-len(digits)))
		return
	}
	b.WriteString(digits[:exp+1])
	b.WriteString(".")
	b.WriteString(digits[exp+1:])
}

// writeScientific renders "d.dddEx", dropping a ".0" mantissa
// ("1E10" rather than "1.0E10") and using "~" for a negative
// exponent.
func writeScientific(b *strings.Builder, digits string, exp int) {
	b.WriteString(digits[:1])
	if len(digits) > 1 {
		b.WriteString(".")
		b.WriteString(digits[1:])
	}
	b.WriteString("E")
	if exp < 0 {
		b.WriteString("~")
		exp = -exp
	}
	b.WriteString(strconv.Itoa(exp))
}

func asVals(v eval.Val) []eval.Val {
	vals, ok := v.([]eval.Val)
	if !ok {
		return nil
	}
	return vals
}
