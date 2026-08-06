<!--
{% comment %}
Licensed to Julian Hyde under one or more contributor license
agreements.  See the NOTICE file distributed with this work
for additional information regarding copyright ownership.
Julian Hyde licenses this file to you under the Apache
License, Version 2.0 (the "License"); you may not use this
file except in compliance with the License.  You may obtain a
copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
either express or implied.  See the License for the specific
language governing permissions and limitations under the
License.
{% endcomment %}
-->
# Morel Go HOWTO

This document describes how to do various things with Morel Go.

## How to make a release (for committers)

Morel Go is a Go module, so a release is a git tag. There is nothing
to upload: the module proxy fetches the tag, and `go install` works as
soon as the proxy has seen it.

Go's module proxy recognises only tags of the form `vX.Y.Z` for a
module rooted at the repository root. Morel Java tags `morel-0.8.0`
and Morel Rust tags `morel-rust-0.2.0`, but Morel Go must tag
`v0.8.0`.

Write release notes, and append them to [CHANGELOG.md](../CHANGELOG.md).

Update the version number in `internal/shell/sys.go` (the
`productVersion` constant, which does not carry the 'v'; the banner
supplies it), in [README](../README) and in
[README.md](../README.md), and the copyright date in
[NOTICE](../NOTICE).

Check that dependencies are tidy, and that the sandbox is clean:

```bash
go mod tidy
git clean -nx
```

Make sure that `fullMake` passes, and that the GitHub build is green
on Linux, macOS and Windows.

Tag and push:

```bash
git tag v0.8.0
git push origin v0.8.0
```

Tell the module proxy to index the new version:

```bash
GOPROXY=proxy.golang.org go list -m github.com/hydromatic/morel-go@v0.8.0
```

Check that the release is visible, and that a fresh install works:

```bash
go install github.com/hydromatic/morel-go/cmd/morel@v0.8.0
```

Verify the release by hand (see below), add the release notes to the
[github release list](https://github.com/hydromatic/morel-go/releases),
and announce the release.

## Cleaning up after a failed release attempt (for committers)

Deleting a tag is safe only if nothing has fetched it through the
module proxy. Once the proxy and the checksum database have seen a
version, its contents are pinned forever: republishing the same
version with different contents makes `go` report a checksum
mismatch. Release `vX.Y.Z+1` instead, and add a `retract` directive
to `go.mod` for the bad version.

If the tag has not escaped:

```bash
# Make sure that the tag you are about to generate does not already
# exist (due to a failed release attempt)
git tag

# If the tag exists, delete it locally and remotely
git tag -d vX.Y.Z
git push origin :refs/tags/vX.Y.Z

# Check whether there are modified files and if so, go back to the
# original git commit
git status
git reset --hard HEAD
```

## Manually verify a release (for committers)

A few shell behaviors involve an interactive terminal and are not
covered by the automated tests, so check them by hand before
announcing a release. Run the following against the release artifact,
or against a clean build (`go build ./cmd/morel`).

Start the shell, and confirm that it reports the release version:

```bash
$ ./morel
morel-go v0.8.0 (go1.26.5, darwin/arm64)
```

Execute a command, and confirm that the result is printed:

```
- "Hello, world!";
val it = "Hello, world!" : string
```

Quit the shell (type control+D), and confirm that it exits cleanly,
with status 0.

Confirm that a single expression evaluates, and that a script is read
from standard input:

```bash
$ ./morel -e "from i in [1,2,3] where i > 1"
val it = [2,3] : int list

$ echo '1 + 2;' | ./morel -
val it = 3 : int
```

Confirm that a script file runs. A `.smli` file is read in idempotent
mode, so the shell reproduces the script, expected output and all; the
output should be identical to the input.

```bash
$ ./morel testdata/script/simple.smli | diff - testdata/script/simple.smli
```

Confirm that a statement can span lines, and that the continuation
prompt is `= `:

```
- 1 +
= 2;
val it = 3 : int
```

Quit the shell (type control+D), and confirm that the command was
saved to the history file:

```bash
$ cat ~/.morel/history-go
```

The file should contain the commands you typed, one JSON record per
line, with a multi-line statement held as a single entry. If you have
not run the shell before, confirm that the `~/.morel` directory and
the `history-go` file were created.

The file name is not Morel Java's `~/.morel/history` or Morel Rust's
`~/.morel/history-rust`: the three history formats are not
compatible, so each implementation keeps its own.

Start the shell again, press the up-arrow key, and confirm that the
previous statement is recalled — a multi-line statement as a unit.
Execute another command, quit, and confirm that `~/.morel/history-go`
has grown: the new command is appended, and the earlier history is
preserved.
