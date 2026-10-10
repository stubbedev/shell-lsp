package lint

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"
)

type testOperatorKind uint8

const (
	notTestOperator testOperatorKind = iota
	unaryTest
	equalityTest
	regexTest
	strictOrderTest
	inclusiveOrderTest
	arithmeticTest
	fileComparisonTest
)

type testOperator struct {
	kind   testOperatorKind
	unary  syntax.UnTestOperator
	binary syntax.BinTestOperator
}

const (
	testAnd = "-a"
	testOr  = "-o"
)

var testOperators = map[string]testOperator{
	"-a":  {kind: unaryTest, unary: syntax.TsExists},
	"-b":  {kind: unaryTest, unary: syntax.TsBlckSp},
	"-c":  {kind: unaryTest, unary: syntax.TsCharSp},
	"-d":  {kind: unaryTest, unary: syntax.TsDirect},
	"-e":  {kind: unaryTest, unary: syntax.TsExists},
	"-f":  {kind: unaryTest, unary: syntax.TsRegFile},
	"-g":  {kind: unaryTest, unary: syntax.TsGIDSet},
	"-h":  {kind: unaryTest, unary: syntax.TsSmbLink},
	"-k":  {kind: unaryTest, unary: syntax.TsSticky},
	"-n":  {kind: unaryTest, unary: syntax.TsNempStr},
	"-o":  {kind: unaryTest, unary: syntax.TsOptSet},
	"-p":  {kind: unaryTest, unary: syntax.TsNmPipe},
	"-r":  {kind: unaryTest, unary: syntax.TsRead},
	"-s":  {kind: unaryTest, unary: syntax.TsNoEmpty},
	"-t":  {kind: unaryTest, unary: syntax.TsFdTerm},
	"-u":  {kind: unaryTest, unary: syntax.TsUIDSet},
	"-v":  {kind: unaryTest, unary: syntax.TsVarSet},
	"-w":  {kind: unaryTest, unary: syntax.TsWrite},
	"-x":  {kind: unaryTest, unary: syntax.TsExec},
	"-z":  {kind: unaryTest, unary: syntax.TsEmpStr},
	"-G":  {kind: unaryTest, unary: syntax.TsGrpOwn},
	"-L":  {kind: unaryTest, unary: syntax.TsSmbLink},
	"-N":  {kind: unaryTest, unary: syntax.TsModif},
	"-O":  {kind: unaryTest, unary: syntax.TsUsrOwn},
	"-R":  {kind: unaryTest, unary: syntax.TsRefVar},
	"-S":  {kind: unaryTest, unary: syntax.TsSocket},
	"=":   {kind: equalityTest, binary: syntax.TsMatchShort},
	"==":  {kind: equalityTest, binary: syntax.TsMatch},
	"!=":  {kind: equalityTest, binary: syntax.TsNoMatch},
	"=~":  {kind: regexTest, binary: syntax.TsReMatch},
	"<":   {kind: strictOrderTest, binary: syntax.TsBefore},
	">":   {kind: strictOrderTest, binary: syntax.TsAfter},
	"<=":  {kind: inclusiveOrderTest},
	">=":  {kind: inclusiveOrderTest},
	"-eq": {kind: arithmeticTest, binary: syntax.TsEql},
	"-ne": {kind: arithmeticTest, binary: syntax.TsNeq},
	"-lt": {kind: arithmeticTest, binary: syntax.TsLss},
	"-le": {kind: arithmeticTest, binary: syntax.TsLeq},
	"-gt": {kind: arithmeticTest, binary: syntax.TsGtr},
	"-ge": {kind: arithmeticTest, binary: syntax.TsGeq},
	"-nt": {kind: fileComparisonTest, binary: syntax.TsNewer},
	"-ot": {kind: fileComparisonTest, binary: syntax.TsOlder},
	"-ef": {kind: fileComparisonTest, binary: syntax.TsDevIno},
}

func (o testOperator) isBinary() bool { return o.kind > unaryTest }

func (o testOperator) isOrdering() bool {
	return o.kind == strictOrderTest || o.kind == inclusiveOrderTest
}

func (o testOperator) treeBinary() (syntax.BinTestOperator, bool) {
	return o.binary, o.binary != 0
}

func isUnaryTestOperator(name string) bool { return testOperators[name].kind == unaryTest }

func isBinaryTestOperator(name string) bool { return testOperators[name].isBinary() }

var (
	testBinarySpellings = binaryTestSpellings()
	testSymbolOperators = slices.DeleteFunc(slices.Clone(testBinarySpellings), func(op string) bool { return op[0] == '-' })
)

func binaryTestSpellings() []string {
	var out []string
	for name, op := range testOperators {
		if !op.isBinary() {
			continue
		}
		out = append(out, name)
		if op.isOrdering() {
			out = append(out, `\`+name)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b)) })
	return out
}

const unicodeDashes = "֊־‐‑‒–—―﹣－"

func leadingUnicodeDash(s string) int {
	r, size := utf8.DecodeRuneInString(s)
	if !strings.ContainsRune(unicodeDashes, r) {
		return 0
	}
	return size
}

func isDashOperator(s string) bool {
	dash := leadingUnicodeDash(s)
	if strings.HasPrefix(s, "-") {
		dash = 1
	}
	return dash > 0 && dash < len(s)
}

func dashWord(text string) string {
	if !strings.HasPrefix(text, "-") {
		return ""
	}
	letters := leadingLetters(text[1:])
	if letters == "" {
		return ""
	}
	return text[:1+len(letters)]
}

func isOperatorShaped(lit string) bool {
	return lit != testAnd && lit != testOr && lit != "" && dashWord(lit) == lit
}
