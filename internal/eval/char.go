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
	"strconv"
	"strings"
)

// The Char structure. Characters are 8-bit, 0 to 255; the
// classification predicates are ASCII-only, in the "C" locale.

// Bounds of the char type, and the distance between a control
// character and the printable character that names it in a
// "\^" escape.
const (
	minCharVal = rune(0)
	maxCharVal = rune(255)
	ctrlOffset = 64
)

// charSuccFn is "Char.succ c"; Chr if c is maxChar.
func charSuccFn(arg Val) (Val, error) {
	c := asChar(arg)
	if c >= maxCharVal {
		return nil, &MorelError{Exn: ExnChr}
	}
	return c + 1, nil
}

// charPredFn is "Char.pred c"; Chr if c is minChar.
func charPredFn(arg Val) (Val, error) {
	c := asChar(arg)
	if c <= minCharVal {
		return nil, &MorelError{Exn: ExnChr}
	}
	return c - 1, nil
}

// charCompareFn is "Char.compare (c1, c2)".
func charCompareFn(arg Val) (Val, error) {
	a, b := asPair(arg)
	return orderVal(cmpOrdered(asChar(a), asChar(b))), nil
}

// charOp adapts a binary char predicate to a built-in.
func charOp(f func(a, b rune) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		return f(asChar(a), asChar(b)), nil
	}
}

// charContainsFn is "Char.contains s c"; charNotContainsFn is
// its negation.
func charContainsFn(want bool) Fn {
	return Curry2(func(s, c Val) (Val, error) {
		return strings.ContainsRune(asString(s),
			asChar(c)) == want, nil
	})
}

// charPredicate adapts a classification predicate to a built-in.
func charPredicate(f func(c rune) bool) Fn {
	return func(arg Val) (Val, error) {
		return f(asChar(arg)), nil
	}
}

// The classification predicates, ASCII-only.

func isUpperChar(c rune) bool { return 'A' <= c && c <= 'Z' }
func isLowerChar(c rune) bool { return 'a' <= c && c <= 'z' }
func isDigitChar(c rune) bool { return '0' <= c && c <= '9' }

func isAlphaChar(c rune) bool {
	return isUpperChar(c) || isLowerChar(c)
}

func isAlphaNumChar(c rune) bool {
	return isAlphaChar(c) || isDigitChar(c)
}

//nolint:revive // Ascii, not ASCII: the casing matches Char.isAscii.
func isAsciiChar(c rune) bool { return c <= '\u007f' }

// isGraphChar reports a visible (printable, non-space)
// character.
func isGraphChar(c rune) bool { return c >= '!' && c <= '~' }

func isPrintChar(c rune) bool { return isGraphChar(c) || c == ' ' }

func isCntrlChar(c rune) bool {
	return isAsciiChar(c) && !isPrintChar(c)
}

func isHexDigitChar(c rune) bool {
	return isDigitChar(c) || 'a' <= c && c <= 'f' ||
		'A' <= c && c <= 'F'
}

func isOctDigitChar(c rune) bool { return '0' <= c && c <= '7' }

func isPunctChar(c rune) bool {
	return isGraphChar(c) && !isAlphaNumChar(c)
}

func isSpaceChar(c rune) bool {
	return c >= '\t' && c <= '\r' || c == ' '
}

// charToLowerFn is "Char.toLower c".
func charToLowerFn(arg Val) (Val, error) {
	c := asChar(arg)
	if isUpperChar(c) {
		return c - 'A' + 'a', nil
	}
	return c, nil
}

// charToUpperFn is "Char.toUpper c".
func charToUpperFn(arg Val) (Val, error) {
	c := asChar(arg)
	if isLowerChar(c) {
		return c - 'a' + 'A', nil
	}
	return c, nil
}

// CharToString converts a character to how it appears in a
// character literal: the named
// escapes \a \b \t \n \v \f \r, escaped double-quote and
// backslash, control escapes ("\^@" for character 0), decimal
// escapes ("\255") above 126, and the character itself
// otherwise.
func CharToString(c rune) string {
	// lint: sort until '^	}' where '^	case '
	switch c {
	case '"':
		return `\"`
	case '\\':
		return `\\`
	case '\a':
		return `\a`
	case '\b':
		return `\b`
	case '\f':
		return `\f`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	case '\v':
		return `\v`
	}
	switch {
	case c < ' ':
		return `\^` + string(c+ctrlOffset)
	case c > '~':
		return `\` + strconv.Itoa(int(c))
	default:
		return string(c)
	}
}

// charToStringFn is "Char.toString c".
func charToStringFn(arg Val) (Val, error) {
	return CharToString(asChar(arg)), nil
}

// charFromString parses the character literal body at the start
// of s (the inverse of CharToString), returning the character
// and whether the parse succeeded.
func charFromString(s string) (rune, bool) {
	if s == "" {
		return 0, false
	}
	if s[0] != '\\' {
		return rune(s[0]), true
	}
	rest := s[1:]
	if rest == "" {
		return 0, false
	}
	// lint: sort until '^	}' where '^	case '
	switch rest[0] {
	case '"':
		return '"', true
	case '\\':
		return '\\', true
	case '^':
		if len(rest) == 1 || rest[1] < '@' || rest[1] > '_' {
			return 0, false
		}
		return rune(rest[1]) - ctrlOffset, true
	case 'a':
		return '\a', true
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	case 'v':
		return '\v', true
	}
	if isDigitChar(rune(rest[0])) {
		if len(rest) < len("255") ||
			!isDigitChar(rune(rest[1])) ||
			!isDigitChar(rune(rest[2])) {
			return 0, false
		}
		n, err := strconv.Atoi(rest[:3])
		if err != nil || n > int(maxCharVal) {
			return 0, false
		}
		//nolint:gosec // n is at most 255.
		return rune(n), true
	}
	return 0, false
}

// charFromStringFn is "Char.fromString s": the first character
// of s, decoding an SML escape, or NONE.
func charFromStringFn(arg Val) (Val, error) {
	c, ok := charFromString(asString(arg))
	if !ok {
		return noneVal, nil
	}
	return someVal(c), nil
}

// Bases and digit widths of the numeric C escapes.
const (
	octalBase   = 8
	hexBase     = 16
	octalDigits = 3
	hexDigits   = 2
)

// charToCStringFn is "Char.toCString c": the character in C string
// syntax.
func charToCStringFn(arg Val) (Val, error) {
	return CharToCString(asChar(arg)), nil
}

// CharToCString renders a character as Char.toCString does: the C
// escapes, and a three-digit octal escape for any other
// non-printing character.
func CharToCString(c rune) string {
	// lint: sort until '^\t}' where '^\tcase '
	switch c {
	case '"':
		return `\"`
	case '\\':
		return `\\`
	case '\a':
		return `\a`
	case '\b':
		return `\b`
	case '\f':
		return `\f`
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	case '\v':
		return `\v`
	}
	if c < ' ' || c > '~' {
		o := strconv.FormatInt(int64(c), octalBase)
		return `\` + strings.Repeat("0", octalDigits-len(o)) + o
	}
	return string(c)
}

// cStringEscapes are the single-letter C escapes that
// Char.fromCString decodes.
var cStringEscapes = map[byte]rune{
	'"': '"', '\'': '\'', '?': '?', '\\': '\\',
	'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n',
	'r': '\r', 't': '\t', 'v': '\v',
}

// charFromCStringFn is "Char.fromCString s": the first character of
// s, decoding a leading C escape, or NONE.
func charFromCStringFn(arg Val) (Val, error) {
	s := asString(arg)
	if s == "" {
		return noneVal, nil
	}
	if s[0] != '\\' {
		return someVal(rune(s[0])), nil
	}
	if len(s) == 1 { // a lone backslash
		return noneVal, nil
	}
	if r, ok := cStringEscapes[s[1]]; ok {
		return someVal(r), nil
	}
	if s[1] == 'x' {
		return cCharCode(s[2:], hexBase, hexDigits)
	}
	if s[1] >= '0' && s[1] <= '7' {
		return cCharCode(s[1:], octalBase, octalDigits)
	}
	return noneVal, nil
}

// cCharCode decodes up to maxDigits digits of a numeric C escape in
// the given base into a character, or NONE if it is out of range.
func cCharCode(s string, base, maxDigits int) (Val, error) {
	end := 0
	for end < maxDigits && end < len(s) && isBaseDigit(s[end], base) {
		end++
	}
	if end == 0 {
		return noneVal, nil
	}
	n, err := strconv.ParseInt(s[:end], base, 32)
	if err != nil || n > int64(maxCharVal) {
		return noneVal, nil //nolint:nilerr // out of range is NONE
	}
	return someVal(rune(n)), nil
}

// isBaseDigit reports whether b is a digit in base 8 or 16.
func isBaseDigit(b byte, base int) bool {
	if base == hexBase {
		return isHexByte(b)
	}
	return b >= '0' && b <= '7'
}

// charFromIntFn is "Char.fromInt i": SOME (chr i), or NONE if i
// is out of range.
func charFromIntFn(arg Val) (Val, error) {
	i := asInt(arg)
	if i < 0 || i > maxCharVal {
		return noneVal, nil
	}
	return someVal(i), nil
}
