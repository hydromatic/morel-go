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
	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/types"
)

// Test hooks for unexported printing functions.

func PrettyBindingForTest(c Config, name string, v eval.Val,
	t types.Type,
) string {
	return c.prettyBinding(name, v, t)
}

func FormatRealForTest(f float32) string {
	return eval.FormatReal(f)
}

// Test hooks for the structLib consistency checks.

// StructLibFilesForTest returns the Morel source file (lib/*.sml) of
// each structLib.
func StructLibFilesForTest() []string {
	files := make([]string, 0, len(structLibs()))
	for _, l := range structLibs() {
		files = append(files, l.file)
	}
	return files
}

// UnimplementedMembersForTest boots a kernel and returns the
// qualified name of every structure member that fell through to
// the notImplemented placeholder — one for which
// values["Struct.member"] is absent, the same signal
// buildStructureRecords uses. It scans the boot snapshot, so
// structures bound by evaluation (scott) are out of scope.
func UnimplementedMembersForTest() []string {
	k := NewKernel("test")
	var missing []string
	for i := range k.bootBindings {
		b := &k.bootBindings[i]
		record, ok := b.Type.(*types.Record)
		if !ok {
			continue
		}
		for _, f := range record.Fields {
			qualified := b.Name + "." + f.Label
			if _, ok := k.bootValues[qualified]; !ok {
				missing = append(missing, qualified)
			}
		}
	}
	return missing
}

// UnimplementedStructLibMembersForTest boots a kernel and returns
// the qualified names of any structLib structure member that fell
// through to the notImplemented placeholder — that is, one for which
// values["Struct.member"] is absent, the same signal
// buildStructureRecords uses.
func UnimplementedStructLibMembersForTest() []string {
	k := NewKernel("test")
	var missing []string
	for _, l := range structLibs() {
		var record *types.Record
		for i := range k.bindings {
			if k.bindings[i].Name == l.name {
				record, _ = k.bindings[i].Type.(*types.Record)
				break
			}
		}
		if record == nil {
			missing = append(missing, l.name+" (not a structure)")
			continue
		}
		for _, f := range record.Fields {
			if _, ok := k.values[l.name+"."+f.Label]; !ok {
				missing = append(missing, l.name+"."+f.Label)
			}
		}
	}
	return missing
}
