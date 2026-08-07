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

package datalog

import (
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/csv"
)

// CSVFacts converts CSV content — read in the dialect that
// package csv describes — into facts for a declared relation,
// matching columns to the declaration's parameters by name, as the
// reference implementation's .input loading does. The facts keep
// the file's row order.
//
// A program names the file it wants, so a file that does not match
// the declaration is an error rather than something to read around;
// that is where this parts company with the "file" value, which
// browses whatever it finds.
func CSVFacts(decl *Declaration, content string) ([]Statement, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, errorf("Input file for relation '%s' is empty",
			decl.Name)
	}
	columns := map[string]csv.Column{}
	for _, col := range csv.ParseHeader(lines[0]) {
		columns[col.Name] = col
	}
	selected := make([]csv.Column, len(decl.Params))
	for i, p := range decl.Params {
		col, ok := columns[p.Name]
		if !ok {
			return nil, errorf(
				"Column '%s' not found in input file for "+
					"relation '%s'", p.Name, decl.Name,
			)
		}
		selected[i] = col
	}
	var facts []Statement
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		values := csv.Cells(line)
		args := make([]Term, len(selected))
		for i, col := range selected {
			if col.Index >= len(values) {
				return nil, errorf(
					"Row %q in input file for relation '%s' is "+
						"too short", line, decl.Name,
				)
			}
			c, err := csvConstant(values[col.Index], col.Type)
			if err != nil {
				return nil, err
			}
			args[i] = c
		}
		facts = append(facts, &Fact{Atom: &Atom{
			Name: decl.Name, Args: args,
		}})
	}
	return facts, nil
}

// csvConstant parses one CSV value by its column type. Datalog
// knows only int and string, so a column declaring anything else
// is read as a string; a value that does not parse as an int is an
// error.
func csvConstant(value, typ string) (*Constant, error) {
	value = strings.TrimSpace(value)
	if typ == typeInt {
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, &Error{Msg: err.Error()}
		}
		return &Constant{Type: typeInt, Int: n}, nil
	}
	return &Constant{Type: typeString, Str: csv.Unquote(value)}, nil
}
