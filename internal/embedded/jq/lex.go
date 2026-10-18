package jq

import (
	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/textline"
)

type kind uint8

const (
	tEOF kind = iota
	tIllegal
	tUnterminated
	tIdent
	tField
	tVar
	tFormat
	tNumber
	tString
	tStringHead
	tStringMiddle
	tStringTail
	tDot
	tDotDot
	tPipe
	tComma
	tAlt
	tAltDestructure
	tAssign
	tUpdate
	tAddAssign
	tSubAssign
	tMulAssign
	tDivAssign
	tModAssign
	tAltAssign
	tEqual
	tNotEqual
	tLess
	tLessEq
	tGreater
	tGreaterEq
	tAdd
	tSub
	tMul
	tDiv
	tMod
	tQuestion
	tLParen
	tRParen
	tLBracket
	tRBracket
	tLBrace
	tRBrace
	tColon
	tSemicolon
	tDef
	tIf
	tThen
	tElif
	tElse
	tEnd
	tAs
	tReduce
	tForeach
	tTry
	tCatch
	tLabel
	tImport
	tInclude
	tModule
	tAnd
	tOr
	tBreak
)

var keywords = map[string]kind{
	"def": tDef, "if": tIf, "then": tThen, "elif": tElif, "else": tElse, "end": tEnd, "as": tAs,
	"reduce": tReduce, "foreach": tForeach, "try": tTry, "catch": tCatch, "label": tLabel,
	"import": tImport, "include": tInclude, "module": tModule, "and": tAnd, "or": tOr, "break": tBreak,
}

var literals = map[string]bool{"true": true, "false": true, "null": true}

var operators = lang.NewOperators(map[string]kind{
	"|=": tUpdate, "|": tPipe,
	"//=": tAltAssign, "//": tAlt, "/=": tDivAssign, "/": tDiv,
	"?//": tAltDestructure, "?": tQuestion,
	"==": tEqual, "=": tAssign, "!=": tNotEqual,
	"<=": tLessEq, "<": tLess, ">=": tGreaterEq, ">": tGreater,
	"+=": tAddAssign, "+": tAdd, "-=": tSubAssign, "-": tSub,
	"*=": tMulAssign, "*": tMul, "%=": tModAssign, "%": tMod,
}, map[string]kind{
	"(": tLParen, ")": tRParen, "[": tLBracket, "]": tRBracket, "{": tLBrace, "}": tRBrace,
	":": tColon, ";": tSemicolon, ",": tComma,
})

type token = lang.Lexeme[kind]

type lexer struct {
	src    string
	pos    int
	res    *lang.Result
	nested []int
	quiet  bool
}

func (l *lexer) fail(start, end int, message string) {
	if !l.quiet {
		l.res.Fail(start, end, message)
	}
}

func (l *lexer) next() token {
	for l.pos < len(l.src) {
		switch c := l.src[l.pos]; c {
		case ' ', '\t', '\n', '\r':
			l.pos++
		case '#':
			start := l.pos
			l.pos = textline.End(l.src, l.pos)
			l.res.Emit(start, l.pos, lang.Comment, 0)
		default:
			return l.token(c)
		}
	}
	return token{Kind: tEOF, Start: len(l.src), End: len(l.src)}
}

func (l *lexer) token(c byte) token {
	start := l.pos
	switch {
	case c == '"':
		l.pos++
		return l.stringPart(start, tString, tStringHead)
	case c == ')' && len(l.nested) > 0 && l.nested[len(l.nested)-1] == 0:
		l.nested = l.nested[:len(l.nested)-1]
		l.pos++
		l.res.Emit(start, l.pos, lang.Operator, 0)
		return l.stringPart(l.pos, tStringTail, tStringMiddle)
	case lang.StartsNumber(l.src, start):
		l.pos = lang.ScanNumber(l.src, start)
		return l.emit(tNumber, start, lang.Number, 0)
	case c == '.':
		return l.dot()
	case c == '$' || c == '@':
		return l.sigil(c)
	case scan.NameStart(c):
		return l.ident()
	}
	if t, ok := operators.Scan(l.res, l.src, start); ok {
		l.pos = t.End
		l.balance(t.Kind)
		return t
	}
	l.pos = lang.RuneEnd(l.src, start)
	l.fail(start, l.pos, lang.Illegal(l.src, start, l.pos))
	return token{Kind: tIllegal, Start: start, End: l.pos}
}

func (l *lexer) emit(k kind, start int, sem lang.Kind, mods lang.Modifier) token {
	return token{Kind: k, Start: start, End: l.pos, Sem: l.res.Emit(start, l.pos, sem, mods)}
}

func (l *lexer) balance(k kind) {
	if len(l.nested) == 0 {
		return
	}
	switch k {
	case tLParen:
		l.nested[len(l.nested)-1]++
	case tRParen:
		l.nested[len(l.nested)-1]--
	default:
	}
}

func (l *lexer) stringPart(start int, closed, interpolated kind) token {
	textStart := l.pos
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case '"':
			l.pos++
			l.res.Emit(textStart-boolInt(closed == tString), l.pos, lang.String, 0)
			return token{Kind: closed, Start: start, End: l.pos}
		case '\\':
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '(' {
				l.res.Emit(textStart-boolInt(closed == tString), l.pos, lang.String, 0)
				l.pos += 2
				l.res.Emit(l.pos-2, l.pos, lang.Operator, 0)
				l.nested = append(l.nested, 0)
				return token{Kind: interpolated, Start: start, End: l.pos}
			}
			l.escape()
			continue
		}
		l.pos++
	}
	l.res.Emit(textStart-boolInt(closed == tString), l.pos, lang.String, 0)
	l.fail(start, l.pos, "unterminated string")
	return token{Kind: tUnterminated, Start: start, End: l.pos}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (l *lexer) escape() {
	at := l.pos
	l.pos++
	if l.pos >= len(l.src) {
		return
	}
	switch l.src[l.pos] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		l.pos++
	case 'u':
		l.pos++
		for range 4 {
			if l.pos >= len(l.src) || !scan.HexDigit(l.src[l.pos]) {
				l.fail(at, l.pos, "invalid escape "+lang.Quote(l.src, at, l.pos)+" needs four hexadecimal digits")
				return
			}
			l.pos++
		}
	default:
		end := lang.RuneEnd(l.src, l.pos)
		l.fail(at, end, "invalid escape "+lang.Quote(l.src, at, end))
		l.pos = end
	}
}

func (l *lexer) dot() token {
	start := l.pos
	l.pos++
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		l.pos++
		return l.emit(tDotDot, start, lang.Operator, 0)
	}
	if l.pos < len(l.src) && scan.NameStart(l.src[l.pos]) {
		l.pos = lang.ScanName(l.src, l.pos)
		return l.emit(tField, start, lang.Property, 0)
	}
	return l.emit(tDot, start, lang.Operator, 0)
}

func (l *lexer) sigil(c byte) token {
	start := l.pos
	l.pos++
	if l.pos >= len(l.src) || !scan.NameStart(l.src[l.pos]) {
		l.fail(start, l.pos, lang.Quote(l.src, start, l.pos)+" needs a name")
		return token{Kind: tIllegal, Start: start, End: l.pos}
	}
	l.pos = lang.ScanQualifiedName(l.src, l.pos)
	name := l.src[start:l.pos]
	if c == '@' {
		_, known := formats[name]
		return l.emit(tFormat, start, lang.Function, builtinIf(known))
	}
	_, known := variables[name]
	return l.emit(tVar, start, lang.Variable, builtinIf(known))
}

func builtinIf(known bool) lang.Modifier {
	if known {
		return lang.Builtin
	}
	return 0
}

func (l *lexer) ident() token {
	start := l.pos
	l.pos = lang.ScanQualifiedName(l.src, start)
	name := l.src[start:l.pos]
	if k, ok := keywords[name]; ok {
		return l.emit(k, start, lang.Keyword, 0)
	}
	if literals[name] {
		return l.emit(tIdent, start, lang.Keyword, 0)
	}
	_, known := functions[name]
	return l.emit(tIdent, start, lang.Function, builtinIf(known))
}
