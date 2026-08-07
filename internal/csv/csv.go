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

// Package csv reads the comma-separated dialect that Morel's data
// files are written in: a header line that names and types the
// columns, then one row per line.
//
// The dialect is deliberately small. Commas separate, with no
// escaping, so a value cannot contain one; a string may be wrapped
// in single quotes, which is how a value containing a space is
// usually written.
//
// Two readers share it: the "file" value, which turns a data file
// into a list of records, and Datalog's ".input" directive, which
// turns one into facts. They differ in what they make of the
// columns -- which types they know, and whether a malformed row is
// an error or a zero -- but not in how they read them, and that is
// what lives here.
package csv

import "strings"

// Column is one column of a header: the name it gives the column,
// the type it declares for it (empty if it declares none), and the
// column's position in a row.
type Column struct {
	Name  string
	Type  string
	Index int
}

// ParseHeader parses a header line -- "name[:type]" fields
// separated by commas, as in "deptno:int,dname:string,loc" -- into
// one Column per field, in the order they are written. A field
// that declares no type gives a Column whose Type is empty; each
// reader decides what that means, and what to make of a type it
// does not know. Space around a name or a type is not part of it.
// A line that is empty, or all space, has no columns.
func ParseHeader(line string) []Column {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	fields := Cells(line)
	columns := make([]Column, len(fields))
	for i, field := range fields {
		name, typeName, _ := strings.Cut(field, ":")
		columns[i] = Column{
			Name:  strings.TrimSpace(name),
			Type:  strings.TrimSpace(typeName),
			Index: i,
		}
	}
	return columns
}

// Cells splits a row into its comma-separated cells. There is no
// escaping: every comma separates, so the count of cells is one
// more than the count of commas.
func Cells(line string) []string {
	return strings.Split(line, ",")
}

// Unquote strips one pair of surrounding single quotes, so that
// "'SMITH'" reads as "SMITH" and "'Cheap, cheerful'" would read as
// written -- were a value able to contain a comma. A value that is
// not quoted is returned unchanged.
func Unquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

// End csv.go
