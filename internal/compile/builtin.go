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
	absName    = "abs"
	opAt       = "op @"
	opCaret    = "op ^"
	opCons     = "op ::"
	opElem     = "op elem"
	opGe       = "op >="
	opGt       = "op >"
	opLe       = "op <="
	opLt       = "op <"
	opNe       = "op <>"
	opNotElem  = "op notelem"
	notName    = "not"
	comparison = "'a * 'a -> bool"
	realToInt  = "real -> int"
	strToUnit  = "string -> unit"
	boolName   = "bool"
	intName    = "int"
	sumName    = "sum"
	realName   = "real"
	wordName   = "word"
	stringName = "string"
)

// topBuiltins are the built-in values that no structure's
// signature file declares: the operators, and the top-level
// aliases of common functions.
var topBuiltins = map[string]topBuiltin{
	absName:       {"'a -> 'a", intName},
	"app":         {"('a -> unit) -> 'a list -> unit", ""},
	"bag":         {"'a list -> 'a bag", ""},
	"ceil":        {realToInt, ""},
	"chr":         {"int -> char", ""},
	"compare":     {"'a * 'a -> `order`", ""},
	"concat":      {"string list -> string", ""},
	"env":         {"unit -> (string * string) list", ""},
	"exnMessage":  {"exn -> string", ""},
	"exnName":     {"exn -> string", ""},
	"explode":     {"string -> char list", ""},
	"fields":      {"(char -> bool) -> string -> string list", ""},
	"floor":       {realToInt, ""},
	"foldl":       {"('a * 'b -> 'b) -> 'b -> 'a list -> 'b", ""},
	"foldr":       {"('a * 'b -> 'b) -> 'b -> 'a list -> 'b", ""},
	"getOpt":      {"'a option * 'a -> 'a", ""},
	"hd":          {"'a list -> 'a", ""},
	"implode":     {"char list -> string", ""},
	"isSome":      {"'a option -> bool", ""},
	"iterate":     {"'a list -> ('a list * 'a list -> 'a list) -> 'a list", ""},
	"length":      {"'a list -> int", ""},
	"map":         {"('a -> 'b) -> 'a list -> 'b list", ""},
	notName:       {"bool -> bool", ""},
	"null":        {"'a list -> bool", ""},
	opTimes:       {numPair, intName},
	opPlus:        {numPair, intName},
	opMinus:       {numPair, intName},
	"op /":        {numPair, realName},
	opCons:        {"'a * 'a list -> 'a list", ""},
	opLt:          {comparison, ""},
	opLe:          {comparison, ""},
	opNe:          {comparison, ""},
	eqOpName:      {comparison, ""},
	opGt:          {comparison, ""},
	opGe:          {comparison, ""},
	opAt:          {"'a list * 'a list -> 'a list", ""},
	opCaret:       {"string * string -> string", ""},
	opDiv:         {numPair, intName},
	opMod:         {numPair, intName},
	"op o":        {"('b -> 'c) * ('a -> 'b) -> 'a -> 'c", ""},
	opNegate:      {"'a -> 'a", intName},
	"ord":         {"char -> int", ""},
	"plan":        {"unit -> string", ""},
	"real":        {"int -> real", ""},
	"rev":         {"'a list -> 'a list", ""},
	"round":       {realToInt, ""},
	"set":         {"string * 'a -> unit", ""},
	"show":        {"string -> string option", ""},
	"showAll":     {"unit -> (string * string option) list", ""},
	"size":        {"string -> int", ""},
	"str":         {"char -> string", ""},
	"substring":   {"string * int * int -> string", ""},
	"tl":          {"'a list -> 'a list", ""},
	"tokens":      {"(char -> bool) -> string -> string list", ""},
	"unset":       {strToUnit, ""},
	"trunc":       {realToInt, ""},
	"use":         {strToUnit, ""},
	"useSilently": {strToUnit, ""},
	"valOf":       {"'a option -> 'a", ""},
	"vector":      {"'a list -> 'a vector", ""},
}

// eqOpName is the top-level binding of the equality operator.
const eqOpName = "op ="

// infixOpNames maps an infix operator's Op to the name of its
// top-level binding.
var infixOpNames = map[ast.Op]string{
	ast.AtOp:      opAt,
	ast.CaretOp:   opCaret,
	ast.ComposeOp: "op o",
	ast.ConsOp:    opCons,
	ast.ElemOp:    opElem,
	ast.NotElemOp: opNotElem,
	ast.DivOp:     opDiv,
	ast.DivideOp:  "op /",
	ast.EqOp:      eqOpName,
	ast.GeOp:      opGe,
	ast.GtOp:      opGt,
	ast.LeOp:      opLe,
	ast.LtOp:      opLt,
	ast.MinusOp:   opMinus,
	ast.ModOp:     opMod,
	ast.NeOp:      opNe,
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
	return append(bindings, collectionBindings(sys)...)
}

// collectionBindings returns the built-ins whose type involves a
// collection (list or bag) with free orderedness — "elem" and
// "notelem" over a collection, and "only". Their types cannot be
// written as a plain string, so they are built here.
func collectionBindings(sys *types.System) []Binding {
	a := sys.Var(0)
	coll := sys.Collection(a)
	// "x elem c" and "x notelem c": 'a * 'a collection -> bool.
	elemType := sys.Fn(sys.Tuple(a, coll), sys.Bool)
	// Aggregates and "only" adapt to their input's orderedness (a
	// list or a bag), so they take a collection of free
	// orderedness.
	collToElem := sys.Fn(coll, a)        // max, min, sum, only
	collToInt := sys.Fn(coll, sys.Int)   // count
	collToBool := sys.Fn(coll, sys.Bool) // empty, nonEmpty
	return []Binding{
		{Name: opElem, Type: elemType},
		{Name: opNotElem, Type: elemType},
		{Name: "count", Type: collToInt},
		{Name: "empty", Type: collToBool},
		{Name: "nonEmpty", Type: collToBool},
		{Name: "max", Type: collToElem},
		{Name: "min", Type: collToElem},
		{Name: sumName, Type: collToElem},
		{Name: "only", Type: collToElem},
	}
}

// CollectionAggType returns the collection-of-free-orderedness type
// of a Relational aggregate or "only" member, or nil for any other
// member. It lets the Relational structure adapt its aggregates to
// a list or a bag, like the top-level aliases.
func CollectionAggType(sys *types.System, member string,
	field types.Type,
) types.Type {
	fn, ok := field.(*types.Fn)
	if !ok {
		return nil
	}
	// The member's declared parameter is "'a bag"; replace it with
	// a collection of the same element type.
	bag, ok := fn.Param.(*types.Named)
	if !ok || bag.Name != bagTyCon || len(bag.Args) != 1 {
		return nil
	}
	switch member {
	case "count", "empty", "max", "min", "nonEmpty", "only", sumName:
		return sys.Fn(sys.Collection(bag.Args[0]), fn.Result)
	case "iterate":
		// Every bag in the signature becomes a collection; their
		// shared orderedness makes iterate a list function on
		// lists and a bag function on bags.
		return bagsToCollections(sys, fn)
	default:
		return nil
	}
}

// bagsToCollections replaces each bag in a type with a collection
// of the same element.
func bagsToCollections(sys *types.System, t types.Type) types.Type {
	// lint: sort until '^\t}' where '^\tcase '
	switch t := t.(type) {
	case *types.Fn:
		return sys.Fn(bagsToCollections(sys, t.Param),
			bagsToCollections(sys, t.Result))
	case *types.Named:
		if t.Name == bagTyCon && len(t.Args) == 1 {
			return sys.Collection(bagsToCollections(sys, t.Args[0]))
		}
		return t
	case *types.Tuple:
		args := make([]types.Type, len(t.Args))
		for i, arg := range t.Args {
			args[i] = bagsToCollections(sys, arg)
		}
		return sys.Tuple(args...)
	default:
		return t
	}
}
