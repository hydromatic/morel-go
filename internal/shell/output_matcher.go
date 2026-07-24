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
	"strings"

	"github.com/hydromatic/morel-go/internal/types"
)

// equivalentOutput reports whether two statement-output strings are
// semantically equivalent: bag-typed values compare as multisets
// (order-independent) and whitespace is normalized. Both must be of
// the form "val name = value : type" or "value : type", carry the
// same type, and their values must match, parsed by that type. When
// in doubt it returns false — a false positive would hide a real
// divergence.
func equivalentOutput(sys *types.System, actual, expected string) bool {
	a := splitOutput(actual)
	e := splitOutput(expected)
	if a.val == "" || e.val == "" {
		return false
	}
	if a.typ == "" || a.typ != e.typ {
		return false
	}
	// The prefix ("val name = ", and anything before it such as a
	// warning) must match too, so that output differing in a warning
	// — which sits in the prefix — is not treated as equivalent.
	if normalizeWhitespace(a.prefix) != normalizeWhitespace(e.prefix) {
		return false
	}
	t, err := sys.Parse(a.typ)
	if err != nil {
		return false
	}
	return valuesEquivalent(sys, t, a.val, e.val)
}

// valuesEquivalent parses two value strings guided by their type
// and compares them, treating bags as multisets.
func valuesEquivalent(sys *types.System, t types.Type,
	code0, code1 string,
) bool {
	// The scanners panic on malformed input; treat that as "not
	// equivalent" rather than a crash.
	equal := false
	func() {
		defer func() { _ = recover() }()
		m := &outputMatcher{sys: sys}
		o0 := m.parseValue(newValScanner(normalizeWhitespace(code0)), t)
		o1 := m.parseValue(newValScanner(normalizeWhitespace(code1)), t)
		equal = m.valuesEqual(t, o0, o1)
	}()
	return equal
}

// outputMatcher parses and compares value strings against types.
type outputMatcher struct {
	sys *types.System
}

// splitOutput is an output line split into its prefix, value, and
// type.
type outputSplit struct {
	prefix string
	val    string
	typ    string
}

// splitOutput divides "val name = value : type" (or "value : type")
// into a prefix ("val name = ", plus anything before it), the
// value, and the type. It returns empty val/typ when there is no
// top-level " : ".
func splitOutput(s string) outputSplit {
	start := valueStart(s)
	colon := lastTopColon(s)
	// The value is s[start:colon-1]; when the last top-level colon
	// falls at or before the value start (e.g. a ":"-annotated
	// binding pattern, "val a : b = c"), that slice would invert and
	// panic, so treat the line as having no value or type.
	if colon < start+1 {
		return outputSplit{prefix: s}
	}
	return outputSplit{
		prefix: s[:start],
		val:    s[start : colon-1],
		typ:    s[colon+1:],
	}
}

// valueStart is the index where the value begins: after "val name =",
// or 0 when there is no such binding prefix.
func valueStart(s string) int {
	eq := indexOfEqSpace(s)
	if eq < 0 || !strings.Contains(s[:eq], "val ") {
		return 0
	}
	i := eq + 1
	for i < len(s) && isSpaceByte(s[i]) {
		i++
	}
	return i
}

// indexOfEqSpace is the index of the first '=' followed by
// whitespace.
func indexOfEqSpace(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '=' && isSpaceByte(s[i+1]) {
			return i
		}
	}
	return -1
}

// lastTopColon is the index of the last top-level " : " (a colon
// surrounded by spaces, outside brackets and strings).
func lastTopColon(s string) int {
	depth := 0
	inString := false
	last := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch c {
			case '"':
				inString = false
			case '\\':
				i++
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ':' && depth == 0 && i > 0 && s[i-1] == ' ' &&
			i+1 < len(s) && s[i+1] == ' ':
			last = i
		}
	}
	return last
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

// normalizeWhitespace collapses runs of whitespace to a single
// space, dropping spaces that are not needed between tokens, and
// leaves string literals untouched.
func normalizeWhitespace(s string) string {
	var b strings.Builder
	inString := false
	lastWasSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			if c == '"' {
				inString = false
			} else if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
			continue
		}
		switch {
		case c == '"':
			if lastWasSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteByte(c)
			inString = true
			lastWasSpace = false
		case isSpaceByte(c):
			lastWasSpace = true
		default:
			if lastWasSpace && b.Len() > 0 && needsSpace(b.String(), c) {
				b.WriteByte(' ')
			}
			b.WriteByte(c)
			lastWasSpace = false
		}
	}
	return b.String()
}

// needsSpace reports whether a space is needed between the last
// character written and the next character c: between two word
// characters, or around "=" (but not inside comparison operators).
func needsSpace(buf string, c byte) bool {
	prev := buf[len(buf)-1]
	if isWordByte(prev) && isWordByte(c) {
		return true
	}
	if prev == '=' && c != '{' && c != '[' && c != '(' && c != ')' {
		return true
	}
	return c == '=' && prev != '>' && prev != '<' && prev != '!'
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '_' || c == '\'' || c == '~'
}

// parseValue parses a value from whitespace-normalized text, guided
// by its type: a string for an atom, or a []any for a compound
// value (list, bag, tuple, record — as its fields in type order —
// or a datatype as [constructor] or [constructor, arg]).
func (m *outputMatcher) parseValue(sc *valScanner, t types.Type) any {
	if sc.peek() == '(' {
		if _, isTuple := t.(*types.Tuple); !isTuple {
			sc.consume("(")
			v := m.parseValue(sc, t)
			sc.consume(")")
			return v
		}
	}
	// lint: sort until '^	}' where '^	case '
	switch t := t.(type) {
	case *types.List:
		return m.parseElements(sc, t.Elem)
	case *types.Named:
		if t.Name == bagType && len(t.Args) == 1 {
			return m.parseElements(sc, t.Args[0])
		}
		return m.parseDatatype(sc, t)
	case *types.Record:
		return m.parseRecord(sc, t)
	case *types.Tuple:
		return m.parseTuple(sc, t.Args)
	default:
		return parseAtom(sc)
	}
}

// parseElements parses "[e1, e2, ...]" into a slice.
func (m *outputMatcher) parseElements(sc *valScanner,
	elem types.Type,
) []any {
	sc.consume("[")
	var elems []any
	if sc.peek() != ']' {
		for {
			elems = append(elems, m.parseValue(sc, elem))
			if sc.peek() != ',' {
				break
			}
			sc.consume(",")
		}
	}
	sc.consume("]")
	return elems
}

// parseTuple parses "(e1, e2, ...)" into a slice.
func (m *outputMatcher) parseTuple(sc *valScanner,
	args []types.Type,
) []any {
	sc.consume("(")
	if sc.peek() == ')' {
		sc.consume(")")
		return []any{}
	}
	fields := make([]any, len(args))
	for i, arg := range args {
		if i > 0 {
			sc.consume(",")
		}
		fields[i] = m.parseValue(sc, arg)
	}
	sc.consume(")")
	return fields
}

// parseRecord parses "{f1=v1, ...}" into a slice of the field values
// in the type's (label) order.
func (m *outputMatcher) parseRecord(sc *valScanner,
	t *types.Record,
) []any {
	sc.consume("{")
	byName := map[string]any{}
	fieldType := map[string]types.Type{}
	for _, f := range t.Fields {
		fieldType[f.Label] = f.Type
	}
	if sc.peek() != '}' {
		for {
			name := sc.consumeWord()
			sc.consume("=")
			if ft, ok := fieldType[name]; ok {
				byName[name] = m.parseValue(sc, ft)
			} else {
				byName[name] = parseAtom(sc)
			}
			if sc.peek() != ',' {
				break
			}
			sc.consume(",")
		}
	}
	sc.consume("}")
	values := make([]any, len(t.Fields))
	for i, f := range t.Fields {
		values[i] = byName[f.Label]
	}
	return values
}

// parseDatatype parses a constructor name and its optional argument,
// returning [name] or [name, arg].
func (m *outputMatcher) parseDatatype(sc *valScanner,
	t *types.Named,
) []any {
	ctor := sc.consumeWord()
	if !sc.hasMore() || sc.peek() == ',' || sc.peek() == ')' ||
		sc.peek() == ']' || sc.peek() == '}' {
		return []any{ctor}
	}
	argType := m.ctorArgType(ctor, t)
	if argType == nil {
		return []any{ctor, parseAtom(sc)}
	}
	return []any{ctor, m.parseValue(sc, argType)}
}

// ctorArgType is the argument type of a datatype's constructor,
// with the datatype instance's type arguments substituted in, or
// nil if the constructor is unknown or takes no argument.
func (m *outputMatcher) ctorArgType(ctor string,
	t *types.Named,
) types.Type {
	tc, ok := m.sys.LookupTyCon(ctor)
	if !ok || tc.Arg == nil {
		return nil
	}
	return m.sys.Substitute(tc.Arg, t.Args)
}

// parseAtom parses a single atom: a string, char, number, unit, or
// bare word.
func parseAtom(sc *valScanner) string {
	switch c := sc.peek(); {
	case c == '#':
		sc.consume("#")
		return "#" + sc.consumeString()
	case c == '"':
		return sc.consumeString()
	case c == '~' || c >= '0' && c <= '9':
		return sc.consumeNumber()
	case c == '(' && sc.peekAt(1) == ')':
		sc.consume("(")
		sc.consume(")")
		return "()"
	default:
		return sc.consumeWord()
	}
}

// valuesEqual compares two parsed values by their type, treating
// bag-typed collections as multisets.
func (m *outputMatcher) valuesEqual(t types.Type, o0, o1 any) bool {
	// lint: sort until '^	}' where '^	case '
	switch t := t.(type) {
	case *types.List:
		return m.seqEqual(t.Elem, o0, o1, false)
	case *types.Named:
		if t.Name == bagType && len(t.Args) == 1 {
			return m.seqEqual(t.Args[0], o0, o1, true)
		}
		return m.datatypeEqual(t, o0, o1)
	case *types.Record:
		types2 := make([]types.Type, len(t.Fields))
		for i, f := range t.Fields {
			types2[i] = f.Type
		}
		return m.fieldsEqual(types2, o0, o1)
	case *types.Tuple:
		return m.fieldsEqual(t.Args, o0, o1)
	default:
		return o0 == o1
	}
}

// seqEqual compares two collections: as multisets when bag is true,
// else element-wise in order.
func (m *outputMatcher) seqEqual(elem types.Type, o0, o1 any,
	bag bool,
) bool {
	l0, ok0 := o0.([]any)
	l1, ok1 := o1.([]any)
	if !ok0 || !ok1 {
		return o0 == o1
	}
	if len(l0) != len(l1) {
		return false
	}
	if !bag {
		for i := range l0 {
			if !m.valuesEqual(elem, l0[i], l1[i]) {
				return false
			}
		}
		return true
	}
	remaining := append([]any(nil), l1...)
	for _, a := range l0 {
		j := -1
		for k, b := range remaining {
			if m.valuesEqual(elem, a, b) {
				j = k
				break
			}
		}
		if j < 0 {
			return false
		}
		remaining = append(remaining[:j], remaining[j+1:]...)
	}
	return true
}

// fieldsEqual compares two tuples/records element-wise with
// per-field types.
func (m *outputMatcher) fieldsEqual(fieldTypes []types.Type,
	o0, o1 any,
) bool {
	l0, ok0 := o0.([]any)
	l1, ok1 := o1.([]any)
	if !ok0 || !ok1 {
		return o0 == o1
	}
	if len(l0) != len(l1) || len(l0) != len(fieldTypes) {
		return false
	}
	for i := range l0 {
		if !m.valuesEqual(fieldTypes[i], l0[i], l1[i]) {
			return false
		}
	}
	return true
}

// valScanner scans over whitespace-normalized value text.
type valScanner struct {
	s   string
	pos int
}

func newValScanner(s string) *valScanner {
	return &valScanner{s: s}
}

func (sc *valScanner) skipSpaces() {
	for sc.pos < len(sc.s) && sc.s[sc.pos] == ' ' {
		sc.pos++
	}
}

func (sc *valScanner) hasMore() bool {
	sc.skipSpaces()
	return sc.pos < len(sc.s)
}

func (sc *valScanner) peek() byte {
	sc.skipSpaces()
	if sc.pos < len(sc.s) {
		return sc.s[sc.pos]
	}
	return 0
}

func (sc *valScanner) peekAt(offset int) byte {
	sc.skipSpaces()
	if i := sc.pos + offset; i < len(sc.s) {
		return sc.s[i]
	}
	return 0
}

func (sc *valScanner) consume(expected string) {
	sc.skipSpaces()
	if !strings.HasPrefix(sc.s[sc.pos:], expected) {
		panic("expected '" + expected + "'")
	}
	sc.pos += len(expected)
}

func (sc *valScanner) consumeWord() string {
	sc.skipSpaces()
	start := sc.pos
	for sc.pos < len(sc.s) && isWordByte(sc.s[sc.pos]) {
		sc.pos++
	}
	if sc.pos == start {
		panic("expected word")
	}
	return sc.s[start:sc.pos]
}

func (sc *valScanner) consumeString() string {
	sc.skipSpaces()
	if sc.pos >= len(sc.s) || sc.s[sc.pos] != '"' {
		panic("expected string")
	}
	start := sc.pos
	sc.pos++
	for sc.pos < len(sc.s) && sc.s[sc.pos] != '"' {
		if sc.s[sc.pos] == '\\' {
			sc.pos++
		}
		sc.pos++
	}
	sc.pos++
	return sc.s[start:sc.pos]
}

func (sc *valScanner) consumeNumber() string {
	sc.skipSpaces()
	start := sc.pos
	for sc.pos < len(sc.s) && !isValueDelim(sc.s[sc.pos]) {
		sc.pos++
	}
	return sc.s[start:sc.pos]
}

// isValueDelim reports whether c ends an atom in a value.
func isValueDelim(c byte) bool {
	switch c {
	case ',', ' ', '(', ')', '[', ']', '{', '}':
		return true
	default:
		return false
	}
}

// datatypeEqual compares two datatype values ([name] or [name, arg]).
func (m *outputMatcher) datatypeEqual(t *types.Named, o0, o1 any) bool {
	l0, ok0 := o0.([]any)
	l1, ok1 := o1.([]any)
	if !ok0 || !ok1 || len(l0) != len(l1) || len(l0) == 0 {
		return false
	}
	if l0[0] != l1[0] {
		return false
	}
	if len(l0) == 1 {
		return true
	}
	ctor, _ := l0[0].(string)
	argType := m.ctorArgType(ctor, t)
	return argType != nil && m.valuesEqual(argType, l0[1], l1[1])
}
