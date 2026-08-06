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

package shell_test

import (
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/shell"
)

// TestSchemeForBackground checks the scheme a terminal background
// implies. The background is the string an xterm answers an OSC 11
// query with, and is what the "terminalBackground" property holds.
func TestSchemeForBackground(t *testing.T) {
	for _, tc := range []struct{ background, want string }{
		// Black and white, at each of the widths a terminal
		// may answer with.
		{"rgb:0000/0000/0000", "dark"},
		{"rgb:ffff/ffff/ffff", "light"},
		{"rgb:00/00/00", "dark"},
		{"rgb:ff/ff/ff", "light"},
		{"rgb:0/0/0", "dark"},
		{"rgb:f/f/f", "light"},
		{"rgb:FFFF/FFFF/FFFF", "light"},
		// Green weighs most, blue least, under Rec. 601: a
		// pure green background is light, a pure blue one
		// dark, though the channel values are the same.
		{"rgb:0000/ffff/0000", "light"},
		{"rgb:0000/0000/ffff", "dark"},
		{"rgb:ffff/0000/0000", "dark"},
		// Grey straddles the 0.5 threshold at half of ffff.
		{"rgb:7fff/7fff/7fff", "dark"},
		{"rgb:8000/8000/8000", "light"},
		// Unset or unparsable: no scheme, so the caller falls
		// back to "none".
		{"", ""},
		{"black", ""},
		{"rgb:0000/0000", ""},
		{"rgb:0000/0000/0000/0000", ""},
		{"rgb:zzzz/0000/0000", ""},
		{"rgb://", ""},
	} {
		got := shell.SchemeForBackgroundForTest(tc.background)
		if got != tc.want {
			t.Errorf("scheme for %q = %q, want %q",
				tc.background, got, tc.want)
		}
	}
}

// TestFindColorScheme checks that the built-in schemes are found
// by name, and that an unknown name is not.
func TestFindColorScheme(t *testing.T) {
	for _, name := range []string{"dark", "light", "none"} {
		s, ok := shell.FindColorScheme(name)
		if !ok || s.Name != name {
			t.Errorf("FindColorScheme(%q) = %v, %v", name,
				s.Name, ok)
		}
	}
	if _, ok := shell.FindColorScheme("purple"); ok {
		t.Error(`FindColorScheme("purple") should not be found`)
	}
	// "none" styles nothing.
	s, _ := shell.FindColorScheme("none")
	if got := s.Style(shell.CatKeyword); got != "" {
		t.Errorf(`"none" styles keywords as %q`, got)
	}
	// "dark" is morel-java's scheme, value for value.
	d, _ := shell.FindColorScheme("dark")
	if got := d.Style(shell.CatKeyword); got != "bold cyan" {
		t.Errorf(`"dark" styles keywords as %q`, got)
	}
}

// TestAnsiPrefix checks the conversion of a style spec into the
// SGR sequence that begins it.
func TestAnsiPrefix(t *testing.T) {
	const esc = "\x1b["
	for _, tc := range []struct{ style, want string }{
		{"", ""},
		{"bold", esc + "1m"},
		{"cyan", esc + "36m"},
		{"bold cyan", esc + "1;36m"},
		{"underline red", esc + "4;31m"},
		{"italic bright-black", esc + "3;90m"},
		// A palette index, and a #rrggbb color.
		{"245", esc + "38;5;245m"},
		{"italic 245", esc + "3;38;5;245m"},
		{"#ff8000", esc + "38;2;255;128;0m"},
		// Unrecognized words contribute nothing.
		{"chartreuse", ""},
		{"256", ""},
		{"#fff", ""},
		{"bold chartreuse", esc + "1m"},
	} {
		if got := shell.AnsiPrefix(tc.style); got != tc.want {
			t.Errorf("AnsiPrefix(%q) = %q, want %q", tc.style,
				got, tc.want)
		}
	}
}

// TestHighlight checks that a scheme wraps each span, and that
// "none" leaves the source alone.
func TestHighlight(t *testing.T) {
	const src = `val x = 1 (* hi *) "s";`
	none, _ := shell.FindColorScheme("none")
	if got := none.Highlight(src); got != src {
		t.Errorf(`"none" changed the source: %q`, got)
	}
	dark, _ := shell.FindColorScheme("dark")
	got := dark.Highlight(src)
	// Every styled span is closed, and the visible text is
	// unchanged once the escapes are removed.
	if plain := stripSGR(got); plain != src {
		t.Errorf("highlighting changed the text:\n got %q\nwant %q",
			plain, src)
	}
	for _, want := range []string{
		"\x1b[1;36mval\x1b[0m",            // keyword: bold cyan
		"\x1b[33m1\x1b[0m",                // numeric: yellow
		"\x1b[32m\"s\"\x1b[0m",            // string: green
		"\x1b[3;38;5;245m(* hi *)\x1b[0m", // comment
	} {
		if !strings.Contains(got, want) {
			t.Errorf("highlighted source lacks %q", want)
		}
	}
}

// stripSGR removes SGR escape sequences.
func stripSGR(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], 'm')
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}
