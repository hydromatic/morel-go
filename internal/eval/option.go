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

// The option values that other built-ins (such as Bool.fromString)
// return, and the Option structure's three native members. Its
// other members are derived in Morel (lib/option.sml); getOpt,
// isSome, and valOf stay native because they are also top-level
// built-ins.

// optionDatatype names the datatype of option values.
const optionDatatype = "option"

// NoneVal is the value NONE.
var NoneVal = Con{Datatype: optionDatatype, Name: "NONE"}

// noneVal is NoneVal under its historical package-local name.
var noneVal Val = NoneVal

// SomeVal returns the value "SOME v".
func SomeVal(v Val) Val {
	return someVal(v)
}

// someVal returns the value "SOME v".
func someVal(v Val) Val {
	return Con{
		Datatype: optionDatatype,
		Name:     "SOME",
		Ordinal:  1,
		Arg:      v,
	}
}

// asOption reports whether an option value is SOME, and its
// argument.
func asOption(v Val) (Val, bool) {
	con, ok := v.(Con)
	if !ok || con.Datatype != optionDatatype {
		panic(fmt.Sprintf("expected option, got %T", v))
	}
	return con.Arg, con.Ordinal == 1
}

// getOptFn is "getOpt (opt, d)": the content of opt, or d.
func getOptFn(arg Val) (Val, error) {
	opt, dflt := asPair(arg)
	if v, isSome := asOption(opt); isSome {
		return v, nil
	}
	return dflt, nil
}

// isSomeFn is "isSome opt".
func isSomeFn(arg Val) (Val, error) {
	_, isSome := asOption(arg)
	return isSome, nil
}

// valOfFn is "valOf opt"; valOf NONE raises Option.
func valOfFn(arg Val) (Val, error) {
	v, isSome := asOption(arg)
	if !isSome {
		return nil, &MorelError{Exn: "Option"}
	}
	return v, nil
}
