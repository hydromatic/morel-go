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

package types

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/ast"
)

// System interns types: equal types are the same pointer. It
// also registers datatypes and their constructors.
type System struct {
	byKey     map[string]Type
	datatypes map[string]int
	tycons    map[string]TyCon
	conCount  map[string]int
	aliases   map[string]Alias
	// aliasGen counts the declarations of each alias name; see
	// Alias.Gen.
	aliasGen map[string]int
	// checkCodes holds the compiled form of each checked type's
	// conditions; see SetCheckCode.
	checkCodes map[*ast.Fn]any

	// typeofResolver deduces the type of the expression of a
	// "typeof" written inside a type declaration. The shell sets
	// it; a system with no evaluator behind it leaves it nil, and
	// "typeof" there is an error as it was.
	typeofResolver func(ast.Expr) (Type, error)

	// A datatype has an internal name that is unique to its
	// declaration, so a later declaration that reuses the name does
	// not conflate the two: the first "d" is "d", a redefinition is
	// "d~2", and so on. datatypeKey maps a source name to the
	// internal name it currently refers to; datatypeGen counts how
	// many times a source name has been declared. A datatype whose
	// source name now maps to a different internal name is displaced
	// and renders as "?.d".
	datatypeKey map[string]string
	datatypeGen map[string]int

	Bool   Type
	Char   Type
	Int    Type
	Real   Type
	String Type
	Unit   Type
	Word   Type
}

// TyCon describes a datatype constructor: its argument type (nil
// for a constant constructor), the datatype it constructs, and
// its ordinal among the datatype's constructors in declaration
// order. Runtime constructor values carry (datatype, ordinal).
type TyCon struct {
	Arg    Type
	Result Type
	// Surface is the argument type as it was written, with any
	// type alias intact, or nil where it is the same as Arg. A
	// checked type reaches a constructor only through this: Arg has
	// its aliases expanded, so the condition is not in it.
	Surface Type
	Ordinal int
}

// NewSystem returns a system with the primitive types
// registered.
func NewSystem() *System {
	s := &System{
		byKey:       map[string]Type{},
		datatypes:   map[string]int{},
		tycons:      map[string]TyCon{},
		conCount:    map[string]int{},
		aliases:     map[string]Alias{},
		aliasGen:    map[string]int{},
		checkCodes:  map[*ast.Fn]any{},
		datatypeKey: map[string]string{},
		datatypeGen: map[string]int{},
	}
	prim := func(name string) Type {
		t := &Primitive{typeBase{name, ""}}
		s.byKey[name] = t
		return t
	}
	s.Bool = prim("bool")
	s.Char = prim("char")
	s.Int = prim("int")
	s.Real = prim("real")
	s.String = prim("string")
	s.Unit = prim("unit")
	s.Word = prim("word")
	return s
}

// DatatypeArity returns the number of type parameters of a
// registered datatype. name may be a source name (resolved to the
// datatype it currently refers to) or an internal name.
func (s *System) DatatypeArity(name string) (int, bool) {
	if internal, ok := s.datatypeKey[name]; ok {
		return s.datatypes[internal], true
	}
	arity, ok := s.datatypes[name]
	return arity, ok
}

// DatatypeInternal returns the internal name a source name
// currently refers to (e.g. "d" or "d~2"), with its arity. A name
// that is already an internal name resolves to itself.
func (s *System) DatatypeInternal(name string) (string, int, bool) {
	if internal, ok := s.datatypeKey[name]; ok {
		return internal, s.datatypes[internal], true
	}
	if arity, ok := s.datatypes[name]; ok {
		return name, arity, true
	}
	return "", 0, false
}

// DeclareDatatype registers a datatype and its arity, returning its
// internal name: the source name for a first declaration, or a
// suffixed name ("d~2", ...) for one that reuses a name. A
// zero-arity datatype is interned immediately so that Lookup finds
// it.
func (s *System) DeclareDatatype(name string, arity int) string {
	gen := s.datatypeGen[name] + 1
	s.datatypeGen[name] = gen
	internal := name
	if gen > 1 {
		internal = name + "~" + strconv.Itoa(gen)
	}
	s.datatypeKey[name] = internal
	s.datatypes[internal] = arity
	// A datatype takes over the name from any alias that held it.
	delete(s.aliases, name)
	if arity == 0 {
		s.Named(internal)
	}
	return internal
}

// DatatypeDisplay returns how a datatype's internal name is shown to
// the user: its source name, or "?.d" if the name now refers to a
// different datatype. A name that is not a datatype is returned
// unchanged.
func (s *System) DatatypeDisplay(internal string) string {
	if _, ok := s.datatypes[internal]; !ok {
		return internal
	}
	// The source name strips the redefinition suffix ("d~2" -> "d").
	src, _, _ := strings.Cut(internal, "~")
	// The name is displaced if it now resolves to something else: an
	// alias takes precedence over a datatype, and a later datatype
	// takes over the name outright.
	if _, isAlias := s.aliases[src]; isAlias {
		return "?." + src
	}
	if current, ok := s.datatypeKey[src]; ok && current != internal {
		return "?." + src
	}
	return src
}

// SetCheckCode records the compiled form of one condition of a
// checked type. The type system does not look inside it -- it is
// a core expression, which the type system cannot name -- but it
// is where it lives, because a checked type may be declared in
// one statement and claimed in another, and the system is what
// outlives a statement.
//
// A condition is keyed by the syntax it was written as, which the
// alias carries, so the type that claims it and the declaration
// that compiled it agree by construction.
func (s *System) SetCheckCode(check *ast.Fn, code any) {
	s.checkCodes[check] = code
}

// CheckCode returns the compiled form of a condition, if it has
// been compiled.
func (s *System) CheckCode(check *ast.Fn) (any, bool) {
	code, ok := s.checkCodes[check]
	return code, ok
}

// Alias is a type alias's definition: its type parameters, the
// body they parameterize (an AST type, expanded at each use), and
// the conditions that make it a checked type, if it has any.
type Alias struct {
	TyVars []string
	Body   ast.Type
	Checks []*ast.Fn
	// Gen counts how many times the name has been declared. A name
	// declared again is a different type, and the two must be
	// distinguishable where a type is written as its name alone.
	Gen int
}

// DeclareAlias registers a transparent type alias, e.g.
// "type 'a my_list = 'a list"; checks, if any, make it a checked
// type.
func (s *System) DeclareAlias(name string, tyVars []string,
	body ast.Type, checks []*ast.Fn,
) {
	s.aliasGen[name]++
	s.aliases[name] = Alias{
		TyVars: tyVars, Body: body, Checks: checks,
		Gen: s.aliasGen[name],
	}
}

// SetTypeofResolver supplies the deduction that a "typeof" in a
// type declaration needs. A type declaration is elaborated before
// anything in it is deduced, and a name in its body refers to the
// environment before the declaration, so the expression is closed
// and its type can be deduced on its own.
func (s *System) SetTypeofResolver(f func(ast.Expr) (Type, error)) {
	s.typeofResolver = f
}

// LookupAlias returns the alias with the given name, if any.
func (s *System) LookupAlias(name string) (Alias, bool) {
	a, ok := s.aliases[name]
	return a, ok
}

// expandAlias substitutes an alias's arguments for its parameters
// in its body.
func expandAlias(alias Alias, args []ast.Type) ast.Type {
	subst := make(map[string]ast.Type, len(alias.TyVars))
	for i, tv := range alias.TyVars {
		subst[tv] = args[i]
	}
	return ast.SubstituteType(alias.Body, subst)
}

// DeclareTyCon registers a datatype constructor; arg is nil for
// a constant constructor. Constructors of one datatype get
// ordinals in declaration order.
func (s *System) DeclareTyCon(name string, arg, result Type) {
	s.DeclareTyConSurface(name, arg, result, nil)
}

// DeclareTyConSurface is DeclareTyCon, also recording the
// argument type as it was written; see TyCon.Surface.
func (s *System) DeclareTyConSurface(name string, arg, result,
	surface Type,
) {
	key := datatypeName(result)
	ordinal := s.conCount[key]
	s.conCount[key] = ordinal + 1
	s.tycons[name] = TyCon{
		Arg:     arg,
		Result:  result,
		Surface: surface,
		Ordinal: ordinal,
	}
}

// NumConstructors returns the number of constructors of a
// datatype, or 0 if it is not a datatype.
func (s *System) NumConstructors(datatype string) int {
	return s.conCount[datatype]
}

// Constructor describes one constructor of a datatype: its name,
// its declared argument type (nil for a constant constructor),
// and its ordinal.
type Constructor struct {
	Arg Type
	// Surface is the argument type as written, with any alias
	// intact; nil where it is the same as Arg. See TyCon.Surface.
	Surface Type
	Name    string
	Ordinal int
}

// Constructors returns a datatype's constructors in name order,
// or nil if the name is not a datatype.
func (s *System) Constructors(datatype string) []Constructor {
	var cons []Constructor
	for name, tc := range s.tycons {
		if datatypeName(tc.Result) == datatype {
			cons = append(cons, Constructor{
				Arg:     tc.Arg,
				Surface: tc.Surface,
				Name:    name,
				Ordinal: tc.Ordinal,
			})
		}
	}
	slices.SortFunc(cons, func(a, b Constructor) int {
		return strings.Compare(a.Name, b.Name)
	})
	return cons
}

// datatypeName is the name a datatype's constructors are counted
// under.
func datatypeName(result Type) string {
	if named, ok := result.(*Named); ok {
		return named.Name
	}
	return result.String()
}

// Lookup returns the type with the given name (a primitive or a
// zero-arity datatype), or nil.
func (s *System) Lookup(name string) Type {
	return s.byKey[name]
}

// LookupTyCon returns a registered datatype constructor.
func (s *System) LookupTyCon(name string) (TyCon, bool) {
	tc, ok := s.tycons[name]
	return tc, ok
}

// Alias returns a use of the transparent type alias with the
// given name, arguments and expansion; checks, if any, make it a
// checked type. It prints as the name it was written as, or, if it
// has none, as its body and conditions in full; Unalias reads what
// it abbreviates.
func (s *System) Alias(name string, args []Type, base Type,
	checks []*ast.Fn,
) Type {
	desc := AliasDesc(name, args, base, checks)
	// An alias prints as its name, so its mark carries what the
	// name abbreviates -- its body, and, where the name hides them,
	// its conditions. That is what tells one declaration of the
	// name from the next, here and in every type built over it. A
	// name redeclared with the same body and a different condition
	// would otherwise be the type it replaced, and a value claimed
	// at it would be checked against the condition it used to have.
	mark := "$" + desc + "=" + base.String() + base.Mark()
	if name != "" {
		mark += ast.UnparseChecks(checks)
	}
	gen := s.aliasGen[name]
	return s.intern("$alias:"+mark, func() Type {
		return &AliasType{
			typeBase{desc, mark}, args, name, base, checks, gen,
		}
	})
}

// AliasKey is how an alias is named where types are terms: its
// name, and which declaration of that name it is. A name declared
// again is a different type, and a term names a type by its name
// alone, so the two would otherwise be one term and would meet.
func AliasKey(name string, gen int) string {
	if gen <= 1 {
		return name
	}
	return name + "~" + strconv.Itoa(gen)
}

// AliasDesc returns how an alias is written: by its name, or, if
// it has none, by its body and its conditions. It is also what
// identifies a checked type, so two are the same type when their
// conditions are textually equal.
func AliasDesc(name string, args []Type, base Type,
	checks []*ast.Fn,
) string {
	if name != "" {
		// A named type is written by its name, conditions and all.
		// Two checked types of one name and different conditions are
		// then indistinguishable, which is why a name may be
		// redeclared but not shadowed.
		return namedDesc(name, args)
	}
	return base.String() + ast.UnparseChecks(checks)
}

// SetAliasChecks replaces the conditions of an alias already
// declared, without counting another declaration of the name. The
// conditions the compiler makes of a checked type replace the ones
// it was parsed with, and that is still the one declaration the
// user wrote.
func (s *System) SetAliasChecks(name string, checks []*ast.Fn) {
	if a, ok := s.aliases[name]; ok {
		a.Checks = checks
		s.aliases[name] = a
	}
}

// Named returns the instance of a datatype with the given type
// arguments, e.g. Named("option", Int) is "int option".
func (s *System) Named(name string, args ...Type) Type {
	desc, mark := namedDesc(name, args), marks(args...)
	return s.intern(desc+mark, func() Type {
		return &Named{typeBase{desc, mark}, args, name}
	})
}

// Substitute replaces each type variable with the argument at
// its ordinal, e.g. substituting ['b] into "'a option" gives
// "'b option".
func (s *System) Substitute(t Type, args []Type) Type {
	// lint: sort until '^	}' where '^	case '
	switch t := t.(type) {
	case *Fn:
		return s.Fn(s.Substitute(t.Param, args),
			s.Substitute(t.Result, args))
	case *List:
		return s.List(s.Substitute(t.Elem, args))
	case *Named:
		args2 := make([]Type, len(t.Args))
		for i, arg := range t.Args {
			args2[i] = s.Substitute(arg, args)
		}
		return s.Named(t.Name, args2...)
	case *Record:
		fields := make([]Field, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = Field{
				Label: f.Label,
				Type:  s.Substitute(f.Type, args),
			}
		}
		return s.Record(fields)
	case *Tuple:
		args2 := make([]Type, len(t.Args))
		for i, arg := range t.Args {
			args2[i] = s.Substitute(arg, args)
		}
		return s.Tuple(args2...)
	case *Var:
		if t.Ordinal < len(args) {
			return args[t.Ordinal]
		}
		return t
	default:
		return t
	}
}

// Qualified returns a type qualified by overload predicates. The
// predicates and the base type share type variables; predicates
// must be non-empty (an empty predicate list is just the base
// type).
func (s *System) Qualified(predicates []Predicate, base Type) Type {
	if len(predicates) == 0 {
		return base
	}
	desc, mark := qualifiedDesc(predicates, base), marks(base)
	return s.intern(desc+mark, func() Type {
		return &Qualified{typeBase{desc, mark}, predicates, base}
	})
}

// Var returns the type variable with the given ordinal.
func (s *System) Var(ordinal int) Type {
	name := varName(ordinal)
	return s.intern(name, func() Type {
		return &Var{typeBase{name, ""}, ordinal}
	})
}

// List returns the type "elem list".
func (s *System) List(elem Type) Type {
	desc, mark := descArg(elem)+" list", marks(elem)
	return s.intern(desc+mark, func() Type {
		return &List{typeBase{desc, mark}, elem}
	})
}

// Collection returns the type of a list-or-bag of elem, with
// orderedness free. It prints as a bag (the default when
// orderedness is unforced).
func (s *System) Collection(elem Type) Type {
	desc, mark := descArg(elem)+" $collection", marks(elem)
	return s.intern(desc+mark, func() Type {
		return &Collection{typeBase{desc, mark}, elem}
	})
}

// Fn returns the type "param -> result".
func (s *System) Fn(param, result Type) Type {
	desc := descParam(param) + " -> " + descResult(result)
	mark := marks(param, result)
	return s.intern(desc+mark, func() Type {
		return &Fn{typeBase{desc, mark}, param, result}
	})
}

// Tuple returns the type "t1 * t2 * ...".
func (s *System) Tuple(args ...Type) Type {
	descs := make([]string, len(args))
	for i, a := range args {
		descs[i] = descArg(a)
	}
	desc, mark := strings.Join(descs, " * "), marks(args...)
	return s.intern(desc+mark, func() Type {
		return &Tuple{typeBase{desc, mark}, args}
	})
}

// Record returns the record type with the given fields, sorted
// into label order.
func (s *System) Record(fields []Field) Type {
	if len(fields) == 0 {
		// "{}" is unit, as in SML.
		return s.Unit
	}
	return s.record(fields, false)
}

// ProgressiveRecord returns the progressive record type with the
// fields discovered so far. Unlike Record, it does not collapse the
// empty case to unit: "{...}", the record whose fields are not yet
// known, is a record — it is what a directory or data file has
// before it is browsed.
func (s *System) ProgressiveRecord(fields []Field) Type {
	return s.record(fields, true)
}

func (s *System) record(fields []Field, progressive bool) Type {
	sorted := make([]Field, len(fields))
	copy(sorted, fields)
	sort.Slice(sorted, func(i, j int) bool {
		return LabelLess(sorted[i].Label, sorted[j].Label)
	})
	desc, mark := recordDesc(sorted, progressive), fieldMarks(sorted)
	return s.intern(desc+mark, func() Type {
		return &Record{typeBase{desc, mark}, sorted, progressive}
	})
}

func (s *System) intern(key string, mk func() Type) Type {
	if t, ok := s.byKey[key]; ok {
		return t
	}
	t := mk()
	s.byKey[key] = t
	return t
}
