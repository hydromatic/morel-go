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

import "github.com/hydromatic/morel-go/internal/pp"

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

// PPFunctions returns the PP structure's native primitives, keyed
// by their bare names; the kernel binds each as a "PP" member. The
// derived combinators are defined in Morel (lib/pp.sml) over these.
func PPFunctions() map[string]Val {
	return map[string]Val{
		"empty":     pp.Empty(),
		"line":      pp.Line(),
		"lineBreak": pp.LineBreak(),
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
		"fillSep": ppListFn(func(ds []pp.Doc) pp.Doc {
			return pp.Fill(pp.Line(), ds)
		}),
		"fillCat": ppListFn(func(ds []pp.Doc) pp.Doc {
			return pp.Fill(pp.LineBreak(), ds)
		}),
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
