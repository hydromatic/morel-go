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
 * The derivable members of the Option structure. The native
 * members (getOpt, isSome, valOf) are supplied by the kernel and
 * are in scope here.
 *)
fun filter f a = if f a then SOME a else NONE;
fun `join` (SOME v) = v
  | `join` NONE = NONE;
fun app f (SOME v) = f v
  | app f NONE = ();
fun map f (SOME v) = SOME (f v)
  | map f NONE = NONE;
fun mapPartial f (SOME v) = f v
  | mapPartial f NONE = NONE;
fun compose (f, g) a =
  case g a of NONE => NONE | SOME v => SOME (f v);
fun composePartial (f, g) a =
  case g a of NONE => NONE | SOME v => f v;
