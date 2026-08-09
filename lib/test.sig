(*
 * Licensed to Julian Hyde under one or more contributor license
 * agreements.  See the NOTICE file distributed with this work
 * for additional information regarding copyright ownership.
 * Julian Hyde licenses this file to you under the Apache
 * License, Version 2.0 (the "License"); you may not use this
 * file except in compliance with the License.  You may obtain a
 * copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied.  See the License for the specific
 * language governing permissions and limitations under the
 * License.
 *
 * The TEST signature, a Morel extension.
 *)
(**
 * The `Test` structure provides functions that exist so that the
 * implementation can be tested from a script. The `excludeStructures`
 * property hides it by default, because it is of no use in a session.
 *)
signature TEST =
sig

  (**
   * tokenizes `s` as Morel code and returns the tokens in a concise
   * textual form, each token written as its CSS class followed by its
   * text in braces; for example, `highlight "val x = 1"` returns
   * `"kr{val} nv{x} p{=} mi{1}"`. Whitespace is written verbatim.
   * Useful for testing the syntax highlighter from a script.
   *)
  val highlight : string -> string [@@prototype "highlight s"]
end
[@@description "Functions for testing the implementation."]
[@@specified "morel"]

(*) End test.sig
