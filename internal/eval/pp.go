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

import "strings"

// ppDoc is a pretty-printer document: a tree of text and break
// points, laid out by ppRender in the style of Lindig's "Strictly
// Pretty". A doc value is opaque to Morel (the abstract type
// "doc").
type ppDoc struct {
	kind ppKind
	s    string // ppText, ppBreak: the text or flat separator
	n    int    // ppNest: the added indent
	a, b *ppDoc // ppCons: two documents; ppNest/ppGroup: a
}

type ppKind int

const (
	pkNil ppKind = iota
	pkText
	pkBreak // a space (flat) or newline (broken)
	pkHard  // always a newline; forces its group to break
	pkCons
	pkNest
	pkGroup
)

var ppEmpty = &ppDoc{kind: pkNil}

func ppText(s string) *ppDoc { return &ppDoc{kind: pkText, s: s} }
func ppBesideD(a, b *ppDoc) *ppDoc {
	return &ppDoc{kind: pkCons, a: a, b: b}
}
func ppGroupD(a *ppDoc) *ppDoc { return &ppDoc{kind: pkGroup, a: a} }
func ppNestD(n int, a *ppDoc) *ppDoc {
	return &ppDoc{kind: pkNest, n: n, a: a}
}

// ppIntersperse folds docs into one, inserting sep between each.
func ppIntersperse(docs []*ppDoc, sep *ppDoc) *ppDoc {
	if len(docs) == 0 {
		return ppEmpty
	}
	out := docs[0]
	for _, d := range docs[1:] {
		out = ppBesideD(out, ppBesideD(sep, d))
	}
	return out
}

// ppItem is a document with its indent and layout mode, the unit
// the renderer processes.
type ppItem struct {
	indent int
	flat   bool
	doc    *ppDoc
}

// ppFits reports whether the items lay out flat within w columns.
func ppFits(w int, items []ppItem) bool {
	for len(items) > 0 {
		if w < 0 {
			return false
		}
		it := items[len(items)-1]
		items = items[:len(items)-1]
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch it.doc.kind {
		case pkBreak:
			if it.flat {
				w -= len(it.doc.s)
			} else {
				return true
			}
		case pkCons:
			items = append(items,
				ppItem{it.indent, it.flat, it.doc.b},
				ppItem{it.indent, it.flat, it.doc.a})
		case pkGroup:
			items = append(items,
				ppItem{it.indent, true, it.doc.a})
		case pkHard:
			return false
		case pkNest:
			items = append(items, ppItem{
				it.indent + it.doc.n, it.flat, it.doc.a,
			})
		case pkNil:
		case pkText:
			w -= len(it.doc.s)
		}
	}
	return w >= 0
}

// ppRender lays doc out into a string no wider than w where it can.
func ppRender(w int, doc *ppDoc) string {
	var b strings.Builder
	k := 0 // current column
	stack := []ppItem{{0, false, doc}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch it.doc.kind {
		case pkBreak:
			if it.flat {
				b.WriteString(it.doc.s)
				k += len(it.doc.s)
			} else {
				b.WriteString("\n")
				b.WriteString(strings.Repeat(" ", it.indent))
				k = it.indent
			}
		case pkCons:
			stack = append(stack,
				ppItem{it.indent, it.flat, it.doc.b},
				ppItem{it.indent, it.flat, it.doc.a})
		case pkGroup:
			flat := ppFits(w-k, []ppItem{{it.indent, true, it.doc.a}})
			stack = append(stack, ppItem{it.indent, flat, it.doc.a})
		case pkHard:
			b.WriteString("\n")
			b.WriteString(strings.Repeat(" ", it.indent))
			k = it.indent
		case pkNest:
			stack = append(stack, ppItem{
				it.indent + it.doc.n, it.flat, it.doc.a,
			})
		case pkNil:
		case pkText:
			b.WriteString(it.doc.s)
			k += len(it.doc.s)
		}
	}
	return b.String()
}

// asDoc reads a doc value.
func asDoc(v Val) *ppDoc {
	d, _ := v.(*ppDoc)
	if d == nil {
		return ppEmpty
	}
	return d
}

// docList reads a list of doc values.
func docList(v Val) []*ppDoc {
	list := asList(v)
	out := make([]*ppDoc, len(list))
	for i, e := range list {
		out[i] = asDoc(e)
	}
	return out
}

// ppLine is a space when flat, a newline when broken; ppLineBreak
// is nothing when flat.
var (
	ppLine     = &ppDoc{kind: pkBreak, s: " "}
	ppLineBrk  = &ppDoc{kind: pkBreak, s: ""}
	ppHardLine = &ppDoc{kind: pkHard}
)

// PPFunctions returns the PP structure's members, keyed by their
// bare names; the kernel binds each as a "PP" member.
func PPFunctions() map[string]Val {
	return map[string]Val{
		"empty":     ppEmpty,
		"line":      ppLine,
		"lineBreak": ppLineBrk,
		"softLine":  ppGroupD(ppLine),
		"softBreak": ppGroupD(ppLineBrk),
		"hardLine":  ppHardLine,
		"text": Fn(func(a Val) (Val, error) {
			return ppText(asString(a)), nil
		}),
		"beside": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			return ppBesideD(asDoc(x), asDoc(y)), nil
		}),
		"nest": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			return ppNestD(int(asInt(x)), asDoc(y)), nil
		}),
		"group": Fn(func(a Val) (Val, error) {
			return ppGroupD(asDoc(a)), nil
		}),
		"hsep": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppIntersperse(ds, ppText(" "))
		}),
		"vsep": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppIntersperse(ds, ppLine)
		}),
		"sep": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppGroupD(ppIntersperse(ds, ppLine))
		}),
		"hcat": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppIntersperse(ds, ppEmpty)
		}),
		"vcat": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppIntersperse(ds, ppLineBrk)
		}),
		"cat": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppGroupD(ppIntersperse(ds, ppLineBrk))
		}),
		"fillSep": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppIntersperse(ds, ppGroupD(ppLine))
		}),
		"fillCat": ppListFn(func(ds []*ppDoc) *ppDoc {
			return ppIntersperse(ds, ppGroupD(ppLineBrk))
		}),
		"parens":   ppEncloseFn("(", ")"),
		"braces":   ppEncloseFn("{", "}"),
		"brackets": ppEncloseFn("[", "]"),
		"indent": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			n := int(asInt(x))
			return ppNestD(n,
				ppBesideD(ppText(strings.Repeat(" ", n)), asDoc(y))), nil
		}),
		"punctuate":  Fn(ppPunctuate),
		"encloseSep": Fn(ppEncloseSep),
		"render": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			return ppRender(int(asInt(x)), asDoc(y)), nil
		}),
	}
}

// ppListFn adapts a "doc list -> doc" combinator to a builtin.
func ppListFn(f func([]*ppDoc) *ppDoc) Fn {
	return func(a Val) (Val, error) {
		return f(docList(a)), nil
	}
}

// ppEncloseFn wraps a document in fixed delimiters.
func ppEncloseFn(left, right string) Fn {
	return func(a Val) (Val, error) {
		return ppBesideD(ppText(left),
			ppBesideD(asDoc(a), ppText(right))), nil
	}
}

// ppPunctuate is "punctuate (sep, docs)": sep is appended to every
// document but the last.
func ppPunctuate(a Val) (Val, error) {
	sep, docsVal := asPair(a)
	docs := docList(docsVal)
	out := make([]Val, len(docs))
	for i, d := range docs {
		if i < len(docs)-1 {
			out[i] = ppBesideD(d, asDoc(sep))
		} else {
			out[i] = d
		}
	}
	return out, nil
}

// ppEncloseSep is "encloseSep (left, right, sep, docs)": the
// documents between delimiters, each pair separated by sep and a
// line break, laid out flat if it fits.
func ppEncloseSep(a Val) (Val, error) {
	args := a.([]Val) //nolint:forcetypeassert // a is a 4-tuple
	left, right, sep := asDoc(args[0]), asDoc(args[1]), asDoc(args[2])
	docs := docList(args[3])
	if len(docs) == 0 {
		return ppBesideD(left, right), nil
	}
	body := docs[0]
	for _, d := range docs[1:] {
		body = ppBesideD(body,
			ppBesideD(sep, ppBesideD(ppLine, d)))
	}
	return ppGroupD(ppBesideD(left, ppBesideD(body, right))), nil
}
