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

// Command morel is an interpreter for Morel, a functional query
// language. With no file arguments it reads statements from
// standard input; it also evaluates an expression given with
// "-e", or runs the script files named on the command line.
package main

import (
	"fmt"
	"os"

	"github.com/hydromatic/morel-go/internal/shell"
)

func main() {
	args := shell.ParseArgs(os.Args[1:])
	err := args.Run(os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "morel: "+err.Error())
		os.Exit(1)
	}
}
