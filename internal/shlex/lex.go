package shlex

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

const maxDepth = 1000

var operators = func() []Kind {
	var kinds []Kind
	for k := AndIf; k <= LessGreat; k++ {
		if k != DLParen && k != DRParen && k != LBrace && k != RBrace {
			kinds = append(kinds, k)
		}
	}
	slices.SortStableFunc(kinds, func(a, b Kind) int { return cmp.Compare(len(kindText[b]), len(kindText[a])) })
	return kinds
}()

var declarations = []string{"declare", "export", "local", "readonly", "typeset"}

func Lex(src string, d dialect.Dialect) ([]Token, error) {
	l := lexer{src: src, limit: len(src), dialect: d}
	tokens := l.list(frameOpen)
	if l.err != nil {
		return nil, l.err
	}
	return tokens, nil
}

type lexer struct {
	src     string
	pos     int
	limit   int
	depth   int
	err     error
	dialect dialect.Dialect
}

type heredoc struct {
	delimiter string
	dash      bool
}

type wordMode uint8

const (
	modeCommand wordMode = iota
	modeArithmetic
	modeRegex
)

type closing uint8

const (
	closedNot closing = iota
	closedSingle
	closedDouble
)

func (l *lexer) more() bool {
	return l.err == nil && l.pos < l.limit
}

func (l *lexer) at(offset int) byte {
	if l.pos+offset < l.limit {
		return l.src[l.pos+offset]
	}
	return 0
}

func (l *lexer) enter() bool {
	l.depth++
	if l.depth > maxDepth && l.err == nil {
		l.err = fmt.Errorf("lex shell source: constructs nested deeper than %d levels at byte %d", maxDepth, l.pos)
	}
	return l.err == nil
}

func (l *lexer) leave() {
	l.depth--
}

func (l *lexer) skipBlanks() {
	for l.more() {
		switch {
		case l.src[l.pos] == ' ' || l.src[l.pos] == '\t':
			l.pos++
		case l.src[l.pos] == '\\' && l.at(1) == '\n':
			l.pos += 2
		default:
			return
		}
	}
}

func (l *lexer) single(kind Kind) Token {
	l.pos++
	return Token{Kind: kind, Start: l.pos - 1, End: l.pos}
}

func (l *lexer) lineEnd() int {
	if i := strings.IndexByte(l.src[l.pos:l.limit], '\n'); i >= 0 {
		return l.pos + i
	}
	return l.limit
}

func (l *lexer) atProcessSubstitution() bool {
	return (l.src[l.pos] == '<' || l.src[l.pos] == '>') && l.at(1) == '('
}

func isOperator(c byte) bool {
	return strings.IndexByte(";&|()<>", c) >= 0
}

func (l *lexer) list(kind frameKind) []Token {
	if !l.enter() {
		return nil
	}
	defer l.leave()
	f := frame{kind: kind}
	if kind == frameArray {
		f.state = stateArgs
	}
	for l.skipBlanks(); l.more(); l.skipBlanks() {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			f.tokens = append(f.tokens, l.single(Newline))
			f.newline()
			l.readHeredocs(&f)
		case c == '#':
			end := l.lineEnd()
			f.tokens = append(f.tokens, Token{Kind: Comment, Start: l.pos, End: end})
			l.pos = end
		case c == ')' && kind != frameOpen && len(f.open) == 0:
			return f.tokens
		case f.regex && strings.IndexByte(";&<>)", c) < 0:
			f.regex = false
			l.wordToken(&f, modeRegex)
		case f.cond && (c == '<' || c == '>') && !l.atProcessSubstitution():
			t := l.single(Word)
			t.Parts = []Part{{Kind: Literal, Start: t.Start, End: t.End}}
			f.tokens = append(f.tokens, t)
		case isOperator(c) && !l.atProcessSubstitution():
			l.operator(&f)
		default:
			l.wordToken(&f, modeCommand)
		}
	}
	return f.tokens
}

func (l *lexer) wordToken(f *frame, mode wordMode) {
	t := l.word(mode, f.allowsArrays())
	if f.state == stateTarget && f.operator != 0 {
		f.heredocs = append(f.heredocs, heredoc{delimiter: unquoted(l.src, t.Parts), dash: f.operator == DLessDash})
	}
	f.word(l.src, &t)
	f.tokens = append(f.tokens, t)
}

func (l *lexer) operator(f *frame) {
	start := l.pos
	kind := l.operatorKind(f)
	l.pos += len(kindText[kind])
	f.regex = false
	f.tokens = append(f.tokens, Token{Kind: kind, Start: start, End: l.pos})
	if kind == DLParen {
		l.arithmeticCommand(f)
		return
	}
	f.afterOperator(kind)
}

func (l *lexer) operatorKind(f *frame) Kind {
	if l.src[l.pos] == '(' && l.at(1) == '(' && f.opensArithmetic() {
		return DLParen
	}
	rest := l.src[l.pos:l.limit]
	for _, k := range operators {
		if strings.HasPrefix(rest, kindText[k]) {
			return k
		}
	}
	panic(fmt.Sprintf("shlex: no operator starts at byte %d", l.pos))
}

func (l *lexer) arithmeticCommand(f *frame) {
	open := len(f.tokens) - 1
	var closed closing
	f.tokens, closed = l.arithmetic(f.tokens)
	switch closed {
	case closedDouble:
		f.tokens = append(f.tokens, Token{Kind: DRParen, Start: l.pos - 2, End: l.pos})
	case closedSingle:
		f.tokens = append(f.tokens, Token{Kind: RParen, Start: l.pos - 1, End: l.pos})
		f.tokens[open].Flags |= Unterminated
	case closedNot:
		f.tokens[open].Flags |= Unterminated
	}
	f.state = stateCommand
}

func (l *lexer) arithmetic(tokens []Token) ([]Token, closing) {
	if !l.enter() {
		return tokens, closedNot
	}
	defer l.leave()
	depth := 0
	for l.skipBlanks(); l.more(); l.skipBlanks() {
		var t Token
		switch c := l.src[l.pos]; {
		case c == ')' && depth == 0 && l.at(1) == ')':
			l.pos += 2
			return tokens, closedDouble
		case c == ')' && depth < 0:
			l.pos++
			return tokens, closedSingle
		case c == ')':
			depth--
			t = l.single(RParen)
		case c == '(':
			depth++
			t = l.single(LParen)
		case c == ';':
			t = l.single(Semi)
		case c == '\n':
			t = l.single(Newline)
		default:
			t = l.word(modeArithmetic, false)
		}
		t.Flags |= Arithmetic
		tokens = append(tokens, t)
	}
	return tokens, closedNot
}

func (l *lexer) readHeredocs(f *frame) {
	pending := f.heredocs
	f.heredocs = nil
	for _, h := range pending {
		if !l.more() {
			return
		}
		body := Token{Kind: HeredocBody, Start: l.pos, Flags: Unterminated}
		for l.more() {
			end := l.lineEnd()
			line := l.src[l.pos:end]
			if h.dash {
				line = strings.TrimLeft(line, "\t")
			}
			l.pos = min(end+1, l.limit)
			if line == h.delimiter {
				body.Flags = 0
				break
			}
		}
		body.End = l.pos
		f.tokens = append(f.tokens, body)
	}
}

func endsWord(mode wordMode, c byte, depth int) bool {
	switch mode {
	case modeArithmetic:
		return strings.IndexByte(" \t\n;()", c) >= 0
	case modeRegex:
		return c == '\n' || depth == 0 && strings.IndexByte(" \t;&<>)", c) >= 0
	case modeCommand:
	}
	return strings.IndexByte(" \t\n;&|()<>", c) >= 0
}

func (l *lexer) word(mode wordMode, arrays bool) Token {
	start := l.pos
	var parts []Part
	depth := 0
	for l.more() {
		c := l.src[l.pos]
		switch {
		case c == '\\':
			parts = append(parts, l.escape())
		case c == '\'':
			parts = append(parts, l.singleQuoted())
		case c == '"':
			parts = append(parts, l.doubleQuoted(DoubleQuoted, 1))
		case c == '$':
			parts = l.dollar(parts, false)
		case c == '`':
			parts = append(parts, l.backquote())
		case mode == modeCommand && l.atProcessSubstitution():
			parts = append(parts, l.substitution(ProcessSubst, 2))
		case mode == modeCommand && c == '(' && arrays && l.assignedSoFar(start):
			parts = append(parts, l.array())
		case mode == modeCommand && l.atExtGlob():
			parts = append(parts, l.extGlob())
		case mode == modeCommand && l.pos > start && l.atGlobGroup():
			parts = append(parts, l.globGroup())
		case mode == modeRegex && c == '(':
			depth++
			parts = l.literal(parts)
		case mode == modeRegex && c == ')' && depth > 0:
			depth--
			parts = l.literal(parts)
		case endsWord(mode, c, depth):
			return finishWord(start, l.pos, parts)
		default:
			parts = l.literal(parts)
		}
	}
	return finishWord(start, l.pos, parts)
}

const (
	extGlobOperators   = "?*+@!"
	kshGroupPrefixes   = "}~%-"
	groupOpen          = '('
	groupClose         = ')'
	extGlobOpenerWidth = 2
)

func (l *lexer) atExtGlob() bool {
	return l.dialect != dialect.Zsh && strings.IndexByte(extGlobOperators, l.at(0)) >= 0 &&
		l.at(1) == groupOpen && l.at(2) != groupClose
}

func (l *lexer) atGlobGroup() bool {
	if l.at(0) != groupOpen || l.at(1) == groupClose {
		return false
	}
	switch l.dialect {
	case dialect.Zsh:
		return true
	case dialect.Ksh:
		return strings.IndexByte(kshGroupPrefixes, l.src[l.pos-1]) >= 0
	case dialect.Bash, dialect.Posix, dialect.Dash, dialect.Busybox:
	}
	return false
}

func (l *lexer) extGlob() Part {
	p := Part{Kind: ExtGlob, Start: l.pos}
	l.pos += extGlobOpenerWidth
	for depth := 1; l.more(); l.pos++ {
		switch l.src[l.pos] {
		case groupOpen:
			depth++
		case groupClose:
			if depth--; depth == 0 {
				l.pos++
				p.End = l.pos
				return p
			}
		}
	}
	p.End, p.Unterminated = l.pos, true
	return p
}

func (l *lexer) globGroup() Part {
	p := Part{Kind: GlobGroup, Start: l.pos}
	l.pos++
	var quote byte
	for depth := 1; l.more(); l.pos++ {
		c := l.src[l.pos]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				l.skipEscaped()
			}
		case c == '\\':
			l.skipEscaped()
		case c == '\'' || c == '"':
			quote = c
		case c == groupOpen:
			depth++
		case c == groupClose:
			if depth--; depth == 0 {
				l.pos++
				p.End = l.pos
				return p
			}
		}
	}
	p.End, p.Unterminated = l.pos, true
	return p
}

func (l *lexer) skipEscaped() {
	if l.pos+1 < l.limit {
		l.pos++
	}
}

func finishWord(start, end int, parts []Part) Token {
	t := Token{Kind: Word, Start: start, End: end, Parts: parts}
	if slices.ContainsFunc(parts, func(p Part) bool { return p.Unterminated }) {
		t.Flags |= Unterminated
	}
	return t
}

func (l *lexer) literal(parts []Part) []Part {
	l.pos++
	if n := len(parts); n > 0 && parts[n-1].Kind == Literal && parts[n-1].End == l.pos-1 {
		parts[n-1].End = l.pos
		return parts
	}
	return append(parts, Part{Kind: Literal, Start: l.pos - 1, End: l.pos})
}

func (l *lexer) assignedSoFar(start int) bool {
	n, ok := AssignmentPrefix(l.src[start:l.pos])
	return ok && start+n == l.pos
}

func (l *lexer) closeWith(p Part, closer string) Part {
	if l.err == nil && strings.HasPrefix(l.src[l.pos:l.limit], closer) {
		l.pos += len(closer)
	} else {
		p.Unterminated = true
	}
	p.End = l.pos
	return p
}

func (l *lexer) array() Part {
	start := l.pos
	l.pos++
	return l.closeWith(Part{Kind: Array, Start: start, Tokens: l.list(frameArray)}, ")")
}

func (l *lexer) substitution(kind PartKind, opener int) Part {
	start := l.pos
	l.pos += opener
	return l.closeWith(Part{Kind: kind, Start: start, Tokens: l.list(frameSubst)}, ")")
}

func (l *lexer) escape() Part {
	start := l.pos
	switch {
	case l.pos+1 >= l.limit:
		l.pos++
		return Part{Kind: Escaped, Unterminated: true, Start: start, End: l.pos}
	case l.src[l.pos+1] == '\n':
		l.pos += 2
		return Part{Kind: Continuation, Start: start, End: l.pos}
	}
	_, size := utf8.DecodeRuneInString(l.src[l.pos+1 : l.limit])
	l.pos += 1 + size
	return Part{Kind: Escaped, Start: start, End: l.pos}
}

func (l *lexer) singleQuoted() Part {
	start := l.pos
	end := strings.IndexByte(l.src[start+1:l.limit], '\'')
	if end < 0 {
		l.pos = l.limit
		return Part{Kind: SingleQuoted, Unterminated: true, Start: start, End: l.pos}
	}
	l.pos = start + end + 2
	return Part{Kind: SingleQuoted, Start: start, End: l.pos}
}

func (l *lexer) dollarSingleQuoted() Part {
	start := l.pos
	for l.pos += 2; l.more(); {
		switch l.src[l.pos] {
		case '\\':
			l.pos = min(l.pos+2, l.limit)
		case '\'':
			l.pos++
			return Part{Kind: DollarSingleQuoted, Start: start, End: l.pos}
		default:
			l.pos++
		}
	}
	return Part{Kind: DollarSingleQuoted, Unterminated: true, Start: start, End: l.pos}
}

func (l *lexer) doubleQuoted(kind PartKind, opener int) Part {
	start := l.pos
	l.pos += opener
	p := Part{Kind: kind, Unterminated: true, Start: start}
	if !l.enter() {
		return p
	}
	defer l.leave()
	for l.more() && p.Unterminated {
		switch l.src[l.pos] {
		case '"':
			l.pos++
			p.Unterminated = false
		case '\\':
			p.Parts = append(p.Parts, l.escape())
		case '$':
			p.Parts = l.dollar(p.Parts, true)
		case '`':
			p.Parts = append(p.Parts, l.backquote())
		default:
			p.Parts = l.literal(p.Parts)
		}
	}
	p.End = l.pos
	return p
}

func (l *lexer) braced() Part {
	start := l.pos
	l.pos += 2
	p := Part{Kind: Param, Unterminated: true, Start: start}
	if !l.enter() {
		return p
	}
	defer l.leave()
	depth := 0
	for l.more() && p.Unterminated {
		switch c := l.src[l.pos]; {
		case c == '}' && depth == 0:
			l.pos++
			p.Unterminated = false
		case c == '{':
			depth++
			p.Parts = l.literal(p.Parts)
		case c == '}':
			depth--
			p.Parts = l.literal(p.Parts)
		case c == '\\':
			p.Parts = append(p.Parts, l.escape())
		case c == '\'':
			p.Parts = append(p.Parts, l.singleQuoted())
		case c == '"':
			p.Parts = append(p.Parts, l.doubleQuoted(DoubleQuoted, 1))
		case c == '$':
			p.Parts = l.dollar(p.Parts, false)
		case c == '`':
			p.Parts = append(p.Parts, l.backquote())
		default:
			p.Parts = l.literal(p.Parts)
		}
	}
	p.End = l.pos
	return p
}

func (l *lexer) dollar(parts []Part, quoted bool) []Part {
	start := l.pos
	switch next := l.at(1); {
	case next == '(' && l.at(2) == '(':
		l.pos += 3
		tokens, closed := l.arithmetic(nil)
		return append(parts, Part{Kind: ArithmeticExpansion, Unterminated: closed != closedDouble, Start: start, End: l.pos, Tokens: tokens})
	case next == '(':
		return append(parts, l.substitution(CommandSubst, 2))
	case next == '{':
		return append(parts, l.braced())
	case next == '\'' && !quoted:
		return append(parts, l.dollarSingleQuoted())
	case next == '"' && !quoted:
		return append(parts, l.doubleQuoted(DollarDoubleQuoted, 2))
	case next == '_' || 'a' <= next && next <= 'z' || 'A' <= next && next <= 'Z':
		for l.pos += 2; l.more() && IsNameChar(l.src[l.pos]); {
			l.pos++
		}
		return append(parts, Part{Kind: Param, Start: start, End: l.pos})
	case isDigit(next) || strings.IndexByte("@*#?$!-", next) >= 0:
		l.pos += 2
		return append(parts, Part{Kind: Param, Start: start, End: l.pos})
	}
	return l.literal(parts)
}

func (l *lexer) backquote() Part {
	start := l.pos
	end := start + 1
	for end < l.limit && l.src[end] != '`' {
		if l.src[end] == '\\' {
			end++
		}
		end++
	}
	end = min(end, l.limit)
	outer := l.limit
	l.limit, l.pos = end, start+1
	tokens := l.list(frameOpen)
	l.limit, l.pos = outer, end
	p := Part{Kind: Backquote, Unterminated: end == l.limit, Start: start, Tokens: tokens}
	if !p.Unterminated {
		l.pos++
	}
	p.End = l.pos
	return p
}

func unquoted(src string, parts []Part) string {
	if len(parts) == 1 && parts[0].Kind == Literal {
		return parts[0].Text(src)
	}
	var b strings.Builder
	for _, p := range parts {
		text := p.Text(src)
		switch p.Kind {
		case Escaped:
			b.WriteString(text[1:])
		case Continuation:
		case SingleQuoted, DoubleQuoted:
			b.WriteString(quotedContent(text, 1, p.Unterminated))
		case DollarSingleQuoted, DollarDoubleQuoted:
			b.WriteString(quotedContent(text, 2, p.Unterminated))
		default:
			b.WriteString(text)
		}
	}
	return b.String()
}

func quotedContent(text string, opener int, unterminated bool) string {
	if unterminated {
		return text[opener:]
	}
	return text[opener : len(text)-1]
}
