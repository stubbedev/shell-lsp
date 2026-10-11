package jq

import "github.com/stubbedev/shell-lsp/internal/embedded/lang"

type context uint8

const (
	noComma context = 1 << iota
	noBinding
)

type bailout struct{}

type parser struct {
	lex lexer
	tok token
}

func Parse(src string) lang.Result {
	res := lang.NewResult(src)
	p := &parser{lex: lexer{src: src, res: &res}}
	p.run()
	for p.tok.kind != tEOF {
		p.advance()
	}
	return res
}

func (p *parser) run() {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
		}
	}()
	p.advance()
	p.program()
}

func (p *parser) advance() { p.tok = p.lex.next() }

func (p *parser) failAt(t token, message string) {
	p.lex.res.Fail(t.start, t.end, message)
	panic(bailout{})
}

func (p *parser) unexpected() {
	p.failAt(p.tok, lang.Unexpected(p.lex.src, p.tok.start, p.tok.end))
}

func (p *parser) expect(k kind) token {
	if p.tok.kind != k {
		p.unexpected()
	}
	t := p.tok
	p.advance()
	return t
}

func (p *parser) closing(k kind, open token) {
	if p.tok.kind == tEOF {
		p.failAt(open, lang.Unclosed(p.lex.src, open.start, open.end))
	}
	p.expect(k)
}

func (p *parser) reclassify(t token, k lang.Kind, mods lang.Modifier) {
	if t.sem >= 0 {
		p.lex.res.Tokens[t.sem].Kind = k
		p.lex.res.Tokens[t.sem].Modifiers = mods
	}
}

func (p *parser) program() {
	if p.tok.kind == tModule {
		p.advance()
		p.object()
		p.expect(tSemicolon)
	}
	for p.tok.kind == tImport || p.tok.kind == tInclude {
		directive := p.tok.kind
		p.advance()
		p.expect(tString)
		if directive == tImport {
			p.expect(tAs)
			if p.tok.kind != tIdent && p.tok.kind != tVar {
				p.unexpected()
			}
			p.advance()
		}
		if p.tok.kind == tLBrace {
			p.object()
		}
		p.expect(tSemicolon)
	}
	if p.tok.kind == tEOF {
		return
	}
	p.pipe(0)
	if p.tok.kind != tEOF {
		p.unexpected()
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
	switch p.tok.kind {
	case tDef:
		p.definition()
		if !endsExpression(p.tok.kind) {
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
	if p.tok.kind == tPipe {
		p.advance()
		p.pipe(ctx)
	}
}

func (p *parser) binding() {
	if p.tok.kind != tVar {
		p.unexpected()
	}
	p.reclassify(p.tok, lang.Variable, lang.Declaration)
	p.advance()
}

func (p *parser) definition() {
	p.advance()
	if p.tok.kind != tIdent {
		p.unexpected()
	}
	p.reclassify(p.tok, lang.Function, lang.Declaration)
	p.advance()
	if p.tok.kind == tLParen {
		open := p.tok
		p.advance()
		for {
			if p.tok.kind != tIdent && p.tok.kind != tVar {
				p.unexpected()
			}
			kind := lang.Parameter
			if p.tok.kind == tIdent {
				kind = lang.Function
			}
			p.reclassify(p.tok, kind, lang.Declaration)
			p.advance()
			if p.tok.kind != tSemicolon {
				break
			}
			p.advance()
		}
		p.closing(tRParen, open)
	}
	p.expect(tColon)
	p.pipe(0)
	p.expect(tSemicolon)
}

func (p *parser) comma(ctx context) {
	p.alternative(ctx)
	for ctx&noComma == 0 && p.tok.kind == tComma {
		p.advance()
		p.alternative(ctx)
	}
}

func (p *parser) alternative(ctx context) {
	p.assignment(ctx)
	if p.tok.kind == tAlt || p.tok.kind == tAltDestructure {
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
	for isAssignment(p.tok.kind) {
		p.advance()
		p.or(ctx)
	}
}

func (p *parser) or(ctx context) {
	p.and(ctx)
	for p.tok.kind == tOr {
		p.advance()
		p.and(ctx)
	}
}

func (p *parser) and(ctx context) {
	p.comparison(ctx)
	for p.tok.kind == tAnd {
		p.advance()
		p.comparison(ctx)
	}
}

func (p *parser) comparison(ctx context) {
	p.additive(ctx)
	for {
		switch p.tok.kind {
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
	for p.tok.kind == tAdd || p.tok.kind == tSub {
		p.advance()
		p.multiplicative(ctx)
	}
}

func (p *parser) multiplicative(ctx context) {
	p.unary(ctx)
	for p.tok.kind == tMul || p.tok.kind == tDiv || p.tok.kind == tMod {
		p.advance()
		p.unary(ctx)
	}
}

func (p *parser) unary(ctx context) {
	if p.tok.kind == tSub {
		p.advance()
		p.unary(ctx)
		return
	}
	p.postfix(ctx)
}

func (p *parser) postfix(ctx context) {
	if p.tok.kind == tTry {
		p.advance()
		p.postfix(ctx | noBinding)
		if p.tok.kind == tCatch {
			p.advance()
			p.postfix(ctx | noBinding)
		}
		return
	}
	p.term()
	p.suffixes()
	if p.tok.kind == tAs && ctx&noBinding == 0 {
		p.advance()
		p.patterns()
		p.expect(tPipe)
		p.pipe(ctx)
	}
}

func (p *parser) suffixes() {
	for {
		switch p.tok.kind {
		case tField, tQuestion:
			p.advance()
		case tDot:
			p.advance()
			switch p.tok.kind {
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
	if p.tok.kind == tRBracket {
		p.advance()
		return
	}
	if p.tok.kind != tColon {
		p.pipe(0)
	}
	if p.tok.kind == tColon {
		p.advance()
		if p.tok.kind != tRBracket {
			p.pipe(0)
		}
	}
	p.closing(tRBracket, open)
}

func (p *parser) str() {
	head := p.tok
	if head.kind == tString {
		p.advance()
		return
	}
	p.advance()
	for {
		p.pipe(0)
		switch p.tok.kind {
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
	switch p.tok.kind {
	case tNumber, tField, tDotDot, tVar:
		p.advance()
	case tString, tStringHead:
		p.str()
	case tFormat:
		p.advance()
		if p.tok.kind == tString || p.tok.kind == tStringHead {
			p.str()
		}
	case tDot:
		p.advance()
		if p.tok.kind == tString || p.tok.kind == tStringHead {
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
		if p.tok.kind != tRBracket {
			p.pipe(0)
		}
		p.closing(tRBracket, open)
	case tLBrace:
		p.object()
	case tIdent:
		p.advance()
		if p.tok.kind == tLParen {
			p.arguments()
		}
	case tReduce, tForeach:
		p.fold()
	case tIf:
		p.conditional()
	case tBreak:
		p.advance()
		if p.tok.kind != tVar {
			p.unexpected()
		}
		p.advance()
	default:
		p.unexpected()
	}
}

func (p *parser) arguments() {
	open := p.tok
	p.advance()
	p.pipe(0)
	for p.tok.kind == tSemicolon {
		p.advance()
		p.pipe(0)
	}
	p.closing(tRParen, open)
}

func (p *parser) fold() {
	keyword := p.tok.kind
	p.advance()
	p.postfix(noBinding)
	p.expect(tAs)
	p.patterns()
	open := p.tok
	p.expect(tLParen)
	p.pipe(0)
	p.expect(tSemicolon)
	p.pipe(0)
	if keyword == tForeach && p.tok.kind == tSemicolon {
		p.advance()
		p.pipe(0)
	}
	p.closing(tRParen, open)
}

func (p *parser) conditional() {
	open := p.tok
	p.advance()
	p.pipe(0)
	p.expect(tThen)
	p.pipe(0)
	for p.tok.kind == tElif {
		p.advance()
		p.pipe(0)
		p.expect(tThen)
		p.pipe(0)
	}
	if p.tok.kind == tElse {
		p.advance()
		p.pipe(0)
	}
	p.closing(tEnd, open)
}

func (p *parser) patterns() {
	p.pattern()
	for p.tok.kind == tAltDestructure {
		p.advance()
		p.pattern()
	}
}

func (p *parser) pattern() {
	open := p.tok
	switch p.tok.kind {
	case tVar:
		p.binding()
	case tLBracket:
		p.advance()
		p.pattern()
		for p.tok.kind == tComma {
			p.advance()
			p.pattern()
		}
		p.closing(tRBracket, open)
	case tLBrace:
		p.advance()
		p.objectPattern()
		for p.tok.kind == tComma {
			p.advance()
			p.objectPattern()
		}
		p.closing(tRBrace, open)
	default:
		p.unexpected()
	}
}

func (p *parser) objectPattern() {
	if p.tok.kind == tVar {
		p.binding()
		if p.tok.kind != tColon {
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
	case p.tok.kind == tIdent || isKeyword(p.tok.kind):
		p.reclassify(p.tok, lang.Property, 0)
		p.advance()
	case p.tok.kind == tVar:
		p.advance()
	case p.tok.kind == tString || p.tok.kind == tStringHead:
		p.str()
	case p.tok.kind == tFormat:
		p.advance()
		if p.tok.kind == tString || p.tok.kind == tStringHead {
			p.str()
		}
	case p.tok.kind == tLParen:
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
	for p.tok.kind != tRBrace && p.tok.kind != tEOF {
		if computed := p.key(); computed || p.tok.kind == tColon {
			p.expect(tColon)
			p.pipe(noComma)
		}
		if p.tok.kind != tComma {
			break
		}
		p.advance()
	}
	p.closing(tRBrace, open)
}
