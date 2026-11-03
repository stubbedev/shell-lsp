package syntax

import (
	"strings"
	"unicode/utf8"
)

type TokenKind uint8

const (
	TokEOF TokenKind = iota
	TokInvalid
	TokIdent
	TokInt
	TokFloat
	TokPath
	TokHomePath
	TokSearchPath
	TokURI
	TokQuote
	TokIndentQuote
	TokInterp
	TokIf
	TokThen
	TokElse
	TokAssert
	TokWith
	TokLet
	TokIn
	TokRec
	TokInherit
	TokOrKeyword
	TokEllipsis
	TokLBrace
	TokRBrace
	TokLBracket
	TokRBracket
	TokLParen
	TokRParen
	TokDot
	TokComma
	TokSemicolon
	TokColon
	TokAssign
	TokAt
	TokQuestion
	TokNot
	TokPlus
	TokMinus
	TokStar
	TokSlash
	TokUpdate
	TokConcat
	TokEq
	TokNe
	TokLt
	TokLe
	TokGt
	TokGe
	TokAnd
	TokOr
	TokImplies
	TokPipeInto
	TokPipeFrom
	TokWhitespace
	TokLineComment
	TokBlockComment
	TokStringText
	TokEscape
	TokPathText
	TokUnparsed
	tokenKinds
)

type token struct {
	kind  TokenKind
	start int
	end   int
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool { return isLetter(c) || c == '_' }

func isIdentByte(c byte) bool { return isIdentStart(c) || isDigit(c) || c == '\'' || c == '-' }

func isPathByte(c byte) bool {
	return isLetter(c) || isDigit(c) || c == '.' || c == '_' || c == '-' || c == '+'
}

func isSchemeByte(c byte) bool { return isLetter(c) || isDigit(c) || c == '+' || c == '-' || c == '.' }

func isURIByte(c byte) bool {
	return isLetter(c) || isDigit(c) || strings.IndexByte("%/?:@&=+$,-_.!~*'", c) >= 0
}

type triviaEnd uint8

const (
	triviaPlain triviaEnd = iota
	triviaComments
	triviaUnclosed
)

type run struct {
	start int
	end   int
}

type lexer struct {
	src       string
	pathRun   run
	schemeRun run
	comments  []Comment
	tokens    []Token
	emitted   int
}

func (l *lexer) emit(kind TokenKind, start, end int) {
	if end <= start {
		return
	}
	l.tokens = append(l.tokens, Token{start: int32(start), end: int32(end), Kind: kind}) //nolint:gosec // offsets of a source file, which the transport caps well below 2^31
	l.emitted = end
}

func (l *lexer) comment(kind TokenKind, comment Comment) {
	l.comments = append(l.comments, comment)
	l.emit(kind, comment.Span.Start, comment.Span.End)
}

func (l *lexer) runOf(memo *run, at int, member func(byte) bool) int {
	if memo.start <= at && at < memo.end {
		return memo.end
	}
	end := at
	for end < len(l.src) && member(l.src[end]) {
		end++
	}
	if end > at {
		*memo = run{start: at, end: end}
	}
	return end
}

func (l *lexer) matchPath(at int) (int, bool) {
	src := l.src
	if !isPathByte(src[at]) && src[at] != '/' {
		return 0, false
	}
	slash := l.runOf(&l.pathRun, at, isPathByte)
	if slash >= len(src) || src[slash] != '/' {
		return 0, false
	}
	return l.pathSegments(slash)
}

func (l *lexer) matchHomePath(at int) (int, bool) {
	if at+1 >= len(l.src) || l.src[at+1] != '/' {
		return 0, false
	}
	return l.pathSegments(at + 1)
}

func (l *lexer) pathSegments(slash int) (int, bool) {
	src := l.src
	segments := 0
	for {
		next := slash + 1
		end := l.runOf(&l.pathRun, next, isPathByte)
		if end == next {
			switch {
			case strings.HasPrefix(src[next:], "${"), segments > 0:
				return next, true
			}
			return 0, false
		}
		segments++
		if end >= len(src) || src[end] != '/' {
			return end, true
		}
		slash = end
	}
}

func (l *lexer) matchSearchPath(at int) (int, bool) {
	src := l.src
	i := at + 1
	for {
		end := i
		for end < len(src) && isPathByte(src[end]) {
			end++
		}
		if end == i || end >= len(src) {
			return 0, false
		}
		switch src[end] {
		case '>':
			return end + 1, true
		case '/':
			i = end + 1
		default:
			return 0, false
		}
	}
}

func (l *lexer) matchURI(at int) (int, bool) {
	src := l.src
	colon := l.runOf(&l.schemeRun, at, isSchemeByte)
	if colon >= len(src) || src[colon] != ':' {
		return 0, false
	}
	end := colon + 1
	for end < len(src) && isURIByte(src[end]) {
		end++
	}
	return end, end > colon+1
}

func matchFloat(src string, at int) (int, bool) {
	digits := func(i int) int {
		for i < len(src) && isDigit(src[i]) {
			i++
		}
		return i
	}
	i := at
	fraction := 0
	if i < len(src) && src[i] >= '1' && src[i] <= '9' {
		i = digits(i)
	} else {
		if i < len(src) && src[i] == '0' {
			i++
		}
		fraction = 1
	}
	if i >= len(src) || src[i] != '.' {
		return 0, false
	}
	i++
	end := digits(i)
	if end-i < fraction {
		return 0, false
	}
	return exponent(src, end, digits), true
}

func exponent(src string, at int, digits func(int) int) int {
	if at >= len(src) || src[at] != 'e' && src[at] != 'E' {
		return at
	}
	i := at + 1
	if i < len(src) && (src[i] == '+' || src[i] == '-') {
		i++
	}
	if end := digits(i); end > i {
		return end
	}
	return at
}

func keyword(text string) TokenKind {
	switch text {
	case "if":
		return TokIf
	case "then":
		return TokThen
	case "else":
		return TokElse
	case "assert":
		return TokAssert
	case "with":
		return TokWith
	case "let":
		return TokLet
	case "in":
		return TokIn
	case "rec":
		return TokRec
	case "inherit":
		return TokInherit
	case "or":
		return TokOrKeyword
	}
	return TokIdent
}

func punctuation(s string) (TokenKind, int) {
	var second byte
	if len(s) > 1 {
		second = s[1]
	}
	switch s[0] {
	case '.':
		if strings.HasPrefix(s, "...") {
			return TokEllipsis, 3
		}
		return TokDot, 1
	case '$':
		if second == '{' {
			return TokInterp, 2
		}
	case '\'':
		if second == '\'' {
			return TokIndentQuote, 2
		}
	case '/':
		return choose(second == '/', TokUpdate, TokSlash)
	case '+':
		return choose(second == '+', TokConcat, TokPlus)
	case '=':
		return choose(second == '=', TokEq, TokAssign)
	case '!':
		return choose(second == '=', TokNe, TokNot)
	case '>':
		return choose(second == '=', TokGe, TokGt)
	case '-':
		return choose(second == '>', TokImplies, TokMinus)
	case '<':
		if second == '|' {
			return TokPipeFrom, 2
		}
		return choose(second == '=', TokLe, TokLt)
	case '&':
		if second == '&' {
			return TokAnd, 2
		}
	case '|':
		if second == '>' {
			return TokPipeInto, 2
		}
		if second == '|' {
			return TokOr, 2
		}
	case '"':
		return TokQuote, 1
	case '{':
		return TokLBrace, 1
	case '}':
		return TokRBrace, 1
	case '[':
		return TokLBracket, 1
	case ']':
		return TokRBracket, 1
	case '(':
		return TokLParen, 1
	case ')':
		return TokRParen, 1
	case ',':
		return TokComma, 1
	case ';':
		return TokSemicolon, 1
	case ':':
		return TokColon, 1
	case '@':
		return TokAt, 1
	case '?':
		return TokQuestion, 1
	case '*':
		return TokStar, 1
	}
	_, width := utf8.DecodeRuneInString(s)
	return TokInvalid, width
}

func choose(double bool, long, short TokenKind) (TokenKind, int) {
	if double {
		return long, 2
	}
	return short, 1
}

func (l *lexer) trivia(at int, record bool) (int, triviaEnd) {
	src := l.src
	end := triviaPlain
	run := at
	for at < len(src) {
		switch c := src[at]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			at++
			continue
		case c == '#':
			stop := len(src)
			if i := strings.IndexAny(src[at:], "\r\n"); i >= 0 {
				stop = at + i
			}
			if record {
				l.emit(TokWhitespace, run, at)
				l.comment(TokLineComment, Comment{Span: Span{Start: at, End: stop}, Body: Span{Start: at + 1, End: stop}, Kind: LineComment})
			}
			end = triviaComments
			at, run = stop, stop
			continue
		case c == '/' && strings.HasPrefix(src[at:], "/*"):
			i := strings.Index(src[at+2:], "*/")
			if i < 0 {
				if record {
					l.emit(TokWhitespace, run, at)
					l.comment(TokBlockComment, Comment{Span: Span{Start: at, End: len(src)}, Body: Span{Start: at + 2, End: len(src)}, Kind: BlockComment})
				}
				return at, triviaUnclosed
			}
			stop := at + 2 + i
			if record {
				l.emit(TokWhitespace, run, at)
				l.comment(TokBlockComment, Comment{Span: Span{Start: at, End: stop + 2}, Body: Span{Start: at + 2, End: stop}, Kind: BlockComment})
			}
			end = triviaComments
			at, run = stop+2, stop+2
			continue
		}
		break
	}
	if record {
		l.emit(TokWhitespace, run, at)
	}
	return at, end
}

func (l *lexer) scan(at int) token {
	src := l.src
	if at >= len(src) {
		return token{kind: TokEOF, start: len(src), end: len(src)}
	}
	c := src[at]
	if end, ok := l.matchPath(at); ok {
		return token{kind: TokPath, start: at, end: end}
	}
	switch {
	case c == '~':
		if end, ok := l.matchHomePath(at); ok {
			return token{kind: TokHomePath, start: at, end: end}
		}
	case c == '<':
		if end, ok := l.matchSearchPath(at); ok {
			return token{kind: TokSearchPath, start: at, end: end}
		}
	case isLetter(c):
		if end, ok := l.matchURI(at); ok {
			return token{kind: TokURI, start: at, end: end}
		}
	}
	if end, ok := matchFloat(src, at); ok {
		return token{kind: TokFloat, start: at, end: end}
	}
	if isDigit(c) {
		end := at
		for end < len(src) && isDigit(src[end]) {
			end++
		}
		return token{kind: TokInt, start: at, end: end}
	}
	if isIdentStart(c) {
		end := at
		for end < len(src) && isIdentByte(src[end]) {
			end++
		}
		return token{kind: keyword(src[at:end]), start: at, end: end}
	}
	kind, width := punctuation(src[at:])
	return token{kind: kind, start: at, end: at + width}
}
