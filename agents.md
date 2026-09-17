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
# Development notes for Claude

## Commit discipline

**Every commit must pass `fullMake`.** Run `fullMake --no-clean`
from the repository root and confirm it passes before you commit —
including each commit of a multi-commit change, since commits are
cherry-picked and squashed from work branches onto `main` and must
be green independently. For morel-go, `fullMake` builds
(`go build`), lints (`golangci-lint run`, which includes vet and
formatting checks), and runs the tests (`go test`). Never commit
if it fails; fix the code until it is green.

**Never mix planning changes with code changes in one commit.**
Planning changes (`plan.md`) and code changes (everything else,
including `agents.md`) go in separate commits, so that either kind
can be cherry-picked onto `main` without the other. If a piece of
work updates both, make two commits.

## Repositories

Morel has implementations in Go, Rust, and Java. The repositories
are [morel-go](https://github.com/hydromatic/morel-go),
[morel-rust](https://github.com/hydromatic/morel-rust), and
[morel-java](https://github.com/hydromatic/morel).

morel-java is the reference implementation; morel-go and morel-rust
are ports of it. A common task is to propagate a change from one
repository into another — usually from morel-java (or morel-rust)
into morel-go.

> morel-go is early-stage, so some tooling referenced below (the
> script harness, `etc/check-convergence.py`) is described by
> `plan.md` and will appear as the port matures.

## Two forms of script

A script in `testdata/script/` comes in one of two forms, and the
difference is where its expected output lives.

A **`.smli` script is idempotent**: it carries its own expected
output, on `> `-prefixed lines after each statement, and running it
reproduces the file. This is the form almost everything uses.

A **`.sml` script carries no expected output**; its transcript is
the companion `.sml.out` file — every input line echoed, and after
each statement the output it produced, unprefixed and followed by a
blank line. The form earns its keep for a script whose output you
would not want interleaved with it: one that `use`s another file,
where the whole of the inner file's transcript lands in the middle
of the outer one.

**Two things pick the form**, as they do in morel-java. First the
script harness is engaged, by `--idempotent` or by a first file
ending `.smli`; without it every source is an ordinary program,
streamed, so `morel prog.sml` runs a program, which is what `.sml`
ordinarily means. Then, within the harness, the extension picks:
`.smli` is rewritten, anything else has its transcript written, and
standard input is read as `.smli` would be. `Args.FormOf` is the one
place that decides, and `TestScripts` — one walker over both
extensions — reaches the same two forms directly.

Both forms count towards convergence. `etc/check-convergence.py`
measures `.smli`, `.sml` and `.sml.out` alike, so a divergence in a
transcript is as visible as one in a script.

## Growing the test corpus

The `.smli` corpus (in `testdata/script/`) is grown
component-by-component, not imported whole. When implementing a
feature, pull in the hunks of `.smli` that morel-java added for
that feature and that have not changed significantly since. Pulled
hunks are verbatim from java's present-day files — never adapted
for Go.

Never disable a section inside a `.smli` file. morel-go does not
use `set("mode","validate")` brackets or `(* ... *)` disablement
(a morel-rust mistake — it was very hard to tell which sections
were disabled, and every edit is divergence from java). A section
exists in a morel-go `.smli` file only when it passes; what is
missing is exactly what the divergence report shows. Sole
exception: `Sys.plan` output is matched best-effort, and a plan
line that is infeasible to match may be commented out.

## Propagation process

Once a `.smli` file has caught up with morel-java, changes to it
are propagated commit-by-commit.

### Reading the source change

Read the commit message, the code changes, and especially the test
changes in the java repository's
`src/test/resources/script/*.smli`.

### Implementing the feature

Move every changed `.smli` section from morel-java verbatim — all
sections, always; never adapt, trim, or skip a section because the
implementation is hard. (Dropping and adapting sections were
costly mistakes in morel-rust.) Then implement the feature in Go.

### Verifying

Run `fullMake --no-clean` and confirm it passes. The gates, all of
which must pass before committing:

- `fullMake` (build, lint, tests);
- `etc/check-convergence.py HEAD` — per-file divergence from
  morel-java may never increase; a propagation or corpus-growth
  commit should show it decreasing.

New tests originate in morel-java: add them there first, then
propagate back — do not grow a go-only test fork. One go-local
script is an exception. The corpus regeneration tooling
(`pull-passing --apply`, `era_trim`, whole-file regens) leaves it
alone, but only because of how it picks its files: it works on the
*shared* ones, `go_files & java_files`, so a go-local script is safe
exactly as long as morel-java has no file of that name.

**So the name is the protection, and it is not reserved.** If
morel-java creates a file that collides, the script is no longer
go-local as far as the tooling can tell: `pull-passing --apply`
starts from morel-java's copy and only ever deletes from it, so the
go-only content is not merged — it is dropped, quietly, in a run
that looks like an ordinary pull. Give a go-local script a name
morel-java will not want. That is what happened on 2026-09-15, when
morel-java added a `parse.smli` of its own; the scaffolding that
collided with it has since been upstreamed, which is the better end
for a go-local script than a rename.

The one:

- `backswing.smli`, regression tests for bugs fixed in morel-go
  that morel-java's corpus does not yet pin. Each entry names the
  bug and the upstream `.smli` file its statements belong in; a
  future "backswing" task in morel-java adopts them there, after
  which a pull returns them and they are deleted from here (see
  `plan.md` task R45).

### Commit message

Use the original morel-java commit summary as the first line of the
commit message. Append a blank line and then a propagation line
that cites the morel-java issue and commit SHA:

```
Join (hydromatic/morel#72)

Add clauses to `from` to support inner joins. We continue to
allow comma joins, but only up until the first step (`where`,
`join`, `group`, `yield` or `order` keyword). After that,
commas would introduce ambiguity when combined with the
commas in `group` or `compute`.

We will add outer joins (`left`, `right`, `full` keywords)
in a later commit.

Propagates hydromatic/morel#72 commit ab102172
```

If a morel-java commit uses the old `[MOREL-NNN]` format, convert
it to the new format `hydromatic/morel#NNN`. For example,
`[MOREL-72] Join` becomes `Join (hydromatic/morel#72)`.

## Regular development

Regular features (originating in morel-go) use a commit message
that references the morel-go issue:

```
Add `banner`, `productName`, `productVersion` properties (#30)

Add three new read-only properties to the Sys structure.

Fixes #30
```

## Quick experiments

To run a single Morel expression from the shell, pass `-e` (or
`--eval`, or `--eval=EXPR`) to the binary; the result is printed
and the process exits. Useful when reproducing a bug from a
one-liner without needing a script file:

```
$ go build ./cmd/morel
$ ./morel -e '1 + 2'
val it = 3 : int
$ ./morel --eval='from x in [1,2,3] yield x * 2'
val it = [2,4,6] : int list
```
