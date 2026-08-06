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
[![Build Status](https://github.com/hydromatic/morel-go/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/hydromatic/morel-go/actions?query=branch%3Amain)
[![Go Reference](https://pkg.go.dev/badge/github.com/hydromatic/morel-go.svg)](https://pkg.go.dev/github.com/hydromatic/morel-go)
<img align="right" alt="Morel mushroom (credit: OldDesignShop.com)" src="etc/morel-1200x1200.jpg" with="300" height="300">

# Morel

A functional query language, implemented in Go.

Morel is Standard ML with relational extensions: a `from` expression
that queries collections the way SQL queries tables, but inside a
language that has lambdas, polymorphism and pattern-matching.

## Requirements

Go version 1.25 or higher.

## Get Morel

### Install

```bash
$ go install github.com/hydromatic/morel-go/cmd/morel@latest
```

This puts a `morel` binary in `$(go env GOPATH)/bin`.

### Download and build

```bash
$ git clone git://github.com/hydromatic/morel-go.git
$ cd morel-go
$ go build ./cmd/morel; ./morel
```

### Run the shell

```bash
$ ./morel
morel-go v0.8.0 (go1.26.5, darwin/arm64)
- "Hello, world!";
val it = "Hello, world!" : string
```

Type control+D to exit the shell.

Within the shell, the `use` function reads and evaluates source from a
file:

```
- use "script.sml";
```

For quick testing, use the `-e` flag.

```bash
$ ./morel -e "1 + 2"
val it = 3 : int

$ ./morel -e "from i in [1,2,3] where i > 1"
val it = [2,3] : int list

$ ./morel -e 'let fun double x = x * 2 in map double [2,3,4] end'
val it = [4,6,8] : int list
```

Named files are run as scripts; a file argument of `-` means standard
input. Run `./morel --help` for the full list of options.

## Documentation

Morel Go does not yet have documentation of its own; the language it
implements is the one described by Morel Java.

* [Morel Java language reference](https://github.com/hydromatic/morel/blob/main/docs/reference.md)
* [Query reference](https://github.com/hydromatic/morel/blob/main/docs/query.md)
* [Datalog support](https://github.com/hydromatic/morel/blob/main/docs/datalog.md)
* ["How to" guide](docs/howto.md)
* [Change log](CHANGELOG.md)
* Reading [test scripts](testdata/script)
  can be instructive; try, for example,
  [relational.smli](testdata/script/relational.smli)
  or [built-in.smli](testdata/script/built-in.smli)

## Status

Morel Go aims to be compatible with the Morel language as implemented
by [Morel Java](https://github.com/hydromatic/morel), and its strategy
is to copy that project's `.smli` test scripts and get them to pass.
Compatibility is therefore measured by how much of Morel Java's script
corpus Morel Go reproduces line for line: currently about 87%. (See
["Morel on Go"](http://blog.hydromatic.net/2026/08/01/morel-on-go.html)
for how the port was done.)

Implemented:
* Literals, variables, and comments
* `let`, `val` (including `val rec`), `fun`, `fn`, and function
  application
* `if`, `case`, and pattern-matching over constants, wildcards,
  tuples, records, and lists
* Type inference (Hindley-Milner) and type variables
* Primitive, function, list, bag, tuple, record, and vector types
* `datatype` and `type` declarations
* `raise`, and the built-in exceptions
* Relational expressions: `from`, `join` (including `left`, `right`
  and `full` outer joins), `where`, `group`, `compute`, `order`,
  `skip`, `take`, `yield`, `yieldAll` and `into`
* Datalog
* The standard library: 460 members in 25 structures, based on the
  [Standard ML Basis Library](https://smlfamily.github.io/Basis/)
  and extended with `Bag`, `Datalog`, `Interact`, `Range`,
  `Relational`, `Sys` and `Variant`
* A shell that reads scripts or standard input, evaluates a single
  expression with `-e`, and checks a script against its expected
  output in `.smli` (idempotent) format
* Command-line editing in the shell, with Emacs and Vi bindings and
  `.inputrc` support, and history that persists across sessions in
  `~/.morel/history-go`

Not implemented:
* `exception` declarations, and `handle`
* `structure`, `struct`, `signature`, `sig`, `open`
* `local`
* `while`
* References, and operators `!` and `:=`
* User-defined operators (`infix`, `infixr`)
* Overloaded functions (`over`, `val inst`); they parse, but do not
  compile
* External data: reading from the file system, or from a database
  via ODBC or JDBC
* Attributes (`[@ ... ]`) and quoted identifiers
* Syntax highlighting in the shell
* Tab completion in the shell

See also [GitHub issues](https://github.com/hydromatic/morel-go/issues).

## More information

* License: <a href="LICENSE">Apache License, Version 2.0</a>
* Author: Julian Hyde (<a href="https://twitter.com/julianhyde">@julianhyde</a>)
* Blog: http://blog.hydromatic.net
* Source code: https://github.com/hydromatic/morel-go
* Issues: https://github.com/hydromatic/morel-go/issues
* <a href="CHANGELOG.md">Change log and release notes</a>
