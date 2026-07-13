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
propagate back — do not grow a go-only test fork. (Exception:
`parse.smli`, temporary parser scaffolding; see `plan.md`
task 11.)

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
