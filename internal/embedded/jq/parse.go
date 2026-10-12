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

type parser struct {
	lex  lexer
	tok  token
	last token
	open lang.Nesting[kind]
}

func Parse(src string) lang.Result {
	res := lang.NewResult(src)
	p := &parser{lex: lexer{src: src, res: &res}}
	lang.Run(p.run)
	p.lex.quiet = true
	for p.tok.Kind != tEOF {
		p.tok = p.lex.next()
	}
	return res
}

func (p *parser) run() {
	p.advance()
	if !lang.Attempt(p.directives) && !p.resync() {
		return
	}
	for p.tok.Kind != tEOF {
		if !lang.Attempt(p.filter) && !p.resync() {
			return
		}
	}
}

func (p *parser) resync() bool {
	p.lex.quiet = true
	defer func() { p.lex.quiet = false }()
	for {
		k := p.tok.Kind
		top, _ := p.open.Top()
		switch {
		case k == tEOF || k == tUnterminated || !p.open.Tracked():
			return false
		case k == tPipe && p.open.Depth() == 0:
			p.advance()
			return true
		case k == tIf:
			p.push(tEnd)
		case k == tDef:
			p.push(tSemicolon)
		case k == tSemicolon && top == tSemicolon, k == tEnd && top == tEnd:
			p.pop(k)
		case p.open.Depth() == 0 && (isCloser(k) || k == tEnd):
			p.tok = p.lex.next()
			continue
		case k == tEnd:
			return false
		}
		p.advance()
	}
}

func isCloser(k kind) bool {
	return k == tRParen || k == tRBracket || k == tRBrace || k == tStringTail
}

func (p *parser) push(closer kind) { p.open.Push(closer) }

func (p *parser) pop(closer kind) {
	if !p.open.Pop(closer) {
		lang.Abort()
	}
}

func (p *parser) advance() {
	switch k := p.tok.Kind; k {
	case tLParen:
		p.push(tRParen)
	case tLBracket:
		p.push(tRBracket)
	case tLBrace:
		p.push(tRBrace)
	case tStringHead:
		p.push(tStringTail)
	case tRParen, tRBracket, tRBrace, tStringTail:
		p.pop(k)
	}
	p.last = p.tok
	p.tok = p.lex.next()
}

func (p *parser) failAt(t token, message string) {
	p.lex.res.Fail(t.Start, t.End, message)
	lang.Bail()
}

func (p *parser) unexpected() {
	p.failAt(p.tok, lang.Unexpected(p.lex.src, p.tok.Start, p.tok.End))
}

func (p *parser) expect(k kind) token {
	if p.tok.Kind != k {
		p.unexpected()
	}
	t := p.tok
	p.advance()
	return t
}

func (p *parser) closing(k kind, open token) {
	if p.tok.Kind == tEOF {
		p.failAt(open, lang.Unclosed(p.lex.src, open.Start, open.End))
	}
	p.expect(k)
}

func (p *parser) reclassify(t token, k lang.Kind, mods lang.Modifier) {
	p.lex.res.Reclassify(t.Sem, k, mods)
}

func (p *parser) filter() {
	p.pipe(0)
	if p.tok.Kind != tEOF {
		p.unexpected()
	}
}

func (p *parser) directives() {
	if p.tok.Kind == tModule {
		p.advance()
		p.object()
		p.expect(tSemicolon)
	}
	for p.tok.Kind == tImport || p.tok.Kind == tInclude {
		directive := p.tok.Kind
		p.advance()
		p.expect(tString)
		if directive == tImport {
			p.expect(tAs)
			if p.tok.Kind != tIdent && p.tok.Kind != tVar {
				p.unexpected()
			}
			p.advance()
		}
		if p.tok.Kind == tLBrace {
			p.object()
		}
		p.expect(tSemicolon)
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
	switch p.tok.Kind {
	case tDef:
		p.definition()
		if !endsExpression(p.tok.Kind) {
			p.pipe(ctx)
		}
		return
	case tLabel:
		p.advance()
		p.binding()
		p.expect(tPipe)
		p.pipe(ctx)
		return
	}
	p.comma(ctx)
	if p.tok.Kind == tPipe {
		p.advance()
		p.pipe(ctx)
	}
}

func (p *parser) binding() {
	if p.tok.Kind != tVar {
		p.unexpected()
	}
	p.reclassify(p.tok, lang.Variable, lang.Declaration)
	p.advance()
}

func (p *parser) definition() {
	p.advance()
	p.push(tSemicolon)
	if p.tok.Kind != tIdent {
		p.unexpected()
	}
	p.reclassify(p.tok, lang.Function, lang.Declaration)
	p.advance()
	if p.tok.Kind == tLParen {
		open := p.tok
		p.advance()
		for {
			if p.tok.Kind != tIdent && p.tok.Kind != tVar {
				p.unexpected()
			}
			kind := lang.Parameter
			if p.tok.Kind == tIdent {
				kind = lang.Function
			}
			p.reclassify(p.tok, kind, lang.Declaration)
			p.advance()
			if p.tok.Kind != tSemicolon {
				break
			}
			p.advance()
		}
		p.closing(tRParen, open)
	}
	p.expect(tColon)
	p.pipe(0)
	p.expect(tSemicolon)
	p.pop(tSemicolon)
}

func (p *parser) comma(ctx context) {
	p.alternative(ctx)
	for ctx&noComma == 0 && p.tok.Kind == tComma {
		p.advance()
		p.alternative(ctx)
	}
}

func (p *parser) alternative(ctx context) {
	p.assignment(ctx)
	if p.tok.Kind == tAlt || p.tok.Kind == tAltDestructure {
		p.advance()
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
	for isAssignment(p.tok.Kind) {
		p.advance()
		p.or(ctx)
	}
}

func (p *parser) or(ctx context) {
	p.and(ctx)
	for p.tok.Kind == tOr {
		p.advance()
		p.and(ctx)
	}
}

func (p *parser) and(ctx context) {
	p.comparison(ctx)
	for p.tok.Kind == tAnd {
		p.advance()
		p.comparison(ctx)
	}
}

func (p *parser) comparison(ctx context) {
	p.additive(ctx)
	for {
		switch p.tok.Kind {
		case tEqual, tNotEqual, tLess, tLessEq, tGreater, tGreaterEq:
			p.advance()
			p.additive(ctx)
		default:
			return
		}
	}
}

func (p *parser) additive(ctx context) {
	p.multiplicative(ctx)
	for p.tok.Kind == tAdd || p.tok.Kind == tSub {
		p.advance()
		p.multiplicative(ctx)
	}
}

func (p *parser) multiplicative(ctx context) {
	p.unary(ctx)
	for p.tok.Kind == tMul || p.tok.Kind == tDiv || p.tok.Kind == tMod {
		p.advance()
		p.unary(ctx)
	}
}

func (p *parser) unary(ctx context) {
	if p.tok.Kind == tSub {
		p.advance()
		p.unary(ctx)
		return
	}
	p.postfix(ctx)
}

func (p *parser) postfix(ctx context) {
	if p.tok.Kind == tTry {
		p.advance()
		p.postfix(ctx | noBinding)
		if p.tok.Kind == tCatch {
			p.advance()
			p.postfix(ctx | noBinding)
		}
		return
	}
	p.term()
	p.suffixes()
	if p.tok.Kind == tAs && ctx&noBinding == 0 {
		p.advance()
		p.patterns()
		p.expect(tPipe)
		p.pipe(ctx)
	}
}

func (p *parser) suffixes() {
	for {
		switch p.tok.Kind {
		case tField, tQuestion:
			p.advance()
		case tDot:
			p.advance()
			switch p.tok.Kind {
			case tString, tStringHead:
				p.str()
			case tLBracket:
			default:
				p.unexpected()
			}
		case tLBracket:
			p.index()
		default:
			return
		}
	}
}

func (p *parser) index() {
	open := p.tok
	p.advance()
	if p.tok.Kind == tRBracket {
		p.advance()
		return
	}
	if p.tok.Kind != tColon {
		p.pipe(0)
	}
	if p.tok.Kind == tColon {
		p.advance()
		if p.tok.Kind != tRBracket {
			p.pipe(0)
		}
	}
	p.closing(tRBracket, open)
}

func (p *parser) str() {
	head := p.tok
	if head.Kind == tString {
		p.advance()
		return
	}
	p.advance()
	for {
		p.pipe(0)
		switch p.tok.Kind {
		case tStringMiddle:
			p.advance()
		case tStringTail:
			p.advance()
			return
		case tEOF:
			p.failAt(head, "unterminated string")
		default:
			p.unexpected()
		}
	}
}

func (p *parser) term() {
	switch p.tok.Kind {
	case tNumber, tField, tDotDot, tVar:
		p.advance()
	case tString, tStringHead:
		p.str()
	case tFormat:
		p.advance()
		if p.tok.Kind == tString || p.tok.Kind == tStringHead {
			p.str()
		}
	case tDot:
		p.advance()
		if p.tok.Kind == tString || p.tok.Kind == tStringHead {
			p.str()
		}
	case tLParen:
		open := p.tok
		p.advance()
		p.pipe(0)
		p.closing(tRParen, open)
	case tLBracket:
		open := p.tok
		p.advance()
		if p.tok.Kind != tRBracket {
			p.pipe(0)
		}
		p.closing(tRBracket, open)
	case tLBrace:
		p.object()
	case tIdent:
		name := p.tok
		p.advance()
		if p.tok.Kind == tLParen {
			p.arguments(p.lex.src[name.Start:name.End])
		}
	case tReduce, tForeach:
		p.fold()
	case tIf:
		p.conditional()
	case tBreak:
		p.advance()
		if p.tok.Kind != tVar {
			p.unexpected()
		}
		p.advance()
	default:
		p.unexpected()
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
	open := p.tok
	p.advance()
	call, isRegex := regexCalls[function]
	var pattern, flags token
	literalPattern, literalFlags := false, false
	count := 0
	for {
		first := p.tok
		p.pipe(0)
		literal := first.Kind == tString && p.last == first
		switch count {
		case 0:
			pattern, literalPattern = first, literal
		case call.flags:
			flags, literalFlags = first, literal
		}
		count++
		if p.tok.Kind != tSemicolon {
			break
		}
		p.advance()
	}
	p.closing(tRParen, open)
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
	keyword := p.tok.Kind
	p.advance()
	p.postfix(noBinding)
	p.expect(tAs)
	p.patterns()
	open := p.tok
	p.expect(tLParen)
	p.pipe(0)
	p.expect(tSemicolon)
	p.pipe(0)
	if keyword == tForeach && p.tok.Kind == tSemicolon {
		p.advance()
		p.pipe(0)
	}
	p.closing(tRParen, open)
}

func (p *parser) conditional() {
	open := p.tok
	p.advance()
	p.push(tEnd)
	p.pipe(0)
	p.expect(tThen)
	p.pipe(0)
	for p.tok.Kind == tElif {
		p.advance()
		p.pipe(0)
		p.expect(tThen)
		p.pipe(0)
	}
	if p.tok.Kind == tElse {
		p.advance()
		p.pipe(0)
	}
	p.closing(tEnd, open)
	p.pop(tEnd)
}

func (p *parser) patterns() {
	p.pattern()
	for p.tok.Kind == tAltDestructure {
		p.advance()
		p.pattern()
	}
}

func (p *parser) pattern() {
	open := p.tok
	switch p.tok.Kind {
	case tVar:
		p.binding()
	case tLBracket:
		p.advance()
		p.pattern()
		for p.tok.Kind == tComma {
			p.advance()
			p.pattern()
		}
		p.closing(tRBracket, open)
	case tLBrace:
		p.advance()
		p.objectPattern()
		for p.tok.Kind == tComma {
			p.advance()
			p.objectPattern()
		}
		p.closing(tRBrace, open)
	default:
		p.unexpected()
	}
}

func (p *parser) objectPattern() {
	if p.tok.Kind == tVar {
		p.binding()
		if p.tok.Kind != tColon {
			return
		}
		p.advance()
		p.pattern()
		return
	}
	p.key()
	p.expect(tColon)
	p.pattern()
}

func (p *parser) key() bool {
	switch {
	case p.tok.Kind == tIdent || isKeyword(p.tok.Kind):
		p.reclassify(p.tok, lang.Property, 0)
		p.advance()
	case p.tok.Kind == tVar:
		p.advance()
	case p.tok.Kind == tString || p.tok.Kind == tStringHead:
		p.str()
	case p.tok.Kind == tFormat:
		p.advance()
		if p.tok.Kind == tString || p.tok.Kind == tStringHead {
			p.str()
		}
	case p.tok.Kind == tLParen:
		open := p.tok
		p.advance()
		p.pipe(0)
		p.closing(tRParen, open)
		return true
	default:
		p.unexpected()
	}
	return false
}

func isKeyword(k kind) bool { return k >= tDef && k <= tBreak }

func (p *parser) object() {
	open := p.expect(tLBrace)
	for p.tok.Kind != tRBrace && p.tok.Kind != tEOF {
		if computed := p.key(); computed || p.tok.Kind == tColon {
			p.expect(tColon)
			p.pipe(noComma)
		}
		if p.tok.Kind != tComma {
			break
		}
		p.advance()
	}
	p.closing(tRBrace, open)
}
