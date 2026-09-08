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

# morel-go commit → upstream morel issue map

This table pairs each commit on the morel-go `main-candidate` branch (173
commits, oldest→newest) with the hydromatic/morel GitHub issue number(s) it
corresponds to.

Commits are keyed by their **commit message**, not their SHA, because the
morel-go SHAs are being rewritten and will change. The morel-go messages
are deliberately provenance-free; the issue numbers here were inferred by
matching each commit's feature against the upstream Java implementation's
git log (where subjects carry the issue as `(#NNN)` or a legacy
`[MOREL-NNN]` / `[SMLJ-NNN]` prefix — same tracker, same numbers). Every
`#NNN` below was confirmed to appear in the morel-java log for a matching
feature.

`—` means no upstream issue (bootstrap / Go-port plumbing / test-harness or
`Sys.plan`-rendering internals that predate or fall outside issue tracking).
`?` marks a genuinely ambiguous match (see notes).

| # | morel-go commit message | morel#NNN | notes |
|---|---|---|---|
| 1 | Initial | — | repo bootstrap |
| 2 | Add GitHub Actions CI workflow | — | CI infra |
| 3 | Add development notes | — | infra/docs |
| 4 | Add golangci-lint configuration | — | lint infra |
| 5 | Add project lint test | — | lint infra |
| 6 | Add lexer | — | port scaffolding (java lexer predates issues) |
| 7 | Add statement splitter | — | port scaffolding |
| 8 | Add kernel, script runner, and command | — | port scaffolding |
| 9 | Add idempotent script harness | #198 | ports MOREL-198 "Idempotent mode for test scripts" |
| 10 | Add convergence and corpus tooling | — | Go-local test tooling |
| 11 | Add AST and parse-tree dump | — | parser bootstrap |
| 12 | Add `Sys.parseTree` and the first expression parser | — | java "Add Sys.parseTree" commit has no issue number |
| 13 | Parse literals, tuples, lists, records, selectors | — | parser bootstrap |
| 14 | Parse operators | — | parser bootstrap |
| 15 | Parse `if`, `fn`, `case`, and patterns | — | parser bootstrap |
| 16 | Parse declarations, types, and datatypes | — | parser bootstrap |
| 17 | Parse queries | — | parser bootstrap |
| 18 | Parser hardening: error messages and spans | — | parser bootstrap |
| 19 | Add interned type representations | — | type-system bootstrap |
| 20 | Add the unifier | — | java "Unifier" predates issues |
| 21 | Add the Core IR and the type resolver | — | Core/TypeResolver bootstrap |
| 22 | Add `:t` statements to type without evaluating | #310 | MOREL-310 validation-mode `:t` |
| 23 | Type tuples, records, and selectors | — | type-resolver bootstrap |
| 24 | Type `case`, `fun`, and `val rec` | — | type-resolver bootstrap |
| 25 | Type datatypes and constructor patterns | — | type-resolver bootstrap |
| 26 | Load built-in signatures; check numeric operators | — | bootstrap |
| 27 | Type annotations | #138 | MOREL-138 type annotations in patterns/decls/exprs |
| 28 | Check field references against record types | — | no clear discrete issue (cf. "Validate field references") |
| 29 | Add the runtime value representation | — | evaluator bootstrap |
| 30 | Add Code, Frame, and the compiler; statements evaluate | — | evaluator bootstrap |
| 31 | Add the printing engine | — | printer bootstrap |
| 32 | Evaluate `case`, `if`, and negation | — | evaluator bootstrap |
| 33 | Evaluate functions and closures | — | evaluator bootstrap |
| 34 | Evaluate recursive functions | — | evaluator bootstrap |
| 35 | Evaluate `val` patterns, currying, and sequential declarations | — | evaluator bootstrap |
| 36 | Evaluate list and string operators, equality, and comparison | — | evaluator bootstrap |
| 37 | Report compile and runtime errors | — | evaluator bootstrap |
| 38 | Evaluate parallel `val ... and ...` bindings | — | evaluator bootstrap |
| 39 | Evaluate `datatype` values | — | evaluator bootstrap |
| 40 | Wrap long types across lines | #210 | MOREL-210 fold long types when printing |
| 41 | Pull the first evaluation corpus | — | Go-local test corpus |
| 42 | Stress-test closures and recursion across statements | — | Go-local test (java equivalent has no issue) |
| 43 | Add the built-in registry | — | bootstrap |
| 44 | Add structure test scripts and their lint check | — | Go-local test infra |
| 45 | Parse the command line | — | bootstrap |
| 46 | Run .smli scripts through the shared runner | #300 | cf. MOREL-300 run `.smli` scripts from command line |
| 47 | Surface not-implemented errors interactively; `MOREL_GAPS` inventory | — | Go-local gap tooling |
| 48 | `op` sections | #311 | MOREL-311 `op` keyword (operator sections) |
| 49 | `General` structure | — | no discrete upstream issue |
| 50 | `Option` structure | #38 | MOREL-38 option datatype & functions |
| 51 | `Bool` structure | — | no discrete upstream issue |
| 52 | `Sys` structure | — | no discrete upstream issue |
| 53 | Add the interactive shell | — | SMLJ-1 shell predates issue tracking; port plumbing |
| 54 | `StringCvt` structure | #371 | MOREL-371 StringCvt / Real.fmt / Int.fmt |
| 55 | `Int` structure | #228 | MOREL-228 Int structure |
| 56 | `Real` structure | #102 | MOREL-102 Real structure |
| 57 | `Math` structure | #88 | MOREL-88 Math structure |
| 58 | `Word` structure | #396 | word type & Word structure (#396) |
| 59 | `String` structure | #279 | MOREL-279 String structure |
| 60 | `Char` structure | #264 | MOREL-264 Char structure |
| 61 | Treat strings as byte sequences, escaping high bytes | — | Go-local string representation |
| 62 | Tabular output mode | #259 | MOREL-259 tabular mode |
| 63 | `List` structure | #27 | MOREL-27 structures List and String |
| 64 | `Bag` structure | #235 | MOREL-235 bag type & Bag structure |
| 65 | `Vector` structure | #39 | MOREL-39 vector & order data types |
| 66 | `ListPair` structure | #295 | MOREL-295 ListPair structure |
| 67 | `Either` structure | #302 | MOREL-302 Either structure |
| 68 | `Fn` structure | #301 | MOREL-301 Fn structure |
| 69 | `Range` structure | #338 | Range structure (#338) |
| 70 | `Variant` structure | #324 | variant datatype & Variant structure (#324) |
| 71 | `Time` structure | #351 | Time structure (#351) |
| 72 | `Date` structure | #278 | Date structure (#278) |
| 73 | Postfix method calls | #346 | postfix method-call syntax `x.f arg` (#346) |
| 74 | Type `from` queries over lists | — | base `from` typing (java `from` predates issues) |
| 75 | Implement `Sys.plan` | #41 | MOREL-41 `Sys.plan ()` (later rendering commits are Go-local) |
| 76 | Unify `list` and `bag` in type resolution via an orderedness atom | #407 | orderedness atom (#407) |
| 77 | Bind `current` in query steps to the current row | #265 | MOREL-265 `current` keyword |
| 78 | Expose a non-record `yield`'s implicit-label field | ? | plausibly #10 (implicit labels) / #262; no clean single match |
| 79 | Type `join ... on` conditions | #216, #72 | comma scans + `on` (#216); base join (#72) |
| 80 | Type query passthrough and forcer steps | — | Go-local internal query typing |
| 81 | Type `group` and `compute` steps with aggregates | #6, #69 | group/compute clauses (SMLJ-6); compute monoid comprehensions (#69) |
| 82 | Type `exists`, `forall`, `require`, `into`, and `through` | #241, #171 | forall/exists (#241); into/through (#171) |
| 83 | Compare statement output semantically | — | Go-local test infra |
| 84 | Render bags, options, and nested fields in tabular output mode | #376, #382 | tabular nested collections (#376); option values (#382) |
| 85 | Add the `scott` dataset | — | Go-local dataset |
| 86 | Evaluate `from` queries: scan, `where`, `yield` | — | base `from` eval (predates issues) |
| 87 | Validate step placement in queries | — | Go-local validation |
| 88 | Evaluate open record patterns `{a, ...}` | #36 | MOREL-36 optional labels in record patterns |
| 89 | Derive a punned record field label from a selector | #10 | MOREL-10 implicit labels in record expressions |
| 90 | Evaluate joins: comma scans, `join ... on`, dependent scans | #216, #275 | comma scans + on (#216); dependent joins (#275) |
| 91 | Evaluate `order`, `distinct`, `skip`, and `take` | #20, #231, #204 | order (#20); distinct (#231); skip/take (#204) |
| 92 | Equality and order handle variants, unit, and cleared slots | — | Go-local eval detail |
| 93 | Evaluate `union`, `intersect`, and `except` | #30 | MOREL-30 union/intersect/except operators |
| 94 | Distinct keys the current row, not the full frame | — | Go-local bugfix |
| 95 | Evaluate `group` and `compute` | #6, #69 | group/compute clauses (SMLJ-6); compute (#69) |
| 96 | Evaluate `exists`, `forall`, `require`, `into`, and `through` | #241, #171 | forall/exists (#241); into/through (#171) |
| 97 | Evaluate `current`, `ordinal`, and `unorder` | #265, #276, #277 | current (#265); ordinal (#276); unorder (#277) |
| 98 | Evaluate mid-query `yield` and `yield` binders | #262, #52 | atomic yield anywhere (#262); multiple yields (#52) |
| 99 | Honor a `group` binder | #387 | single "binder" variable for yield/yieldAll/group (#387) |
| 100 | Validate `group` label derivation and duplicate fields | #305 | MOREL-305 "cannot derive label" |
| 101 | Normalize `datatype` echo and reject unbound type variables | #356 | disallow unbound type variables (#356) |
| 102 | Implement `Relational.compare`, `compare`, and descending order | #282 | Descending datatype & Relational.compare (#282) |
| 103 | Parse `over` and `val inst` declarations | #237 | operator overloading, over/inst (#237) |
| 104 | Overload `elem`/`notelem`/`only` on lists and bags | ? | overloading builtins over list/bag; relates #237 / #235; no clean single match |
| 105 | Type local `over`/`val inst` overloads | #237 | over/inst overloading (#237) |
| 106 | Add the global `scott` dataset | — | Go-local dataset |
| 107 | Match layered (`as`) patterns | #103 | MOREL-103 layered patterns & composite val |
| 108 | Make `List.except` and `List.intersect` multiset operations | #321 | intersect/except should count & preserve order (#321) |
| 109 | Reject top-level `val inst` in the core resolver | #237 | validation for over/inst overloading (#237) |
| 110 | Aggregates adapt to `list` or `bag`; zero-field `group` rule | #271, #328 | aggregates adapt to collection type (#271); zero-field distinct vs group{} (#328) |
| 111 | Evaluate the empty record as unit | #14 | tuple = record = unit when empty (#14) |
| 112 | Implement transparent type aliases | #285 | MOREL-285 type abbreviations / alias types |
| 113 | Implement `type_string` | #406 | type_string operator (#406) |
| 114 | Warn on nonexhaustive and redundant matches | #55 | MOREL-55 match coverage analysis |
| 115 | Implement the `PP` pretty-printer structure | #398, #339 | PP structure & print values (#398, #339) |
| 116 | Build `PP` on the shared pretty-printer | #398 | PP structure (#398) |
| 117 | Print an abstract type as `-` | — | Go-local printing detail |
| 118 | Define `Fn` and `Option` derivable members in Morel | — | Go-local structLib (derive members in Morel source) |
| 119 | Make `PP` a structLib client: derive combinators in Morel | — | Go-local structLib |
| 120 | Add structLib consistency tests | — | Go-local test infra |
| 121 | Record update: `{e with lab = v, ...}` | #249 | MOREL-249 `with` functional update |
| 122 | Report "cannot derive label" for a record field | #305 | MOREL-305 cannot-derive-label |
| 123 | Range-list syntax: `[1 .. 5]`, `[0 ..^ 10, 20]` | #372 | list constructor allows ranges (#372) |
| 124 | Factor `compileApply` out of `compileExp` | — | Go-local refactor |
| 125 | `Sys.plan`: `tailApply` for applications in tail position | — | Go-local Sys.plan rendering |
| 126 | Enable range enumeration and membership over non-discrete types | ? | relates to Range (#338); no clean single match |
| 127 | `Relational.iterate`: semi-naive transitive-closure builtin | — | java "Add Relational.iterate" commit has no issue number |
| 128 | Convert `Either` and `Bool` derivable members to Morel | — | Go-local structLib |
| 129 | `Sys.plan`: type-directed, structure-qualified builtin names | — | Go-local Sys.plan rendering |
| 130 | `Sys.plan`: render a field selector as `nth:N` | — | Go-local Sys.plan rendering |
| 131 | `Sys.plan`: render `from`-queries as a sink pipeline | — | Go-local Sys.plan rendering |
| 132 | `Sys.plan`: render `case` as a match function; tyCon constructors | — | Go-local Sys.plan rendering |
| 133 | `Sys.plan`: `General.o`, type-directed `mod`, tuple scan-pattern names | — | Go-local Sys.plan rendering |
| 134 | Fix stack overflow on recursive type alias | — | Go-local bugfix (cf. #285) |
| 135 | Type the zero of an empty `sum` | — | Go-local eval detail |
| 136 | Scope an `into` function at the query's root | — | Go-local bugfix (cf. #171) |
| 137 | Classify a layered pattern by its body for coverage | — | Go-local bugfix (cf. #55/#103) |
| 138 | Set `Sys.plan` for a `fun`; keep it across planless decls | — | Go-local Sys.plan handling |
| 139 | Parse `type_string` operand at precedence 9 | #406 | type_string operator (#406) |
| 140 | Reset expected output after a Split error; guard `splitOutput` | — | Go-local test harness |
| 141 | Advance `ordinal` in `group` key and aggregate evaluation | — | Go-local bugfix (cf. #276) |
| 142 | Clarify the stack-offset plan convention | — | Go-local doc |
| 143 | Fix minor type-alias, tyvar, coverage, and doc issues | — | Go-local cleanup |
| 144 | Add `MOREL_DEBUG` escape hatch for the blanket recover | — | Go-local |
| 145 | Inline expressions: analyzer, inliner, and the pass loop | #53 | MOREL-53 optimize core by inlining expressions |
| 146 | Fold a `case` on a constant to its matching branch | #330 | inline `case x` when x is constant (#330) |
| 147 | Extents: a sourceless scan iterates all values of its type | #217, #202 | unbounded vars via predicate inversion (#217); unbounded vars, remove suchthat (#202) |
| 148 | Ground unbounded query variables by inverting predicates | #217 | queries with unbounded vars by inverting predicates (#217) |
| 149 | Invert membership predicates into collection scans | #217 | predicate inversion (#217) |
| 150 | Invert bound predicates into range scans | #217 | predicate inversion (#217) |
| 151 | Invert disjunctions into unions of generators | #217 | predicate inversion (#217) |
| 152 | Invert quantified predicates into projected scans | #217 | predicate inversion (#217) |
| 153 | Invert case predicates; the prefix generator; tuple substitution | #341 | invert `case` with multiple arms (#341) |
| 154 | Invert recursive predicates into fixed-point iterations | #217 | predicate inversion, recursive (#217) |
| 155 | Implement `Sys.planEx`: re-plan a statement to a numbered pass | #329 | `Sys.planEx phase` (#329) |
| 156 | Strengthen filters by feasibility-based bound tightening | #373 | FBBT (#373) |
| 157 | Implement the `Datalog` structure: parse, analyze, translate, execute | #323 | Datalog (#323) |
| 158 | Pin the unimplemented structure members | — | Go-local |
| 159 | First-class exceptions: the `exn` datatype, `raise`, `exnName`, `exnMessage` | #364 | MOREL-364 `raise` command |
| 160 | `Interact.use` and `useSilently` execute a file in the session | #86 | MOREL-86 `use` function |
| 161 | Compute steps: standalone `compute`, elements, aggregates in expressions | #304 | MOREL-304 `elements` collection in compute |
| 162 | `yieldAll` step: flatten a collection-valued expression | #257 | yieldAll flatMap step (#257) |
| 163 | Zero-scan queries iterate one empty row; `current`-outside-query error | #17 | MOREL-17 `from` with 0 sources |
| 164 | Dispatch a postfix method by its identifier receiver type | #346 | postfix method-call syntax (#346) |
| 165 | `Range.contains` on continuous and discrete sets | #338 | Range structure (#338) |
| 166 | Scalar scans: `pat = exp` binds the value of `exp` per row | #11 | MOREL-11 `from`, allow `variable = value` |
| 167 | Gap samples; pin queries the set-op row fix enabled | — | Go-local test infra |
| 168 | `typeof` annotations: the type of an expression | #291 | MOREL-291 `typeof` operator |
| 169 | Signature declarations parse and echo | #315 | MOREL-315 parse `signature` |
| 170 | Outer joins and the `?.` safe-navigation operator | #75, #378 | outer joins (#75); safe navigation `?.` (#378) |
| 171 | Type declarations are not recursive; displaced datatypes keep identity | #429 | redefining a type name (#429) |
| 172 | Retire kernel session tests into the script corpus | — | Go-local test migration |
| 173 | Reject a binder after `compute` | #387 | validation from single-binder syntax (#387) |
