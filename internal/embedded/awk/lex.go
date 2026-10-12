package awk

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/embedded/regex"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

type kind uint8

const (
	tEOF kind = iota
	tIllegal
	tUnterminated
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
	tContextual
)

var keywords = map[string]kind{
	"BEGIN": tBegin, "END": tEnd, "function": tFunction,
	"if": tIf, "else": tElse, "while": tWhile, "for": tFor, "do": tDo,
	"break": tBreak, "continue": tContinue, "next": tNext, "nextfile": tNextFile,
	"exit": tExit, "return": tReturn, "delete": tDelete, "getline": tGetline,
	"print": tPrint, "printf": tPrintf, "in": tIn,
}

var operators = lang.NewOperators(map[string]kind{
	"+=": tAddAssign, "++": tIncr, "+": tAdd,
	"-=": tSubAssign, "--": tDecr, "-": tSub,
	"**=": tPowAssign, "**": tPow, "*=": tMulAssign, "*": tMul,
	"/=": tDivAssign, "/": tDiv,
	"%=": tModAssign, "%": tMod,
	"^=": tPowAssign, "^": tPow,
	"!=": tNotEqual, "!~": tNoMatch, "!": tNot,
	"<=": tLessEq, "<": tLess,
	">=": tGreaterEq, ">>": tAppend, ">": tGreater,
	"==": tEqual, "=": tAssign,
	"~": tMatch, "&&": tAnd,
	"||": tOr, "|&": tPipeBoth, "|": tPipe,
	"?": tQuestion, ":": tColon, "$": tDollar, "@": tAt,
}, map[string]kind{
	"{": tLBrace, "}": tRBrace, "(": tLParen, ")": tRParen, "[": tLBracket, "]": tRBracket, ",": tComma, ";": tSemicolon,
})

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

type token = lang.Lexeme[kind]

type lexer struct {
	src   string
	pos   int
	prev  kind
	res   *lang.Result
	quiet bool
}

func (l *lexer) fail(start, end int, message string) {
	if !l.quiet {
		l.res.Fail(start, end, message)
	}
}

func (l *lexer) next() token {
	t := l.scan()
	l.prev = t.Kind
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
			l.pos = textline.End(l.src, l.pos)
			l.res.Emit(start, l.pos, lang.Comment, 0)
		default:
			return l.token(c)
		}
	}
	return token{Kind: tEOF, Start: len(l.src), End: len(l.src)}
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

func (l *lexer) token(c byte) token {
	start := l.pos
	switch {
	case c == '\n':
		l.pos++
		return token{Kind: tNewline, Start: start, End: l.pos}
	case c == '"':
		return l.quoted(tString, lang.String, '"', "unterminated string")
	case c == '/' && l.regexpAllowed():
		return l.quoted(tRegexp, lang.Regexp, '/', "unterminated regular expression")
	case lang.StartsNumber(l.src, start):
		l.pos = scanNumber(l.src, start)
		return l.emit(tNumber, start, lang.Number, 0)
	case lang.IsNameStart(c):
		return l.name()
	}
	if t, ok := operators.Scan(l.res, l.src, start); ok {
		l.pos = t.End
		return t
	}
	l.pos = lang.RuneEnd(l.src, start)
	l.fail(start, l.pos, lang.Illegal(l.src, start, l.pos))
	return token{Kind: tIllegal, Start: start, End: l.pos}
}

func (l *lexer) emit(k kind, start int, sem lang.Kind, mods lang.Modifier) token {
	return token{Kind: k, Start: start, End: l.pos, Sem: l.res.Emit(start, l.pos, sem, mods)}
}

func (l *lexer) regexpAllowed() bool {
	switch l.prev {
	case tName, tFuncName, tBuiltinFunc, tNumber, tString, tRegexp, tRParen, tRBracket, tDollar, tIncr, tDecr, tGetline:
		return false
	default:
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
			return l.unterminated(sem, start, unterminated)
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
			if k == tRegexp && !l.quiet {
				l.res.CheckRegex(lang.Verbatim(l.src, start+1, l.pos-1), regex.Awk)
			}
			return l.emit(k, start, sem, 0)
		}
		l.pos++
	}
	l.pos = len(l.src)
	return l.unterminated(sem, start, unterminated)
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

func (l *lexer) unterminated(sem lang.Kind, start int, message string) token {
	l.pos = min(l.pos, len(l.src))
	l.fail(start, l.pos, message)
	return l.emit(tUnterminated, start, sem, 0)
}

func scanNumber(src string, at int) int {
	if src[at] != '0' || at+2 >= len(src) || src[at+1]|0x20 != 'x' || !lang.IsHexDigit(src[at+2]) {
		return lang.ScanNumber(src, at)
	}
	at += 2
	for at < len(src) && lang.IsHexDigit(src[at]) {
		at++
	}
	return at
}

func (l *lexer) name() token {
	start := l.pos
	l.pos = lang.ScanQualifiedName(l.src, start)
	name := l.src[start:l.pos]
	w, known := words[name]
	switch {
	case known && w.class == keywordClass:
		return l.emit(w.kind, start, lang.Keyword, 0)
	case known && w.class == functionClass && (name == "length" || l.callFollows()):
		return l.emit(tBuiltinFunc, start, lang.Function, lang.Builtin)
	case l.pos < len(l.src) && l.src[l.pos] == '(':
		return l.emit(tFuncName, start, lang.Function, 0)
	case known && w.class == variableClass:
		return l.emit(tName, start, lang.Variable, lang.Builtin)
	}
	return l.emit(tName, start, lang.Variable, 0)
}

func (l *lexer) callFollows() bool {
	rest := strings.TrimLeft(l.src[l.pos:], " \t")
	return rest != "" && rest[0] == '('
}
