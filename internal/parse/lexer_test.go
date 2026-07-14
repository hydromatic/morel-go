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

package parse_test

import (
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/token"
)

// lex returns the kinds and texts of all tokens in src, or the
// error message.
func lex(t *testing.T, src string) []token.Token {
	t.Helper()
	l := parse.NewLexer("stdIn", src)
	var tokens []token.Token
	for {
		tok, err := l.Next()
		if err != nil {
			t.Fatalf("lex %q: %v", src, err)
		}
		if tok.Kind == token.EOF {
			return tokens
		}
		tokens = append(tokens, tok)
	}
}

// kinds is a compact rendering: one "kind:text" per token.
func kinds(t *testing.T, src string) string {
	t.Helper()
	toks := lex(t, src)
	parts := make([]string, 0, len(toks))
	for _, tok := range toks {
		parts = append(parts, tok.Kind.String()+":"+tok.Text)
	}
	return strings.Join(parts, " ")
}

func lexError(t *testing.T, src string) string {
	t.Helper()
	l := parse.NewLexer("stdIn", src)
	for {
		tok, err := l.Next()
		if err != nil {
			return err.Error()
		}
		if tok.Kind == token.EOF {
			t.Fatalf("lex %q: expected error", src)
		}
	}
}

func check(t *testing.T, src, want string) {
	t.Helper()
	if got := kinds(t, src); got != want {
		t.Errorf("lex %q:\n got %q\nwant %q", src, got, want)
	}
}

func TestLexIdentifiers(t *testing.T) {
	check(t, "x x1 x_y x'y hdX",
		"identifier:x identifier:x1 identifier:x_y "+
			"identifier:x'y identifier:hdX")
	check(t, "vals fromX", "identifier:vals identifier:fromX")
}

func TestLexKeywords(t *testing.T) {
	check(t, "val fun from where yieldAll o",
		"val:val fun:fun from:from where:where "+
			"yieldAll:yieldAll o:o")
	// "yieldall" is not the keyword; case matters.
	check(t, "yieldall", "identifier:yieldall")
	for kind := token.And; kind <= token.Over; kind++ {
		text := kind.String()
		if got := token.Lookup(text); got != kind {
			t.Errorf("Lookup(%q) = %v, want %v", text, got, kind)
		}
	}
}

func TestLexNumbers(t *testing.T) {
	check(t, "0 1 123 007",
		"integer literal:0 integer literal:1 "+
			"integer literal:123 integer literal:007")
	check(t, "1.5 0.001",
		"real literal:1.5 real literal:0.001")
	// A dot not followed by a digit is not part of the number.
	check(t, "1.x", "integer literal:1 .:. identifier:x")
}

func TestLexStrings(t *testing.T) {
	check(t, `"hello"`, `string literal:"hello"`)
	check(t, `""`, `string literal:""`)
	check(t, `"a\nb \\ \" \097 \^A"`,
		`string literal:"a\nb \\ \" \097 \^A"`)
	// A close-comment inside a string is just text.
	check(t, `"*)"`, `string literal:"*)"`)
	// A string may contain a raw newline.
	check(t, "\"ab\ncd\"", "string literal:\"ab\ncd\"")
	check(t, `#"a" #"\n"`, `char literal:#"a" char literal:#"\n"`)
}

func TestLexTyVars(t *testing.T) {
	check(t, "'a 'b2 'a_b 'a'b",
		"type variable:'a type variable:'b2 "+
			"type variable:'a_b type variable:'a'b")
	check(t, "('a, 'b) 'a list",
		"(:( type variable:'a ,:, type variable:'b ):) "+
			"type variable:'a identifier:list")
}

func TestLexQuotedIdents(t *testing.T) {
	check(t, "`<<` `a b`",
		"quoted identifier:`<<` quoted identifier:`a b`")
	check(t, "`a``b`", "quoted identifier:`a``b`")
	check(t, "Word.`<<` (a, b)",
		"identifier:Word .:. quoted identifier:`<<` "+
			"(:( identifier:a ,:, identifier:b ):)")
}

func TestLexNegativeNumbers(t *testing.T) {
	check(t, "~1 ~1.5",
		"integer literal:~1 real literal:~1.5")
	// "~" directly before a digit is part of the literal; before
	// anything else it is the negation operator.
	check(t, "1 ~2", "integer literal:1 integer literal:~2")
	check(t, "1 ~ 2",
		"integer literal:1 ~:~ integer literal:2")
	check(t, "1~2", "integer literal:1 integer literal:~2")
	check(t, "~x", "~:~ identifier:x")
}

func TestLexScientificNumbers(t *testing.T) {
	check(t, "1e2 1E2 1e~2 1.5e10 ~1.5e~2",
		"scientific literal:1e2 scientific literal:1E2 "+
			"scientific literal:1e~2 scientific literal:1.5e10 "+
			"scientific literal:~1.5e~2")
	// An "e" not followed by an exponent is an identifier.
	check(t, "1e", "integer literal:1 identifier:e")
	check(t, "1.5E", "real literal:1.5 identifier:E")
	check(t, "1e+5",
		"integer literal:1 identifier:e +:+ integer literal:5")
}

func TestLexUnderscore(t *testing.T) {
	check(t, "fun fst (x, _) = x",
		"fun:fun identifier:fst (:( identifier:x ,:, _:_ ):) "+
			"=:= identifier:x")
	// "_" alone is the wildcard; inside an identifier it is an
	// identifier character.
	check(t, "x_y _ _x",
		"identifier:x_y _:_ _:_ identifier:x")
}

func TestLexLabels(t *testing.T) {
	check(t, "#1 #deptno #x'",
		"label:#1 label:#deptno label:#x'")
}

func TestLexSymbols(t *testing.T) {
	check(t, "( ) { } [ ] ; | . , => -> ...",
		"(:( ):) {:{ }:} [:[ ]:] ;:; |:| .:. ,:, =>:=> ->:-> "+
			"...:...")
	check(t, "= > < : <= >= <> + - ^ * / ~ :: @",
		"=:= >:> <:< ::: <=:<= >=:>= <>:<> +:+ -:- ^:^ *:* /:/ "+
			"~:~ ::::: @:@")
	// Maximal munch.
	check(t, "1::2", "integer literal:1 ::::: integer literal:2")
	check(t, "x<=y", "identifier:x <=:<= identifier:y")
}

func TestLexComments(t *testing.T) {
	check(t, "1 (* comment *) 2",
		"integer literal:1 integer literal:2")
	check(t, "1 (* outer (* inner *) outer *) 2",
		"integer literal:1 integer literal:2")
	check(t, "(**) 1", "integer literal:1")
	check(t, "(***) 1", "integer literal:1")
	check(t, "(** doc comment *) 1", "integer literal:1")
	check(t, "(*) line comment\n1", "integer literal:1")
	check(t, "(*) unterminated line comment", "")
	// A semicolon inside a comment is not a token.
	check(t, "(* ; *) (*) ;\n", "")
	// "(*)" inside a block comment starts a line comment, within
	// which "*)" does not close the block.
	check(t, "(* a (*) hidden *)\nstill comment *) 1",
		"integer literal:1")
}

func TestLexSpans(t *testing.T) {
	toks := lex(t, "val x =\n  \"ab\";")
	want := []string{
		"1.1-1.4", "1.5", "1.7", "2.3-2.7",
		"2.7",
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d", len(toks), len(want))
	}
	for i, w := range want {
		if got := toks[i].Span.String(); got != w {
			t.Errorf("token %d (%s): span %s, want %s",
				i, toks[i].Text, got, w)
		}
	}
}

func TestLexErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`"abc`, "stdIn:1.1-1.5: unclosed string"},
		{"\"abc\ndef", "stdIn:1.1-2.4: unclosed string"},
		{"(* abc", "stdIn:1.1-1.7: unclosed comment"},
		{"(* a (* b *)", "stdIn:1.1-1.13: unclosed comment"},
		{`"bad \q escape"`, "stdIn:1.6: illegal escape"},
		{"a ? b", "stdIn:1.3: illegal character"},
		{"#?", "stdIn:1.1: illegal character"},
		{"''a", "stdIn:1.1: illegal character"},
		{"`abc", "stdIn:1.1-1.5: unclosed quoted identifier"},
	} {
		if got := lexError(t, tc.src); got != tc.want {
			t.Errorf("lex %q:\n got %q\nwant %q",
				tc.src, got, tc.want)
		}
	}
}

func TestSpanString(t *testing.T) {
	s := token.Span{
		Start: token.Pos{Line: 1, Col: 14},
		End:   token.Pos{Line: 1, Col: 25},
	}
	if got := s.String(); got != "1.14-1.25" {
		t.Errorf("got %q", got)
	}
	point := token.Span{
		Start: token.Pos{Line: 2, Col: 3},
		End:   token.Pos{Line: 2, Col: 3},
	}
	if got := point.String(); got != "2.3" {
		t.Errorf("got %q", got)
	}
	oneChar := token.Span{
		Start: token.Pos{Line: 1, Col: 9},
		End:   token.Pos{Line: 1, Col: 10},
	}
	if got := oneChar.String(); got != "1.9" {
		t.Errorf("got %q", got)
	}
}
