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

package compile

import (
	"sort"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/types"
)

// topBuiltin describes a top-level built-in value: its type and,
// for the operators that SML overloads over the numeric types,
// the type to prefer if unification leaves it undetermined.
type topBuiltin struct {
	typ       string
	preferred string
}

// Types shared by several built-ins.
const (
	numPair    = "'a * 'a -> 'a"
	opTimes    = "op *"
	opPlus     = "op +"
	opMinus    = "op -"
	opDiv      = "op div"
	opMod      = "op mod"
	opNegate   = "op ~"
	comparison = "'a * 'a -> bool"
	realToInt  = "real -> int"
	boolName   = "bool"
	intName    = "int"
	realName   = "real"
	wordName   = "word"
)

// topBuiltins are the built-in values that no structure's
// signature file declares: the operators, and the top-level
// aliases of common functions.
var topBuiltins = map[string]topBuiltin{
	"abs":     {"'a -> 'a", intName},
	"ceil":    {realToInt, ""},
	"chr":     {"int -> char", ""},
	"concat":  {"string list -> string", ""},
	"explode": {"string -> char list", ""},
	"floor":   {realToInt, ""},
	"getOpt":  {"'a option * 'a -> 'a", ""},
	"hd":      {"'a list -> 'a", ""},
	"implode": {"char list -> string", ""},
	"isSome":  {"'a option -> bool", ""},
	"length":  {"'a list -> int", ""},
	"map":     {"('a -> 'b) -> 'a list -> 'b list", ""},
	"not":     {"bool -> bool", ""},
	"null":    {"'a list -> bool", ""},
	opTimes:   {numPair, intName},
	opPlus:    {numPair, intName},
	opMinus:   {numPair, intName},
	"op /":    {numPair, realName},
	"op ::":   {"'a * 'a list -> 'a list", ""},
	"op <":    {comparison, ""},
	"op <=":   {comparison, ""},
	"op <>":   {comparison, ""},
	"op =":    {comparison, ""},
	"op >":    {comparison, ""},
	"op >=":   {comparison, ""},
	"op @":    {"'a list * 'a list -> 'a list", ""},
	"op ^":    {"string * string -> string", ""},
	opDiv:     {numPair, intName},
	opMod:     {numPair, intName},
	"op o":    {"('b -> 'c) * ('a -> 'b) -> 'a -> 'c", ""},
	opNegate:  {"'a -> 'a", intName},
	"ord":     {"char -> int", ""},
	"real":    {"int -> real", ""},
	"rev":     {"'a list -> 'a list", ""},
	"round":   {realToInt, ""},
	"size":    {"string -> int", ""},
	"str":     {"char -> string", ""},
	"tl":      {"'a list -> 'a list", ""},
	"trunc":   {realToInt, ""},
	"valOf":   {"'a option -> 'a", ""},
	"env":     {"unit -> (string * string) list", ""},
	"plan":    {"unit -> string", ""},
	"set":     {"string * 'a -> unit", ""},
	"show":    {"string -> string option", ""},
	"showAll": {"unit -> (string * string option) list", ""},
	"unset":   {"string -> unit", ""},
}

// infixOpNames maps an infix operator's Op to the name of its
// top-level binding.
var infixOpNames = map[ast.Op]string{
	ast.AtOp:      "op @",
	ast.CaretOp:   "op ^",
	ast.ComposeOp: "op o",
	ast.ConsOp:    "op ::",
	ast.DivOp:     opDiv,
	ast.DivideOp:  "op /",
	ast.EqOp:      "op =",
	ast.GeOp:      "op >=",
	ast.GtOp:      "op >",
	ast.LeOp:      "op <=",
	ast.LtOp:      "op <",
	ast.MinusOp:   opMinus,
	ast.ModOp:     opMod,
	ast.NeOp:      "op <>",
	ast.PlusOp:    opPlus,
	ast.TimesOp:   opTimes,
}

// TopBindings returns the bindings of the top-level built-in
// values, sorted by name.
func TopBindings(sys *types.System) []Binding {
	names := make([]string, 0, len(topBuiltins))
	for name := range topBuiltins {
		names = append(names, name)
	}
	sort.Strings(names)
	bindings := make([]Binding, 0, len(names))
	for _, name := range names {
		t, err := sys.Parse(topBuiltins[name].typ)
		if err != nil {
			panic("bad built-in type for " + name + ": " +
				err.Error())
		}
		bindings = append(bindings,
			Binding{Name: name, Type: t})
	}
	return bindings
}
