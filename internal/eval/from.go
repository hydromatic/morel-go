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

package eval

// From returns code that evaluates a query: it scans a source
// collection, binding pat to each element, keeps the elements the
// where conditions accept, and collects the value of collect for
// each — a list or a bag, the same representation either way.
func From(source Code, pat Pat, wheres []Code, collect Code) Code {
	return &fromCode{
		source:  source,
		pat:     pat,
		wheres:  wheres,
		collect: collect,
	}
}

type fromCode struct {
	source  Code
	pat     Pat
	collect Code
	wheres  []Code
}

func (c *fromCode) Eval(f *Frame) (Val, error) {
	coll, err := c.source.Eval(f)
	if err != nil {
		return nil, err
	}
	elems, _ := coll.([]Val)
	out := []Val{}
	for _, elem := range elems {
		if !c.pat.Match(elem, f) {
			continue
		}
		keep, err := c.accept(f)
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		row, err := c.collect.Eval(f)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func (c *fromCode) Describe() string {
	return "from(" + c.collect.Describe() + ")"
}

// accept reports whether the current row passes every where
// condition.
func (c *fromCode) accept(f *Frame) (bool, error) {
	for _, w := range c.wheres {
		v, err := w.Eval(f)
		if err != nil {
			return false, err
		}
		if b, _ := v.(bool); !b {
			return false, nil
		}
	}
	return true, nil
}
