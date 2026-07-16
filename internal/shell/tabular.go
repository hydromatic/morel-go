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
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/pp"
	"github.com/hydromatic/morel-go/internal/types"
)

// outputTabular is the value of the "output" property that renders a
// list of records as a table.
const outputTabular = "TABULAR"

// tabularBinding renders "name : type" preceded by a table, when the
// value is a list of records whose fields are all primitives. The
// table has a left-aligned header row, a row of dashes, and one row
// per record, with numeric columns right-aligned. A blank line
// separates the table from the "val name : type" line, whose type is
// laid out flat. The bool result is false when the type is not
// tabular-printable, so the caller falls back to classic rendering.
func (c *Config) tabularBinding(name string, v eval.Val,
	t types.Type,
) (string, bool) {
	list, ok := t.(*types.List)
	if !ok {
		return "", false
	}
	rec, ok := list.Elem.(*types.Record)
	if !ok || len(rec.Fields) == 0 {
		return "", false
	}
	n := len(rec.Fields)
	widths := make([]int, n)
	right := make([]bool, n)
	labels := make([]string, n)
	for i, f := range rec.Fields {
		if _, ok := f.Type.(*types.Primitive); !ok {
			return "", false
		}
		labels[i] = f.Label
		widths[i] = len(f.Label)
		right[i] = isNumericType(f.Type)
	}

	rows := asVals(v)
	cells := make([][]string, len(rows))
	for r, row := range rows {
		fields := asVals(row)
		cells[r] = make([]string, n)
		for i, f := range rec.Fields {
			s := c.tabularCell(f.Type, fields[i])
			cells[r][i] = s
			if len(s) > widths[i] {
				widths[i] = len(s)
			}
		}
	}

	var b strings.Builder
	b.WriteString(tabularLine(labels, widths, nil))
	b.WriteByte('\n')
	dashes := make([]string, n)
	for i, w := range widths {
		dashes[i] = strings.Repeat("-", w)
	}
	b.WriteString(tabularLine(dashes, widths, nil))
	for _, row := range cells {
		b.WriteByte('\n')
		b.WriteString(tabularLine(row, widths, right))
	}
	b.WriteString("\n\nval " + parse.QuoteIdent(name) + " : " +
		pp.Render(math.MaxInt32, c.typeDoc(t)))
	return b.String(), true
}

// tabularLine joins cells with a single space, padding each to its
// column width (right-aligned where right[i], left-aligned
// otherwise, or all left-aligned when right is nil), then strips
// trailing spaces.
func tabularLine(cells []string, widths []int, right []bool) string {
	var b strings.Builder
	for i, s := range cells {
		if i > 0 {
			b.WriteByte(' ')
		}
		pad := widths[i] - len(s)
		if right != nil && right[i] {
			b.WriteString(strings.Repeat(" ", pad))
			b.WriteString(s)
		} else {
			b.WriteString(s)
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// tabularCell renders one primitive value as it appears in a table
// cell: an int in plain decimal (with a leading "-" when negative,
// unlike the "~" of classic output), a string unquoted.
func (c *Config) tabularCell(t types.Type, v eval.Val) string {
	prim, ok := t.(*types.Primitive)
	if !ok {
		return ""
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch prim.String() {
	case boolType:
		b, _ := v.(bool)
		return strconv.FormatBool(b)
	case charType:
		r, _ := v.(rune)
		return `#"` + eval.CharToString(r) + `"`
	case intType:
		i, _ := v.(int32)
		return strconv.FormatInt(int64(i), 10)
	case realType:
		r, _ := v.(float32)
		return eval.FormatReal(r)
	case stringType:
		s, _ := v.(string)
		if c.StringDepth >= 0 && len(s) > c.StringDepth {
			return s[:c.StringDepth] + "#"
		}
		return s
	default:
		return "()"
	}
}

// isNumericType reports whether a table column of this type is
// right-aligned, as int, real, and word columns are.
func isNumericType(t types.Type) bool {
	prim, ok := t.(*types.Primitive)
	if !ok {
		return false
	}
	switch prim.String() {
	case intType, realType, wordType:
		return true
	default:
		return false
	}
}
