package jq

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/embedded/regex"
)

type context uint8

const (
	noComma context = 1 << iota
	noBinding
)

var brackets = lang.NewBrackets(map[kind]kind{tLParen: tRParen, tLBracket: tRBracket, tLBrace: tRBrace, tStringHead: tStringTail})

type parser struct {
	lang.Cursor[kind]
	lex lexer
}

func Parse(src string) lang.Result {
	res := lang.NewResult(src)
	p := &parser{lex: lexer{src: src, res: &res}}
	p.Cursor = lang.NewCursor(src, &res, tEOF, brackets, p.lex.next)
	lang.Run(p.run)
	p.lex.quiet = true
	p.Drain()
	return res
}

func (p *parser) run() {
	p.Advance()
	if !lang.Attempt(p.directives) && !p.resync() {
		return
	}
	for p.Tok.Kind != tEOF {
		if !lang.Attempt(p.filter) && !p.resync() {
			return
		}
	}
}

func (p *parser) resync() bool {
	p.lex.quiet = true
	defer func() { p.lex.quiet = false }()
	for {
		k := p.Tok.Kind
		top, _ := p.Open.Top()
		switch {
		case k == tEOF || k == tUnterminated || !p.Open.Tracked():
			return false
		case k == tPipe && p.Open.Depth() == 0:
			p.Advance()
			return true
		case k == tIf:
			p.Push(tEnd)
		case k == tDef:
			p.Push(tSemicolon)
		case k == tSemicolon && top == tSemicolon, k == tEnd && top == tEnd:
			p.Pop(k)
		case p.Open.Depth() == 0 && (p.Closes(k) || k == tEnd):
			p.Skip()
			continue
		case k == tEnd:
			return false
		}
		p.Advance()
	}
}

func (p *parser) filter() {
	p.pipe(0)
	if p.Tok.Kind != tEOF {
		p.Unexpected()
	}
}

func (p *parser) directives() {
	if p.Tok.Kind == tModule {
		p.Advance()
		p.object()
		p.Expect(tSemicolon)
	}
	for p.Tok.Kind == tImport || p.Tok.Kind == tInclude {
		directive := p.Tok.Kind
		p.Advance()
		p.Expect(tString)
		if directive == tImport {
			p.Expect(tAs)
			if p.Tok.Kind != tIdent && p.Tok.Kind != tVar {
				p.Unexpected()
			}
			p.Advance()
		}
		if p.Tok.Kind == tLBrace {
			p.object()
		}
		p.Expect(tSemicolon)
	}
}

func endsExpression(k kind) bool {
	switch k {
	case tEOF, tRParen, tRBracket, tRBrace, tSemicolon, tThen, tElif, tElse, tEnd, tCatch, tStringMiddle, tStringTail:
		return true
	}
	return false
}

func (p *parser) pipe(ctx context) {
	switch p.Tok.Kind {
	case tDef:
		p.definition()
		if !endsExpression(p.Tok.Kind) {
			p.pipe(ctx)
		}
		return
	case tLabel:
		p.Advance()
		p.binding()
		p.Expect(tPipe)
		p.pipe(ctx)
		return
	}
	p.comma(ctx)
	if p.Tok.Kind == tPipe {
		p.Advance()
		p.pipe(ctx)
	}
}

func (p *parser) binding() {
	if p.Tok.Kind != tVar {
		p.Unexpected()
	}
	p.Reclassify(p.Tok, lang.Variable, lang.Declaration)
	p.Advance()
}

func (p *parser) definition() {
	p.Advance()
	p.Push(tSemicolon)
	if p.Tok.Kind != tIdent {
		p.Unexpected()
	}
	p.Reclassify(p.Tok, lang.Function, lang.Declaration)
	p.Advance()
	if p.Tok.Kind == tLParen {
		open := p.Tok
		p.Advance()
		for {
			if p.Tok.Kind != tIdent && p.Tok.Kind != tVar {
				p.Unexpected()
			}
			kind := lang.Parameter
			if p.Tok.Kind == tIdent {
				kind = lang.Function
			}
			p.Reclassify(p.Tok, kind, lang.Declaration)
			p.Advance()
			if p.Tok.Kind != tSemicolon {
				break
			}
			p.Advance()
		}
		p.Closing(tRParen, open)
	}
	p.Expect(tColon)
	p.pipe(0)
	p.Expect(tSemicolon)
	p.Pop(tSemicolon)
}

func (p *parser) comma(ctx context) {
	p.alternative(ctx)
	for ctx&noComma == 0 && p.Tok.Kind == tComma {
		p.Advance()
		p.alternative(ctx)
	}
}

func (p *parser) alternative(ctx context) {
	p.assignment(ctx)
	if p.Tok.Kind == tAlt || p.Tok.Kind == tAltDestructure {
		p.Advance()
		p.alternative(ctx)
	}
}

func isAssignment(k kind) bool {
	switch k {
	case tAssign, tUpdate, tAddAssign, tSubAssign, tMulAssign, tDivAssign, tModAssign, tAltAssign:
		return true
	}
	return false
}

func (p *parser) assignment(ctx context) {
	p.or(ctx)
	for isAssignment(p.Tok.Kind) {
		p.Advance()
		p.or(ctx)
	}
}

func (p *parser) or(ctx context) {
	p.and(ctx)
	for p.Tok.Kind == tOr {
		p.Advance()
		p.and(ctx)
	}
}

func (p *parser) and(ctx context) {
	p.comparison(ctx)
	for p.Tok.Kind == tAnd {
		p.Advance()
		p.comparison(ctx)
	}
}

func (p *parser) comparison(ctx context) {
	p.additive(ctx)
	for {
		switch p.Tok.Kind {
		case tEqual, tNotEqual, tLess, tLessEq, tGreater, tGreaterEq:
			p.Advance()
			p.additive(ctx)
		default:
			return
		}
	}
}

func (p *parser) additive(ctx context) {
	p.multiplicative(ctx)
	for p.Tok.Kind == tAdd || p.Tok.Kind == tSub {
		p.Advance()
		p.multiplicative(ctx)
	}
}

func (p *parser) multiplicative(ctx context) {
	p.unary(ctx)
	for p.Tok.Kind == tMul || p.Tok.Kind == tDiv || p.Tok.Kind == tMod {
		p.Advance()
		p.unary(ctx)
	}
}

func (p *parser) unary(ctx context) {
	if p.Tok.Kind == tSub {
		p.Advance()
		p.unary(ctx)
		return
	}
	p.postfix(ctx)
}

func (p *parser) postfix(ctx context) {
	if p.Tok.Kind == tTry {
		p.Advance()
		p.postfix(ctx | noBinding)
		if p.Tok.Kind == tCatch {
			p.Advance()
			p.postfix(ctx | noBinding)
		}
		return
	}
	p.term()
	p.suffixes()
	if p.Tok.Kind == tAs && ctx&noBinding == 0 {
		p.Advance()
		p.patterns()
		p.Expect(tPipe)
		p.pipe(ctx)
	}
}

func (p *parser) suffixes() {
	for {
		switch p.Tok.Kind {
		case tField, tQuestion:
			p.Advance()
		case tDot:
			p.Advance()
			switch p.Tok.Kind {
			case tString, tStringHead:
				p.str()
			case tLBracket:
			default:
				p.Unexpected()
			}
		case tLBracket:
			p.index()
		default:
			return
		}
	}
}

func (p *parser) index() {
	open := p.Tok
	p.Advance()
	if p.Tok.Kind == tRBracket {
		p.Advance()
		return
	}
	if p.Tok.Kind != tColon {
		p.pipe(0)
	}
	if p.Tok.Kind == tColon {
		p.Advance()
		if p.Tok.Kind != tRBracket {
			p.pipe(0)
		}
	}
	p.Closing(tRBracket, open)
}

func (p *parser) str() {
	head := p.Tok
	if head.Kind == tString {
		p.Advance()
		return
	}
	p.Advance()
	for {
		p.pipe(0)
		switch p.Tok.Kind {
		case tStringMiddle:
			p.Advance()
		case tStringTail:
			p.Advance()
			return
		case tEOF:
			p.FailAt(head, "unterminated string")
		default:
			p.Unexpected()
		}
	}
}

func (p *parser) term() {
	switch p.Tok.Kind {
	case tNumber, tField, tDotDot, tVar:
		p.Advance()
	case tString, tStringHead:
		p.str()
	case tFormat:
		p.Advance()
		if p.Tok.Kind == tString || p.Tok.Kind == tStringHead {
			p.str()
		}
	case tDot:
		p.Advance()
		if p.Tok.Kind == tString || p.Tok.Kind == tStringHead {
			p.str()
		}
	case tLParen:
		open := p.Tok
		p.Advance()
		p.pipe(0)
		p.Closing(tRParen, open)
	case tLBracket:
		open := p.Tok
		p.Advance()
		if p.Tok.Kind != tRBracket {
			p.pipe(0)
		}
		p.Closing(tRBracket, open)
	case tLBrace:
		p.object()
	case tIdent:
		name := p.Tok
		p.Advance()
		if p.Tok.Kind == tLParen {
			p.arguments(p.lex.src[name.Start:name.End])
		}
	case tReduce, tForeach:
		p.fold()
	case tIf:
		p.conditional()
	case tBreak:
		p.Advance()
		if p.Tok.Kind != tVar {
			p.Unexpected()
		}
		p.Advance()
	default:
		p.Unexpected()
	}
}

type regexCall struct {
	arguments int
	flags     int
}

var regexCalls = map[string]regexCall{
	"test": {1, 1}, "match": {1, 1}, "capture": {1, 1}, "scan": {1, 1}, "splits": {1, 1},
	"split": {2, 1}, "sub": {2, 2}, "gsub": {2, 2},
}

func (p *parser) arguments(function string) {
	open := p.Tok
	p.Advance()
	call, isRegex := regexCalls[function]
	var pattern, flags token
	literalPattern, literalFlags := false, false
	count := 0
	for {
		first := p.Tok
		p.pipe(0)
		literal := first.Kind == tString && p.Last == first
		switch count {
		case 0:
			pattern, literalPattern = first, literal
		case call.flags:
			flags, literalFlags = first, literal
		}
		count++
		if p.Tok.Kind != tSemicolon {
			break
		}
		p.Advance()
	}
	p.Closing(tRParen, open)
	if !isRegex || count < call.arguments || !literalPattern {
		return
	}
	if count > call.flags && (!literalFlags || strings.Contains(p.lex.src[flags.Start:flags.End], "x")) {
		return
	}
	if excerpt, ok := stringExcerpt(p.lex.src, pattern.Start+1, pattern.End-1); ok {
		p.lex.res.CheckRegex(excerpt, regex.Oniguruma)
	}
}

var stringEscapes = map[byte]string{
	'"': `"`, '\\': `\`, '/': "/", 'b': "\b", 'f': "\f", 'n': "\n", 'r': "\r", 't': "\t",
}

const unicodeEscape = len(`\u0000`)

func stringExcerpt(src string, start, end int) (lang.Excerpt, bool) {
	if !strings.Contains(src[start:end], `\`) {
		return lang.Verbatim(src, start, end), true
	}
	b := lang.NewExcerptBuilder(end - start)
	for i := start; i < end; i++ {
		switch {
		case src[i] != '\\':
			b.Add(src[i:i+1], i, i+1)
		case src[i+1] == 'u':
			if i+unicodeEscape > end {
				return lang.Excerpt{}, false
			}
			code, err := strconv.ParseUint(src[i+2:i+unicodeEscape], 16, 16)
			if err != nil || utf16.IsSurrogate(rune(code)) {
				return lang.Excerpt{}, false
			}
			b.Add(string(rune(code)), i, i+unicodeEscape)
			i += unicodeEscape - 1
		default:
			decoded, known := stringEscapes[src[i+1]]
			if !known {
				return lang.Excerpt{}, false
			}
			b.Add(decoded, i, i+2)
			i++
		}
	}
	return b.Excerpt(), true
}

func (p *parser) fold() {
	keyword := p.Tok.Kind
	p.Advance()
	p.postfix(noBinding)
	p.Expect(tAs)
	p.patterns()
	open := p.Tok
	p.Expect(tLParen)
	p.pipe(0)
	p.Expect(tSemicolon)
	p.pipe(0)
	if keyword == tForeach && p.Tok.Kind == tSemicolon {
		p.Advance()
		p.pipe(0)
	}
	p.Closing(tRParen, open)
}

func (p *parser) conditional() {
	open := p.Tok
	p.Advance()
	p.Push(tEnd)
	p.pipe(0)
	p.Expect(tThen)
	p.pipe(0)
	for p.Tok.Kind == tElif {
		p.Advance()
		p.pipe(0)
		p.Expect(tThen)
		p.pipe(0)
	}
	if p.Tok.Kind == tElse {
		p.Advance()
		p.pipe(0)
	}
	p.Closing(tEnd, open)
	p.Pop(tEnd)
}

func (p *parser) patterns() {
	p.pattern()
	for p.Tok.Kind == tAltDestructure {
		p.Advance()
		p.pattern()
	}
}

func (p *parser) pattern() {
	open := p.Tok
	switch p.Tok.Kind {
	case tVar:
		p.binding()
	case tLBracket:
		p.Advance()
		p.pattern()
		for p.Tok.Kind == tComma {
			p.Advance()
			p.pattern()
		}
		p.Closing(tRBracket, open)
	case tLBrace:
		p.Advance()
		p.objectPattern()
		for p.Tok.Kind == tComma {
			p.Advance()
			p.objectPattern()
		}
		p.Closing(tRBrace, open)
	default:
		p.Unexpected()
	}
}

func (p *parser) objectPattern() {
	if p.Tok.Kind == tVar {
		p.binding()
		if p.Tok.Kind != tColon {
			return
		}
		p.Advance()
		p.pattern()
		return
	}
	p.key()
	p.Expect(tColon)
	p.pattern()
}

func (p *parser) key() bool {
	switch {
	case p.Tok.Kind == tIdent || isKeyword(p.Tok.Kind):
		p.Reclassify(p.Tok, lang.Property, 0)
		p.Advance()
	case p.Tok.Kind == tVar:
		p.Advance()
	case p.Tok.Kind == tString || p.Tok.Kind == tStringHead:
		p.str()
	case p.Tok.Kind == tFormat:
		p.Advance()
		if p.Tok.Kind == tString || p.Tok.Kind == tStringHead {
			p.str()
		}
	case p.Tok.Kind == tLParen:
		open := p.Tok
		p.Advance()
		p.pipe(0)
		p.Closing(tRParen, open)
		return true
	default:
		p.Unexpected()
	}
	return false
}

func isKeyword(k kind) bool { return k >= tDef && k <= tBreak }

func (p *parser) object() {
	open := p.Expect(tLBrace)
	for p.Tok.Kind != tRBrace && p.Tok.Kind != tEOF {
		if computed := p.key(); computed || p.Tok.Kind == tColon {
			p.Expect(tColon)
			p.pipe(noComma)
		}
		if p.Tok.Kind != tComma {
			break
		}
		p.Advance()
	}
	p.Closing(tRBrace, open)
}
