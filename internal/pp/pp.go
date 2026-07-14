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

// Package pp is a pretty-printer that lays out a document within
// a line-width limit.
//
// A Doc represents a set of possible layouts; Render chooses the
// best layout that fits a given line width. The design draws on
// Oppen's "Prettyprinting" (1980), Wadler's "A prettier printer"
// (2002), and Leijen's Column, Nesting, and FlatAlt extensions
// (which enable Align). Rendering follows Lindig's "Strictly
// Pretty" (2000): a single pass over an explicit work list,
// linear in the size of the document and using constant native
// stack however deeply the document nests.
package pp

import (
	"slices"
	"strings"
)

// Doc is a document that can be laid out in multiple ways.
// Instances are created by the functions in this package.
type Doc interface {
	doc()
}

type emptyDoc struct{}

// hardLineDoc is a newline, then the current indent, then the
// rest.
type hardLineDoc struct{}

type textDoc struct {
	text string
}

// flatAltDoc is primary when broken across lines, flat when
// flattened to one line.
type flatAltDoc struct {
	primary Doc
	flat    Doc
}

type catDoc struct {
	a Doc
	b Doc
}

type nestDoc struct {
	d      Doc
	indent int
}

// groupDoc lays out its document flat if it fits the remaining
// space, otherwise broken.
type groupDoc struct {
	d Doc
}

// unionDoc chooses wide if its first line fits the remaining
// space, otherwise narrow.
type unionDoc struct {
	wide   Doc
	narrow Doc
}

// columnDoc produces a document from the current column.
type columnDoc struct {
	f func(int) Doc
}

// nestingDoc produces a document from the current nesting level.
type nestingDoc struct {
	f func(int) Doc
}

func (emptyDoc) doc()    {}
func (hardLineDoc) doc() {}
func (textDoc) doc()     {}
func (flatAltDoc) doc()  {}
func (catDoc) doc()      {}
func (nestDoc) doc()     {}
func (groupDoc) doc()    {}
func (unionDoc) doc()    {}
func (columnDoc) doc()   {}
func (nestingDoc) doc()  {}

// Empty is the empty document.
func Empty() Doc { return emptyDoc{} }

// Text is a document containing literal text, which must not
// contain newlines.
func Text(s string) Doc {
	if s == "" {
		return emptyDoc{}
	}
	return textDoc{text: s}
}

// Line is a line break that becomes a space when flattened.
func Line() Doc {
	return flatAltDoc{primary: hardLineDoc{}, flat: Text(" ")}
}

// LineBreak is a line break that becomes nothing when flattened.
func LineBreak() Doc {
	return flatAltDoc{primary: hardLineDoc{}, flat: emptyDoc{}}
}

// HardLine is a line break that is always rendered, even when
// flattened.
func HardLine() Doc { return hardLineDoc{} }

// Beside concatenates two documents.
func Beside(a, b Doc) Doc { return catDoc{a: a, b: b} }

// Concat concatenates documents.
func Concat(docs ...Doc) Doc {
	result := Doc(emptyDoc{})
	for _, d := range slices.Backward(docs) {
		result = Beside(d, result)
	}
	return result
}

// Nest increases the indentation of a document.
func Nest(indent int, d Doc) Doc {
	if indent == 0 {
		return d
	}
	return nestDoc{indent: indent, d: d}
}

// Group lays a document out flat (line breaks become their flat
// alternatives) if it fits the remaining width, and otherwise
// broken.
func Group(d Doc) Doc {
	if _, ok := d.(groupDoc); ok {
		return d
	}
	return groupDoc{d: d}
}

// Union lays out wide if its first line fits the remaining
// space, otherwise narrow. Unlike Group, the alternatives may
// differ in structure.
func Union(wide, narrow Doc) Doc {
	return unionDoc{wide: wide, narrow: narrow}
}

// Align lays out a document with the nesting level set to the
// current column, so its later lines align under its first.
func Align(d Doc) Doc {
	return columnDoc{f: func(k int) Doc {
		return nestingDoc{f: func(i int) Doc {
			return nestDoc{indent: k - i, d: d}
		}}
	}}
}

// Fill packs documents onto as many lines as needed, putting as
// many as fit on each line, joined by glue when packed and by a
// line break otherwise. Each gap is decided by whether the
// following document, laid out flat, fits on the current line; a
// document is an indivisible unit even if it contains its own
// line breaks.
func Fill(glue Doc, docs []Doc) Doc {
	if len(docs) == 0 {
		return emptyDoc{}
	}
	// The first element renders normally; each later element is
	// preceded by a gap that is either glue (stay on the line) or
	// a line break. Built right to left so each suffix is shared.
	tail := Doc(emptyDoc{})
	for _, d := range slices.Backward(docs[1:]) {
		tail = Union(
			Beside(glue, Beside(Flatten(d), tail)),
			Beside(hardLineDoc{}, Beside(d, tail)))
	}
	return Beside(docs[0], tail)
}

// Flatten returns the flattened form of a document: every soft
// line break takes its flat alternative and every Group is laid
// out flat. A HardLine cannot be flattened and stays a break.
func Flatten(d Doc) Doc {
	// lint: sort until '^	}' where '^	case '
	switch d := d.(type) {
	case catDoc:
		return Beside(Flatten(d.a), Flatten(d.b))
	case columnDoc:
		return columnDoc{f: func(k int) Doc {
			return Flatten(d.f(k))
		}}
	case emptyDoc, hardLineDoc, textDoc:
		return d
	case flatAltDoc:
		return Flatten(d.flat)
	case groupDoc:
		return Flatten(d.d)
	case nestDoc:
		return nestDoc{indent: d.indent, d: Flatten(d.d)}
	case nestingDoc:
		return nestingDoc{f: func(i int) Doc {
			return Flatten(d.f(i))
		}}
	case unionDoc:
		return Flatten(d.wide)
	}
	return d
}

// item is an entry in the layout work list: render doc at the
// given indent and mode, then continue with next. The list is
// immutable, so a tail is shared between the flat and broken
// alternatives of a group.
type item struct {
	next   *item
	d      Doc
	indent int
	flat   bool
}

// Render renders a document to a string, choosing the best
// layout for the given line width.
func Render(width int, d Doc) string {
	var b strings.Builder
	k := 0 // current column
	it := &item{d: d}
	for it != nil {
		i, flat, next := it.indent, it.flat, it.next
		// lint: sort until '^		}' where '^		case '
		switch d := it.d.(type) {
		case catDoc:
			it = &item{
				d:      d.a,
				indent: i,
				flat:   flat,
				next: &item{
					d:      d.b,
					indent: i,
					flat:   flat,
					next:   next,
				},
			}
		case columnDoc:
			it = &item{
				d: d.f(k), indent: i, flat: flat,
				next: next,
			}
		case emptyDoc:
			it = next
		case flatAltDoc:
			d2 := d.primary
			if flat {
				d2 = d.flat
			}
			it = &item{d: d2, indent: i, flat: flat, next: next}
		case groupDoc:
			flatItem := &item{
				d:      d.d,
				indent: i,
				flat:   true,
				next:   next,
			}
			if fits(width, k, flatItem) {
				it = flatItem
			} else {
				it = &item{d: d.d, indent: i, next: next}
			}
		case hardLineDoc:
			b.WriteString("\n")
			writeSpaces(&b, i)
			k = i
			it = next
		case nestDoc:
			it = &item{
				d:      d.d,
				indent: i + d.indent,
				flat:   flat,
				next:   next,
			}
		case nestingDoc:
			it = &item{
				d: d.f(i), indent: i, flat: flat,
				next: next,
			}
		case textDoc:
			b.WriteString(d.text)
			k += len([]rune(d.text))
			it = next
		case unionDoc:
			wideItem := &item{
				d:      d.wide,
				indent: i,
				flat:   true,
				next:   next,
			}
			if fits(width, k, wideItem) {
				it = wideItem
			} else {
				it = &item{d: d.narrow, indent: i, next: next}
			}
		}
	}
	return b.String()
}

// fits reports whether the work list fits in the remaining space
// on the current line. It scans forward until the first line
// break (which ends the current line, so what precedes it fits)
// or until the width is exceeded.
func fits(width, col int, it *item) bool {
	for {
		if col > width {
			return false
		}
		if it == nil {
			return true
		}
		i, flat, next := it.indent, it.flat, it.next
		// lint: sort until '^		}' where '^		case '
		switch d := it.d.(type) {
		case catDoc:
			it = &item{
				d:      d.a,
				indent: i,
				flat:   flat,
				next: &item{
					d:      d.b,
					indent: i,
					flat:   flat,
					next:   next,
				},
			}
		case columnDoc:
			it = &item{
				d: d.f(col), indent: i, flat: flat,
				next: next,
			}
		case emptyDoc:
			it = next
		case flatAltDoc:
			d2 := d.primary
			if flat {
				d2 = d.flat
			}
			it = &item{d: d2, indent: i, flat: flat, next: next}
		case groupDoc:
			flatItem := &item{
				d:      d.d,
				indent: i,
				flat:   true,
				next:   next,
			}
			if !flat && !fits(width, col, flatItem) {
				return true
			}
			it = flatItem
		case hardLineDoc:
			// In a broken layout a line break ends the current
			// line, so what precedes it fits; a hard line cannot
			// be flattened, so a flat layout does not fit.
			return !flat
		case nestDoc:
			it = &item{
				d:      d.d,
				indent: i + d.indent,
				flat:   flat,
				next:   next,
			}
		case nestingDoc:
			it = &item{
				d: d.f(i), indent: i, flat: flat,
				next: next,
			}
		case textDoc:
			col += len([]rune(d.text))
			it = next
		case unionDoc:
			wideItem := &item{
				d:      d.wide,
				indent: i,
				flat:   true,
				next:   next,
			}
			if !fits(width, col, wideItem) {
				return true
			}
			it = wideItem
		}
	}
}

func writeSpaces(b *strings.Builder, n int) {
	for range max(n, 0) {
		b.WriteString(" ")
	}
}
