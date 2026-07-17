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

// Package eval holds runtime values and the built-in functions.
package eval

import (
	"fmt"
	"math"

	"github.com/hydromatic/morel-go/internal/ast"
	"github.com/hydromatic/morel-go/internal/core"
	"github.com/hydromatic/morel-go/internal/parse"
)

// Val is a runtime value. Interpretation is driven by static
// types, so values are bare. The concrete
// types are: int32 (int), float32 (real; computed in float64),
// string, rune (char), bool, core.Unit, []Val (lists, tuples,
// and records in canonical field order), Con (a datatype
// constructor value), and function values (Fn for built-ins,
// Closure for user functions).
type Val = any

// Fn is the implementation of a built-in function: one argument
// at a time, returning a value or an error that carries a source
// position. Every built-in follows this convention uniformly. A
// built-in whose Morel type is curried returns another Fn at
// each step, so partial application yields an ordinary function
// value; a built-in whose Morel argument is a tuple receives a
// []Val.
type Fn func(arg Val) (Val, error)

// Curry2 adapts a two-argument function to the built-in
// convention: applying the result to the first argument returns
// an Fn awaiting the second.
func Curry2(f func(a, b Val) (Val, error)) Fn {
	return func(a Val) (Val, error) {
		return Fn(func(b Val) (Val, error) {
			return f(a, b)
		}), nil
	}
}

// Curry3 adapts a three-argument function to the built-in
// convention.
func Curry3(f func(a, b, c Val) (Val, error)) Fn {
	return func(a Val) (Val, error) {
		return Curry2(func(b, c Val) (Val, error) {
			return f(a, b, c)
		}), nil
	}
}

// Builtins maps a built-in's name to its value — usually an Fn,
// but a constant such as Int.maxInt is its value directly. The
// registry is validated against lib/*.sig.
var Builtins = map[string]Val{
	// lint: sort until '^}' where '^\t"'
	"Bag.@":          atFn,
	"Bag.all":        Fn(allFn),
	"Bag.app":        Fn(appFn),
	"Bag.concat":     Fn(listConcatFn),
	"Bag.drop":       Fn(dropFn),
	"Bag.exists":     Fn(existsFn),
	"Bag.filter":     Fn(filterFn),
	"Bag.find":       Fn(findFn),
	"Bag.fold":       fold(true),
	"Bag.fromList":   Fn(bagFromListFn),
	"Bag.getItem":    Fn(getItemFn),
	"Bag.hd":         hdFn,
	"Bag.length":     lengthFn,
	"Bag.map":        mapFn,
	"Bag.mapPartial": Fn(mapPartialFn),
	"Bag.nil":        []Val{},
	"Bag.nth":        Fn(nthFn),
	"Bag.null":       nullFn,
	"Bag.only":       Fn(onlyFn),
	"Bag.partition":  Fn(partitionFn),
	"Bag.tabulate":   Fn(tabulateFn),
	"Bag.take":       Fn(takeFn),
	"Bag.tl":         tlFn,
	"Bag.toList":     Fn(bagToListFn),
	"Bool.<": boolOp(func(a, b bool) bool {
		return !a && b
	}),
	"Bool.<>": boolOp(func(a, b bool) bool {
		return a != b
	}),
	"Bool.=": boolOp(func(a, b bool) bool {
		return a == b
	}),
	"Bool.>": boolOp(func(a, b bool) bool {
		return a && !b
	}),
	"Bool.andalso": boolOp(func(a, b bool) bool {
		return a && b
	}),
	"Bool.fromString": boolFromStringFn,
	"Bool.implies": boolOp(func(a, b bool) bool {
		return !a || b
	}),
	"Bool.not": notFn,
	"Bool.orelse": boolOp(func(a, b bool) bool {
		return a || b
	}),
	"Bool.toString": boolToStringFn,
	"Char.<":        charOp(func(a, b rune) bool { return a < b }),
	"Char.<=": charOp(func(a, b rune) bool {
		return a <= b
	}),
	"Char.<>": charOp(func(a, b rune) bool {
		return a != b
	}),
	"Char.=": charOp(func(a, b rune) bool { return a == b }),
	"Char.>": charOp(func(a, b rune) bool { return a > b }),
	"Char.>=": charOp(func(a, b rune) bool {
		return a >= b
	}),
	"Char.chr":         chrFn,
	"Char.compare":     Fn(charCompareFn),
	"Char.contains":    charContainsFn(true),
	"Char.fromCString": Fn(charFromCStringFn),
	"Char.fromInt":     Fn(charFromIntFn),
	"Char.fromString":  Fn(charFromStringFn),
	"Char.isAlpha":     charPredicate(isAlphaChar),
	"Char.isAlphaNum":  charPredicate(isAlphaNumChar),
	"Char.isAscii":     charPredicate(isAsciiChar),
	"Char.isCntrl":     charPredicate(isCntrlChar),
	"Char.isDigit":     charPredicate(isDigitChar),
	"Char.isGraph":     charPredicate(isGraphChar),
	"Char.isHexDigit":  charPredicate(isHexDigitChar),
	"Char.isLower":     charPredicate(isLowerChar),
	"Char.isOctDigit":  charPredicate(isOctDigitChar),
	"Char.isPrint":     charPredicate(isPrintChar),
	"Char.isPunct":     charPredicate(isPunctChar),
	"Char.isSpace":     charPredicate(isSpaceChar),
	"Char.isUpper":     charPredicate(isUpperChar),
	"Char.maxChar":     maxCharVal,
	"Char.maxOrd":      maxCharVal,
	"Char.minChar":     minCharVal,
	"Char.notContains": charContainsFn(false),
	"Char.ord":         ordFn,
	"Char.pred":        Fn(charPredFn),
	"Char.succ":        Fn(charSuccFn),
	"Char.toCString":   Fn(charToCStringFn),
	"Char.toLower":     Fn(charToLowerFn),
	"Char.toString":    Fn(charToStringFn),
	"Char.toUpper":     Fn(charToUpperFn),
	"Either.app":       Fn(eitherAppFn),
	"Either.appLeft":   eitherAppSide(true),
	"Either.appRight":  eitherAppSide(false),
	"Either.asLeft":    Fn(eitherAsLeftFn),
	"Either.asRight":   Fn(eitherAsRightFn),
	"Either.fold":      Fn(eitherFoldFn),
	"Either.isLeft":    Fn(eitherIsLeftFn),
	"Either.isRight":   Fn(eitherIsRightFn),
	"Either.map":       Fn(eitherMapFn),
	"Either.mapLeft":   eitherMapSide(true),
	"Either.mapRight":  eitherMapSide(false),
	"Either.partition": Fn(eitherPartitionFn),
	"Either.proj":      Fn(eitherProjFn),
	"Fn.apply":         Fn(fnApplyFn),
	"Fn.const":         Fn(fnConstFn),
	"Fn.curry":         Fn(fnCurryFn),
	"Fn.equal":         fnEqual(false),
	"Fn.flip":          Fn(fnFlipFn),
	"Fn.id":            Fn(identityFn),
	"Fn.notEqual":      fnEqual(true),
	"Fn.o":             composeFn,
	"Fn.repeat":        Fn(fnRepeatFn),
	"Fn.uncurry":       Fn(fnUncurryFn),
	"General.before":   beforeFn,
	"General.ignore":   ignoreFn,
	"General.o":        composeFn,
	"Int.*":            arith(mulInt, nil),
	"Int.+":            arith(addInt, nil),
	"Int.-":            arith(subInt, nil),
	"Int.<":            compareFn(func(c int) bool { return c < 0 }),
	"Int.<=": compareFn(func(c int) bool {
		return c <= 0
	}),
	"Int.>": compareFn(func(c int) bool { return c > 0 }),
	"Int.>=": compareFn(func(c int) bool {
		return c >= 0
	}),
	"Int.abs":               absFn,
	"Int.compare":           Fn(intCompareFn),
	"Int.div":               arith(divInt, nil),
	"Int.fmt":               Fn(intFmtFn),
	"Int.fromInt":           Fn(identityFn),
	"Int.fromLarge":         Fn(identityFn),
	"Int.fromString":        Fn(intFromStringFn),
	"Int.max":               Fn(intMaxFn),
	"Int.maxInt":            someVal(int32(math.MaxInt32)),
	"Int.min":               Fn(intMinFn),
	"Int.minInt":            someVal(int32(math.MinInt32)),
	"Int.mod":               arith(modInt, nil),
	"Int.precision":         someVal(intPrecision),
	"Int.quot":              Fn(intQuotFn),
	"Int.rem":               Fn(intRemFn),
	"Int.sameSign":          Fn(intSameSignFn),
	"Int.sign":              Fn(intSignFn),
	"Int.toInt":             Fn(identityFn),
	"Int.toLarge":           Fn(identityFn),
	"Int.toString":          Fn(intToStringFn),
	"Int.~":                 negFn,
	"List.@":                atFn,
	"List.all":              Fn(allFn),
	"List.app":              Fn(appFn),
	"List.collate":          Fn(listCollateFn),
	"List.concat":           Fn(listConcatFn),
	"List.drop":             Fn(dropFn),
	"List.except":           Fn(exceptFn),
	"List.exists":           Fn(existsFn),
	"List.filter":           Fn(filterFn),
	"List.find":             Fn(findFn),
	"List.foldl":            fold(true),
	"List.foldr":            fold(false),
	"List.getItem":          Fn(getItemFn),
	"List.hd":               hdFn,
	"List.intersect":        Fn(intersectFn),
	"List.last":             Fn(lastFn),
	"List.length":           lengthFn,
	"List.map":              mapFn,
	"List.mapPartial":       Fn(mapPartialFn),
	"List.mapi":             Fn(mapiFn),
	"List.nil":              []Val{},
	"List.nth":              Fn(nthFn),
	"List.null":             nullFn,
	"List.only":             Fn(onlyFn),
	"List.partition":        Fn(partitionFn),
	"List.rev":              revFn,
	"List.revAppend":        Fn(revAppendFn),
	"List.tabulate":         Fn(tabulateFn),
	"List.take":             Fn(takeFn),
	"List.tl":               tlFn,
	"ListPair.all":          listPairTest(true),
	"ListPair.allEq":        Fn(listPairAllEqFn),
	"ListPair.app":          listPairApp(false),
	"ListPair.appEq":        listPairApp(true),
	"ListPair.exists":       listPairTest(false),
	"ListPair.foldl":        listPairFold(true, false),
	"ListPair.foldlEq":      listPairFold(true, true),
	"ListPair.foldr":        listPairFold(false, false),
	"ListPair.foldrEq":      listPairFold(false, true),
	"ListPair.map":          listPairMap(false),
	"ListPair.mapEq":        listPairMap(true),
	"ListPair.unzip":        Fn(listPairUnzipFn),
	"ListPair.zip":          listPairZip(false),
	"ListPair.zipEq":        listPairZip(true),
	"Math.acos":             real1(math.Acos),
	"Math.asin":             real1(math.Asin),
	"Math.atan":             real1(math.Atan),
	"Math.atan2":            real2(math.Atan2),
	"Math.cos":              real1(math.Cos),
	"Math.cosh":             real1(math.Cosh),
	"Math.e":                float32(math.E),
	"Math.exp":              real1(math.Exp),
	"Math.ln":               real1(math.Log),
	"Math.log10":            real1(math.Log10),
	"Math.pi":               float32(math.Pi),
	"Math.pow":              Fn(mathPowFn),
	"Math.sin":              real1(math.Sin),
	"Math.sinh":             real1(math.Sinh),
	"Math.sqrt":             real1(math.Sqrt),
	"Math.tan":              real1(math.Tan),
	"Math.tanh":             real1(math.Tanh),
	"Option.app":            optionAppFn,
	"Option.compose":        optionComposeFn,
	"Option.composePartial": optionComposePartialFn,
	"Option.filter":         optionFilterFn,
	"Option.getOpt":         getOptFn,
	"Option.isSome":         isSomeFn,
	"Option.join":           optionJoinFn,
	"Option.map":            optionMapFn,
	"Option.mapPartial":     optionMapPartialFn,
	"Option.valOf":          valOfFn,
	"Range.complement":      Fn(rangeComplementFn),
	"Range.contains":        rangeContainsFn,
	"Range.continuousSetOf": Fn(rangeContinuousSetOfFn),
	"Range.discreteSetOf":   Fn(rangeDiscreteSetOfFn),
	"Range.flatten":         Fn(rangeFlattenFn),
	"Range.ranges":          Fn(rangeRangesFn),
	"Range.toBag":           Fn(rangeToBagFn),
	"Range.toList":          Fn(rangeToListFn),
	"Real.*":                arith(nil, mulReal),
	"Real.+":                arith(nil, addReal),
	"Real.-":                arith(nil, subReal),
	"Real./":                arith(nil, divReal),
	"Real.<":                compareFn(func(c int) bool { return c < 0 }),
	"Real.<=":               compareFn(func(c int) bool { return c <= 0 }),
	"Real.<>": realPairPredicate(func(a, b float64) bool {
		return a != b || math.IsNaN(a) || math.IsNaN(b)
	}),
	"Real.=": realPairPredicate(func(a, b float64) bool {
		return a == b
	}),
	"Real.>":          compareFn(func(c int) bool { return c > 0 }),
	"Real.>=":         compareFn(func(c int) bool { return c >= 0 }),
	"Real.abs":        absFn,
	"Real.ceil":       realToIntFn(math.Ceil),
	"Real.checkFloat": Fn(realCheckFloatFn),
	"Real.compare":    Fn(realCompareFn),
	"Real.copySign":   real2(realCopySign),
	"Real.floor":      realToIntFn(math.Floor),
	"Real.fmt":        Fn(realFmtFn),
	"Real.fromInt":    Fn(realFromIntFn),
	"Real.fromManExp": Fn(realFromManExpFn),
	"Real.fromString": Fn(realFromStringFn),
	"Real.isFinite": realPredicate(func(a float64) bool {
		return !math.IsInf(a, 0) && !math.IsNaN(a)
	}),
	"Real.isNan": realPredicate(math.IsNaN),
	"Real.isNormal": realPredicate(func(a float64) bool {
		return !math.IsNaN(a) && !math.IsInf(a, 0) &&
			math.Abs(a) >= minNormal
	}),
	"Real.max":          Fn(realMaxFn),
	"Real.maxFinite":    float32(math.MaxFloat32),
	"Real.min":          Fn(realMinFn),
	"Real.minNormalPos": float32(minNormal),
	"Real.minPos":       float32(math.SmallestNonzeroFloat32),
	"Real.negInf":       float32(math.Inf(-1)),
	"Real.posInf":       float32(math.Inf(1)),
	"Real.precision":    realPrecision,
	"Real.radix":        realRadix,
	"Real.realCeil":     real1(math.Ceil),
	"Real.realFloor":    real1(math.Floor),
	"Real.realMod":      Fn(realModFn),
	"Real.realRound":    real1(math.RoundToEven),
	"Real.realTrunc":    Fn(realTruncFn),
	"Real.rem":          real2(math.Mod),
	"Real.round":        realToIntFn(math.RoundToEven),
	"Real.sameSign": realPairPredicate(func(a, b float64) bool {
		return realIsNegative(a) == realIsNegative(b)
	}),
	"Real.sign":     Fn(realSignFn),
	"Real.signBit":  realPredicate(realIsNegative),
	"Real.split":    Fn(realSplitFn),
	"Real.toManExp": Fn(realToManExpFn),
	"Real.toString": Fn(realToStringFn),
	"Real.trunc":    realToIntFn(math.Trunc),
	"Real.unordered": realPairPredicate(func(a, b float64) bool {
		return math.IsNaN(a) || math.IsNaN(b)
	}),
	"Real.~": negFn,
	"String.<": stringOp(func(a, b string) bool {
		return a < b
	}),
	"String.<=": stringOp(func(a, b string) bool {
		return a <= b
	}),
	"String.<>": stringOp(func(a, b string) bool {
		return a != b
	}),
	"String.=": stringOp(func(a, b string) bool {
		return a == b
	}),
	"String.>": stringOp(func(a, b string) bool {
		return a > b
	}),
	"String.>=": stringOp(func(a, b string) bool {
		return a >= b
	}),
	"String.^":           caretFn,
	"String.collate":     Fn(stringCollateFn),
	"String.compare":     Fn(stringCompareFn),
	"String.concat":      concatFn,
	"String.concatWith":  Fn(stringConcatWithFn),
	"String.explode":     explodeFn,
	"String.extract":     Fn(stringExtractFn),
	"String.fields":      stringSplit(true),
	"String.implode":     implodeFn,
	"String.isPrefix":    Fn(stringIsPrefixFn),
	"String.isSubstring": Fn(stringIsSubstringFn),
	"String.isSuffix":    Fn(stringIsSuffixFn),
	"String.map":         Fn(stringMapFn),
	"String.maxSize":     stringMaxSize,
	"String.size":        sizeFn,
	"String.str":         strFn,
	"String.sub":         Fn(stringSubFn),
	"String.substring":   Fn(stringSubstringFn),
	"String.tokens":      stringSplit(false),
	"String.translate":   Fn(stringTranslateFn),
	"StringCvt.padLeft":  Curry3(stringCvtPadLeftFn),
	"StringCvt.padRight": Curry3(stringCvtPadRightFn),
	"Sys.parseTree":      parseTree,
	"Vector.all":         Fn(allFn),
	"Vector.app":         Fn(appFn),
	"Vector.appi":        Fn(vectorAppiFn),
	"Vector.collate":     Fn(listCollateFn),
	"Vector.concat":      Fn(listConcatFn),
	"Vector.exists":      Fn(existsFn),
	"Vector.find":        Fn(findFn),
	"Vector.findi":       Fn(vectorFindiFn),
	"Vector.foldl":       fold(true),
	"Vector.foldli":      vectorFoldi(true),
	"Vector.foldr":       fold(false),
	"Vector.foldri":      vectorFoldi(false),
	"Vector.fromList":    Fn(vectorFromListFn),
	"Vector.length":      lengthFn,
	"Vector.map":         mapFn,
	"Vector.mapi":        Fn(vectorMapiFn),
	"Vector.maxLen":      vectorMaxLen,
	"Vector.sub":         Fn(vectorSubFn),
	"Vector.tabulate":    Fn(tabulateFn),
	"Vector.update":      Fn(vectorUpdateFn),
	"Word.*":             word2(func(a, b uint64) uint64 { return a * b }),
	"Word.+":             word2(func(a, b uint64) uint64 { return a + b }),
	"Word.-":             word2(func(a, b uint64) uint64 { return a - b }),
	"Word.<":             wordCmp(func(a, b uint64) bool { return a < b }),
	"Word.<<":            word2(func(a, b uint64) uint64 { return a << b }),
	"Word.<=":            wordCmp(func(a, b uint64) bool { return a <= b }),
	"Word.>":             wordCmp(func(a, b uint64) bool { return a > b }),
	"Word.>=":            wordCmp(func(a, b uint64) bool { return a >= b }),
	"Word.>>":            word2(func(a, b uint64) uint64 { return a >> b }),
	"Word.andb":          word2(func(a, b uint64) uint64 { return a & b }),
	"Word.compare":       Fn(wordCompareFn),
	"Word.div":           Fn(wordDivFn),
	"Word.fmt":           Fn(wordFmtFn),
	"Word.fromInt":       Fn(wordFromIntFn),
	"Word.fromLarge":     Fn(identityFn),
	"Word.fromLargeInt":  Fn(wordFromIntFn),
	"Word.fromLargeWord": Fn(identityFn),
	"Word.fromString":    Fn(wordFromStringFn),
	"Word.max":           word2(func(a, b uint64) uint64 { return max(a, b) }),
	"Word.min":           word2(func(a, b uint64) uint64 { return min(a, b) }),
	"Word.mod":           Fn(wordModFn),
	"Word.notb":          Fn(wordNotbFn),
	"Word.orb":           word2(func(a, b uint64) uint64 { return a | b }),
	"Word.toInt":         Fn(wordToIntFn),
	"Word.toIntX":        Fn(wordToIntXFn),
	"Word.toLarge":       Fn(identityFn),
	"Word.toLargeInt":    Fn(wordToIntFn),
	"Word.toLargeIntX":   Fn(wordToIntXFn),
	"Word.toLargeWord":   Fn(identityFn),
	"Word.toLargeWordX":  Fn(identityFn),
	"Word.toLargeX":      Fn(identityFn),
	"Word.toString":      Fn(wordToStringFn),
	"Word.wordSize":      wordSize,
	"Word.xorb":          word2(func(a, b uint64) uint64 { return a ^ b }),
	"Word.~":             Fn(wordNegFn),
	"Word.~>>":           Fn(wordAshrFn),
	"abs":                absFn,
	"app":                Fn(appFn),
	"bag":                Fn(bagFromListFn),
	"ceil":               realToIntFn(math.Ceil),
	"chr":                chrFn,
	"concat":             concatFn,
	"explode":            explodeFn,
	"fields":             stringSplit(true),
	"floor":              realToIntFn(math.Floor),
	"foldl":              fold(true),
	"foldr":              fold(false),
	"getOpt":             getOptFn,
	"hd":                 hdFn,
	"ignore":             ignoreFn,
	"implode":            implodeFn,
	"isSome":             isSomeFn,
	"length":             lengthFn,
	"map":                mapFn,
	"not":                notFn,
	"null":               nullFn,
	"op *":               arithW(mulInt, mulReal, mulWord),
	"op +":               arithW(addInt, addReal, addWord),
	"op -":               arithW(subInt, subReal, subWord),
	"op /":               arith(nil, divReal),
	"op ::":              consFn,
	"op <":               compareFn(func(c int) bool { return c < 0 }),
	"op <=":              compareFn(func(c int) bool { return c <= 0 }),
	"op <>":              equalFn(true),
	"op =":               equalFn(false),
	"op >":               compareFn(func(c int) bool { return c > 0 }),
	"op >=":              compareFn(func(c int) bool { return c >= 0 }),
	"op @":               atFn,
	"op ^":               caretFn,
	"op div":             arithW(divInt, nil, divWord),
	"op mod":             arithW(modInt, nil, modWord),
	"op o":               composeFn,
	"op ~":               negFn,
	"ord":                ordFn,
	"real":               Fn(realFromIntFn),
	"rev":                revFn,
	"round":              realToIntFn(math.RoundToEven),
	"size":               sizeFn,
	"str":                strFn,
	"substring":          Fn(stringSubstringFn),
	"tl":                 tlFn,
	"tokens":             stringSplit(false),
	"trunc":              realToIntFn(math.Trunc),
	"valOf":              valOfFn,
	"vector":             Fn(vectorFromListFn),
}

// Precisions and radixes of the numeric types.
const (
	intPrecision  = int32(32)
	realPrecision = int32(24)
	realRadix     = int32(2)
)

// unitVal is the unit value.
var unitVal = core.Unit{}

// boolToStringFn is "Bool.toString b".
func boolToStringFn(arg Val) (Val, error) {
	if asBool(arg) {
		return "true", nil
	}
	return "false", nil
}

// boolFromStringFn is "Bool.fromString s".
func boolFromStringFn(arg Val) (Val, error) {
	switch asString(arg) {
	case "false":
		return someVal(false), nil
	case "true":
		return someVal(true), nil
	default:
		return noneVal, nil
	}
}

// boolOp adapts a binary bool function to a built-in. (As a
// function value, "Bool.andalso" evaluates both operands; only
// the infix form short-circuits.)
func boolOp(f func(a, b bool) bool) Fn {
	return func(arg Val) (Val, error) {
		a, b := asPair(arg)
		return f(asBool(a), asBool(b)), nil
	}
}

// composeFn is "f o g".
func composeFn(arg Val) (Val, error) {
	f, g := asPair(arg)
	return Fn(func(a Val) (Val, error) {
		v, err := ApplyVal(g, a)
		if err != nil {
			return nil, err
		}
		return ApplyVal(f, v)
	}), nil
}

// beforeFn is "a before b": a, discarding b.
func beforeFn(arg Val) (Val, error) {
	a, _ := asPair(arg)
	return a, nil
}

// ignoreFn is "ignore a".
func ignoreFn(Val) (Val, error) {
	return unitVal, nil
}

// The scalar accessors panic on the wrong type: built-in
// arguments are guaranteed by type inference.

func asBool(v Val) bool {
	b, ok := v.(bool)
	if !ok {
		panic(fmt.Sprintf("expected bool, got %T", v))
	}
	return b
}

func asChar(v Val) rune {
	c, ok := v.(rune)
	if !ok {
		panic(fmt.Sprintf("expected char, got %T", v))
	}
	return c
}

func asInt(v Val) int32 {
	i, ok := v.(int32)
	if !ok {
		panic(fmt.Sprintf("expected int, got %T", v))
	}
	return i
}

func asString(v Val) string {
	s, ok := v.(string)
	if !ok {
		panic(fmt.Sprintf("expected string, got %T", v))
	}
	return s
}

// absFn is "abs x". It is overloaded on int and real, so it
// switches on the runtime type.
func absFn(arg Val) (Val, error) {
	switch v := arg.(type) {
	case int32:
		if v < 0 {
			// -minInt does not fit in int, so raise Overflow.
			return checkIntRange(-int64(v))
		}
		return v, nil
	case float32:
		return float32(math.Abs(float64(v))), nil
	default:
		panic(fmt.Sprintf("expected int or real, got %T", arg))
	}
}

// chrFn is "chr i", the character with code i.
func chrFn(arg Val) (Val, error) {
	i := asInt(arg)
	if i < 0 || i > maxCharVal {
		return nil, &MorelError{Exn: ExnChr}
	}
	// rune and int32 are one type; the result is a char only
	// statically.
	return i, nil
}

// arith adapts a pair of binary implementations, one per numeric
// type, to a built-in that dispatches on its operands' runtime
// type.
func arith(intFn func(a, b int32) (Val, error),
	realFn func(a, b float32) (Val, error),
) Fn {
	return func(arg Val) (Val, error) {
		vals, ok := arg.([]Val)
		if !ok || len(vals) != 2 {
			panic(fmt.Sprintf("expected pair, got %T", arg))
		}
		switch a := vals[0].(type) {
		case int32:
			return intFn(a, asInt(vals[1]))
		case float32:
			f, isReal := vals[1].(float32)
			if !isReal {
				panic(fmt.Sprintf("expected real, got %T",
					vals[1]))
			}
			return realFn(a, f)
		default:
			panic(fmt.Sprintf("expected int or real, got %T",
				vals[0]))
		}
	}
}

// arithW is arith extended to words, for the operators overloaded
// over int, real, and word (+, -, *, div, mod).
func arithW(intFn func(a, b int32) (Val, error),
	realFn func(a, b float32) (Val, error),
	wordFn func(a, b uint64) (Val, error),
) Fn {
	base := arith(intFn, realFn)
	return func(arg Val) (Val, error) {
		vals, ok := arg.([]Val)
		if !ok || len(vals) != 2 {
			panic(fmt.Sprintf("expected pair, got %T", arg))
		}
		if a, isWord := vals[0].(uint64); isWord {
			return wordFn(a, asWord(vals[1]))
		}
		return base(arg)
	}
}

// checkIntRange rejects a result outside int32, raising
// Overflow as SML integer arithmetic does.
func checkIntRange(i int64) (Val, error) {
	if i < math.MinInt32 || i > math.MaxInt32 {
		return nil, &MorelError{Exn: ExnOverflow}
	}
	return int32(i), nil
}

func addInt(a, b int32) (Val, error) {
	return checkIntRange(int64(a) + int64(b))
}

func subInt(a, b int32) (Val, error) {
	return checkIntRange(int64(a) - int64(b))
}

func mulInt(a, b int32) (Val, error) {
	return checkIntRange(int64(a) * int64(b))
}

// divInt is SML's "div": floor division, rounding toward
// negative infinity (unlike Go's, which rounds toward zero).
func divInt(a, b int32) (Val, error) {
	if b == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	q := int64(a) / int64(b)
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return checkIntRange(q)
}

// modInt is SML's "mod": the remainder of floor division, with
// the divisor's sign.
func modInt(a, b int32) (Val, error) {
	if b == 0 {
		return nil, &MorelError{Exn: ExnDiv}
	}
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r, nil
}

// The real operations compute in float64 and round once to
// float32.

func addReal(a, b float32) (Val, error) {
	return float32(float64(a) + float64(b)), nil
}

func subReal(a, b float32) (Val, error) {
	return float32(float64(a) - float64(b)), nil
}

func mulReal(a, b float32) (Val, error) {
	return float32(float64(a) * float64(b)), nil
}

func divReal(a, b float32) (Val, error) {
	return float32(float64(a) / float64(b)), nil
}

// negFn is "~ x". It is overloaded on int, real, and word, so it
// switches on the runtime type.
func negFn(arg Val) (Val, error) {
	// lint: sort until '^\t}' where '^\tcase '
	switch v := arg.(type) {
	case float32:
		return -v, nil
	case int32:
		if v == math.MinInt32 {
			return nil, &MorelError{Exn: ExnOverflow}
		}
		return -v, nil
	case uint64:
		return -v, nil
	default:
		panic(fmt.Sprintf("expected int, real, or word, got %T",
			arg))
	}
}

// notFn is "not b".
func notFn(arg Val) (Val, error) {
	return !asBool(arg), nil
}

// ordFn is "ord c", the character code of c.
func ordFn(arg Val) (Val, error) {
	return asChar(arg), nil
}

// sizeFn is "size s", the number of characters in s.
func sizeFn(arg Val) (Val, error) {
	//nolint:gosec // a string's length fits in an int.
	return int32(len(asString(arg))), nil
}

// strFn is "str c", the single-character string containing c.
func strFn(arg Val) (Val, error) {
	return string(asChar(arg)), nil
}

// parseTree parses its argument as a declaration or expression
// and returns the S-expression form of the parse tree.
func parseTree(arg Val) (Val, error) {
	s, ok := arg.(string)
	if !ok {
		panic("parseTree: argument is not a string")
	}
	n, err := parse.DeclOrExpr("parseTree", s)
	if err != nil {
		return nil, err
	}
	return ast.Dump(n), nil
}
