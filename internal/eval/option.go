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

// The Option structure, and the option values that other
// built-ins (such as Bool.fromString) return.

// optionDatatype names the datatype of option values.
const optionDatatype = "option"

// noneVal is the value NONE.
var noneVal = Con{Datatype: optionDatatype, Name: "NONE"}

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

// optionFilterFn is "Option.filter f a": SOME a if f a is true,
// otherwise NONE.
func optionFilterFn(f Val) (Val, error) {
	return Fn(func(a Val) (Val, error) {
		keep, err := ApplyVal(f, a)
		if err != nil {
			return nil, err
		}
		if asBool(keep) {
			return someVal(a), nil
		}
		return noneVal, nil
	}), nil
}

// optionJoinFn is "Option.join opt": flattens an option option.
func optionJoinFn(arg Val) (Val, error) {
	if v, isSome := asOption(arg); isSome {
		return v, nil
	}
	return noneVal, nil
}

// optionAppFn is "Option.app f opt": applies f to the content
// for its effect.
func optionAppFn(f Val) (Val, error) {
	return Fn(func(opt Val) (Val, error) {
		if v, isSome := asOption(opt); isSome {
			_, err := ApplyVal(f, v)
			if err != nil {
				return nil, err
			}
		}
		return unitVal, nil
	}), nil
}

// optionMapFn is "Option.map f opt".
func optionMapFn(f Val) (Val, error) {
	return Fn(func(opt Val) (Val, error) {
		v, isSome := asOption(opt)
		if !isSome {
			return noneVal, nil
		}
		r, err := ApplyVal(f, v)
		if err != nil {
			return nil, err
		}
		return someVal(r), nil
	}), nil
}

// optionMapPartialFn is "Option.mapPartial f opt".
func optionMapPartialFn(f Val) (Val, error) {
	return Fn(func(opt Val) (Val, error) {
		v, isSome := asOption(opt)
		if !isSome {
			return noneVal, nil
		}
		return ApplyVal(f, v)
	}), nil
}

// optionComposeFn is "Option.compose (f, g) a": applies g, and f
// on its SOME content.
func optionComposeFn(arg Val) (Val, error) {
	f, g := asPair(arg)
	return Fn(func(a Val) (Val, error) {
		opt, err := ApplyVal(g, a)
		if err != nil {
			return nil, err
		}
		v, isSome := asOption(opt)
		if !isSome {
			return noneVal, nil
		}
		r, err := ApplyVal(f, v)
		if err != nil {
			return nil, err
		}
		return someVal(r), nil
	}), nil
}

// optionComposePartialFn is "Option.composePartial (f, g) a".
func optionComposePartialFn(arg Val) (Val, error) {
	f, g := asPair(arg)
	return Fn(func(a Val) (Val, error) {
		opt, err := ApplyVal(g, a)
		if err != nil {
			return nil, err
		}
		v, isSome := asOption(opt)
		if !isSome {
			return noneVal, nil
		}
		return ApplyVal(f, v)
	}), nil
}
