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

package eval

// The Bag built-ins. A bag holds its elements in the list
// representation ([]Val), so all its element-wise operations —
// null, length, hd, tl, getItem, take, drop, @, concat, app, map,
// mapPartial, find, filter, partition, fold, exists, all,
// tabulate, nth, only — reuse the List code. Only the conversions
// between the identical bag and list representations are named
// here.

// bagFromListFn is "Bag.fromList l" and the global "bag": a bag
// holds the same elements as the list, in the same representation.
func bagFromListFn(arg Val) (Val, error) {
	return arg, nil
}

// bagToListFn is "Bag.toList b": the list of a bag's elements.
func bagToListFn(arg Val) (Val, error) {
	return arg, nil
}
