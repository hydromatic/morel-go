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
 * The derived members of the PP pretty-printer, over its native
 * primitives (empty, line, lineBreak, hardLine, text, beside, nest,
 * group, align, pack, render), which are in scope here.
 * interpose is a private helper.
 *)
val softLine = `group` line;
val softBreak = `group` lineBreak;

fun hang (i, d) = align (nest (i, d));

fun indent (i, d) =
  nest (i, beside (text (implode (List.tabulate (i, fn _ => #" "))), d));

fun interpose (_, []) = empty
  | interpose (_, [d]) = d
  | interpose (sep, d :: ds) = beside (d, beside (sep, interpose (sep, ds)));

fun hsep ds = interpose (text " ", ds);
fun vsep ds = interpose (line, ds);
fun vcat ds = interpose (lineBreak, ds);
fun fillSep ds = interpose (softLine, ds);
fun fillCat ds = interpose (softBreak, ds);
fun sep ds = `group` (vsep ds);
fun cat ds = `group` (vcat ds);

fun hcat [] = empty
  | hcat [d] = d
  | hcat (d :: ds) = beside (d, hcat ds);

fun parens d = beside (text "(", beside (d, text ")"));
fun braces d = beside (text "{", beside (d, text "}"));
fun brackets d = beside (text "[", beside (d, text "]"));

fun punctuate (_, []) = []
  | punctuate (_, [d]) = [d]
  | punctuate (sep, d :: ds) = beside (d, sep) :: punctuate (sep, ds);

fun encloseSep (open_, close_, _, []) = beside (open_, close_)
  | encloseSep (open_, close_, sep, d :: ds) =
    `group` (beside (open_,
      beside (List.foldl
          (fn (e, acc) => beside (acc, beside (sep, beside (line, e)))) d ds,
        close_)));
