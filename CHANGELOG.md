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
# Morel release history and change log

For a full list of releases, see
<a href="https://github.com/hydromatic/morel-go/releases">GitHub</a>.

<!--
## <a id="v0.x.0" href="https://github.com/hydromatic/morel-go/releases/tag/v0.x.0">v0.x.0</a> / xxxx-xx-xx

Release v0.x.0 ...

Contributors:

### Features

### Bug-fixes and internal improvements

### Build and tests

### Component upgrades

### Site and documentation

* Release v0.x.0 (#xxx)

-->

## <a id="v0.8.0" href="https://github.com/hydromatic/morel-go/releases/tag/v0.8.0">v0.8.0</a> / 2026-08-04

Initial release.

Go version numbers start with 'v'. The number starts at v0.8.0,
rather than v0.1.0, so that it matches the release of
[Morel Java](https://github.com/hydromatic/morel) that this release is
compatible with.

There are too many changes to list here. Our goal is to be compatible
with the Morel language as implemented by Morel Java, and our strategy
is to copy the `.smli` test scripts from that project and get them to
pass. Compatibility is therefore measured by how much of Morel Java's
script corpus Morel Go reproduces line for line; at this release, it
is about 87%.

Key features:
 * The parser is complete, apart from attributes and quoted
   identifiers; it parses 94% of the statements in Morel Java's
   corpus, and almost all of the rest are `:t` test-harness
   directives.
 * Type resolution (via the Hindley-Milner algorithm and unification)
   and evaluation are complete for expressions, function
   declarations, `datatype` and `type` declarations, and the built-in
   types including `list`, `bag`, `vector`, `option` and `either`.
 * The relational extensions work: `from`, `join` (including `left`,
   `right` and `full` outer joins), `where`, `group`, `compute`,
   `order`, `skip`, `take`, `yield`, `yieldAll` and `into`.
 * Datalog is supported.
 * There are 460 built-in values in 25 structures.
   [`Bool`](https://smlfamily.github.io/Basis/bool.html),
   [`Char`](https://smlfamily.github.io/Basis/char.html),
   [`Date`](https://smlfamily.github.io/Basis/date.html),
   [`Fn`](https://smlfamily.github.io/Basis/fn.html),
   [`General`](https://smlfamily.github.io/Basis/general.html),
   [`Int`](https://smlfamily.github.io/Basis/integer.html),
   [`List`](https://smlfamily.github.io/Basis/list.html),
   [`ListPair`](https://smlfamily.github.io/Basis/list-pair.html),
   [`Math`](https://smlfamily.github.io/Basis/math.html),
   [`Option`](https://smlfamily.github.io/Basis/option.html),
   [`Real`](https://smlfamily.github.io/Basis/real.html),
   [`String`](https://smlfamily.github.io/Basis/string.html),
   [`StringCvt`](https://smlfamily.github.io/Basis/string-cvt.html),
   [`Time`](https://smlfamily.github.io/Basis/time.html),
   [`Vector`](https://smlfamily.github.io/Basis/vector.html) and
   [`Word`](https://smlfamily.github.io/Basis/word.html)
   are based on the Standard ML Basis Library;
   [`Either`](https://github.com/SMLFamily/BasisLibrary/wiki/2015-002-Addition-of-Either-module)
   is a proposed extension to the Standard ML Basis Library; and
   `Bag`,
   `Datalog`,
   `Interact`,
   `PP`,
   `Range`,
   `Relational`,
   `Sys` and
   `Variant`
   are Morel-specific.
 * The shell runs script files or standard input, evaluates a single
   expression given with `-e`, and checks a script against its
   expected output in `.smli` (idempotent) format.

Among the remaining tasks to achieve parity with Morel Java are
`exception` declarations and `handle`; `structure`, `signature` and
`open`; `local`; `while`; references; user-defined operators;
overloaded functions (`over` and `val inst`, which parse but do not
compile); and external data, from the file system or from a database
via ODBC or JDBC.

The shell has no command-line editing and no history: there is no
up-arrow recall, and nothing is saved between sessions.

Contributors:
Julian Hyde

### Features

* Outer joins and the `?.` safe-navigation operator
  ([morel#75](https://github.com/hydromatic/morel/issues/75),
  [morel#378](https://github.com/hydromatic/morel/issues/378))
* Signature declarations parse and echo
  ([morel#315](https://github.com/hydromatic/morel/issues/315))
* `typeof` annotations: the type of an expression
  ([morel#291](https://github.com/hydromatic/morel/issues/291))
* Scalar scans: `pat = exp` binds the value of `exp` per row
  ([morel#11](https://github.com/hydromatic/morel/issues/11))
* `Range.contains` on continuous and discrete sets
  ([morel#338](https://github.com/hydromatic/morel/issues/338), continued)
* Dispatch a postfix method by its identifier receiver type
  ([morel#346](https://github.com/hydromatic/morel/issues/346), continued)
* Zero-scan queries iterate one empty row; `current`-outside-query error
  ([morel#17](https://github.com/hydromatic/morel/issues/17))
* `yieldAll` step: flatten a collection-valued expression
  ([morel#257](https://github.com/hydromatic/morel/issues/257))
* Compute steps: standalone `compute`, elements, aggregates in expressions
  ([morel#304](https://github.com/hydromatic/morel/issues/304))
* `Interact.use` and `useSilently` execute a file in the session
  ([morel#86](https://github.com/hydromatic/morel/issues/86))
* First-class exceptions: the `exn` datatype, `raise`, `exnName`,
  `exnMessage` ([morel#364](https://github.com/hydromatic/morel/issues/364))
* Datalog ([morel#323](https://github.com/hydromatic/morel/issues/323))
* Implement `Sys.planEx`: re-plan a statement to a numbered pass
  ([morel#329](https://github.com/hydromatic/morel/issues/329))
* Invert recursive predicates into fixed-point iterations
  ([morel#217](https://github.com/hydromatic/morel/issues/217), continued)
* Invert case predicates; the prefix generator; tuple substitution
  ([morel#341](https://github.com/hydromatic/morel/issues/341))
* Invert quantified predicates into projected scans
  ([morel#217](https://github.com/hydromatic/morel/issues/217), continued)
* Invert disjunctions into unions of generators
  ([morel#217](https://github.com/hydromatic/morel/issues/217), continued)
* Invert bound predicates into range scans
  ([morel#217](https://github.com/hydromatic/morel/issues/217), continued)
* Invert membership predicates into collection scans
  ([morel#217](https://github.com/hydromatic/morel/issues/217), continued)
* Ground unbounded query variables by inverting predicates
  ([morel#217](https://github.com/hydromatic/morel/issues/217), continued)
* Extents: a sourceless scan iterates all values of its type
  ([morel#217](https://github.com/hydromatic/morel/issues/217),
  [morel#202](https://github.com/hydromatic/morel/issues/202))
* `Relational.iterate`: semi-naive transitive-closure builtin
* Enable range enumeration and membership over non-discrete types
* Range-list syntax: `[1 .. 5]`, `[0 ..^ 10, 20]`
  ([morel#372](https://github.com/hydromatic/morel/issues/372))
* Record update: `{e with lab = v, ...}`
  ([morel#249](https://github.com/hydromatic/morel/issues/249))
* Implement the `PP` pretty-printer structure
  ([morel#398](https://github.com/hydromatic/morel/issues/398),
  [morel#339](https://github.com/hydromatic/morel/issues/339))
* Warn on nonexhaustive and redundant matches
  ([morel#55](https://github.com/hydromatic/morel/issues/55))
* Implement `type_string`
  ([morel#406](https://github.com/hydromatic/morel/issues/406))
* Implement transparent type aliases
  ([morel#285](https://github.com/hydromatic/morel/issues/285))
* Evaluate the empty record as unit
  ([morel#14](https://github.com/hydromatic/morel/issues/14))
* Aggregates adapt to `list` or `bag`; zero-field `group` rule
  ([morel#271](https://github.com/hydromatic/morel/issues/271),
  [morel#328](https://github.com/hydromatic/morel/issues/328))
* Match layered (`as`) patterns
  ([morel#103](https://github.com/hydromatic/morel/issues/103))
* Add the global `scott` dataset
* Type local `over`/`val inst` overloads
  ([morel#237](https://github.com/hydromatic/morel/issues/237), continued)
* Overload `elem`/`notelem`/`only` on lists and bags
* Parse `over` and `val inst` declarations
  ([morel#237](https://github.com/hydromatic/morel/issues/237))
* Implement `Relational.compare`, `compare`, and descending order
  ([morel#282](https://github.com/hydromatic/morel/issues/282))
* Normalize `datatype` echo and reject unbound type variables
  ([morel#356](https://github.com/hydromatic/morel/issues/356))
* Honor a `group` binder
  ([morel#387](https://github.com/hydromatic/morel/issues/387))
* Evaluate mid-query `yield` and `yield` binders
  ([morel#262](https://github.com/hydromatic/morel/issues/262),
  [morel#52](https://github.com/hydromatic/morel/issues/52))
* Evaluate `current`, `ordinal`, and `unorder`
  ([morel#265](https://github.com/hydromatic/morel/issues/265), continued;
  [morel#276](https://github.com/hydromatic/morel/issues/276);
  [morel#277](https://github.com/hydromatic/morel/issues/277))
* Evaluate `exists`, `forall`, `require`, `into`, and `through`
  ([morel#241](https://github.com/hydromatic/morel/issues/241), continued;
  [morel#171](https://github.com/hydromatic/morel/issues/171), continued)
* Evaluate `group` and `compute`
  ([morel#6](https://github.com/hydromatic/morel/issues/6), continued;
  [morel#69](https://github.com/hydromatic/morel/issues/69), continued)
* Evaluate `union`, `intersect`, and `except`
  ([morel#30](https://github.com/hydromatic/morel/issues/30))
* Evaluate `order`, `distinct`, `skip`, and `take`
  ([morel#20](https://github.com/hydromatic/morel/issues/20),
  [morel#231](https://github.com/hydromatic/morel/issues/231),
  [morel#204](https://github.com/hydromatic/morel/issues/204))
* Evaluate joins: comma scans, `join ... on`, dependent scans
  ([morel#216](https://github.com/hydromatic/morel/issues/216), continued;
  [morel#275](https://github.com/hydromatic/morel/issues/275))
* Derive a punned record field label from a selector
  ([morel#10](https://github.com/hydromatic/morel/issues/10))
* Evaluate open record patterns `{a, ...}`
  ([morel#36](https://github.com/hydromatic/morel/issues/36))
* Validate step placement in queries
* Evaluate `from` queries: scan, `where`, `yield`
* Render bags, options, and nested fields in tabular output mode
  ([morel#376](https://github.com/hydromatic/morel/issues/376),
  [morel#382](https://github.com/hydromatic/morel/issues/382))
* Type `exists`, `forall`, `require`, `into`, and `through`
  ([morel#241](https://github.com/hydromatic/morel/issues/241),
  [morel#171](https://github.com/hydromatic/morel/issues/171))
* Type `group` and `compute` steps with aggregates
  ([morel#6](https://github.com/hydromatic/morel/issues/6),
  [morel#69](https://github.com/hydromatic/morel/issues/69))
* Type query passthrough and forcer steps
* Type `join ... on` conditions
  ([morel#216](https://github.com/hydromatic/morel/issues/216),
  [morel#72](https://github.com/hydromatic/morel/issues/72))
* Expose a non-record `yield`'s implicit-label field
* Bind `current` in query steps to the current row
  ([morel#265](https://github.com/hydromatic/morel/issues/265))
* Unify `list` and `bag` in type resolution via an orderedness atom
  ([morel#407](https://github.com/hydromatic/morel/issues/407))
* Implement `Sys.plan`
  ([morel#41](https://github.com/hydromatic/morel/issues/41))
* Type `from` queries over lists
* Postfix method calls
  ([morel#346](https://github.com/hydromatic/morel/issues/346))
* `Date` structure
  ([morel#278](https://github.com/hydromatic/morel/issues/278))
* `Time` structure
  ([morel#351](https://github.com/hydromatic/morel/issues/351))
* `Variant` structure
  ([morel#324](https://github.com/hydromatic/morel/issues/324))
* `Range` structure
  ([morel#338](https://github.com/hydromatic/morel/issues/338))
* `Fn` structure ([morel#301](https://github.com/hydromatic/morel/issues/301))
* `Either` structure
  ([morel#302](https://github.com/hydromatic/morel/issues/302))
* `ListPair` structure
  ([morel#295](https://github.com/hydromatic/morel/issues/295))
* `Vector` structure
  ([morel#39](https://github.com/hydromatic/morel/issues/39))
* `Bag` structure
  ([morel#235](https://github.com/hydromatic/morel/issues/235))
* `List` structure ([morel#27](https://github.com/hydromatic/morel/issues/27))
* Tabular output mode
  ([morel#259](https://github.com/hydromatic/morel/issues/259))
* `Char` structure
  ([morel#264](https://github.com/hydromatic/morel/issues/264))
* `String` structure
  ([morel#279](https://github.com/hydromatic/morel/issues/279))
* `Word` structure
  ([morel#396](https://github.com/hydromatic/morel/issues/396))
* `Math` structure ([morel#88](https://github.com/hydromatic/morel/issues/88))
* `Real` structure
  ([morel#102](https://github.com/hydromatic/morel/issues/102))
* `Int` structure
  ([morel#228](https://github.com/hydromatic/morel/issues/228))
* `StringCvt` structure
  ([morel#371](https://github.com/hydromatic/morel/issues/371))
* Add the interactive shell
* `Sys` structure
* `Bool` structure
* `Option` structure
  ([morel#38](https://github.com/hydromatic/morel/issues/38))
* `General` structure
* `op` sections ([morel#311](https://github.com/hydromatic/morel/issues/311))
* Parse the command line
* Wrap long types across lines
  ([morel#210](https://github.com/hydromatic/morel/issues/210))
* Evaluate `datatype` values
* Evaluate parallel `val ... and ...` bindings
* Report compile and runtime errors
* Evaluate list and string operators, equality, and comparison
* Evaluate `val` patterns, currying, and sequential declarations
* Evaluate recursive functions
* Evaluate functions and closures
* Evaluate `case`, `if`, and negation
* Add the printing engine
* Add Code, Frame, and the compiler; statements evaluate
* Add the runtime value representation
* Check field references against record types
* Type annotations
  ([morel#138](https://github.com/hydromatic/morel/issues/138))
* Load built-in signatures; check numeric operators
* Type datatypes and constructor patterns
* Type `case`, `fun`, and `val rec`
* Type tuples, records, and selectors
* Add the Core IR and the type resolver
* Add the unifier
* Add interned type representations
* Parse queries
* Parse declarations, types, and datatypes
* Parse `if`, `fn`, `case`, and patterns
* Parse operators
* Parse literals, tuples, lists, records, selectors
* Add `Sys.parseTree` and the first expression parser
* Add AST and parse-tree dump
* Add kernel, script runner, and command
* Add statement splitter
* Add lexer

### Bug-fixes and internal improvements

* Shorten the shell banner
* Character constant that is not exactly one character crashes the shell
  ([morel#420](https://github.com/hydromatic/morel/issues/420))
* Throw if `ordinal` is used in an unordered step
* Reject a binder after `compute`
  ([morel#387](https://github.com/hydromatic/morel/issues/387), related)
* Type declarations are not recursive; displaced datatypes keep identity
  ([morel#429](https://github.com/hydromatic/morel/issues/429))
* Feasibility-based bound tightening (FBBT)
  ([morel#373](https://github.com/hydromatic/morel/issues/373))
* Fold a `case` on a constant to its matching branch
  ([morel#330](https://github.com/hydromatic/morel/issues/330))
* Inline expressions
  ([morel#53](https://github.com/hydromatic/morel/issues/53))
* Add `MOREL_DEBUG` escape hatch for the blanket recover
* Fix minor type-alias, tyvar, coverage, and doc issues
* Clarify the stack-offset plan convention
* Advance `ordinal` in `group` key and aggregate evaluation
* Reset expected output after a Split error; guard `splitOutput`
* Parse `type_string` operand at precedence 9
  ([morel#406](https://github.com/hydromatic/morel/issues/406), continued)
* Set `Sys.plan` for a `fun`; keep it across planless decls
* Classify a layered pattern by its body for coverage
* Scope an `into` function at the query's root
* Type the zero of an empty `sum`
* Fix stack overflow on recursive type alias
* `Sys.plan`: `General.o`, type-directed `mod`, tuple scan-pattern names
* `Sys.plan`: render `case` as a match function; tyCon constructors
* `Sys.plan`: render `from`-queries as a sink pipeline
* `Sys.plan`: render a field selector as `nth:N`
* `Sys.plan`: type-directed, structure-qualified builtin names
* Convert `Either` and `Bool` derivable members to Morel
* `Sys.plan`: `tailApply` for applications in tail position
* Factor `compileApply` out of `compileExp`
* Report "cannot derive label" for a record field
  ([morel#305](https://github.com/hydromatic/morel/issues/305), continued)
* Make `PP` a structLib client: derive combinators in Morel
* Define `Fn` and `Option` derivable members in Morel
* Print an abstract type as `-`
* Build `PP` on the shared pretty-printer
  ([morel#398](https://github.com/hydromatic/morel/issues/398), continued)
* Reject top-level `val inst` in the core resolver
  ([morel#237](https://github.com/hydromatic/morel/issues/237), related)
* Make `List.except` and `List.intersect` multiset operations
  ([morel#321](https://github.com/hydromatic/morel/issues/321))
* Validate `group` label derivation and duplicate fields
  ([morel#305](https://github.com/hydromatic/morel/issues/305))
* Distinct keys the current row, not the full frame
* Equality and order handle variants, unit, and cleared slots
* Treat strings as byte sequences, escaping high bytes
* Add the built-in registry
* Parser hardening: error messages and spans

### Build and tests

* Retire kernel session tests into the script corpus
* Gap samples; pin queries the set-op row fix enabled
* Pin the unimplemented structure members
* Add structLib consistency tests
* Add the `scott` dataset
* Compare statement output semantically
* Surface not-implemented errors interactively; `MOREL_GAPS` inventory
* Run .smli scripts through the shared runner
  ([morel#300](https://github.com/hydromatic/morel/issues/300))
* Add structure test scripts and their lint check
* Stress-test closures and recursion across statements
* Pull the first evaluation corpus
* Add `:t` statements to type without evaluating
  ([morel#310](https://github.com/hydromatic/morel/issues/310))
* Add convergence and corpus tooling
* Add idempotent script harness
* Add project lint test
* Add golangci-lint configuration
* Add GitHub Actions CI workflow
* Initial

### Site and documentation

* Release v0.8.0 ([#3](https://github.com/hydromatic/morel-go/issues/3))
* Expand `README.md`
* Add `docs/howto.md`
* Rename `HISTORY.md` to `CHANGELOG.md`
* Add development notes

