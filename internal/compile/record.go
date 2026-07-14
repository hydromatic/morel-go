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

package compile

import (
	"sort"
	"strconv"
	"strings"

	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/internal/unify"
)

// labelTerm is one field of a record term under construction.
type labelTerm struct {
	label string
	term  unify.Term
}

// recordTerm builds the term for a record with the given fields,
// which must be sorted by label. A record whose labels are the
// integers 1..n (and n is not 1) is a tuple; an empty record is
// unit.
func (r *typeResolver) recordTerm(fields []labelTerm) unify.Term {
	labels := make([]string, len(fields))
	terms := make([]unify.Term, len(fields))
	for i, f := range fields {
		labels[i] = f.label
		terms[i] = f.term
	}
	if contiguousIntegers(labels) && len(labels) != 1 {
		return r.tupleTerm(terms)
	}
	return unify.Apply(recordLabel(labels), terms...)
}

// tupleTerm builds the term for a tuple; an empty tuple is unit.
func (r *typeResolver) tupleTerm(terms []unify.Term) unify.Term {
	if len(terms) == 0 {
		return r.primTerm("unit")
	}
	return unify.Apply(tupleTyCon, terms...)
}

// contiguousIntegers reports whether the labels are "1", "2",
// ..., "n".
func contiguousIntegers(labels []string) bool {
	for i, label := range labels {
		if label != strconv.Itoa(i+1) {
			return false
		}
	}
	return true
}

// recordLabel builds the term operator for a record type with
// the given labels, e.g. "record:a:b". A label that contains ":"
// is quoted with backticks.
func recordLabel(labels []string) string {
	var b strings.Builder
	b.WriteString(recordTyCon)
	for _, label := range labels {
		b.WriteString(":")
		if strings.Contains(label, ":") {
			b.WriteString("`" + label + "`")
		} else {
			b.WriteString(label)
		}
	}
	return b.String()
}

// fieldList is the inverse of recordLabel: the field names of a
// record or tuple term, or nil if the term is neither.
func fieldList(s *unify.Sequence) []string {
	switch {
	case strings.HasPrefix(s.Op, recordTyCon+":"):
		return splitQuoted(s.Op)[1:]
	case s.Op == tupleTyCon:
		labels := make([]string, len(s.Terms))
		for i := range s.Terms {
			labels[i] = strconv.Itoa(i + 1)
		}
		return labels
	default:
		return nil
	}
}

// splitQuoted splits on ":", except within backtick quotes,
// which it strips.
func splitQuoted(s string) []string {
	var parts []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '`':
			quoted = !quoted
		case r == ':' && !quoted:
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(parts, b.String())
}

// lookupField returns the resolved type of a field of a record
// or tuple term, or nil if the term is not a record or has no
// such field.
func lookupField(t unify.Term, fieldName string,
	s *unify.Substitution,
) unify.Term {
	seq, ok := t.(*unify.Sequence)
	if !ok {
		return nil
	}
	for i, label := range fieldList(seq) {
		if label == fieldName {
			return s.Resolve(seq.Terms[i])
		}
	}
	return nil
}

// sortFields sorts record fields by label: numeric labels first,
// in numeric order, then names alphabetically.
func sortFields(fields []labelTerm) {
	sort.Slice(fields, func(i, j int) bool {
		return types.LabelLess(fields[i].label, fields[j].label)
	})
}
