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

package terminal

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/muesli/termenv"
)

// The terminal's background color decides the color scheme when the
// "colorScheme" property does not name one. It is learned by asking
// the terminal — the OSC 11 query, which termenv makes — while the
// terminal is idle, before the line reader starts.

// background is the terminal's background color as
// "rgb:RRRR/GGGG/BBBB", the form an xterm answers an OSC 11 query
// with and the form the "terminalBackground" property holds. It is
// empty when color is off, or when the terminal does not answer; a
// caller then falls back to COLORFGBG, and finally to a dark
// background, as morel-java does.
func background(out *termenv.Output) string {
	if noColor(out) {
		return ""
	}
	color := out.BackgroundColor()
	if color == nil {
		return ""
	}
	r, g, b := termenv.ConvertToRGB(color).RGB255()
	return formatBackground(r, g, b)
}

// formatBackground renders 8-bit channels as an xterm background
// answer. Each byte is doubled, as a terminal reports 16-bit
// channels: 0xff becomes "ffff".
func formatBackground(r, g, b uint8) string {
	channel := func(v uint8) string {
		return fmt.Sprintf("%02x%02x", v, v)
	}
	return "rgb:" + channel(r) + "/" + channel(g) + "/" +
		channel(b)
}

// noColor reports whether color is switched off: NO_COLOR is set,
// or the terminal is "dumb".
func noColor(out *termenv.Output) bool {
	return out.EnvNoColor() || os.Getenv("TERM") == "dumb"
}

// colorFgBg reads the background from the COLORFGBG environment
// variable, which some terminals set to "fg;bg" with ANSI color
// indices. It is morel-java's fallback when the terminal does not
// answer the query. Indices 0-6 and 8 are dark, the rest light; the
// result is only ever black or white, which is enough to choose a
// scheme.
func colorFgBg() string {
	// COLORFGBG is "fg;bg", sometimes with a third field.
	const minFields = 2
	parts := strings.Split(os.Getenv("COLORFGBG"), ";")
	if len(parts) < minFields {
		return ""
	}
	bg, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return ""
	}
	const (
		lastDarkColor = 6
		darkGrey      = 8
		full          = 0xff
	)
	if bg <= lastDarkColor || bg == darkGrey {
		return formatBackground(0, 0, 0)
	}
	return formatBackground(full, full, full)
}

// deduceBackground is the terminal's background: the answer to the
// query, else COLORFGBG, else a dark background — morel-java's
// order. It is empty only when color is off, so that the scheme is
// then "none".
func deduceBackground(out *termenv.Output) string {
	if noColor(out) {
		return ""
	}
	if s := background(out); s != "" {
		return s
	}
	if s := colorFgBg(); s != "" {
		return s
	}
	return formatBackground(0, 0, 0)
}
