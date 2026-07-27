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
	"strings"

	"github.com/hydromatic/morel-go/internal/pp"
)

// The PP structure is a Wadler-Leijen pretty-printer whose "doc"
// values are the documents of the shared internal/pp package — the
// same combinator library the shell uses to lay out its output, so
// there is a single pretty-printer.

// asDoc reads a doc value.
func asDoc(v Val) pp.Doc {
	d, ok := v.(pp.Doc)
	if !ok {
		return pp.Empty()
	}
	return d
}

// docList reads a list of doc values.
func docList(v Val) []pp.Doc {
	list := asList(v)
	out := make([]pp.Doc, len(list))
	for i, e := range list {
		out[i] = asDoc(e)
	}
	return out
}

// intersperseDocs joins docs, inserting sep between each.
func intersperseDocs(docs []pp.Doc, sep pp.Doc) pp.Doc {
	if len(docs) == 0 {
		return pp.Empty()
	}
	out := docs[0]
	for _, d := range docs[1:] {
		out = pp.Beside(out, pp.Beside(sep, d))
	}
	return out
}

// PPFunctions returns the PP structure's members, keyed by their
// bare names; the kernel binds each as a "PP" member.
func PPFunctions() map[string]Val {
	return map[string]Val{
		"empty":     pp.Empty(),
		"line":      pp.Line(),
		"lineBreak": pp.LineBreak(),
		"softLine":  pp.Group(pp.Line()),
		"softBreak": pp.Group(pp.LineBreak()),
		"hardLine":  pp.HardLine(),
		"text": Fn(func(a Val) (Val, error) {
			return pp.Text(asString(a)), nil
		}),
		"beside": ppPairFn(pp.Beside),
		"nest": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			return pp.Nest(int(asInt(x)), asDoc(y)), nil
		}),
		"group": Fn(func(a Val) (Val, error) {
			return pp.Group(asDoc(a)), nil
		}),
		"align": Fn(func(a Val) (Val, error) {
			return pp.Align(asDoc(a)), nil
		}),
		"hang": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			return pp.Align(pp.Nest(int(asInt(x)), asDoc(y))), nil
		}),
		"indent": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			n := int(asInt(x))
			return pp.Nest(n, pp.Beside(
				pp.Text(strings.Repeat(" ", n)), asDoc(y))), nil
		}),
		"hsep": ppListFn(func(ds []pp.Doc) pp.Doc {
			return intersperseDocs(ds, pp.Text(" "))
		}),
		"vsep": ppListFn(func(ds []pp.Doc) pp.Doc {
			return intersperseDocs(ds, pp.Line())
		}),
		"sep": ppListFn(func(ds []pp.Doc) pp.Doc {
			return pp.Group(intersperseDocs(ds, pp.Line()))
		}),
		"hcat": ppListFn(func(ds []pp.Doc) pp.Doc {
			return pp.Concat(ds...)
		}),
		"vcat": ppListFn(func(ds []pp.Doc) pp.Doc {
			return intersperseDocs(ds, pp.LineBreak())
		}),
		"cat": ppListFn(func(ds []pp.Doc) pp.Doc {
			return pp.Group(intersperseDocs(ds, pp.LineBreak()))
		}),
		"fillSep": ppListFn(func(ds []pp.Doc) pp.Doc {
			return pp.Fill(pp.Line(), ds)
		}),
		"fillCat": ppListFn(func(ds []pp.Doc) pp.Doc {
			return pp.Fill(pp.LineBreak(), ds)
		}),
		"parens":     ppEncloseFn("(", ")"),
		"braces":     ppEncloseFn("{", "}"),
		"brackets":   ppEncloseFn("[", "]"),
		"punctuate":  Fn(ppPunctuate),
		"encloseSep": Fn(ppEncloseSep),
		"render": Fn(func(a Val) (Val, error) {
			x, y := asPair(a)
			return pp.Render(int(asInt(x)), asDoc(y)), nil
		}),
	}
}

// ppPairFn adapts a "doc * doc -> doc" combinator to a builtin.
func ppPairFn(f func(a, b pp.Doc) pp.Doc) Fn {
	return func(a Val) (Val, error) {
		x, y := asPair(a)
		return f(asDoc(x), asDoc(y)), nil
	}
}

// ppListFn adapts a "doc list -> doc" combinator to a builtin.
func ppListFn(f func([]pp.Doc) pp.Doc) Fn {
	return func(a Val) (Val, error) {
		return f(docList(a)), nil
	}
}

// ppEncloseFn wraps a document in fixed delimiters.
func ppEncloseFn(left, right string) Fn {
	return func(a Val) (Val, error) {
		return pp.Beside(pp.Text(left),
			pp.Beside(asDoc(a), pp.Text(right))), nil
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
			out[i] = pp.Beside(d, asDoc(sep))
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
	args, _ := a.([]Val)
	left, right, sep := asDoc(args[0]), asDoc(args[1]), asDoc(args[2])
	docs := docList(args[3])
	if len(docs) == 0 {
		return pp.Beside(left, right), nil
	}
	body := docs[0]
	for _, d := range docs[1:] {
		body = pp.Beside(body,
			pp.Beside(sep, pp.Beside(pp.Line(), d)))
	}
	return pp.Group(pp.Beside(left, pp.Beside(body, right))), nil
}
