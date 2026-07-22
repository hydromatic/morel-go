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

// Package lib embeds the built-in library sources: the signatures
// (*.sig), which give the types of the built-ins, and the Morel
// sources (*.sml) that define the derivable members of some
// structures.
package lib

import "embed"

// FS holds the signature and Morel-source files.
//
//go:embed *.sig *.sml
var FS embed.FS
