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

import "fmt"

// First-class exception values: the constructors of the exn
// datatype, the raise primitive, and General.exnName/exnMessage.

// exnNameFn is "exnName ex": the exception constructor's name.
func exnNameFn(arg Val) (Val, error) {
	con, ok := arg.(Con)
	if !ok {
		return nil, fmt.Errorf("exnName: not an exception: %v", arg)
	}
	return con.Name, nil
}

// exnMessageFn is "exnMessage ex".
func exnMessageFn(arg Val) (Val, error) {
	con, ok := arg.(Con)
	if !ok {
		return nil, fmt.Errorf(
			"exnMessage: not an exception: %v", arg,
		)
	}
	return exnMessage(con), nil
}

// exnMessage is an exception value's message: its registered
// description; failing that, "Name: payload" for a constructor
// with a payload; failing that, its bare name. The
// uncaught-exception report brackets the same text.
func exnMessage(con Con) string {
	if d, ok := exnDescriptions[con.Name]; ok {
		return d
	}
	if con.Arg != nil {
		return fmt.Sprintf("%s: %v", con.Name, con.Arg)
	}
	return con.Name
}

// RaiseFn is the runtime behind the "raise" expression: it turns
// an exn value into the Morel error the shell reports as an
// uncaught exception. The application site's span is stamped on
// the error by the evaluator, so the report's "raised at" points
// at the raise expression.
func RaiseFn(arg Val) (Val, error) {
	con, ok := arg.(Con)
	if !ok {
		return nil, fmt.Errorf("raise: not an exception: %v", arg)
	}
	return nil, &MorelError{Exn: con.Name, ExnValue: con}
}
