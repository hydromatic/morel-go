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

package sig

import (
	"fmt"
	"strings"
)

// parseFile reads the specifications of one signature file: the
// items between "sig" and "end", with comments and attributes
// (such as "[@@method]") removed.
func parseFile(name, src string) (*file, error) {
	body, err := signatureBody(name, stripAttributes(
		stripComments(src)))
	if err != nil {
		return nil, err
	}
	f := &file{name: name, structure: structureName(name)}
	for _, item := range splitItems(body) {
		err := f.parseItem(item)
		if err != nil {
			return nil, fmt.Errorf("sig: %s: %w", name, err)
		}
	}
	return f, nil
}

// itemKeywords start the specifications that can appear in a
// signature body.
var itemKeywords = map[string]bool{
	"datatype":  true,
	"eqtype":    true,
	"exception": true,
	"structure": true,
	"type":      true,
	"val":       true,
}

const datatypeKeyword = "datatype"

func (f *file) parseItem(item string) error {
	tokens := strings.Fields(item)
	// lint: sort until '^	}' where '^	case '
	switch tokens[0] {
	case "exception", "structure":
		return nil
	case "val":
		return f.parseValSpec(item)
	case datatypeKeyword, "eqtype", "type":
		return f.parseTypeSpec(tokens[0], item)
	default:
		return fmt.Errorf("unknown specification %q", tokens[0])
	}
}

// parseValSpec reads "val name : type".
func (f *file) parseValSpec(item string) error {
	rest := strings.TrimPrefix(item, "val")
	name, rest, ok := cutToken(rest)
	if !ok {
		return fmt.Errorf("incomplete val %q", item)
	}
	rest = strings.TrimSpace(rest)
	typ, ok := strings.CutPrefix(rest, ":")
	if !ok {
		return fmt.Errorf("expected ':' in %q", item)
	}
	f.vals = append(f.vals, valSpec{
		name: unquote(name),
		typ:  strings.TrimSpace(typ),
	})
	return nil
}

// parseTypeSpec reads "datatype [tyvars] name = con | ..." or
// "type [tyvars] name [= ...]" or "eqtype [tyvars] name".
func (f *file) parseTypeSpec(keyword, item string) error {
	rest := strings.TrimSpace(strings.TrimPrefix(item, keyword))
	tyVars, rest, err := cutTyVars(rest)
	if err != nil {
		return err
	}
	name, rest, ok := cutToken(rest)
	if !ok {
		return fmt.Errorf("incomplete %s %q", keyword, item)
	}
	spec := typeSpec{name: unquote(name), tyVars: tyVars}
	body, hasBody := strings.CutPrefix(
		strings.TrimSpace(rest), "=")
	if keyword == datatypeKeyword {
		if !hasBody {
			return fmt.Errorf("expected '=' in %q", item)
		}
		for _, con := range splitCons(body) {
			conName, of, _ := cutToken(strings.TrimSpace(con))
			of = strings.TrimSpace(of)
			if of != "" {
				var found bool
				of, found = strings.CutPrefix(of, "of")
				if !found {
					return fmt.Errorf("expected 'of' in %q", con)
				}
			}
			spec.cons = append(spec.cons, conSpec{
				name: unquote(conName),
				of:   strings.TrimSpace(of),
			})
		}
	}
	f.typs = append(f.typs, spec)
	return nil
}

// cutTyVars reads an optional type-parameter list: nothing, "'a",
// or "('a, 'b)".
func cutTyVars(s string) ([]string, string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "'") {
		tv, rest, _ := cutToken(s)
		return []string{tv}, rest, nil
	}
	if !strings.HasPrefix(s, "(") || !strings.Contains(s, "'") {
		return nil, s, nil
	}
	end := strings.IndexByte(s, ')')
	if end < 0 {
		return nil, "", fmt.Errorf("unclosed type parameters %q", s)
	}
	var tyVars []string
	for tv := range strings.SplitSeq(s[1:end], ",") {
		tyVars = append(tyVars, strings.TrimSpace(tv))
	}
	return tyVars, s[end+1:], nil
}

// cutToken returns the first whitespace-delimited token, which
// may be backtick-quoted, and the rest.
func cutToken(s string) (string, string, bool) {
	s = strings.TrimLeft(s, " \t\r\n")
	if s == "" {
		return "", "", false
	}
	if s[0] == '`' {
		end := strings.IndexByte(s[1:], '`')
		if end < 0 {
			return "", "", false
		}
		quoted := end + len("``")
		return s[:quoted], s[quoted:], true
	}
	end := strings.IndexAny(s, " \t\r\n")
	if end < 0 {
		return s, "", true
	}
	return s[:end], s[end:], true
}

func unquote(s string) string {
	return strings.Trim(s, "`")
}

// splitCons splits a datatype body on "|".
func splitCons(s string) []string {
	return strings.Split(s, "|")
}

// splitItems splits a signature body into specifications, each
// starting with an item keyword.
func splitItems(body string) []string {
	var items []string
	start := -1
	for _, w := range words(body) {
		if itemKeywords[w.text] && start >= 0 {
			items = append(items, body[start:w.offset])
			start = w.offset
		} else if itemKeywords[w.text] {
			start = w.offset
		}
	}
	if start >= 0 {
		items = append(items, body[start:])
	}
	return items
}

// signatureBody returns the text between "sig" and its matching
// "end".
func signatureBody(name, s string) (string, error) {
	ws := words(s)
	for i, w := range ws {
		if w.text != "sig" {
			continue
		}
		depth := 0
		for _, w2 := range ws[i+1:] {
			switch w2.text {
			case "sig":
				depth++
			case "end":
				if depth == 0 {
					return s[w.offset+len("sig") : w2.offset], nil
				}
				depth--
			}
		}
	}
	return "", fmt.Errorf("sig: %s: no signature body", name)
}

// word is a whitespace-delimited token and its offset.
type word struct {
	text   string
	offset int
}

// words splits a string on whitespace, remembering offsets.
// Backtick-quoted tokens stay whole, so "`end`" is not the
// keyword "end".
func words(s string) []word {
	var ws []word
	i, n := 0, len(s)
	for i < n {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '`':
			end := strings.IndexByte(s[i+1:], '`')
			if end < 0 {
				ws = append(ws, word{s[i:], i})
				return ws
			}
			quoted := end + len("``")
			ws = append(ws, word{s[i : i+quoted], i})
			i += quoted
		default:
			end := strings.IndexAny(s[i:], " \t\r\n`")
			if end < 0 {
				end = n - i
			}
			ws = append(ws, word{s[i : i+end], i})
			i += end
		}
	}
	return ws
}

// stripComments removes "(*...*)" comments (which nest) and
// "(*)" line comments.
func stripComments(s string) string {
	var b strings.Builder
	i, n := 0, len(s)
	for i < n {
		switch {
		case strings.HasPrefix(s[i:], "(*)"):
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				return b.String()
			}
			i += end
		case strings.HasPrefix(s[i:], "(*"):
			i = skipComment(s, i+len("(*"))
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// skipComment returns the position after the "*)" that closes a
// comment; pos is the position after the opening "(*".
func skipComment(s string, pos int) int {
	n := len(s)
	for pos < n {
		switch {
		case strings.HasPrefix(s[pos:], "(*"):
			pos = skipComment(s, pos+len("(*"))
		case strings.HasPrefix(s[pos:], "*)"):
			return pos + len("*)")
		default:
			pos++
		}
	}
	return n
}

// stripAttributes removes "[@@name]" and "[@@name \"text\"]"
// attributes.
func stripAttributes(s string) string {
	var b strings.Builder
	i, n := 0, len(s)
	for i < n {
		if !strings.HasPrefix(s[i:], "[@@") {
			b.WriteByte(s[i])
			i++
			continue
		}
		i += len("[@@")
		inString := false
	attribute:
		for i < n {
			c := s[i]
			i++
			switch {
			case inString && c == '\\':
				i++
			case inString && c == '"':
				inString = false
			case inString:
			case c == '"':
				inString = true
			case c == ']':
				break attribute
			}
		}
	}
	return b.String()
}
