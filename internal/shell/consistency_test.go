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
	"io/fs"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/eval"
	"github.com/hydromatic/morel-go/internal/shell"
	"github.com/hydromatic/morel-go/internal/sig"
	"github.com/hydromatic/morel-go/internal/types"
	"github.com/hydromatic/morel-go/lib"
)

// TestStructLibSourcesClaimed checks that every embedded library
// Morel source (lib/*.sml) is claimed by exactly one structLib, and
// that each structLib names a source that exists.
func TestStructLibSourcesClaimed(t *testing.T) {
	files, err := fs.Glob(lib.FS, "*.sml")
	if err != nil {
		t.Fatal(err)
	}
	claimed := map[string]bool{}
	for _, f := range shell.StructLibFilesForTest() {
		if claimed[f] {
			t.Errorf("lib/%s is claimed by more than one structLib", f)
		}
		claimed[f] = true
		_, err = lib.FS.ReadFile(f)
		if err != nil {
			t.Errorf("structLib source %q: %v", f, err)
		}
	}
	for _, f := range files {
		if !claimed[f] {
			t.Errorf("lib/%s is not claimed by any structLib", f)
		}
	}
}

// TestStructLibStructuresComplete checks that every member of a
// structLib structure has an implementation — none falls through to
// the notImplemented placeholder.
func TestStructLibStructuresComplete(t *testing.T) {
	missing := shell.UnimplementedStructLibMembersForTest()
	if len(missing) > 0 {
		t.Errorf("structLib members not implemented: %v", missing)
	}
}

// TestUnimplementedMembers pins the structure members that still
// fall through to the notImplemented placeholder (calling one
// reports "not implemented: Struct.member"). A new signature
// member cannot land without either an implementation or an entry
// here; an implemented member must be removed from the list.
func TestUnimplementedMembers(t *testing.T) {
	want := []string{
		"Interact.use",
		"Interact.useSilently",
		"Sys.clearEnv",
		"Sys.colorSchemes",
		"Sys.deduceColorScheme",
		"Sys.file",
	}
	got := shell.UnimplementedMembersForTest()
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("unimplemented members changed:\n got %v\nwant %v",
			got, want)
	}
}

// TestBuiltinsHaveSignatures checks that every qualified built-in
// "Struct.member" names a member declared in Struct's signature, so
// a native registration cannot silently misspell or outlive its
// signature.
func TestBuiltinsHaveSignatures(t *testing.T) {
	sys := types.NewSystem()
	result, err := sig.Load(sys, lib.FS)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]map[string]bool{}
	for _, b := range result.Bindings {
		record, ok := b.Type.(*types.Record)
		if !ok {
			continue
		}
		labels := map[string]bool{}
		for _, f := range record.Fields {
			labels[f.Label] = true
		}
		fields[b.Name] = labels
	}
	for key := range eval.Builtins {
		name, member, ok := strings.Cut(key, ".")
		if !ok {
			continue // a top-level built-in, not a structure member
		}
		labels, ok := fields[name]
		if !ok {
			t.Errorf("built-in %q: no signature for structure %s", key, name)
			continue
		}
		if !labels[member] {
			t.Errorf("built-in %q: no member %s in the %s signature",
				key, member, name)
		}
	}
}
