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
 * The Either structure, defined in Morel over its datatype
 * (INL | INR). Every member is derivable, so it has no native
 * members.
 *)
fun isLeft (INL _) = true
  | isLeft (INR _) = false;
fun isRight (INL _) = false
  | isRight (INR _) = true;
fun asLeft (INL x) = SOME x
  | asLeft (INR _) = NONE;
fun asRight (INL _) = NONE
  | asRight (INR y) = SOME y;
fun map (fl, fr) (INL x) = INL (fl x)
  | map (fl, fr) (INR y) = INR (fr y);
fun mapLeft f (INL x) = INL (f x)
  | mapLeft f (INR y) = INR y;
fun mapRight f (INL x) = INL x
  | mapRight f (INR y) = INR (f y);
fun app (fl, fr) (INL x) = fl x
  | app (fl, fr) (INR y) = fr y;
fun appLeft f (INL x) = f x
  | appLeft f (INR _) = ();
fun appRight f (INL _) = ()
  | appRight f (INR y) = f y;
fun fold (fl, fr) init (INL x) = fl (x, init)
  | fold (fl, fr) init (INR y) = fr (y, init);
fun proj (INL x) = x
  | proj (INR y) = y;
fun partition [] = ([], [])
  | partition (INL x :: rest) =
      let val (ls, rs) = partition rest in (x :: ls, rs) end
  | partition (INR y :: rest) =
      let val (ls, rs) = partition rest in (ls, y :: rs) end;
