package awk

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
)

type kind uint8

const (
	tEOF kind = iota
	tIllegal
	tNewline
	tSemicolon
	tLBrace
	tRBrace
	tLParen
	tRParen
	tLBracket
	tRBracket
	tComma
	tNumber
	tString
	tRegexp
	tName
	tFuncName
	tBuiltinFunc
	tBegin
	tEnd
	tBeginFile
	tEndFile
	tFunction
	tIf
	tElse
	tWhile
	tFor
	tDo
	tBreak
	tContinue
	tNext
	tNextFile
	tExit
	tReturn
	tDelete
	tGetline
	tPrint
	tPrintf
	tIn
	tSwitch
	tCase
	tDefault
	tAdd
	tSub
	tMul
	tDiv
	tMod
	tPow
	tNot
	tLess
	tLessEq
	tGreater
	tGreaterEq
	tEqual
	tNotEqual
	tMatch
	tNoMatch
	tAnd
	tOr
	tQuestion
	tColon
	tAssign
	tAddAssign
	tSubAssign
	tMulAssign
	tDivAssign
	tModAssign
	tPowAssign
	tIncr
	tDecr
	tDollar
	tPipe
	tPipeBoth
	tAppend
	tAt
)

var keywords = map[string]kind{
	"BEGIN": tBegin, "END": tEnd, "BEGINFILE": tBeginFile, "ENDFILE": tEndFile,
	"function": tFunction, "func": tFunction,
	"if": tIf, "else": tElse, "while": tWhile, "for": tFor, "do": tDo,
	"break": tBreak, "continue": tContinue, "next": tNext, "nextfile": tNextFile,
	"exit": tExit, "return": tReturn, "delete": tDelete, "getline": tGetline,
	"print": tPrint, "printf": tPrintf, "in": tIn,
	"switch": tSwitch, "case": tCase, "default": tDefault,
}

type operator struct {
	text string
	kind kind
}

var operators = [256][]operator{
	'+': {{"+=", tAddAssign}, {"++", tIncr}, {"+", tAdd}},
	'-': {{"-=", tSubAssign}, {"--", tDecr}, {"-", tSub}},
	'*': {{"**=", tPowAssign}, {"**", tPow}, {"*=", tMulAssign}, {"*", tMul}},
	'/': {{"/=", tDivAssign}, {"/", tDiv}},
	'%': {{"%=", tModAssign}, {"%", tMod}},
	'^': {{"^=", tPowAssign}, {"^", tPow}},
	'!': {{"!=", tNotEqual}, {"!~", tNoMatch}, {"!", tNot}},
	'<': {{"<=", tLessEq}, {"<", tLess}},
	'>': {{">=", tGreaterEq}, {">>", tAppend}, {">", tGreater}},
	'=': {{"==", tEqual}, {"=", tAssign}},
	'~': {{"~", tMatch}},
	'&': {{"&&", tAnd}},
	'|': {{"||", tOr}, {"|&", tPipeBoth}, {"|", tPipe}},
	'?': {{"?", tQuestion}},
	':': {{":", tColon}},
	'$': {{"$", tDollar}},
	'@': {{"@", tAt}},
	'{': {{"{", tLBrace}},
	'}': {{"}", tRBrace}},
	'(': {{"(", tLParen}},
	')': {{")", tRParen}},
	'[': {{"[", tLBracket}},
	']': {{"]", tRBracket}},
	',': {{",", tComma}},
	';': {{";", tSemicolon}},
}

func isPunctuation(k kind) bool {
	switch k {
	case tLBrace, tRBrace, tLParen, tRParen, tLBracket, tRBracket, tComma, tSemicolon:
		return true
	}
	return false
}

type class uint8

const (
	keywordClass class = iota
	functionClass
	variableClass
)

type word struct {
	class class
	kind  kind
}

var words = func() map[string]word {
	out := make(map[string]word, len(keywords)+len(functions)+len(variables))
	for name := range variables {
		out[name] = word{class: variableClass}
	}
	for name := range functions {
		out[name] = word{class: functionClass}
	}
	for name, k := range keywords {
		out[name] = word{class: keywordClass, kind: k}
	}
	return out
}()

type token struct {
	kind       kind
	start, end int
	sem        int
}

type lexer struct {
	src  string
	pos  int
	prev kind
	res  *lang.Result
}

func (l *lexer) next() token {
	t := l.scan()
	l.prev = t.kind
	return t
}

func (l *lexer) scan() token {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.pos++
		case c == '\\' && l.continuation():
		case c == '#':
			start := l.pos
			l.pos = lineEnd(l.src, l.pos)
			l.res.Emit(start, l.pos, lang.Comment, 0)
		default:
			return l.token(c)
		}
	}
	return token{kind: tEOF, start: len(l.src), end: len(l.src), sem: -1}
}

func (l *lexer) continuation() bool {
	rest := l.src[l.pos+1:]
	switch {
	case strings.HasPrefix(rest, "\n"):
		l.pos += 2
	case strings.HasPrefix(rest, "\r\n"):
		l.pos += 3
	default:
		return false
	}
	return true
}

func lineEnd(src string, from int) int {
	if i := strings.IndexByte(src[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(src)
}

func (l *lexer) token(c byte) token {
	start := l.pos
	switch {
	case c == '\n':
		l.pos++
		return token{kind: tNewline, start: start, end: l.pos, sem: -1}
	case c == '"':
		return l.quoted(tString, lang.String, '"', "unterminated string")
	case c == '/' && l.regexpAllowed():
		return l.quoted(tRegexp, lang.Regexp, '/', "unterminated regular expression")
	case lang.IsDigit(c) || (c == '.' && start+1 < len(l.src) && lang.IsDigit(l.src[start+1])):
		l.number()
		return token{kind: tNumber, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, lang.Number, 0)}
	case lang.IsNameStart(c):
		return l.name()
	}
	for _, op := range operators[c] {
		if strings.HasPrefix(l.src[start:], op.text) {
			l.pos += len(op.text)
			sem := -1
			if !isPunctuation(op.kind) {
				sem = l.res.Emit(start, l.pos, lang.Operator, 0)
			}
			return token{kind: op.kind, start: start, end: l.pos, sem: sem}
		}
	}
	l.pos = lang.RuneEnd(l.src, start)
	l.res.Fail(start, l.pos, lang.Illegal(l.src, start, l.pos))
	return token{kind: tIllegal, start: start, end: l.pos, sem: -1}
}

func (l *lexer) regexpAllowed() bool {
	switch l.prev {
	case tName, tFuncName, tBuiltinFunc, tNumber, tString, tRegexp, tRParen, tRBracket, tDollar, tIncr, tDecr, tGetline:
		return false
	}
	return true
}

func (l *lexer) quoted(k kind, sem lang.Kind, closer byte, unterminated string) token {
	start := l.pos
	l.pos++
	inBracket := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			return l.unterminated(k, sem, start, unterminated)
		case c == '\\':
			l.pos += 2
			continue
		case closer == '/' && c == '[' && !inBracket:
			inBracket = true
			l.pos = bracketBody(l.src, l.pos+1)
			continue
		case closer == '/' && c == '[' && l.pos+1 < len(l.src) && strings.IndexByte(":.=", l.src[l.pos+1]) >= 0:
			if end := strings.Index(l.src[l.pos+2:], l.src[l.pos+1:l.pos+2]+"]"); end >= 0 && !strings.Contains(l.src[l.pos+2:l.pos+2+end], "\n") {
				l.pos += end + 4
				continue
			}
		case closer == '/' && c == ']':
			inBracket = false
		case c == closer && !inBracket:
			l.pos++
			return token{kind: k, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, sem, 0)}
		}
		l.pos++
	}
	l.pos = len(l.src)
	return l.unterminated(k, sem, start, unterminated)
}

func bracketBody(src string, at int) int {
	if at < len(src) && src[at] == '^' {
		at++
	}
	if at < len(src) && src[at] == ']' {
		at++
	}
	return at
}

func (l *lexer) unterminated(k kind, sem lang.Kind, start int, message string) token {
	l.pos = min(l.pos, len(l.src))
	l.res.Fail(start, l.pos, message)
	return token{kind: k, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, sem, 0)}
}

func (l *lexer) number() {
	src := l.src
	if src[l.pos] == '0' && l.pos+2 < len(src) && src[l.pos+1]|0x20 == 'x' && lang.IsHexDigit(src[l.pos+2]) {
		l.pos += 2
		for l.pos < len(src) && lang.IsHexDigit(src[l.pos]) {
			l.pos++
		}
		return
	}
	l.digits()
	if l.pos < len(src) && src[l.pos] == '.' {
		l.pos++
		l.digits()
	}
	if l.pos < len(src) && src[l.pos]|0x20 == 'e' {
		at := l.pos + 1
		if at < len(src) && (src[at] == '+' || src[at] == '-') {
			at++
		}
		if at < len(src) && lang.IsDigit(src[at]) {
			l.pos = at
			l.digits()
		}
	}
}

func (l *lexer) digits() {
	for l.pos < len(l.src) && lang.IsDigit(l.src[l.pos]) {
		l.pos++
	}
}

func (l *lexer) name() token {
	start := l.pos
	l.scanName()
	for strings.HasPrefix(l.src[l.pos:], "::") && l.pos+2 < len(l.src) && lang.IsNameStart(l.src[l.pos+2]) {
		l.pos += 2
		l.scanName()
	}
	name := l.src[start:l.pos]
	w, known := words[name]
	switch {
	case known && w.class == keywordClass:
		return token{kind: w.kind, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, lang.Keyword, 0)}
	case known && w.class == functionClass && (name == "length" || l.callFollows()):
		return token{kind: tBuiltinFunc, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, lang.Function, lang.Builtin)}
	case l.pos < len(l.src) && l.src[l.pos] == '(':
		return token{kind: tFuncName, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, lang.Function, 0)}
	case known && w.class == variableClass:
		return token{kind: tName, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, lang.Variable, lang.Builtin)}
	}
	return token{kind: tName, start: start, end: l.pos, sem: l.res.Emit(start, l.pos, lang.Variable, 0)}
}

func (l *lexer) callFollows() bool {
	rest := strings.TrimLeft(l.src[l.pos:], " \t")
	return rest != "" && rest[0] == '('
}

func (l *lexer) scanName() {
	for l.pos < len(l.src) && lang.IsNameChar(l.src[l.pos]) {
		l.pos++
	}
}
