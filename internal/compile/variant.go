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
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/types"
)

// The Variant structure: dynamically-typed values. A variant value
// is an eval.Variant that tags an underlying value with its
// underlying type. The variant datatype's constructors (INT, LIST,
// CONSTRUCT, ...) do not build ordinary datatype values; they build
// eval.Variant values, deriving the underlying type at run time.
// This logic lives in the compile package because it needs both the
// type system and the runtime value representation.

const (
	variantTypeName = "variant"
	typeBag         = "bag"
	typeVector      = "vector"
	typeOption      = "option"
)

// Checked type assertions for runtime values of a known shape.
func toType(a any) types.Type { t, _ := a.(types.Type); return t }

func toVals(v eval.Val) []eval.Val { l, _ := v.([]eval.Val); return l }

func toVariant(v eval.Val) eval.Variant { r, _ := v.(eval.Variant); return r }

func toCon(v eval.Val) eval.Con { c, _ := v.(eval.Con); return c }

// compileVariantCon compiles an application of a variant datatype
// constructor. A nullary constructor folds to a constant; a unary
// one becomes a function that builds the variant at run time, when
// its argument's type is known.
func (c *compiler) compileVariantCon(e *core.Con) (eval.Code, error) {
	sys := c.sys
	if !e.HasArg {
		v, err := variantFromConstructor(e.Name, nil, sys)
		if err != nil {
			return nil, err
		}
		return eval.Constant(v), nil
	}
	name := e.Name
	return eval.Constant(eval.Fn(func(arg eval.Val) (eval.Val, error) {
		return variantFromConstructor(name, arg, sys)
	})), nil
}

// variantFromConstructor builds a variant value from a constructor
// name and its already-evaluated argument.
func variantFromConstructor(
	name string, arg eval.Val, sys *types.System,
) (eval.Val, error) {
	variantType := sys.Named(variantTypeName)
	// lint: sort until '^\t}' where '^\tcase '
	switch name {
	case "BAG":
		return variantCollection(sys, typeBag, asVariants(arg)), nil
	case "BOOL":
		return eval.Variant{Type: sys.Bool, Value: arg}, nil
	case "CHAR":
		return eval.Variant{Type: sys.Char, Value: arg}, nil
	case "CONSTANT":
		s, _ := arg.(string)
		return variantConstant(sys, s)
	case "CONSTRUCT":
		pair := toVals(arg)
		s, _ := pair[0].(string)
		return variantConstruct(sys, s, toVariant(pair[1]))
	case "INT":
		return eval.Variant{Type: sys.Int, Value: arg}, nil
	case "LIST":
		return variantList(sys, asVariants(arg)), nil
	case "REAL":
		return eval.Variant{Type: sys.Real, Value: arg}, nil
	case "RECORD":
		return variantRecord(sys, toVals(arg)), nil
	case "STRING":
		return eval.Variant{Type: sys.String, Value: arg}, nil
	case "UNIT":
		return eval.Variant{Type: sys.Unit, Value: core.Unit{}}, nil
	case "VARIANT_NONE":
		return eval.Variant{
			Type:  sys.Named(typeOption, variantType),
			Value: eval.NoneVal,
		}, nil
	case "VARIANT_SOME":
		v := toVariant(arg)
		return eval.Variant{
			Type:  sys.Named(typeOption, toType(v.Type)),
			Value: eval.SomeVal(v.Value),
		}, nil
	case "VECTOR":
		return variantCollection(sys, typeVector, asVariants(arg)), nil
	default:
		return nil, fmt.Errorf("unknown variant constructor: %s", name)
	}
}

// asVariants views a runtime list argument as a slice of variants.
func asVariants(arg eval.Val) []eval.Variant {
	list := toVals(arg)
	out := make([]eval.Variant, len(list))
	for i, v := range list {
		out[i] = toVariant(v)
	}
	return out
}

// commonElementType returns the shared underlying type of a list of
// variants, or nil if the list is empty or the types differ.
func commonElementType(list []eval.Variant) types.Type {
	if len(list) == 0 {
		return nil
	}
	first := toType(list[0].Type)
	for _, v := range list[1:] {
		if toType(v.Type) != first {
			return nil
		}
	}
	return first
}

// variantList builds a "T list" variant, unwrapping the elements
// when they share a type, else a "variant list" of the variants.
func variantList(sys *types.System, list []eval.Variant) eval.Val {
	if elem := commonElementType(list); elem != nil {
		return eval.Variant{Type: sys.List(elem), Value: unwrap(list)}
	}
	return eval.Variant{
		Type:  sys.List(sys.Named(variantTypeName)),
		Value: rewrap(list),
	}
}

// variantCollection is variantList for a "bag" or "vector".
func variantCollection(
	sys *types.System, kind string, list []eval.Variant,
) eval.Val {
	elem := commonElementType(list)
	if elem == nil {
		elem = sys.Named(variantTypeName)
		return eval.Variant{Type: sys.Named(kind, elem), Value: rewrap(list)}
	}
	return eval.Variant{Type: sys.Named(kind, elem), Value: unwrap(list)}
}

// unwrap is the underlying values of a list of variants.
func unwrap(list []eval.Variant) []eval.Val {
	out := make([]eval.Val, len(list))
	for i, v := range list {
		out[i] = v.Value
	}
	return out
}

// rewrap is the variants of a list, kept as values.
func rewrap(list []eval.Variant) []eval.Val {
	out := make([]eval.Val, len(list))
	for i, v := range list {
		out[i] = v
	}
	return out
}

// variantRecord builds a record variant from a list of
// (name, variant) pairs. Fields are sorted by name; if any field is
// itself variant-typed, all fields become variant-typed.
func variantRecord(sys *types.System, pairs []eval.Val) eval.Val {
	variantType := sys.Named(variantTypeName)
	type nv struct {
		name string
		v    eval.Variant
	}
	items := make([]nv, len(pairs))
	for i, p := range pairs {
		t := toVals(p)
		name, _ := t[0].(string)
		items[i] = nv{name, toVariant(t[1])}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return types.LabelLess(items[i].name, items[j].name)
	})
	anyVariant := false
	for _, it := range items {
		if toType(it.v.Type) == variantType {
			anyVariant = true
		}
	}
	fields := make([]types.Field, len(items))
	values := make([]eval.Val, len(items))
	for i, it := range items {
		if anyVariant {
			fields[i] = types.Field{Label: it.name, Type: variantType}
			values[i] = it.v
		} else {
			fields[i] = types.Field{Label: it.name, Type: toType(it.v.Type)}
			values[i] = it.v.Value
		}
	}
	return eval.Variant{Type: sys.Record(fields), Value: values}
}

// variantConstant builds a variant from the name of a nullary
// constructor, e.g. "LESS" yields "LESS : order variant".
func variantConstant(sys *types.System, name string) (eval.Val, error) {
	tc, ok := sys.LookupTyCon(name)
	if !ok {
		return nil, fmt.Errorf("unknown constructor: %s", name)
	}
	con := eval.Con{
		Datatype: namedName(tc.Result),
		Name:     name,
		Ordinal:  tc.Ordinal,
	}
	return eval.Variant{Type: tc.Result, Value: con}, nil
}

// variantConstruct builds a variant from a unary constructor name
// and its argument, deriving the result type by unifying the
// constructor's declared argument type with the argument's type.
func variantConstruct(
	sys *types.System, name string, arg eval.Variant,
) (eval.Val, error) {
	tc, ok := sys.LookupTyCon(name)
	if !ok {
		return nil, fmt.Errorf("unknown constructor: %s", name)
	}
	subst := map[int]types.Type{}
	unifyVar(tc.Arg, toType(arg.Type), subst)
	con := eval.Con{
		Datatype: namedName(tc.Result),
		Name:     name,
		Ordinal:  tc.Ordinal,
		Arg:      arg.Value,
	}
	return eval.Variant{
		Type:  substituteVars(sys, tc.Result, subst),
		Value: con,
	}, nil
}

// namedName is the datatype name of a Named type.
func namedName(t types.Type) string {
	if named, ok := t.(*types.Named); ok {
		return named.Name
	}
	return ""
}

// unifyVar matches a constructor's declared argument type against a
// concrete type, recording each type variable's binding.
func unifyVar(argType, actual types.Type, subst map[int]types.Type) {
	// lint: sort until '^\t}' where '^\tcase '
	switch at := argType.(type) {
	case *types.List:
		if al, ok := actual.(*types.List); ok {
			unifyVar(at.Elem, al.Elem, subst)
		}
	case *types.Named:
		if an, ok := actual.(*types.Named); ok && an.Name == at.Name &&
			len(an.Args) == len(at.Args) {
			for i := range at.Args {
				unifyVar(at.Args[i], an.Args[i], subst)
			}
		}
	case *types.Tuple:
		if at2, ok := actual.(*types.Tuple); ok &&
			len(at2.Args) == len(at.Args) {
			for i := range at.Args {
				unifyVar(at.Args[i], at2.Args[i], subst)
			}
		}
	case *types.Var:
		subst[at.Ordinal] = actual
	}
}

// substituteVars rebuilds a datatype type, replacing each type
// variable with its binding where one exists and leaving it free
// otherwise, e.g. ('a,'b) either with {0:int} becomes (int,'b)
// either.
func substituteVars(
	sys *types.System, t types.Type, subst map[int]types.Type,
) types.Type {
	named, ok := t.(*types.Named)
	if !ok {
		return t
	}
	args := make([]types.Type, len(named.Args))
	for i, a := range named.Args {
		args[i] = a
		if v, ok := a.(*types.Var); ok {
			if s, ok := subst[v.Ordinal]; ok {
				args[i] = s
			}
		}
	}
	return sys.Named(named.Name, args...)
}

// VariantPrint renders a variant in its constructor form, e.g.
// "LIST [INT 1, INT 2]". It is the inverse of VariantParse.
func VariantPrint(v eval.Val, sys *types.System) string {
	vr := toVariant(v)
	var b strings.Builder
	variantAppend(&b, sys, toType(vr.Type), vr.Value)
	return b.String()
}

// variantAppend writes the constructor form of a value of the given
// underlying type. A value that is itself a variant is unwrapped to
// its own type.
func variantAppend(
	b *strings.Builder, sys *types.System, t types.Type, value eval.Val,
) {
	if vr, ok := value.(eval.Variant); ok {
		variantAppend(b, sys, toType(vr.Type), vr.Value)
		return
	}
	// lint: sort until '^\t}' where '^\tcase '
	switch t {
	case sys.Bool:
		bv, _ := value.(bool)
		b.WriteString("BOOL ")
		b.WriteString(strconv.FormatBool(bv))
	case sys.Char:
		cv, _ := value.(rune)
		b.WriteString("CHAR #\"")
		b.WriteString(eval.CharToString(cv))
		b.WriteString("\"")
	case sys.Int:
		iv, _ := value.(int32)
		b.WriteString("INT ")
		b.WriteString(intToTilde(iv))
	case sys.Real:
		rv, _ := value.(float32)
		b.WriteString("REAL ")
		b.WriteString(eval.FormatReal(rv))
	case sys.String:
		sv, _ := value.(string)
		b.WriteString("STRING \"")
		b.WriteString(escapeStringContent(sv))
		b.WriteString("\"")
	case sys.Unit:
		b.WriteString("UNIT")
	default:
		variantAppendComposite(b, sys, t, value)
	}
}

func variantAppendComposite(
	b *strings.Builder, sys *types.System, t types.Type, value eval.Val,
) {
	// lint: sort until '^\t}' where '^\tcase '
	switch tt := t.(type) {
	case *types.List:
		variantAppendSeq(b, sys, "LIST [", tt.Elem, value)
	case *types.Named:
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch tt.Name {
		case typeBag:
			variantAppendSeq(b, sys, "BAG [", tt.Args[0], value)
		case typeOption:
			con := toCon(value)
			if con.Ordinal == 0 {
				b.WriteString("VARIANT_NONE")
			} else {
				b.WriteString("VARIANT_SOME ")
				variantAppend(b, sys, tt.Args[0], con.Arg)
			}
		case typeVector:
			variantAppendSeq(b, sys, "VECTOR [", tt.Args[0], value)
		default:
			variantAppendDatatype(b, sys, tt, value)
		}
	case *types.Record:
		b.WriteString("RECORD [")
		vals := toVals(value)
		for i, f := range tt.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("(\"")
			b.WriteString(f.Label)
			b.WriteString("\", ")
			variantAppend(b, sys, f.Type, vals[i])
			b.WriteString(")")
		}
		b.WriteString("]")
	}
}

func variantAppendSeq(
	b *strings.Builder, sys *types.System, prefix string,
	elem types.Type, value eval.Val,
) {
	b.WriteString(prefix)
	for i, e := range toVals(value) {
		if i > 0 {
			b.WriteString(", ")
		}
		variantAppend(b, sys, elem, e)
	}
	b.WriteString("]")
}

func variantAppendDatatype(
	b *strings.Builder, sys *types.System, t *types.Named, value eval.Val,
) {
	con := toCon(value)
	if con.Arg == nil {
		b.WriteString("CONSTANT \"")
		b.WriteString(con.Name)
		b.WriteString("\"")
		return
	}
	b.WriteString("CONSTRUCT (\"")
	b.WriteString(con.Name)
	b.WriteString("\", ")
	argType := sys.Named(variantTypeName)
	if tc, ok := sys.LookupTyCon(con.Name); ok && tc.Arg != nil {
		argType = sys.Substitute(tc.Arg, t.Args)
	}
	variantAppend(b, sys, argType, con.Arg)
	b.WriteString(")")
}

// intToTilde renders an int with "~" for the minus sign.
func intToTilde(n int32) string {
	return strings.Replace(strconv.FormatInt(int64(n), 10), "-", "~", 1)
}

// escapeStringContent escapes a string's characters the way they
// appear inside a string literal.
func escapeStringContent(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteString(eval.CharToString(r))
	}
	return b.String()
}

// VariantParse parses a variant from the constructor-form string
// produced by VariantPrint (so parse (print v) = v). It also accepts
// bare literals such as "42", "true", and "()".
func VariantParse(s string, sys *types.System) (eval.Val, error) {
	p := &variantParser{input: s, sys: sys}
	v := p.parseValue()
	if p.err != nil {
		return nil, p.err
	}
	return v, nil
}

// variantParser is a recursive-descent parser with a sticky error:
// once err is set, every method short-circuits.
type variantParser struct {
	input string
	sys   *types.System
	err   error
	pos   int
}

func (p *variantParser) failf(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}

// adopt records an error from a helper and returns its value.
func (p *variantParser) adopt(v eval.Val, err error) eval.Val {
	if err != nil && p.err == nil {
		p.err = err
	}
	return v
}

func (p *variantParser) variant(t types.Type, v eval.Val) eval.Val {
	return eval.Variant{Type: t, Value: v}
}

func (p *variantParser) skipWs() {
	for p.pos < len(p.input) && p.input[p.pos] <= ' ' {
		p.pos++
	}
}

func (p *variantParser) consume(s string) bool {
	p.skipWs()
	if strings.HasPrefix(p.input[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *variantParser) expect(s string) {
	if p.err == nil && !p.consume(s) {
		p.failf("expected %q at position %d", s, p.pos)
	}
}

func (p *variantParser) digits() {
	for p.pos < len(p.input) &&
		p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
		p.pos++
	}
}

func isVariantLetter(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func (p *variantParser) parseValue() eval.Val {
	if p.err != nil {
		return nil
	}
	p.skipWs()
	if p.pos >= len(p.input) {
		p.failf("unexpected end of input at position %d", p.pos)
		return nil
	}
	c := p.input[p.pos]
	switch {
	case c == '(':
		p.expect("()")
		return p.variant(p.sys.Unit, core.Unit{})
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == '"':
		return p.variant(p.sys.String, p.parseQuoted())
	case c == '#':
		return p.parseChar()
	case c == '[':
		p.expect("[")
		return variantList(p.sys, p.parseElements())
	case c == '~' || c >= '0' && c <= '9':
		return p.parseNumber()
	case isVariantLetter(c):
		return p.parseIdentifierValue()
	default:
		p.failf("unexpected character %q at position %d", c, p.pos)
		return nil
	}
}

func (p *variantParser) parseBool() eval.Val {
	switch {
	case p.consume("true"):
		return p.variant(p.sys.Bool, true)
	case p.consume("false"):
		return p.variant(p.sys.Bool, false)
	default:
		p.failf("expected boolean at position %d", p.pos)
		return nil
	}
}

func (p *variantParser) parseNumber() eval.Val {
	p.skipWs()
	start := p.pos
	if p.pos < len(p.input) && p.input[p.pos] == '~' {
		p.pos++
	}
	isReal := false
	p.digits()
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		isReal = true
		p.pos++
		p.digits()
	}
	if p.pos < len(p.input) &&
		(p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
		isReal = true
		p.pos++
		if p.pos < len(p.input) &&
			(p.input[p.pos] == '~' || p.input[p.pos] == '-') {
			p.pos++
		}
		p.digits()
	}
	text := strings.ReplaceAll(p.input[start:p.pos], "~", "-")
	if isReal {
		f, err := strconv.ParseFloat(text, 32)
		if err != nil {
			p.failf("bad real %q: %w", text, err)
			return nil
		}
		return p.variant(p.sys.Real, float32(f))
	}
	n, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		p.failf("bad int %q: %w", text, err)
		return nil
	}
	return p.variant(p.sys.Int, int32(n))
}

// parseQuoted reads a "..."-delimited string, handling escapes.
func (p *variantParser) parseQuoted() string {
	p.expect("\"")
	if p.err != nil {
		return ""
	}
	var b strings.Builder
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if c == '"' {
			p.pos++
			return b.String()
		}
		if c == '\\' && p.pos+1 < len(p.input) {
			p.pos++
			b.WriteByte(unescapeByte(p.input[p.pos]))
			p.pos++
			continue
		}
		b.WriteByte(c)
		p.pos++
	}
	p.failf("unterminated string at position %d", p.pos)
	return ""
}

func unescapeByte(c byte) byte {
	// lint: sort until '^\t}' where '^\tcase '
	switch c {
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	default:
		return c
	}
}

func (p *variantParser) parseChar() eval.Val {
	if p.pos+1 < len(p.input) && p.input[p.pos+1] == '[' {
		p.expect("#[")
		return variantCollection(p.sys, typeVector, p.parseElements())
	}
	p.expect("#\"")
	if p.err != nil {
		return nil
	}
	if p.pos >= len(p.input) {
		p.failf("unterminated char at position %d", p.pos)
		return nil
	}
	var r rune
	if p.input[p.pos] == '\\' && p.pos+1 < len(p.input) {
		p.pos++
		r = rune(unescapeByte(p.input[p.pos]))
	} else {
		r = rune(p.input[p.pos])
	}
	p.pos++
	p.expect("\"")
	return p.variant(p.sys.Char, r)
}

// parseElements reads a comma-separated element list up to a closing
// "]", each element parsed by parseValue.
func (p *variantParser) parseElements() []eval.Variant {
	var out []eval.Variant
	if p.consume("]") {
		return out
	}
	for {
		out = append(out, toVariant(p.parseValue()))
		if p.consume("]") || p.err != nil {
			return out
		}
		p.expect(",")
	}
}

func (p *variantParser) parseIdentifierValue() eval.Val {
	start := p.pos
	for p.pos < len(p.input) &&
		(isVariantLetter(p.input[p.pos]) ||
			p.input[p.pos] >= '0' && p.input[p.pos] <= '9' ||
			p.input[p.pos] == '_') {
		p.pos++
	}
	word := p.input[start:p.pos]
	// lint: sort until '^\t}' where '^\tcase '
	switch word {
	case "BAG":
		p.expect("[")
		return variantCollection(p.sys, typeBag, p.parseElements())
	case "BOOL":
		return p.parseBool()
	case "CHAR":
		return p.parseChar()
	case "CONSTANT":
		return p.adopt(variantConstant(p.sys, p.parseQuoted()))
	case "CONSTRUCT":
		return p.parseConstruct()
	case "INT", "REAL":
		return p.parseNumber()
	case "LIST":
		p.expect("[")
		return variantList(p.sys, p.parseElements())
	case "RECORD":
		return p.parseRecord()
	case "STRING":
		return p.variant(p.sys.String, p.parseQuoted())
	case "UNIT":
		return p.variant(p.sys.Unit, core.Unit{})
	case "VARIANT_NONE":
		return p.adopt(variantFromConstructor("VARIANT_NONE", nil, p.sys))
	case "VARIANT_SOME":
		return p.adopt(
			variantFromConstructor("VARIANT_SOME", p.parseValue(), p.sys))
	case "VECTOR":
		p.expect("[")
		return variantCollection(p.sys, typeVector, p.parseElements())
	default:
		p.failf("unknown identifier: %s", word)
		return nil
	}
}

func (p *variantParser) parseConstruct() eval.Val {
	p.expect("(")
	name := p.parseQuoted()
	p.expect(",")
	arg := p.parseValue()
	p.expect(")")
	if p.err != nil {
		return nil
	}
	return p.adopt(variantConstruct(p.sys, name, toVariant(arg)))
}

func (p *variantParser) parseRecord() eval.Val {
	p.expect("[")
	var pairs []eval.Val
	if p.consume("]") {
		return variantRecord(p.sys, pairs)
	}
	for {
		p.expect("(")
		name := p.parseQuoted()
		p.expect(",")
		v := p.parseValue()
		p.expect(")")
		if p.err != nil {
			return nil
		}
		pairs = append(pairs, []eval.Val{name, v})
		if p.consume("]") {
			return variantRecord(p.sys, pairs)
		}
		p.expect(",")
	}
}
