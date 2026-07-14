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

package shell_test

import (
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/sig"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/lib"
)

// TestRegistryAgainstSignatures validates the built-in registry
// against lib/*.sig: every qualified implementation must
// implement a value that its structure's signature declares.
func TestRegistryAgainstSignatures(t *testing.T) {
	sys := types.NewSystem()
	result, err := sig.Load(sys, lib.FS)
	if err != nil {
		t.Fatal(err)
	}
	structures := map[string]map[string]bool{}
	for _, b := range result.Bindings {
		record, ok := b.Type.(*types.Record)
		if !ok {
			continue
		}
		fields := map[string]bool{}
		for _, f := range record.Fields {
			fields[f.Label] = true
		}
		structures[b.Name] = fields
	}
	implemented := 0
	for name := range eval.Builtins {
		structure, member, qualified := strings.Cut(name, ".")
		if !qualified {
			continue
		}
		fields, ok := structures[structure]
		if !ok {
			t.Errorf("%s: no structure %q in lib/*.sig", name,
				structure)
			continue
		}
		if !fields[member] {
			t.Errorf("%s: structure %s has no member %q in "+
				"its signature", name, structure, member)
			continue
		}
		implemented++
	}
	if implemented == 0 {
		t.Error("no qualified built-ins found")
	}
}
