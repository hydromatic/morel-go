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
)

// CSVFacts converts CSV content — a "name:type,..." header line
// followed by one row per line, with strings optionally
// single-quoted — into facts for a declared relation, matching
// columns to the declaration's parameters by name, as the
// reference implementation's .input loading does. The facts keep
// the file's row order.
func CSVFacts(decl *Declaration, content string) ([]Statement, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, errorf("Input file for relation '%s' is empty",
			decl.Name)
	}
	type column struct {
		index int
		typ   string
	}
	columns := map[string]column{}
	for i, field := range strings.Split(lines[0], ",") {
		name, typ, _ := strings.Cut(strings.TrimSpace(field), ":")
		columns[name] = column{index: i, typ: typ}
	}
	selected := make([]column, len(decl.Params))
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
		values := strings.Split(line, ",")
		args := make([]Term, len(selected))
		for i, col := range selected {
			if col.index >= len(values) {
				return nil, errorf(
					"Row %q in input file for relation '%s' is "+
						"too short", line, decl.Name,
				)
			}
			c, err := csvConstant(values[col.index], col.typ)
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

// csvConstant parses one CSV value by its column type; strings
// shed surrounding single quotes.
func csvConstant(value, typ string) (*Constant, error) {
	value = strings.TrimSpace(value)
	if typ == typeInt {
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, &Error{Msg: err.Error()}
		}
		return &Constant{Type: typeInt, Int: n}, nil
	}
	if len(value) >= 2 && value[0] == '\'' &&
		value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}
	return &Constant{Type: typeString, Str: value}, nil
}
