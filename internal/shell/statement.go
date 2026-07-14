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

// Package shell contains the kernel, the script runner, and the
// statement splitter.
package shell

import (
	"errors"

	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/token"
)

// Split scans src and returns its complete statements, each a
// verbatim slice of src including its terminating ";" and any
// comments and whitespace that precede it, followed by the text
// of any incomplete final statement.
//
// A ";" terminates a statement only at top level: not inside a
// string or comment (the lexer sees neither), and not inside
// parentheses, brackets, braces, or a let/sig ... end construct.
// Input that ends inside a string, comment, or quoted identifier
// is returned as incomplete text, since more input could complete
// it; other lexical errors are returned as errors.
func Split(name, src string) ([]string, string, error) {
	l := parse.NewLexer(name, src)
	runes := []rune(src)
	var stmts []string
	begin, depth := 0, 0
	for {
		tok, err := l.Next()
		if err != nil {
			var perr *parse.Error
			if errors.As(err, &perr) && perr.Unclosed {
				break
			}
			return stmts, string(runes[begin:]), err
		}
		if tok.Kind == token.EOF {
			break
		}
		// lint: sort until '^\t\t}' where '^\t\tcase '
		switch tok.Kind {
		case token.LParen, token.LBracket, token.LBrace,
			token.Let, token.Sig:
			depth++
		case token.RParen, token.RBracket, token.RBrace,
			token.End:
			if depth > 0 {
				depth--
			}
		case token.Semi:
			if depth == 0 {
				end := l.Offset()
				stmts = append(stmts, string(runes[begin:end]))
				begin = end
			}
		default:
		}
	}
	return stmts, string(runes[begin:]), nil
}
