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
// collection of records as a table.
const outputTabular = "TABULAR"

// tabularEllipsis marks a collection truncated by printLength.
const tabularEllipsis = "..."

// tabularBinding renders "name : type" preceded by a table, when the
// value is a collection (list or bag) of records or tuples whose
// fields are tabular-printable. A field may itself be a scalar (a
// primitive or an enum), an option of a scalar (NONE prints blank), a
// collection of scalars (expanding vertically), or a record/tuple --
// recursively -- rendered as a nested sub-table. The bool result is false when
// the type is not tabular-printable, so the caller falls back to
// classic rendering.
func (c *Config) tabularBinding(name string, v eval.Val,
	t types.Type,
) (string, bool) {
	elem := collectionElemType(t)
	if elem == nil || !c.canPrintRecordLike(elem) {
		return "", false
	}
	// The table is at query depth 0; a printDepth of 0 would render
	// the elements as "#", so fall back to classic to match.
	if c.PrintDepth >= 0 && c.PrintDepth < 1 {
		return "", false
	}
	root := c.sectionForRecord("", elem, tabList)
	rootCell, _ := c.buildCell(root, v).(*recordListCell)
	root.finalizeWidths()

	var b strings.Builder
	headerLines := 0
	for _, child := range root.children {
		headerLines = max(headerLines, child.headerDepth())
	}
	for line := range headerLines {
		b.WriteString(emitHeaderLine(root.children, line))
		b.WriteByte('\n')
	}
	b.WriteString(emitSeparatorRow(root.children))
	b.WriteByte('\n')
	c.emitDataRows(&b, root, rootCell)
	b.WriteString("\nval " + parse.QuoteIdent(name) + " : " +
		pp.Render(math.MaxInt32, c.typeDoc(t)))
	return b.String(), true
}

// collectionElemType returns the element type of a list or bag, or
// nil if t is neither. A table can be printed from either.
func collectionElemType(t types.Type) types.Type {
	switch t := t.(type) {
	case *types.List:
		return t.Elem
	case *types.Named:
		if t.Name == bagType && len(t.Args) == 1 {
			return t.Args[0]
		}
	}
	return nil
}

// --- Tabular-printability ------------------------------------------

// canPrintRecordLike reports whether a record or tuple type, and all
// its fields, can be printed as a table.
func (c *Config) canPrintRecordLike(t types.Type) bool {
	if !isRecordLike(t) {
		return false
	}
	for _, f := range recordLikeFields(t) {
		if !c.canPrintField(f.Type) {
			return false
		}
	}
	return true
}

// canPrintField reports whether a field type is acceptable in a row:
// a scalar; an option of a scalar (NONE prints blank); a record
// or tuple (a nested sub-table); a record/tuple option (a sub-table,
// blank for NONE, only if not every field is itself an option); or a
// collection of scalars or of records/tuples (recursively).
func (c *Config) canPrintField(t types.Type) bool {
	if c.isScalar(t) {
		return true
	}
	if _, ok := c.optionScalar(t); ok {
		return true
	}
	if isRecordLike(t) {
		return c.canPrintRecordLike(t)
	}
	if or := optionRecord(t); or != nil {
		return c.canPrintRecordLike(or) && !allFieldsOption(or)
	}
	if elem := collectionElemType(t); elem != nil {
		if c.isScalar(elem) {
			return true
		}
		return c.canPrintRecordLike(elem)
	}
	return false
}

// isScalar reports whether a type prints as a single-token scalar,
// namely a primitive or an enum.
func (c *Config) isScalar(t types.Type) bool {
	if _, ok := t.(*types.Primitive); ok {
		return true
	}
	return c.isEnum(t)
}

// isEnum reports whether t is a datatype every constructor of which
// is nullary, such as "order", or a user-defined "datatype color =
// BLUE | GREEN | RED". Its values print as the bare constructor
// name, and so occupy a scalar column.
//
// A collection ("bag") is a datatype with no constructors, and is
// excluded: it is handled as a collection.
func (c *Config) isEnum(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok || c.sys == nil {
		return false
	}
	cons := c.sys.Constructors(named.Name)
	if len(cons) == 0 {
		return false
	}
	for _, con := range cons {
		if con.Arg != nil {
			return false
		}
	}
	return true
}

// isRecordLike reports whether t is a record or tuple.
func isRecordLike(t types.Type) bool {
	switch t.(type) {
	case *types.Record, *types.Tuple:
		return true
	default:
		return false
	}
}

// recordLikeFields returns the label/type pairs of a record or tuple,
// tuple fields being named "1", "2", ...; nil for any other type.
func recordLikeFields(t types.Type) []types.Field {
	switch t := t.(type) {
	case *types.Record:
		return t.Fields
	case *types.Tuple:
		fields := make([]types.Field, len(t.Args))
		for i, a := range t.Args {
			fields[i] = types.Field{Label: strconv.Itoa(i + 1), Type: a}
		}
		return fields
	default:
		return nil
	}
}

// optionArg returns T and true if t is "T option", else nil, false.
func optionArg(t types.Type) (types.Type, bool) {
	if n, ok := t.(*types.Named); ok &&
		n.Name == "option" && len(n.Args) == 1 {
		return n.Args[0], true
	}
	return nil, false
}

// optionScalar returns T if t is "T option" with T a scalar, else
// nil, false. Such a field is a scalar column: "SOME x" renders as
// x, and NONE as a blank cell.
func (c *Config) optionScalar(t types.Type) (types.Type, bool) {
	if a, ok := optionArg(t); ok && c.isScalar(a) {
		return a, true
	}
	return nil, false
}

// optionRecord returns T if t is "T option" with T a record or tuple,
// else nil.
func optionRecord(t types.Type) types.Type {
	if a, ok := optionArg(t); ok && isRecordLike(a) {
		return a
	}
	return nil
}

// isOption reports whether t is an option type.
func isOption(t types.Type) bool {
	_, ok := optionArg(t)
	return ok
}

// allFieldsOption reports whether every field of a record-like type
// is an option.
func allFieldsOption(t types.Type) bool {
	for _, f := range recordLikeFields(t) {
		if !isOption(f.Type) {
			return false
		}
	}
	return true
}

// isNumericType reports whether a column of this type is right-aligned,
// as int, real, and word columns are.
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

// --- Section tree (column structure) -------------------------------

type tabKind int

const (
	tabScalar tabKind = iota
	tabScalarList
	tabRecordList
)

// tabShape is how a record-list section's value maps to rows.
type tabShape int

const (
	tabList   tabShape = iota // value is a list of records
	tabSingle                 // value is a single record: one row
	tabOption                 // value is a record option: SOME is one row
)

// section is a node in the column tree describing one tabular field.
type section struct {
	kind       tabKind
	name       string
	rightAlign bool
	optional   bool       // a scalar wrapped in option
	prim       types.Type // scalar/scalar-list element primitive
	shape      tabShape
	children   []*section
	width      int
}

// sectionForRecord builds a section for a record or tuple type.
func (c *Config) sectionForRecord(name string, t types.Type,
	shape tabShape,
) *section {
	fields := recordLikeFields(t)
	children := make([]*section, len(fields))
	for i, f := range fields {
		children[i] = c.sectionForField(f.Label, f.Type)
	}
	return &section{
		kind: tabRecordList, name: name, shape: shape,
		children: children, width: len(name),
	}
}

// sectionForField builds a section for one field of a record-like type.
func (c *Config) sectionForField(name string,
	t types.Type,
) *section {
	if c.isScalar(t) {
		return &section{
			kind: tabScalar, name: name, rightAlign: isNumericType(t),
			prim: t, width: len(name),
		}
	}
	if p, ok := c.optionScalar(t); ok {
		return &section{
			kind: tabScalar, name: name, rightAlign: isNumericType(p),
			optional: true, prim: p, width: len(name),
		}
	}
	if isRecordLike(t) {
		return c.sectionForRecord(name, t, tabSingle)
	}
	if or := optionRecord(t); or != nil {
		return c.sectionForRecord(name, or, tabOption)
	}
	elem := collectionElemType(t)
	if c.isScalar(elem) {
		return &section{
			kind: tabScalarList, name: name,
			rightAlign: isNumericType(elem), prim: elem, width: len(name),
		}
	}
	return c.sectionForRecord(name, elem, tabList)
}

// finalizeWidths propagates widths bottom-up: a record-list's width is
// the sum of its children plus separators, and if its name is wider
// the last child grows to compensate.
func (s *section) finalizeWidths() {
	if s.kind != tabRecordList {
		return
	}
	sum := 0
	for _, ch := range s.children {
		ch.finalizeWidths()
		sum += ch.width
	}
	if len(s.children) > 0 {
		sum += len(s.children) - 1
	}
	if len(s.name) > sum && len(s.children) > 0 {
		s.children[len(s.children)-1].growWidth(len(s.name) - sum)
		sum = len(s.name)
	}
	s.width = sum
}

// growWidth widens a section, a record-list passing the slack to its
// last child so it eventually lands on a leaf.
func (s *section) growWidth(extra int) {
	s.width += extra
	if s.kind == tabRecordList && len(s.children) > 0 {
		s.children[len(s.children)-1].growWidth(extra)
	}
}

// headerDepth is the number of header lines this section contributes.
func (s *section) headerDepth() int {
	if s.kind != tabRecordList {
		return 1
	}
	m := 0
	for _, ch := range s.children {
		m = max(m, ch.headerDepth())
	}
	return 1 + m
}

// appendHeaderCell writes this section's header cell at the given line,
// padded to its width.
func (s *section) appendHeaderCell(b *strings.Builder, line int) {
	switch {
	case line == 0:
		b.WriteString(s.name)
		b.WriteString(strings.Repeat(" ", s.width-len(s.name)))
	case s.kind == tabRecordList:
		for i, ch := range s.children {
			if i > 0 {
				b.WriteByte(' ')
			}
			ch.appendHeaderCell(b, line-1)
		}
	default:
		b.WriteString(strings.Repeat(" ", s.width))
	}
}

// appendSeparator writes the dashed separator cells for this section.
func (s *section) appendSeparator(b *strings.Builder) {
	if s.kind == tabRecordList {
		for i, ch := range s.children {
			if i > 0 {
				b.WriteByte(' ')
			}
			ch.appendSeparator(b)
		}
		return
	}
	b.WriteString(strings.Repeat("-", s.width))
}

// pad renders s padded (or right-aligned) to this section's width.
func (s *section) pad(str string) string {
	gap := max(s.width-len(str), 0)
	if s.rightAlign {
		return strings.Repeat(" ", gap) + str
	}
	return str + strings.Repeat(" ", gap)
}

// --- Cell tree (cached row data, mirrors the section tree) ----------

type cell interface {
	iter(s *section) lineIter
}

// scalarCell holds the (folded) lines of a scalar or the items of a
// scalar list.
type scalarCell struct {
	lines []string
}

func (c *scalarCell) iter(s *section) lineIter {
	return &scalarIter{section: s, items: c.lines}
}

// recordListCell holds one row of cells per nested record.
type recordListCell struct {
	records   [][]cell
	truncated bool
}

func (c *recordListCell) iter(s *section) lineIter {
	return &recordListIter{
		section: s, records: c.records, truncated: c.truncated,
	}
}

// foldWidth is the width at which a section's cells fold, or 0
// where they do not. Only a string column folds: "stringFold" is
// the width at which long strings break, and a number that
// happens to be long is not a string.
func (c *Config) foldWidth(s *section) int {
	if s.prim != nil && s.prim.String() == stringType {
		return c.StringFold
	}
	return 0
}

// buildCell builds a cell from a value, updating leaf widths.
func (c *Config) buildCell(s *section, value eval.Val) cell {
	switch s.kind {
	case tabScalar:
		var str string
		if s.optional {
			con, _ := value.(eval.Con)
			switch {
			case con.Name != "SOME":
				str = ""
			case s.prim.String() == stringType:
				sv, _ := con.Arg.(string)
				str = c.optionString(sv)
			default:
				str = c.scalarString(s.prim, con.Arg)
			}
		} else {
			str = c.scalarString(s.prim, value)
		}
		lines := foldString(str, c.foldWidth(s))
		for _, ln := range lines {
			s.width = max(s.width, len(ln))
		}
		return &scalarCell{lines: lines}
	case tabScalarList:
		var items []string
		for i, item := range asVals(value) {
			if c.PrintLength >= 0 && i >= c.PrintLength {
				items = append(items, tabularEllipsis)
				s.width = max(s.width, len(tabularEllipsis))
				break
			}
			item := c.scalarString(s.prim, item)
			for _, ln := range foldString(item, c.foldWidth(s)) {
				s.width = max(s.width, len(ln))
				items = append(items, ln)
			}
		}
		return &scalarCell{lines: items}
	default:
		return c.buildRecordListCell(s, value)
	}
}

// buildRecordListCell builds the cell for a record-list section,
// normalizing the value to a list of records per the section's shape.
func (c *Config) buildRecordListCell(s *section, value eval.Val) cell {
	var recordList []eval.Val
	switch s.shape {
	case tabSingle:
		recordList = []eval.Val{value}
	case tabOption:
		if con, _ := value.(eval.Con); con.Name == "SOME" {
			recordList = []eval.Val{con.Arg}
		}
	default:
		recordList = asVals(value)
	}
	var records [][]cell
	truncated := false
	for i, rec := range recordList {
		if c.PrintLength >= 0 && i >= c.PrintLength {
			truncated = true
			break
		}
		fields := asVals(rec)
		rowCells := make([]cell, len(s.children))
		for j, child := range s.children {
			rowCells[j] = c.buildCell(child, fields[j])
		}
		records = append(records, rowCells)
	}
	if truncated {
		s.width = max(s.width, len(tabularEllipsis))
	}
	return &recordListCell{records: records, truncated: truncated}
}

// --- Line iterators ------------------------------------------------

type lineIter interface {
	hasNext() bool
	next() string
}

// scalarIter emits each cached line, padded to the section's width.
type scalarIter struct {
	section *section
	items   []string
	idx     int
}

func (it *scalarIter) hasNext() bool { return it.idx < len(it.items) }

func (it *scalarIter) next() string {
	s := it.section.pad(it.items[it.idx])
	it.idx++
	return s
}

// rowIter joins child iterators side by side with single-space
// separators, padding shorter children with blanks, and emits at least
// one line even when every child is empty.
type rowIter struct {
	sections  []*section
	children  []lineIter
	firstDone bool
}

func (it *rowIter) hasNext() bool {
	if !it.firstDone {
		return true
	}
	for _, ch := range it.children {
		if ch.hasNext() {
			return true
		}
	}
	return false
}

func (it *rowIter) next() string {
	it.firstDone = true
	var b strings.Builder
	for i, ch := range it.children {
		if i > 0 {
			b.WriteByte(' ')
		}
		if ch.hasNext() {
			b.WriteString(ch.next())
		} else {
			b.WriteString(strings.Repeat(" ", it.sections[i].width))
		}
	}
	return b.String()
}

// recordListIter streams the lines of a nested collection of records,
// one record's rowIter after another, then an ellipsis line if the
// records were truncated.
type recordListIter struct {
	section    *section
	records    [][]cell
	truncated  bool
	recordIdx  int
	currentRow *rowIter
	truncDone  bool
	buffered   bool
	buf        string
	done       bool
}

func (it *recordListIter) compute() (string, bool) {
	for {
		if it.currentRow == nil {
			if it.recordIdx >= len(it.records) {
				if it.truncated && !it.truncDone {
					it.truncDone = true
					return it.section.pad(tabularEllipsis), true
				}
				return "", false
			}
			rec := it.records[it.recordIdx]
			it.recordIdx++
			subIters := make([]lineIter, len(it.section.children))
			for i := range it.section.children {
				subIters[i] = rec[i].iter(it.section.children[i])
			}
			it.currentRow = &rowIter{
				sections: it.section.children, children: subIters,
			}
		}
		if it.currentRow.hasNext() {
			return it.currentRow.next(), true
		}
		it.currentRow = nil
	}
}

func (it *recordListIter) hasNext() bool {
	if it.done {
		return false
	}
	if !it.buffered {
		line, ok := it.compute()
		if !ok {
			it.done = true
			return false
		}
		it.buf, it.buffered = line, true
	}
	return true
}

func (it *recordListIter) next() string {
	if !it.hasNext() {
		return ""
	}
	it.buffered = false
	return it.buf
}

// --- Emit ----------------------------------------------------------

// emitHeaderLine renders one header line across the top-level sections,
// trailing spaces stripped.
func emitHeaderLine(children []*section, line int) string {
	var b strings.Builder
	for i, ch := range children {
		if i > 0 {
			b.WriteByte(' ')
		}
		ch.appendHeaderCell(&b, line)
	}
	return strings.TrimRight(b.String(), " ")
}

// emitSeparatorRow renders the dashed separator row.
func emitSeparatorRow(children []*section) string {
	var b strings.Builder
	for i, ch := range children {
		if i > 0 {
			b.WriteByte(' ')
		}
		ch.appendSeparator(&b)
	}
	return b.String()
}

// emitDataRows writes the data rows: each top-level record drives a
// rowIter to exhaustion (at least one line), then a final ellipsis
// line if the collection was truncated.
func (c *Config) emitDataRows(b *strings.Builder, root *section,
	rootCell *recordListCell,
) {
	for _, record := range rootCell.records {
		iters := make([]lineIter, len(root.children))
		for i := range root.children {
			iters[i] = record[i].iter(root.children[i])
		}
		row := &rowIter{sections: root.children, children: iters}
		for {
			b.WriteString(strings.TrimRight(row.next(), " "))
			b.WriteByte('\n')
			if !row.hasNext() {
				break
			}
		}
	}
	if rootCell.truncated {
		b.WriteString(tabularEllipsis)
		b.WriteByte('\n')
	}
}

// --- Scalar rendering ----------------------------------------------

// optionString renders a string-option's SOME value so it is never
// blank: an SML string literal (double-quoted, with backslashes and
// double-quotes escaped) when empty or containing a double-quote,
// else verbatim (subject to stringDepth truncation).
func (c *Config) optionString(s string) string {
	if s == "" || strings.Contains(s, `"`) {
		r := strings.ReplaceAll(s, `\`, `\\`)
		r = strings.ReplaceAll(r, `"`, `\"`)
		return `"` + r + `"`
	}
	if c.StringDepth >= 0 && len(s) > c.StringDepth {
		return s[:c.StringDepth] + "#"
	}
	return s
}

// scalarString renders a scalar value unquoted for a table cell: an
// int in plain decimal, a real with "-" for its sign, a string
// verbatim (truncated by stringDepth), a word in hexadecimal, an
// enum as its bare constructor name.
func (c *Config) scalarString(prim types.Type, v eval.Val) string {
	if con, ok := v.(eval.Con); ok && con.Arg == nil {
		return con.Name
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
		// A table is not Standard ML text, and reads better with
		// minus signs: "-2.5" and "1.25E-6", not "~2.5" and
		// "1.25E~6". A real's rendering uses "~" only as a sign,
		// on the mantissa and on the exponent, so replacing it is
		// exact. Strings are untouched, so the output of
		// Real.toString still reads as Standard ML in a cell.
		return strings.ReplaceAll(eval.FormatReal(r), "~", "-")
	case stringType:
		s, _ := v.(string)
		if c.StringDepth >= 0 && len(s) > c.StringDepth {
			return s[:c.StringDepth] + "#"
		}
		return s
	case wordType:
		w, _ := v.(uint64)
		return "0wx" + strings.ToUpper(strconv.FormatUint(w, 16))
	default:
		return "()"
	}
}

// foldString folds a string into lines. String folding is not a
// configured property yet, so it returns the string unchanged.
func foldString(s string, width int) []string {
	if width <= 0 || len(s) <= width {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		// Prefer the last space that leaves the line within the
		// width; the space itself is consumed by the break.
		if i := strings.LastIndex(s[:width+1], " "); i > 0 {
			lines = append(lines, s[:i])
			s = s[i+1:]
			continue
		}
		// No break point: split at the width.
		lines = append(lines, s[:width])
		s = s[width:]
	}
	return append(lines, s)
}
