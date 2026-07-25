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

package shell

import (
	"math"
	"os"
	"path/filepath"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/compile"
	"github.com/hydromatic/morel-go/internal/datalog"
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/parse"
	"github.com/hydromatic/morel-go/internal/pp"
)

// datalogBuiltins returns the Datalog structure's implementations
// for NewKernel to inject.
func (k *Kernel) datalogBuiltins() map[string]eval.Val {
	return map[string]eval.Val{
		"Datalog.execute":   eval.Fn(k.datalogExecute),
		"Datalog.translate": eval.Fn(k.datalogTranslate),
		"Datalog.validate":  eval.Fn(k.datalogValidate),
	}
}

// datalogError is the error an invalid program raises from the
// execute built-in; its text is the script harness's rendering of
// the uncaught DatalogException.
type datalogError struct {
	msg string
}

func (e *datalogError) Error() string {
	return "net.hydromatic.morel.datalog.DatalogException: " + e.msg
}

// datalogValidate is "Datalog.validate program": the compiled
// result type on success, the error message on failure.
func (k *Kernel) datalogValidate(arg eval.Val) (eval.Val, error) {
	program, _ := arg.(string)
	compiled, _, err := k.datalogCompile(program)
	if err != nil {
		// validate reports failure as its result string.
		return err.Error(), nil //nolint:nilerr
	}
	bind := compiled.Binds[len(compiled.Binds)-1]
	doc := pp.Flatten(k.config.typeDoc(bind.Pat.T))
	return pp.Render(math.MaxInt32, doc), nil
}

// datalogTranslate is "Datalog.translate program": SOME source if
// the program compiles; if only the Morel compilation of the
// translation fails, SOME of an ERROR report; NONE if the Datalog
// phases themselves fail.
func (k *Kernel) datalogTranslate(arg eval.Val) (eval.Val, error) {
	program, _ := arg.(string)
	_, source, err := k.datalogCompile(program)
	if err == nil {
		return eval.SomeVal(source), nil
	}
	source, err2 := k.datalogTranslateOnly(program)
	if err2 != nil {
		// translate reports failure as NONE.
		return eval.NoneVal, nil //nolint:nilerr
	}
	return eval.SomeVal("ERROR: " + err.Error() +
		"\n\nGenerated:\n" + source), nil
}

// datalogExecute is "Datalog.execute program": evaluates the
// translation and wraps the result, with its type, as a variant.
// An invalid program raises.
func (k *Kernel) datalogExecute(arg eval.Val) (eval.Val, error) {
	program, _ := arg.(string)
	compiled, source, err := k.datalogCompile(program)
	if err != nil {
		return nil, &datalogError{msg: err.Error()}
	}
	frame := eval.NewFrame(compiled.Slots)
	_, err = compiled.Code.Eval(frame)
	if err != nil {
		return nil, &datalogError{
			msg: "Error executing Morel translation: " +
				err.Error() + "\nGenerated Morel code:\n" + source,
		}
	}
	bind := compiled.Binds[len(compiled.Binds)-1]
	return eval.Variant{
		Type:  bind.Pat.T,
		Value: frame.Slots[bind.Slot],
	}, nil
}

// datalogCompile runs the whole pipeline — parse, load inputs,
// analyze, translate, then compile the emitted Morel against the
// boot environment (user bindings are not visible) — returning
// the compiled statement and the emitted source.
func (k *Kernel) datalogCompile(
	program string,
) (*compile.Compiled, string, error) {
	source, err := k.datalogTranslateOnly(program)
	if err != nil {
		return nil, "", err
	}
	compiled, err := k.compileIsolated(source)
	if err != nil {
		return nil, "", &datalog.Error{
			Msg: "Compilation error: " + err.Error(),
		}
	}
	return compiled, source, nil
}

// datalogTranslateOnly runs the Datalog phases alone: parse, load
// inputs, analyze, translate.
func (k *Kernel) datalogTranslateOnly(
	program string,
) (string, error) {
	prog, err := datalog.Parse(program)
	if err != nil {
		return "", &datalog.Error{
			Msg: "Parse error: " + err.Error(),
		}
	}
	err = k.datalogLoadInputs(prog)
	if err != nil {
		return "", &datalog.Error{
			Msg: "Compilation error: " + err.Error(),
		}
	}
	err = datalog.Analyze(prog)
	if err != nil {
		return "", &datalog.Error{
			Msg: "Compilation error: " + err.Error(),
		}
	}
	return datalog.Translate(prog), nil
}

// datalogLoadInputs reads each .input directive's CSV, resolved
// against the "directory" property, and appends its rows as
// facts. An input for an undeclared relation is left for the
// analyzer to report.
func (k *Kernel) datalogLoadInputs(prog *datalog.Program) error {
	for _, in := range prog.Inputs {
		decl := prog.Decls[in.Relation]
		if decl == nil {
			continue
		}
		name := in.EffectiveFileName()
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(k.config.Directory, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			abs, _ := filepath.Abs(path)
			return &datalog.Error{
				Msg: "Input file not found: " + name +
					" (resolved to " + abs + ")",
			}
		}
		facts, err := datalog.CSVFacts(decl, string(data))
		if err != nil {
			return err
		}
		prog.Statements = append(prog.Statements, facts...)
	}
	return nil
}

// compileIsolated compiles one Morel expression against the boot
// bindings and values, so the translation of a Datalog program
// sees only built-ins.
func (k *Kernel) compileIsolated(
	source string,
) (*compile.Compiled, error) {
	n, err := parse.Stmt("datalog", source+";")
	if err != nil {
		return nil, err
	}
	e, ok := n.(ast.Expr)
	if !ok {
		return nil, &datalog.Error{Msg: "not an expression"}
	}
	decl := compile.ItValDecl(e)
	resolved, err := compile.Deduce(k.sys, k.bootBindings, decl)
	if err != nil {
		return nil, err
	}
	coreDecl, err := compile.Resolve(resolved)
	if err != nil {
		return nil, err
	}
	return compile.Statement(coreDecl, k.bootValues, k.sys)
}
