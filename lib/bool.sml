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
 * The derivable members of the Bool structure. The native members
 * (not and the comparison operators) are supplied by the kernel.
 *)
fun toString true = "true"
  | toString false = "false";
fun fromString "true" = SOME true
  | fromString "false" = SOME false
  | fromString _ = NONE;
fun `andalso` (a, b) = a andalso b;
fun `orelse` (a, b) = a orelse b;
fun `implies` (a, b) = a implies b;
