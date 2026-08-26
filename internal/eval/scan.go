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
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// A "scan" function reads a value from a prefix of a character
// stream and returns "SOME (value, rest)", or NONE if it cannot
// read one. The stream is whatever the reader understands; only
// the reader makes sense of it.
//
// charSource gives a scanner one character of lookahead over such
// a reader. It is a cursor: peek reads the character at the
// cursor without moving it, advance moves past it, and stream
// returns the stream positioned at the cursor, which is what a
// scanner returns alongside the value it read.

// noChar is what peek returns at the end of the stream. No
// character equals it, so a scanner can compare without asking
// whether the stream has ended.
const noChar rune = -1

type charSource struct {
	reader Val
	stream Val
	// c is the character at the cursor, or noChar at the end.
	c rune
	// next is the stream that follows c.
	next Val
	// err is the first error the reader returned, if any; a
	// scanner checks it before trusting what it read.
	err error
}

// newCharSource returns a cursor over reader, positioned at
// stream.
func newCharSource(reader, stream Val) *charSource {
	s := &charSource{reader: reader, stream: stream}
	s.read()
	return s
}

// read fills in the character at the cursor.
func (s *charSource) read() {
	option, err := ApplyVal(s.reader, s.stream)
	if err != nil {
		s.err, s.c = err, noChar
		return
	}
	pair, some := asOption(option)
	if !some {
		s.c = noChar
		return
	}
	first, rest := asPair(pair)
	s.c, s.next = asChar(first), rest
}

// peek returns the character at the cursor without moving it, or
// noChar at the end of the stream.
func (s *charSource) peek() rune { return s.c }

// advance moves the cursor past the character it is on.
func (s *charSource) advance() {
	s.stream = s.next
	s.read()
}

// mark returns the stream at the cursor, to return with a scanned
// value or to rewind to.
func (s *charSource) mark() Val { return s.stream }

// reset moves the cursor back to a stream that mark returned.
func (s *charSource) reset(stream Val) {
	s.stream = stream
	s.read()
}

// skipWhitespace moves the cursor past any whitespace.
func (s *charSource) skipWhitespace() {
	for s.c != noChar && unicode.IsSpace(s.c) {
		s.advance()
	}
}

// consume moves the cursor past word if the stream holds it, and
// reports whether it did; if not, the cursor does not move.
func (s *charSource) consume(word string) bool {
	start := s.mark()
	for _, want := range word {
		if s.c != want {
			s.reset(start)
			return false
		}
		s.advance()
	}
	return true
}

// stringReader returns a reader over the characters of s, for a
// "fromString" that is defined as its "scan" over the whole
// string. The stream is a position in s.
func stringReader(s string) Val {
	runes := []rune(s)
	return Fn(func(stream Val) (Val, error) {
		i := int(asInt(stream))
		if i >= len(runes) {
			return noneVal, nil
		}
		//nolint:gosec // a string's length fits in an int32.
		return someVal([]Val{runes[i], int32(i + 1)}), nil
	})
}

// scanString reads a value from a prefix of s with a two-argument
// scan function, and returns it without the stream that follows —
// the shape every "fromString" has.
func scanString(scan func(reader, stream Val) (Val, error),
	s string,
) (Val, error) {
	option, err := scan(stringReader(s), int32(0))
	if err != nil {
		return nil, err
	}
	return scanValue(option), nil
}

// scanValue is the value a scan returned, dropping the stream
// that follows it: "SOME (v, rest)" becomes "SOME v".
func scanValue(option Val) Val {
	pair, some := asOption(option)
	if !some {
		return noneVal
	}
	value, _ := asPair(pair)
	return someVal(value)
}

// boolScanFn is "Bool.scan rdr src": "true" or "false", after any
// leading whitespace.
func boolScanFn(reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	s.skipWhitespace()
	for _, word := range []struct {
		text  string
		value bool
	}{{"true", true}, {"false", false}} {
		if s.consume(word.text) {
			if s.err != nil {
				return nil, s.err
			}
			return someVal([]Val{word.value, s.mark()}), nil
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return noneVal, nil
}

// scanDigits reads the digits of a number in the given base,
// after an optional sign and radix prefix, and returns the digits
// and whether the number is negative. The digits are empty if
// there are none, which is NONE to the caller.
func scanDigits(s *charSource, base int, signed bool) (string, bool) {
	negative := false
	if signed {
		switch s.peek() {
		case '~', '-':
			negative = true
			s.advance()
		case '+':
			s.advance()
		}
	}
	if base == hexBase {
		skipHexPrefix(s, signed)
	}
	var digits []rune
	for digitVal(s.peek(), base) >= 0 {
		digits = append(digits, s.peek())
		s.advance()
	}
	return string(digits), negative
}

// skipHexPrefix moves the cursor past a "0x" prefix, or "0wx" for
// a word. If what follows is not a hexadecimal digit the "0" is a
// digit and the "x" is not ours, so the cursor does not move.
func skipHexPrefix(s *charSource, signed bool) {
	if s.peek() != '0' {
		return
	}
	start := s.mark()
	s.advance()
	if !signed && (s.peek() == 'w' || s.peek() == 'W') {
		s.advance()
	}
	if s.peek() == 'x' || s.peek() == 'X' {
		s.advance()
	}
	if digitVal(s.peek(), hexBase) < 0 {
		s.reset(start)
	}
}

// digitOffset is the value of the first letter digit, "a" or "A",
// in a base above ten.
const digitOffset = 10

// digitVal is the value of a digit in the given base, or -1 if it
// is not one.
func digitVal(c rune, base int) int {
	var v int
	switch {
	case c >= '0' && c <= '9':
		v = int(c - '0')
	case c >= 'a' && c <= 'z':
		v = int(c-'a') + digitOffset
	case c >= 'A' && c <= 'Z':
		v = int(c-'A') + digitOffset
	default:
		return -1
	}
	if v >= base {
		return -1
	}
	return v
}

// intFromStringFn is "Int.fromString s", which the Standard Basis
// defines as "StringCvt.scanString (scan DEC)".
func intFromStringFn(arg Val) (Val, error) {
	return scanString(func(reader, stream Val) (Val, error) {
		return intScan(decRadix, reader, stream)
	}, asString(arg))
}

// wordFromStringFn is "Word.fromString s", which the Standard
// Basis defines as "StringCvt.scanString (scan HEX)".
func wordFromStringFn(arg Val) (Val, error) {
	return scanString(func(reader, stream Val) (Val, error) {
		return wordScan(hexRadix, reader, stream)
	}, asString(arg))
}

// intScanFn is "Int.scan radix rdr src": an optionally signed
// number in the radix's base, after any leading whitespace.
func intScanFn(radix, reader, stream Val) (Val, error) {
	return intScan(radixBase(radix), reader, stream)
}

func intScan(base int, reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	s.skipWhitespace()
	digits, negative := scanDigits(s, base, true)
	if s.err != nil {
		return nil, s.err
	}
	if digits == "" {
		return noneVal, nil
	}
	if negative {
		digits = "-" + digits
	}
	i, err := strconv.ParseInt(digits, base, 32)
	if err != nil {
		return nil, &MorelError{Exn: ExnOverflow}
	}
	return someVal([]Val{int32(i), s.mark()}), nil
}

// wordScanFn is "Word.scan radix rdr src": an unsigned number in
// the radix's base, after any leading whitespace.
func wordScanFn(radix, reader, stream Val) (Val, error) {
	return wordScan(radixBase(radix), reader, stream)
}

func wordScan(base int, reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	s.skipWhitespace()
	digits, _ := scanDigits(s, base, false)
	if s.err != nil {
		return nil, s.err
	}
	if digits == "" {
		return noneVal, nil
	}
	u, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return nil, &MorelError{Exn: ExnOverflow}
	}
	return someVal([]Val{u, s.mark()}), nil
}

// realScanFn is "Real.scan rdr src": a real number, after any
// leading whitespace. It accepts "inf", "infinity" and "nan" in
// any case, as well as the usual digits, point and exponent.
func realScanFn(reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	s.skipWhitespace()
	var b strings.Builder
	negative := false
	switch s.peek() {
	case '~', '-':
		negative = true
		b.WriteByte('-')
		s.advance()
	case '+':
		s.advance()
	}
	if v, ok := scanInfinityOrNan(s, negative); ok {
		if s.err != nil {
			return nil, s.err
		}
		return someVal([]Val{v, s.mark()}), nil
	}
	digits := scanRealDigits(s, &b)
	if s.err != nil {
		return nil, s.err
	}
	if digits == 0 {
		return noneVal, nil
	}
	f, err := strconv.ParseFloat(b.String(), 32)
	if err != nil {
		return noneVal, nil //nolint:nilerr // NONE, not an error
	}
	return someVal([]Val{float32(f), s.mark()}), nil
}

// scanInfinityOrNan reads "inf", "infinity" or "nan", in any
// case, and reports whether it did.
func scanInfinityOrNan(s *charSource, negative bool) (Val, bool) {
	for _, word := range []string{"infinity", infText, nanText} {
		if !s.consumeFold(word) {
			continue
		}
		if word == nanText {
			return float32(math.NaN()), true
		}
		sign := 1.0
		if negative {
			sign = -1.0
		}
		return float32(math.Inf(int(sign))), true
	}
	return nil, false
}

// scanRealDigits reads the digits, fraction and exponent of a
// real number into b, and returns how many digits it read before
// the exponent; zero means there was no number.
func scanRealDigits(s *charSource, b *strings.Builder) int {
	digits := scanDigitRun(s, b)
	if s.peek() == '.' {
		start := s.mark()
		s.advance()
		var frac strings.Builder
		frac.WriteByte('.')
		if n := scanDigitRun(s, &frac); n == 0 {
			// A point that no digit follows is not ours.
			s.reset(start)
		} else {
			b.WriteString(frac.String())
			digits += n
		}
	}
	if digits == 0 {
		return 0
	}
	if s.peek() == 'e' || s.peek() == 'E' {
		start := s.mark()
		s.advance()
		var exp strings.Builder
		exp.WriteByte('e')
		switch s.peek() {
		case '~', '-':
			exp.WriteByte('-')
			s.advance()
		case '+':
			s.advance()
		}
		if scanDigitRun(s, &exp) == 0 {
			// An "e" that no digit follows is not ours.
			s.reset(start)
		} else {
			b.WriteString(exp.String())
		}
	}
	return digits
}

// scanDigitRun reads a run of decimal digits into b, and returns
// how many there were.
func scanDigitRun(s *charSource, b *strings.Builder) int {
	n := 0
	for digitVal(s.peek(), decRadix) >= 0 {
		b.WriteRune(s.peek())
		s.advance()
		n++
	}
	return n
}

// consumeFold is consume, ignoring case.
func (s *charSource) consumeFold(word string) bool {
	start := s.mark()
	for _, want := range word {
		if s.c == noChar || unicode.ToLower(s.c) != want {
			s.reset(start)
			return false
		}
		s.advance()
	}
	return true
}

// realFromStringFn is "Real.fromString s", which the Standard
// Basis defines as "StringCvt.scanString scan".
func realFromStringFn(arg Val) (Val, error) {
	return scanString(realScanFn, asString(arg))
}

// scanChar reads one character of an SML character or string
// constant — a printable character, or an escape sequence — and
// reports whether it read one. quoteOk allows a bare '"', which a
// string constant's body may hold but a character constant's may
// not.
//
// When it cannot read one, the cursor is left where it started,
// so the caller can stop without having consumed anything.
func scanChar(s *charSource, quoteOk bool) (rune, bool) {
	start := s.mark()
	c := s.peek()
	switch {
	case c == noChar:
		return 0, false
	case c != '\\':
		// A raw character stands for itself, but only if it is
		// printable: a tab is written "\t", never as itself.
		if !isPrintChar(c) || (c == '"' && !quoteOk) {
			return 0, false
		}
		s.advance()
		return c, true
	}
	s.advance()
	if escaped, ok := simpleEscape(s.peek()); ok {
		s.advance()
		return escaped, true
	}
	switch {
	case s.peek() == '^':
		// "\^@" is character 0, "\^A" is 1, ..., "\^_" is 31.
		s.advance()
		c3 := s.peek()
		if c3 < '@' || c3 > '_' {
			s.reset(start)
			return 0, false
		}
		s.advance()
		return c3 - ctrlOffset, true
	case s.peek() == 'u':
		// A "u" escape is the character whose code is the four
		// hexadecimal digits that follow.
		s.advance()
		return scanCharCode(s, start, hexBase, uEscapeDigits)
	case digitVal(s.peek(), decRadix) >= 0:
		// "\ddd" is the character whose code is the three decimal
		// digits "ddd".
		return scanCharCode(s, start, decRadix, decEscapeDigits)
	}
	s.reset(start)
	return 0, false
}

// The lengths of the numeric character escapes.
const (
	uEscapeDigits   = 4
	decEscapeDigits = 3
)

// simpleEscape is the character a one-letter escape stands for,
// and whether c introduces one.
func simpleEscape(c rune) (rune, bool) {
	// lint: sort until '^	}' where '^	case '
	switch c {
	case '"':
		return '"', true
	case '\\':
		return '\\', true
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
	default:
		return 0, false
	}
}

// scanCharCode reads the n digits of a numeric escape in the
// given base. It rewinds to start and fails if there are not n
// digits, or if they name no character.
func scanCharCode(s *charSource, start Val, base, n int) (rune,
	bool,
) {
	code := 0
	for range n {
		digit := digitVal(s.peek(), base)
		if digit < 0 {
			s.reset(start)
			return 0, false
		}
		code = code*base + digit
		s.advance()
	}
	if code > int(maxCharVal) {
		s.reset(start)
		return 0, false
	}
	return rune(code), true
}

// skipGaps moves the cursor past any escaped formatting
// sequences: a backslash, one or more whitespace characters, and
// a backslash, which together stand for nothing. A backslash
// followed by whitespace but no closing backslash consumes
// nothing, and the caller finds the backslash and rejects it as
// an ill-formed escape.
func skipGaps(s *charSource) {
	for {
		start := s.mark()
		if s.peek() != '\\' {
			return
		}
		s.advance()
		if s.peek() == noChar || !unicode.IsSpace(s.peek()) {
			s.reset(start)
			return
		}
		s.skipWhitespace()
		if s.peek() != '\\' {
			s.reset(start)
			return
		}
		s.advance()
	}
}

// charScanFn is "Char.scan rdr src": one character constant, an
// escape sequence or a printable character. It does not skip
// leading whitespace — a space is a character like any other —
// but it does skip escaped formatting sequences.
func charScanFn(reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	skipGaps(s)
	c, ok := scanChar(s, false)
	if s.err != nil {
		return nil, s.err
	}
	if !ok {
		return noneVal, nil
	}
	return someVal([]Val{c, s.mark()}), nil
}

// charFromStringFn is "Char.fromString s", which the Standard
// Basis defines as "StringCvt.scanString scan".
func charFromStringFn(arg Val) (Val, error) {
	return scanString(charScanFn, asString(arg))
}

// stringScanFn is "String.scan rdr src": as many character
// constants as it can read. Reading none is NONE, unless the
// stream had ended, when it is the empty string.
func stringScanFn(reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	var b strings.Builder
	for {
		// An escaped formatting sequence stands for nothing, and is
		// consumed even if what follows it cannot be scanned.
		skipGaps(s)
		c, ok := scanChar(s, true)
		if !ok {
			break
		}
		b.WriteRune(c)
	}
	if s.err != nil {
		return nil, s.err
	}
	if b.Len() == 0 && s.peek() != noChar {
		// Nothing was scanned, and not because the stream ended.
		return noneVal, nil
	}
	return someVal([]Val{b.String(), s.mark()}), nil
}

// stringFromStringFn is "String.fromString s", which the Standard
// Basis defines as "StringCvt.scanString scan".
func stringFromStringFn(arg Val) (Val, error) {
	return scanString(stringScanFn, asString(arg))
}

// scanCChar reads one character of a C string constant — a
// character, or a C escape sequence — and reports whether it read
// one. As for scanChar, a failed read leaves the cursor where it
// started.
//
// A C constant admits an unescaped single quote but not an
// unescaped double quote.
func scanCChar(s *charSource) (rune, bool) {
	start := s.mark()
	c := s.peek()
	switch {
	case c == noChar:
		return 0, false
	case c != '\\':
		if !isPrintChar(c) || c == '"' {
			return 0, false
		}
		s.advance()
		return c, true
	}
	s.advance()
	// A C escape letter is ASCII, so a wider character is simply
	// not one.
	if c2 := s.peek(); c2 >= 0 && c2 <= maxCharVal {
		if escaped, ok := cStringEscapes[byte(c2)]; ok {
			s.advance()
			return escaped, true
		}
	}
	switch {
	case s.peek() == 'x':
		s.advance()
		return scanCCharCode(s, start, hexBase, hexDigits)
	case s.peek() >= '0' && s.peek() <= '7':
		return scanCCharCode(s, start, octalBase, octalDigits)
	}
	s.reset(start)
	return 0, false
}

// scanCCharCode reads up to maxDigits digits of a numeric C
// escape in the given base. Unlike an SML escape, whose length is
// fixed, a C escape takes as many digits as it can.
func scanCCharCode(s *charSource, start Val, base, maxDigits int,
) (rune, bool) {
	code, n := 0, 0
	for n < maxDigits && digitVal(s.peek(), base) >= 0 {
		code = code*base + digitVal(s.peek(), base)
		s.advance()
		n++
	}
	if n == 0 || code > int(maxCharVal) {
		s.reset(start)
		return 0, false
	}
	return rune(code), true
}

// stringFromCStringFn is "String.fromCString s": the characters
// of a C string constant, or NONE unless the whole of s is one.
//
// That is where it differs from "fromString", which returns as
// many characters as it can scan and ignores the rest: so
// "fromString \"abc\\ndef\"" is SOME "abc", but "fromCString" of
// the same is NONE.
func stringFromCStringFn(arg Val) (Val, error) {
	s := newCharSource(stringReader(asString(arg)), int32(0))
	var b strings.Builder
	for s.peek() != noChar {
		c, ok := scanCChar(s)
		if !ok {
			return noneVal, nil
		}
		b.WriteRune(c)
	}
	return someVal(b.String()), nil
}

// stringToStringFn is "String.toString s": s with every
// non-printable character replaced by an SML escape sequence,
// which is "translate Char.toString s".
func stringToStringFn(arg Val) (Val, error) {
	var b strings.Builder
	for _, c := range asString(arg) {
		b.WriteString(CharToString(c))
	}
	return b.String(), nil
}

// stringToCStringFn is "String.toCString s": s with every
// non-printable character replaced by a C escape sequence, which
// is "translate Char.toCString s".
func stringToCStringFn(arg Val) (Val, error) {
	var b strings.Builder
	for _, c := range asString(arg) {
		b.WriteString(CharToCString(c))
	}
	return b.String(), nil
}

// nanoDigits is the number of fractional digits a time keeps: a
// nanosecond is the finest morel represents.
const nanoDigits = 9

// timeScanFn is "Time.scan rdr src": a decimal number of seconds,
// after any leading whitespace, with an optional sign and an
// optional fractional part. Digits beyond a nanosecond are
// discarded. It raises Time if the value is too large.
func timeScanFn(reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	s.skipWhitespace()
	negative := false
	switch s.peek() {
	case '~', '-':
		negative = true
		s.advance()
	case '+':
		s.advance()
	}
	var integer, fraction strings.Builder
	scanDigitRun(s, &integer)
	switch {
	case s.peek() == '.':
		// A decimal point must be followed by at least one digit; if
		// it is not, the time is ill-formed, not merely finished.
		s.advance()
		if scanDigitRun(s, &fraction) == 0 {
			return noneVal, nil
		}
	case integer.Len() == 0:
		return noneVal, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	nanos, err := timeNanos(integer.String(), fraction.String())
	if err != nil {
		return nil, err
	}
	if negative {
		nanos = -nanos
	}
	return someVal([]Val{nanos, s.mark()}), nil
}

// timeNanos converts a number of seconds, given as its integer
// and fractional digits, to nanoseconds. The fraction is padded
// or truncated to nanoDigits.
func timeNanos(integer, fraction string) (int64, error) {
	if len(fraction) < nanoDigits {
		fraction += strings.Repeat("0", nanoDigits-len(fraction))
	}
	fraction = fraction[:nanoDigits]
	seconds := int64(0)
	if integer != "" {
		var err error
		seconds, err = strconv.ParseInt(integer, decRadix, 64)
		if err != nil {
			return 0, &MorelError{Exn: ExnTime}
		}
	}
	nanos, err := strconv.ParseInt(fraction, decRadix, 64)
	if err != nil {
		return 0, &MorelError{Exn: ExnTime}
	}
	total := seconds * nsPerSecond
	if seconds != 0 && total/nsPerSecond != seconds {
		return 0, &MorelError{Exn: ExnTime}
	}
	if total > math.MaxInt64-nanos {
		return 0, &MorelError{Exn: ExnTime}
	}
	return total + nanos, nil
}

// timeFromStringFn is "Time.fromString s", which the Standard
// Basis defines as "StringCvt.scanString scan".
func timeFromStringFn(arg Val) (Val, error) {
	return scanString(timeScanFn, asString(arg))
}

// dateScanFn is "Date.scan rdr src": a date in the format
// "Thu Jan 01 00:00:00 1970", which is what "Date.toString"
// writes. It does not skip leading whitespace, and the spacing
// within the date is exact.
func dateScanFn(reader, stream Val) (Val, error) {
	s := newCharSource(reader, stream)
	d, ok := scanDate(s)
	if s.err != nil {
		return nil, s.err
	}
	if !ok {
		return noneVal, nil
	}
	if d.err != nil {
		return nil, d.err
	}
	return someVal([]Val{d.value, s.mark()}), nil
}

// scannedDate is a date the scanner read, or the error that
// building it raised.
type scannedDate struct {
	value Val
	err   error
}

// scanDate reads the fields of a date and builds it.
func scanDate(s *charSource) (scannedDate, bool) {
	// The weekday must be a name, but says nothing that the rest of
	// the date does not; the weekday of the result comes from the
	// date.
	weekday, ok := scanFixedWidth(s, nameWidth)
	if !ok || weekdayFromName(weekday) == 0 {
		return scannedDate{}, false
	}
	if !scanLiteral(s, ' ') {
		return scannedDate{}, false
	}
	monthName, ok := scanFixedWidth(s, nameWidth)
	if !ok {
		return scannedDate{}, false
	}
	month := monthFromName(monthName)
	if month == 0 {
		return scannedDate{}, false
	}
	if !scanLiteral(s, ' ') {
		return scannedDate{}, false
	}
	day, ok := scanDay(s)
	if !ok {
		return scannedDate{}, false
	}
	c, ok := scanClock(s)
	if !ok || !scanLiteral(s, ' ') {
		return scannedDate{}, false
	}
	var yearDigits strings.Builder
	if scanDigitRun(s, &yearDigits) == 0 {
		return scannedDate{}, false
	}
	year, err := strconv.Atoi(yearDigits.String())
	if err != nil {
		return scannedDate{nil, &MorelError{Exn: ExnDate}}, true
	}
	d, err := DateConstruct(year, month, day, c.hour, c.minute,
		c.second, time.UTC)
	return scannedDate{d, err}, true
}

// nameWidth is the width of a weekday or month name, "Mon".
const nameWidth = 3

// scanFixedWidth reads exactly n characters, and reports whether
// the stream held them.
func scanFixedWidth(s *charSource, n int) (string, bool) {
	start := s.mark()
	var b strings.Builder
	for range n {
		if s.peek() == noChar {
			s.reset(start)
			return "", false
		}
		b.WriteRune(s.peek())
		s.advance()
	}
	return b.String(), true
}

// scanLiteral reads the character c, and reports whether the
// stream held it.
func scanLiteral(s *charSource, c rune) bool {
	if s.peek() != c {
		return false
	}
	s.advance()
	return true
}

// scanDay reads a two-character day, which may be written with a
// leading zero or a leading space: "Mar 08" and "Mar  8" are both
// allowed.
func scanDay(s *charSource) (int, bool) {
	day := 0
	if pad := s.peek(); pad != ' ' {
		if digitVal(pad, decRadix) < 0 {
			return 0, false
		}
		day = digitVal(pad, decRadix)
	}
	s.advance()
	d := digitVal(s.peek(), decRadix)
	if d < 0 {
		return 0, false
	}
	s.advance()
	return day*decRadix + d, true
}

// clock is the time of day a date carries.
type clock struct {
	hour   int
	minute int
	second int
}

// scanClock reads " HH:MM:SS", the space that separates it from
// the day included.
func scanClock(s *charSource) (clock, bool) {
	if !scanLiteral(s, ' ') {
		return clock{}, false
	}
	hour, ok := scanTwoDigits(s)
	if !ok || !scanLiteral(s, ':') {
		return clock{}, false
	}
	minute, ok := scanTwoDigits(s)
	if !ok || !scanLiteral(s, ':') {
		return clock{}, false
	}
	second, ok := scanTwoDigits(s)
	return clock{hour, minute, second}, ok
}

// scanTwoDigits reads exactly two digits, as an hour, minute or
// second is written.
func scanTwoDigits(s *charSource) (int, bool) {
	hi := digitVal(s.peek(), decRadix)
	if hi < 0 {
		return 0, false
	}
	s.advance()
	lo := digitVal(s.peek(), decRadix)
	if lo < 0 {
		return 0, false
	}
	s.advance()
	return hi*decRadix + lo, true
}

// dateFromStringFn is "Date.fromString s", which the Standard
// Basis defines as "StringCvt.scanString scan".
func dateFromStringFn(arg Val) (Val, error) {
	return scanString(dateScanFn, asString(arg))
}
