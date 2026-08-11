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

// Project lint checks: license headers, line lengths, and Morel
// script style.
package morel_test

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestStructureScripts checks that every built-in structure (one
// per "lib/*.sig" file) has a "testdata/script/built-in/<name>.smli"
// test script. The
// companion guarantee — that every ".smli" file is run — holds by
// construction, since TestScripts walks the directory.
func TestStructureScripts(t *testing.T) {
	sigs, err := os.ReadDir("lib")
	if err != nil {
		t.Fatalf("read lib: %v", err)
	}
	scriptDir := "testdata/script/built-in"
	// "Test" exists so that the implementation can be tested from a
	// script, and is hidden from a session by the
	// "excludeStructures" property. Its members are exercised by the
	// scripts that use them -- "Test.highlight" by "highlight.smli"
	// -- as they are in morel-java, which has no "built-in/test.smli"
	// either.
	exempt := map[string]bool{"test.sig": true}
	var missing []string
	for _, sig := range sigs {
		name := sig.Name()
		if !strings.HasSuffix(name, ".sig") || exempt[name] {
			continue
		}
		script := strings.TrimSuffix(name, ".sig") + ".smli"
		_, err := os.Stat(scriptDir + "/" + script)
		if err != nil {
			missing = append(missing, script)
		}
	}
	if len(missing) > 0 {
		t.Errorf("missing built-in test scripts in %s: %s",
			scriptDir, strings.Join(missing, ", "))
	}
}

// TestLint checks every file tracked by git against the project
// style rules.
func TestLint(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "git", "ls-files")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var warnings []string
	warn := func(w string) { warnings = append(warnings, w) }
	files := strings.TrimSpace(string(out))
	dirs := map[string]bool{}
	for f := range strings.SplitSeq(files, "\n") {
		f = strings.TrimSpace(f)
		markDir(t, dirs, f)
		lintFile(f, warn)
	}
	if len(warnings) > 0 {
		t.Errorf("Linting issues found:\n%s",
			strings.Join(warnings, "\n"))
	}
}

// markDir reads the file's directory, so that the go test cache
// observes added and removed files; the file list itself comes
// from a git subprocess, which the cache cannot see.
func markDir(t *testing.T, dirs map[string]bool, file string) {
	t.Helper()
	dir := "."
	if i := strings.LastIndex(file, "/"); i >= 0 {
		dir = file[:i]
	}
	if dirs[dir] {
		return
	}
	dirs[dir] = true
	_, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
}

// headerLines returns the license header required at the top of
// every source file, without comment decoration.
func headerLines() []string {
	return []string{
		"Licensed to Julian Hyde under one or more contributor license",
		"agreements.  See the NOTICE file distributed with this work",
		"for additional information regarding copyright ownership.",
		"Julian Hyde licenses this file to you under the Apache",
		`License, Version 2.0 (the "License"); you may not use this`,
		"file except in compliance with the License.  You may obtain a",
		"copy of the License at",
		"",
		"http://www.apache.org/licenses/LICENSE-2.0",
		"",
		"Unless required by applicable law or agreed to in writing,",
		"software distributed under the License is distributed on an",
		`"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,`,
		"either express or implied.  See the License for the specific",
		"language governing permissions and limitations under the",
		"License.",
	}
}

const (
	// mdWidth is the maximum line length in Markdown files.
	mdWidth = 80
	// goWidth is the maximum line length in Go files.
	goWidth = 80
	// morelWidth is the required length of decorative comment
	// lines in Morel files.
	morelWidth = 70
	// noLimit disables the line-length check.
	noLimit = math.MaxInt
	// mlStart opens a block comment in ML-family files.
	mlStart = "(*\n"
	// mlPrefix is the comment-line prefix in ML-family files.
	mlPrefix = " * "
	// mlSuffix closes a block comment in ML-family files.
	mlSuffix = "\n *)"
	// indentStep is one level of indentation in a sort pattern.
	indentStep = "    "
)

// language is the language a source file is written in.
type language int

const (
	langOther language = iota
	langGo
	langMarkdown
	langMorel
)

// commentFormat describes how the license header is rendered in a
// given file type, and the type's maximum line length.
type commentFormat struct {
	blockStart string
	linePrefix string
	blockEnd   string
	maxLen     int
}

// header renders the license header in this comment format.
func (f commentFormat) header() string {
	lines := make([]string, 0, len(headerLines()))
	for _, l := range headerLines() {
		if l == "" {
			lines = append(lines,
				strings.TrimRight(f.linePrefix, " "))
		} else {
			lines = append(lines, f.linePrefix+l)
		}
	}
	s := f.blockStart + strings.Join(lines, "\n")
	// For ML-family files, don't require the closing "*)", since
	// the header comment may have additional content.
	if f.linePrefix != mlPrefix {
		s += f.blockEnd
	}
	return s
}

// formats maps a file suffix to its comment format. A file whose
// suffix is not in the map is not checked.
func formats() map[string]commentFormat {
	return map[string]commentFormat{
		"gitignore": {"", "# ", "", noLimit},
		"go":        {"", "// ", "", goWidth},
		"html": {
			"<!DOCTYPE html>\n<!--\n", "", "\n-->\n",
			noLimit,
		},
		"md": {
			"<!--\n{% comment %}\n", "",
			"\n{% endcomment %}\n-->", mdWidth,
		},
		"mod":  {"", "// ", "", noLimit},
		"py":   {"#!/usr/bin/env python3\n", "# ", "", goWidth},
		"sig":  {mlStart, mlPrefix, mlSuffix, noLimit},
		"sml":  {mlStart, mlPrefix, mlSuffix, noLimit},
		"smli": {mlStart, mlPrefix, mlSuffix, noLimit},
		"toml": {"", "# ", "", noLimit},
		"yml":  {"", "# ", "", noLimit},
	}
}

// languageOf deduces the language of a file from its suffix.
func languageOf(suffix string) language {
	// lint: sort until '^\t}' where '^\tcase '
	switch suffix {
	case "go":
		return langGo
	case "md":
		return langMarkdown
	case "sig", "sml", "smli":
		return langMorel
	default:
		return langOther
	}
}

// lintFile checks one file, appending a message to warn for each
// violation.
func lintFile(name string, warn func(string)) {
	// Working notes are exempt from all checks.
	if name == "plan.md" || name == "scratch.txt" {
		return
	}
	suffix := name[strings.LastIndex(name, ".")+1:]
	f, ok := formats()[suffix]
	if !ok {
		return
	}
	data, err := os.ReadFile(name)
	if err != nil {
		warn(fmt.Sprintf("%s:1: Cannot read: %v", name, err))
		return
	}
	contents := string(data)
	if !strings.HasPrefix(contents, f.header()) {
		warn(name + ":1: File does not start with a header")
	}
	if contents != "" && !strings.HasSuffix(contents, "\n") {
		warn(name + ": No newline at end of file")
	}
	s := &textState{
		name: name,
		f:    f,
		lang: languageOf(suffix),
		warn: warn,
	}
	s.lintText(contents)
}

// textState is the per-file state of the line checks.
type textState struct {
	name       string
	warn       func(string)
	sort       *sortCheck
	prev       string
	goSwitches []goSwitch
	f          commentFormat
	lang       language
	// runStart and runEnd delimit the current run of consecutive
	// "(*)" line comments; runStart is 0 when not in a run.
	runStart int
	runEnd   int
	// commentDepth is the block-comment nesting depth at the start
	// of the line being checked.
	commentDepth int
	// pendingOpenLine is the line on which a block comment opened
	// with a bare "(*"; the line after it says whether the block is
	// prose or commented-out code. It is 0 when none is pending.
	pendingOpenLine int
	// commentedOutCode says that the open block comment holds code
	// rather than prose, so its lines are not starred.
	commentedOutCode bool
	// lintEnableLine is the line at which a "lint:skip" directive
	// stops suppressing warnings.
	lintEnableLine int
	inPre          bool
	inRawString    bool
	inGenerated    bool
}

// goSwitch tracks one open switch statement in a Go file.
type goSwitch struct {
	indent       string
	line         int
	cases        int
	hasDirective bool
}

func (s *textState) warnf(line int, format string, args ...any) {
	if line < s.lintEnableLine {
		return
	}
	prefix := fmt.Sprintf("%s:%d: ", s.name, line)
	s.warn(prefix + fmt.Sprintf(format, args...))
}

// skipMarker is built by concatenation so that this file does not
// suppress its own warnings.
func skipMarker() string {
	return "lint:" + "skip"
}

// skipRegexp matches a "skip" directive in a line comment -- "//"
// in Go, "(*)" in Morel -- optionally followed by a count.
func skipRegexp() *regexp.Regexp {
	return regexp.MustCompile(`(?://|\(\*\)) ` + skipMarker() +
		`(?: (\d+))?\s*$`)
}

// maybeSkip acts on a "skip" directive, which suppresses warnings
// for its own line and for the N lines after it (N defaults to 1).
// It is how a file keeps a deliberately malformed comment, such as
// those that test the parser.
func (s *textState) maybeSkip(n int, l string) {
	m := skipRegexp().FindStringSubmatch(l)
	if m == nil {
		return
	}
	count := 1
	if m[1] != "" {
		var err error
		count, err = strconv.Atoi(m[1])
		if err != nil {
			return
		}
	}
	s.lintEnableLine = n + count + 1
}

func (s *textState) lintText(contents string) {
	lines := strings.Split(
		strings.TrimSuffix(contents, "\n"), "\n")
	// A trailing blank line closes any open comment run.
	lines = append(lines, "")
	n := 0
	for i, l := range lines {
		n = i + 1
		s.line(n, l)
	}
	if s.sort != nil {
		s.warnf(n, "Unterminated 'sort until' directive")
	}
}

func (s *textState) line(n int, l string) {
	s.maybeSkip(n, l)
	s.checkSorted(n, l)
	if strings.HasSuffix(l, " ") {
		s.warnf(n, "Trailing spaces")
	}
	s.updateRegions(l)
	s.checkLength(n, l)
	s.maybeStartSort(n, l)
	if s.lang == langMorel {
		s.checkSetMode(n, l)
		s.checkDecorative(n, l)
		s.checkCommentRun(n, l)
		s.checkBlockComment(n, l)
	}
	if s.lang == langGo {
		s.checkGoSwitch(n, l)
	}
	s.prev = l
}

// minSortedCases is the number of cases at which a Go switch
// must have a sort directive.
const minSortedCases = 3

// checkGoSwitch requires a sort directive on the line above any
// tagged or type switch with minSortedCases or more cases. Guard
// switches ("switch {" and "switch init; {") are exempt: their
// case order is significant.
func (s *textState) checkGoSwitch(n int, l string) {
	if len(s.goSwitches) > 0 {
		top := &s.goSwitches[len(s.goSwitches)-1]
		switch {
		case l == top.indent+"}":
			if top.cases >= minSortedCases &&
				!top.hasDirective {
				s.warnf(top.line, "switch with %d cases needs "+
					"a '%s' directive", top.cases, sortMarker())
			}
			s.goSwitches = s.goSwitches[:len(s.goSwitches)-1]
			return
		case strings.HasPrefix(l, top.indent+"case "):
			top.cases++
		}
	}
	if isGoSwitchStart(l) {
		indent := l[:len(l)-len(strings.TrimLeft(l, "\t"))]
		s.goSwitches = append(s.goSwitches, goSwitch{
			indent:       indent,
			line:         n,
			hasDirective: strings.Contains(s.prev, sortMarker()),
		})
	}
}

func isGoSwitchStart(l string) bool {
	t := strings.TrimLeft(l, "\t")
	return strings.HasPrefix(t, "switch ") &&
		strings.HasSuffix(t, "{") &&
		t != "switch {" &&
		!strings.HasSuffix(t, "; {")
}

// updateRegions tracks regions in which the line-length check is
// suspended: raw strings, HTML pre blocks, and generated Markdown
// blocks.
func (s *textState) updateRegions(l string) {
	if s.lang == langGo && strings.Count(l, "`")%2 == 1 {
		s.inRawString = !s.inRawString
	}
	if strings.Contains(l, "<pre>") ||
		strings.Contains(l, "<pre ") ||
		strings.Contains(l, `<div class="code-`) {
		s.inPre = true
	}
	if strings.Contains(l, "</pre>") ||
		strings.Contains(l, "</div>") {
		s.inPre = false
	}
	if s.lang == langMarkdown {
		if strings.HasPrefix(l, "[//]: # (start:") {
			s.inGenerated = true
		}
		if strings.HasPrefix(l, "[//]: # (end:") {
			s.inGenerated = false
		}
	}
}

func (s *textState) checkLength(n int, l string) {
	if width(l) > s.f.maxLen && !s.lengthExempt(l) {
		s.warnf(n, "Line too long (%d > %d): %s",
			width(l), s.f.maxLen, l)
	}
}

// width is the display width of a line at tab width 4: a tab
// advances to the next multiple of 4, and every other rune
// counts as one column.
func width(l string) int {
	w := 0
	for _, r := range l {
		if r == '	' {
			w += 4 - w%4
		} else {
			w++
		}
	}
	return w
}

func (s *textState) lengthExempt(l string) bool {
	return strings.Contains(l, "://") ||
		strings.Contains(l, `src="`) ||
		strings.Contains(l, `href="`) ||
		strings.HasPrefix(l, "|") ||
		strings.HasPrefix(l, indentStep) ||
		isMdTableSeparator(l) ||
		s.inRawString || s.inPre || s.inGenerated
}

// isMdTableSeparator reports whether the line is a Markdown table
// separator row (e.g. "|---|---|"), which may exceed the width
// limit.
func isMdTableSeparator(l string) bool {
	if l == "" {
		return false
	}
	for _, c := range l {
		if c != '-' && c != '|' && c != ':' {
			return false
		}
	}
	return true
}

func setModeRegexp() *regexp.Regexp {
	return regexp.MustCompile(`^set\s*\(\s*"mode"`)
}

func (s *textState) checkSetMode(n int, l string) {
	if setModeRegexp().MatchString(l) {
		s.warnf(n, `set("mode",...) is not used in morel-go; `+
			"a section enters a .smli file only when it passes "+
			"(see plan.md)")
	}
}

// checkDecorative checks decorative banner comments: "---" (not
// "===" or "***"), block-comment form, exactly morelWidth
// characters.
func (s *textState) checkDecorative(n int, l string) {
	if !strings.HasPrefix(l, "(*") {
		return
	}
	banner := strings.Contains(l, "===") ||
		strings.Contains(l, "***")
	if strings.HasPrefix(l, "(*)") {
		if banner {
			s.warnf(n, "decorative comment should be "+
				"'(* --- ... --- *)'")
		}
		return
	}
	if banner {
		bad := "==="
		if strings.Contains(l, "***") {
			bad = "***"
		}
		s.warnf(n, "decorative comment; use '---' not '%s'", bad)
	}
	if (strings.HasSuffix(l, "*)") || strings.HasSuffix(l, "--")) &&
		strings.Contains(l, "---") &&
		len(l) != morelWidth {
		s.warnf(n, "decorative comment length %d != %d",
			len(l), morelWidth)
	}
}

// checkCommentRun flags two or more consecutive "(*)" line
// comments, which should be a block comment. TODO lines are
// excluded, so a TODO can be deleted without reformatting.
func (s *textState) checkCommentRun(n int, l string) {
	if strings.HasPrefix(strings.TrimSpace(l), "(*)") &&
		!strings.Contains(l, "TODO") {
		if s.runStart == 0 {
			s.runStart = n
		}
		s.runEnd = n
		return
	}
	if s.runStart != 0 && s.runEnd > s.runStart {
		s.warnf(s.runStart, "Consecutive line comments; convert "+
			"lines %d through %d into a block comment.",
			s.runStart, s.runEnd)
	}
	s.runStart = 0
}

// checkBlockComment requires that a continuation line of a block
// comment -- one the comment was already open at the start of --
// begin with zero or more spaces and then "*":
//
//	(* A multi-line
//	 * block comment. *)
//
// The line that closes the comment is exempt, and so is a comment
// that holds code rather than prose: one that opens with a bare
// "(*" whose next line starts flush, or one that opens "(* TODO".
func (s *textState) checkBlockComment(n int, l string) {
	if s.pendingOpenLine != 0 && s.pendingOpenLine == n-1 {
		// The line after a bare "(*" says what kind of comment this
		// is. Code is written flush left, and starring it would have
		// to be undone to bring it back.
		if l != "" && l[0] != ' ' {
			s.commentedOutCode = true
		}
		s.pendingOpenLine = 0
	}
	if s.commentDepth > 0 && !s.commentedOutCode &&
		!strings.HasPrefix(strings.TrimSpace(l), "*)") &&
		!strings.HasPrefix(strings.TrimLeft(l, " "), "*") {
		s.warnf(n, "block comment continuation line must start "+
			"with '*'")
	}
	s.scanComment(n, l)
}

// scanComment advances the block-comment depth over one line, so
// that checkBlockComment reads it at the start of the next.
func (s *textState) scanComment(n int, l string) {
	inString := false
	i := 0
	for i+1 < len(l) {
		switch {
		case inString:
			// Inside a string literal, "(*" and "*)" are ordinary
			// characters; "highlight.smli" is full of such strings.
			if l[i] == '\\' {
				i += 2
				continue
			}
			if l[i] == '"' {
				inString = false
			}
			i++
		case s.commentDepth == 0 && l[i] == '"':
			inString = true
			i++
		case l[i] == '(' && l[i+1] == '*':
			if i+2 < len(l) && l[i+2] == ')' {
				if s.commentDepth == 0 {
					// "(*)" comments to the end of the line, so nothing
					// later on it opens or closes a block. Reading on
					// would see the "(*" in a line such as
					//   "a" ^ (*) line comment with (* fake block open
					// and believe a block comment had opened.
					return
				}
				// Within a block comment, "(*)" is three characters.
				i += 3
				continue
			}
			s.commentDepth++
			if s.commentDepth == 1 {
				s.openBlock(n, l)
			}
			i += 2
		case l[i] == '*' && l[i+1] == ')':
			s.commentDepth--
			if s.commentDepth == 0 {
				s.pendingOpenLine = 0
				s.commentedOutCode = false
			}
			i += 2
		default:
			i++
		}
	}
}

// openBlock notes what an opening "(*" says about its block:
// nothing, if it is bare, in which case the line after it decides;
// or that the block holds code, if it opens "(* TODO".
func (s *textState) openBlock(n int, l string) {
	t := strings.TrimSpace(l)
	switch {
	case t == "(*":
		s.pendingOpenLine = n
	case strings.HasPrefix(t, "(* TODO"):
		// A comment that opens "(* TODO" introduces the code it
		// comments out rather than describing it, and says so on its
		// opening line, so its body needs no line to say so.
		s.commentedOutCode = true
	}
}

// sortMarker is built by concatenation so that this file does not
// lint itself.
func sortMarker() string {
	return "lint: " + "sort until"
}

func (s *textState) maybeStartSort(n int, l string) {
	m := sortMarker()
	if !strings.Contains(l, m) ||
		strings.Contains(l, `"`+m) {
		return
	}
	sc, err := parseSortCheck(l)
	if err != nil {
		s.sort = nil
		s.warnf(n, "Malformed 'sort until' directive: %s", l)
		return
	}
	s.sort = sc
}

func (s *textState) checkSorted(n int, l string) {
	if s.sort == nil {
		return
	}
	sc := s.sort
	l2 := l
	if sc.erase != nil {
		l2 = sc.erase.ReplaceAllString(l, "")
	}
	switch {
	case sc.until.MatchString(l):
		s.sort = nil
	case sc.where == nil || sc.where.MatchString(l):
		if len(sc.previous) > 0 &&
			l2 < sc.previous[len(sc.previous)-1].text {
			target := 0
			for _, p := range slices.Backward(sc.previous) {
				target = p.line
				if l2 > p.text {
					break
				}
			}
			s.warnf(n, "Line out of order; move to line %d",
				target)
		}
		sc.previous = append(sc.previous,
			sortedLine{text: l2, line: n})
	}
}

type sortedLine struct {
	text string
	line int
}

type sortCheck struct {
	until    *regexp.Regexp
	erase    *regexp.Regexp
	where    *regexp.Regexp
	previous []sortedLine
}

// parseSortCheck parses a "sort until 'PATTERN'" directive, with
// optional "erase 'RE'" and "where 'RE'" clauses.
func parseSortCheck(l string) (*sortCheck, error) {
	indent := l[:len(l)-len(strings.TrimLeft(l, " "))]
	until, err := quotedPattern(l, "'", indent)
	if err != nil {
		return nil, err
	}
	erase, err := optionalPattern(l, " erase '", indent)
	if err != nil {
		return nil, err
	}
	where, err := optionalPattern(l, " where '", indent)
	if err != nil {
		return nil, err
	}
	return &sortCheck{until: until, erase: erase, where: where},
		nil
}

func quotedPattern(l, marker, indent string) (*regexp.Regexp,
	error,
) {
	_, rest, found := strings.Cut(l, marker)
	if !found {
		return nil, fmt.Errorf("no %q in %q", marker, l)
	}
	pattern, _, found := strings.Cut(rest, "'")
	if !found {
		return nil, fmt.Errorf("unterminated pattern in %q", l)
	}
	return compilePattern(pattern, indent)
}

func optionalPattern(l, marker, indent string) (*regexp.Regexp,
	error,
) {
	if !strings.Contains(l, marker) {
		return nil, nil //nolint:nilnil // absent optional clause
	}
	return quotedPattern(l, marker, indent)
}

// compilePattern converts a directive pattern to a regular
// expression. "##" is replaced by line-start plus the directive's
// indentation; "#" by line-start plus one level less.
func compilePattern(pattern, indent string) (*regexp.Regexp,
	error,
) {
	p := strings.ReplaceAll(pattern, "##", "^"+indent)
	indent = strings.TrimPrefix(indent, indentStep)
	p = strings.ReplaceAll(p, "#", "^"+indent)
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, fmt.Errorf("bad pattern %q: %w", pattern, err)
	}
	return re, nil
}
