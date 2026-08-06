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
	"fmt"
	"strconv"
	"strings"
)

// The built-in syntax-highlighting color schemes. A style is zero
// or more attributes (bold, italic, underline, faint) followed by a
// color: an ANSI color name, an index 0-255, or #rrggbb. An empty
// style leaves the category in the terminal's default style.
//
// morel-java loads these from .properties resources; the values
// here are the same, so that the three implementations color
// identically.

// The scheme names. "none" is the scheme that styles nothing, and
// is what a shell with no color uses.
const (
	SchemeDark  = "dark"
	SchemeLight = "light"
	SchemeNone  = "none"
)

// Styles used by more than one scheme.
const (
	styleMagenta = "magenta"
	styleRed     = "underline red"
	styleGreen   = "green"
	styleYellow  = "yellow"
)

// ColorScheme is a style for each token category, by name.
type ColorScheme struct {
	Name   string
	Styles map[Category]string
}

// Style is the style for a category, or "" if the category is left
// in the terminal's default style.
func (s ColorScheme) Style(c Category) string {
	return s.Styles[c]
}

// colorSchemes are the built-in schemes, in the order
// Sys.colorSchemes reports them.
var colorSchemes = []ColorScheme{
	{
		Name: SchemeDark,
		Styles: map[Category]string{
			CatComment:  "italic 245",
			CatConstant: styleYellow,
			CatError:    styleRed,
			CatKeyword:  "bold cyan",
			CatNumeric:  styleYellow,
			CatString:   styleGreen,
			CatSymbol:   "cyan",
			CatTypeVar:  styleMagenta,
		},
	},
	{
		Name: SchemeLight,
		Styles: map[Category]string{
			CatComment:  "italic bright-black",
			CatConstant: styleMagenta,
			CatError:    styleRed,
			CatKeyword:  "bold blue",
			CatNumeric:  styleMagenta,
			CatString:   styleGreen,
			CatSymbol:   "blue",
			CatTypeVar:  styleMagenta,
		},
	},
	{Name: SchemeNone, Styles: map[Category]string{}},
}

// schemeFields are the categories that Sys.colorSchemes reports, in
// the order the record's fields are sorted: a Morel record's fields
// are in label order, and "name" falls between "keyword" and
// "numeric".
var schemeFields = []struct {
	label    string
	category Category
}{
	// lint: sort until '^}' where '^\t{"'
	{"comment", CatComment},
	{"constant", CatConstant},
	{"error", CatError},
	{"identifier", CatIdentifier},
	{"keyword", CatKeyword},
	{"name", CatNone}, // the scheme's name, not a category
	{"numeric", CatNumeric},
	{"string", CatString},
	{"symbol", CatSymbol},
	{"typeVar", CatTypeVar},
}

// FindColorScheme returns the built-in scheme of that name.
func FindColorScheme(name string) (ColorScheme, bool) {
	for _, s := range colorSchemes {
		if s.Name == name {
			return s, true
		}
	}
	return ColorScheme{}, false
}

// DeduceColorScheme is the scheme in effect: the "colorScheme"
// property if it names a built-in scheme, otherwise the scheme the
// terminal's background implies, and "none" if that is unknown —
// which is the case in a script, where there is no terminal.
func (k *Kernel) DeduceColorScheme() ColorScheme {
	if s, ok := FindColorScheme(k.config.props["colorScheme"]); ok {
		return s
	}
	if s, ok := FindColorScheme(
		schemeForBackground(k.config.props["terminalBackground"]),
	); ok {
		return s
	}
	s, _ := FindColorScheme(SchemeNone)
	return s
}

// schemeForBackground names the scheme a terminal background calls
// for: "light" for a light background and "dark" for a dark one. An
// empty or unparsable background gives "", so that the caller falls
// back to "none".
func schemeForBackground(background string) string {
	luma, ok := backgroundLuma(background)
	if !ok {
		return ""
	}
	// Rec. 601 luma, and morel-java's 0.5 threshold.
	const midpoint = 0.5
	if luma > midpoint {
		return SchemeLight
	}
	return SchemeDark
}

// backgroundLuma is the perceived brightness, in [0, 1], of a
// background reported as "rgb:RRRR/GGGG/BBBB" — the form an xterm
// answers an OSC 11 query with, and the form the
// "terminalBackground" property holds.
func backgroundLuma(background string) (float64, bool) {
	rest, found := strings.CutPrefix(background, "rgb:")
	if !found {
		return 0, false
	}
	parts := strings.Split(rest, "/")
	const channels = 3
	if len(parts) != channels {
		return 0, false
	}
	var v [channels]float64
	for i, p := range parts {
		f, ok := hexFraction(p)
		if !ok {
			return 0, false
		}
		v[i] = f
	}
	// Rec. 601, as morel-java and morel-rust.
	const (
		redWeight   = 0.299
		greenWeight = 0.587
		blueWeight  = 0.114
	)
	return redWeight*v[0] + greenWeight*v[1] +
		blueWeight*v[2], true
}

// hexFraction reads a hex channel of any width — "f", "ff", "ffff"
// are all full — as a fraction in [0, 1].
func hexFraction(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	var n, full uint64
	const hexBase = 16
	for i := range len(s) {
		d := strings.IndexByte("0123456789abcdef",
			lowerASCII(s[i]))
		if d < 0 {
			return 0, false
		}
		n = n*hexBase + uint64(d)
		full = full*hexBase + hexBase - 1
	}
	return float64(n) / float64(full), true
}

// lowerASCII lowercases an ASCII letter.
func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b - 'A' + 'a'
	}
	return b
}

// ansiAttrs are the style attributes, and their SGR parameters.
var ansiAttrs = map[string]string{
	// lint: sort until '^}' where '^\t"'
	"blink":       "5",
	"bold":        "1",
	"conceal":     "8",
	"crossed-out": "9",
	"faint":       "2",
	"inverse":     "7",
	"italic":      "3",
	"underline":   "4",
}

// ansiColors are the named colors, and their SGR foreground
// parameters.
var ansiColors = map[string]string{
	// lint: sort until '^}' where '^\t"'
	"black":          "30",
	"blue":           "34",
	"bright-black":   "90",
	"bright-blue":    "94",
	"bright-cyan":    "96",
	"bright-green":   "92",
	"bright-magenta": "95",
	"bright-red":     "91",
	"bright-white":   "97",
	"bright-yellow":  "93",
	"cyan":           "36",
	"green":          "32",
	"magenta":        "35",
	"red":            "31",
	"white":          "37",
	"yellow":         "33",
}

// AnsiReset ends a styled span.
const AnsiReset = "\x1b[0m"

// AnsiPrefix converts a style such as "bold cyan" or "italic 245"
// into the SGR sequence that begins it, or "" if the style is empty
// or unrecognized.
func AnsiPrefix(style string) string {
	var params []string
	for tok := range strings.FieldsSeq(style) {
		switch {
		case ansiAttrs[tok] != "":
			params = append(params, ansiAttrs[tok])
		case ansiColors[tok] != "":
			params = append(params, ansiColors[tok])
		default:
			if code, ok := ansiColorCode(tok); ok {
				params = append(params, code)
			}
		}
	}
	if len(params) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(params, ";") + "m"
}

// ansiColorCode is the SGR foreground parameter for a palette index
// 0-255 ("38;5;N") or an "#rrggbb" color ("38;2;R;G;B").
func ansiColorCode(tok string) (string, bool) {
	const (
		hexBase   = 16
		hexDigits = 6
		byteBits  = 8
		redShift  = 2 * byteBits
		maxIndex  = 255
	)
	if hex, ok := strings.CutPrefix(tok, "#"); ok {
		if len(hex) != hexDigits {
			return "", false
		}
		n, err := strconv.ParseUint(hex, hexBase, 32)
		if err != nil {
			return "", false
		}
		r := (n >> redShift) & maxIndex
		g := (n >> byteBits) & maxIndex
		b := n & maxIndex
		return fmt.Sprintf("38;2;%d;%d;%d", r, g, b), true
	}
	n, err := strconv.Atoi(tok)
	if err != nil || n < 0 || n > maxIndex {
		return "", false
	}
	return "38;5;" + strconv.Itoa(n), true
}

// Highlight returns src with each span wrapped in the ANSI escapes
// its category calls for. A category with no style, and every
// category under the "none" scheme, is left unchanged.
func (s ColorScheme) Highlight(src string) string {
	var b strings.Builder
	for _, span := range Scan(src) {
		text := src[span.Start:span.End]
		prefix := AnsiPrefix(s.Style(span.Category))
		if prefix == "" {
			b.WriteString(text)
			continue
		}
		b.WriteString(prefix)
		b.WriteString(text)
		b.WriteString(AnsiReset)
	}
	return b.String()
}
