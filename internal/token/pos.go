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

package token

import "fmt"

// Pos is a source position; Line and Col are 1-based, counted in
// characters.
type Pos struct {
	Line int
	Col  int
}

// Span is a source range. End is exclusive: the position just
// after the last character.
type Span struct {
	Start Pos
	End   Pos
}

// String renders a span in SML-NJ form, e.g. "1.14-1.25", or
// "1.14" when the span is a single point.
func (s Span) String() string {
	if s.Start == s.End {
		return fmt.Sprintf("%d.%d", s.Start.Line, s.Start.Col)
	}
	return fmt.Sprintf("%d.%d-%d.%d",
		s.Start.Line, s.Start.Col, s.End.Line, s.End.Col)
}
