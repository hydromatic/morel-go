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

package morel_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hydromatic/morel-go/internal/shell"
)

// TestScripts runs every script under testdata/script and checks
// that it reproduces itself exactly.
func TestScripts(t *testing.T) {
	root := "testdata/script"
	var files []string
	err := filepath.WalkDir(root,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".smli") {
				files = append(files, path)
			}
			return nil
		})
	if err != nil || len(files) == 0 {
		t.Skipf("no scripts in %s", root)
	}
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		t.Run(rel, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			kernel := shell.NewKernel(rel)
			got, err := shell.RunScript(kernel, rel, string(data))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(data) {
				t.Errorf("not idempotent:%s",
					firstDiff(string(data), got))
			}
		})
	}
}

// firstDiff renders the first line where want and got differ.
func firstDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	for i := range max(len(wl), len(gl)) {
		w, g := "<eof>", "<eof>"
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return "\n line " + strconv.Itoa(i+1) +
				":\n want " + w + "\n  got " + g
		}
	}
	return " (lengths differ)"
}
