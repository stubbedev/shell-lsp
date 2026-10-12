package awk

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/embedded/regex"
)

type context uint8

const (
	inPrint context = 1 << iota
)

type value uint8

const (
	plain value = iota
	assignable
	membership
)

type parser struct {
	lex    lexer
	tok    token
	last   token
	peeked *token
	params []string
	open   lang.Nesting[kind]
}

func Parse(src string) lang.Result {
	res := lang.NewResult(src)
	p := &parser{lex: lexer{src: src, prev: tNewline, res: &res}}
	lang.Run(func() {
		p.advance()
		p.program()
	})
	p.drain()
	return res
}

func (p *parser) drain() {
	p.lex.quiet = true
	for p.tok.Kind != tEOF {
		p.tok = p.next()
	}
}

func (p *parser) attempt(base int, parse func()) bool {
	if lang.Attempt(parse) {
		return true
	}
	p.resync(base)
	return false
}

func (p *parser) resync(base int) {
	p.lex.quiet = true
	defer func() { p.lex.quiet = false }()
	for {
		k := p.tok.Kind
		switch {
		case k == tEOF || k == tUnterminated || !p.open.Tracked():
			lang.Abort()
		case p.open.Depth() == base && (k == tNewline || k == tSemicolon || (k == tRBrace && base > 0)):
			return
		case p.open.Depth() == base && isCloser(k):
			p.tok = p.next()
			continue
		}
		p.advance()
	}
}

func isCloser(k kind) bool { return k == tRBrace || k == tRParen || k == tRBracket }

func (p *parser) track(k kind) {
	switch k {
	case tLBrace:
		p.open.Push(tRBrace)
	case tLParen:
		p.open.Push(tRParen)
	case tLBracket:
		p.open.Push(tRBracket)
	case tRBrace, tRParen, tRBracket:
		if !p.open.Pop(k) {
			lang.Abort()
		}
	}
}

func (p *parser) advance() {
	p.track(p.tok.Kind)
	p.last = p.tok
	p.tok = p.next()
}

func (p *parser) next() token {
	if p.peeked != nil {
		next := *p.peeked
		p.peeked = nil
		return next
	}
	return p.lex.next()
}

func (p *parser) peek() token {
	if p.peeked == nil {
		next := p.lex.next()
		p.peeked = &next
	}
	return *p.peeked
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

func (p *parser) newlines() {
	for p.tok.Kind == tNewline {
		p.advance()
	}
}

func (p *parser) terminators() {
	for p.tok.Kind == tNewline || p.tok.Kind == tSemicolon {
		p.advance()
	}
}

func (p *parser) program() {
	for !p.attempt(0, p.items) {
		p.params = p.params[:0]
	}
}

func (p *parser) items() {
	p.terminators()
	for p.tok.Kind != tEOF {
		p.item()
		p.terminators()
	}
}

func (p *parser) is(word string) bool {
	return (p.tok.Kind == tName || p.tok.Kind == tFuncName) && p.lex.src[p.tok.Start:p.tok.End] == word
}

func (p *parser) contextual() {
	p.reclassify(p.tok, lang.Keyword, 0)
	if p.peeked == nil {
		p.lex.prev = tContextual
	}
	p.advance()
}

func (p *parser) item() {
	switch {
	case p.tok.Kind == tAt:
		if p.directive() {
			return
		}
	case p.tok.Kind == tFunction, p.is("func") && startsName(p.peek().Kind):
		p.function()
		return
	case p.tok.Kind == tBegin, p.tok.Kind == tEnd, (p.is("BEGINFILE") || p.is("ENDFILE")) && p.peek().Kind == tLBrace:
		special := p.tok
		p.contextual()
		if p.tok.Kind != tLBrace {
			p.failAt(special, lang.Quote(p.lex.src, special.Start, special.End)+" needs an action in braces on the same line")
		}
		p.block()
		return
	case p.tok.Kind == tLBrace:
		p.block()
		return
	}
	p.expr(0)
	if p.tok.Kind == tComma {
		p.advance()
		p.newlines()
		p.expr(0)
	}
	if p.tok.Kind == tLBrace {
		p.block()
		return
	}
	switch p.tok.Kind {
	case tNewline, tSemicolon, tEOF:
	default:
		p.unexpected()
	}
}

func (p *parser) directive() bool {
	name := p.peek()
	if name.Kind != tName {
		return false
	}
	switch p.lex.src[name.Start:name.End] {
	case "include", "load", "namespace":
	default:
		return false
	}
	p.advance()
	p.reclassify(p.tok, lang.Keyword, 0)
	p.advance()
	p.expect(tString)
	return true
}

func startsName(k kind) bool { return k == tName || k == tFuncName }

func (p *parser) function() {
	p.contextual()
	name := p.tok
	switch name.Kind {
	case tName, tFuncName:
		p.reclassify(name, lang.Function, lang.Declaration)
	case tBuiltinFunc:
		p.failAt(name, lang.Quote(p.lex.src, name.Start, name.End)+" is a built-in function and cannot be redefined")
	default:
		p.unexpected()
	}
	p.advance()
	open := p.expect(tLParen)
	p.params = p.params[:0]
	for p.tok.Kind == tName {
		p.reclassify(p.tok, lang.Parameter, lang.Declaration)
		p.params = append(p.params, p.lex.src[p.tok.Start:p.tok.End])
		p.advance()
		if p.tok.Kind != tComma {
			break
		}
		p.advance()
		p.newlines()
	}
	p.closing(tRParen, open)
	p.newlines()
	if p.tok.Kind != tLBrace {
		p.unexpected()
	}
	p.block()
	p.params = p.params[:0]
}

func (p *parser) block() {
	open := p.expect(tLBrace)
	base := p.open.Depth()
	for !p.attempt(base, func() { p.blockBody(open, p.statement) }) {
	}
}

func (p *parser) blockBody(open token, parse func()) {
	for {
		p.terminators()
		switch p.tok.Kind {
		case tRBrace:
			p.advance()
			return
		case tEOF:
			p.failAt(open, lang.Unclosed(p.lex.src, open.Start, open.End))
		}
		parse()
	}
}

func (p *parser) statement() {
	if p.is("switch") && (p.tok.Kind == tFuncName || p.peek().Kind == tLParen) {
		p.switchStatement()
		return
	}
	switch p.tok.Kind {
	case tLBrace:
		p.block()
	case tIf:
		p.advance()
		p.condition()
		p.body()
		for p.tok.Kind == tNewline || p.tok.Kind == tSemicolon {
			p.advance()
		}
		if p.tok.Kind == tElse {
			p.advance()
			p.body()
		}
	case tWhile:
		p.advance()
		p.condition()
		p.body()
	case tDo:
		p.advance()
		p.body()
		p.terminators()
		p.expect(tWhile)
		p.condition()
		p.endSimple()
	case tFor:
		p.forStatement()
	case tSemicolon:
		p.advance()
	default:
		p.simple()
		p.endSimple()
	}
}

func (p *parser) body() {
	p.newlines()
	if p.tok.Kind == tSemicolon {
		p.advance()
		return
	}
	p.statement()
}

func (p *parser) condition() {
	open := p.expect(tLParen)
	p.expr(0)
	p.closing(tRParen, open)
}

func (p *parser) endSimple() {
	switch p.tok.Kind {
	case tSemicolon, tNewline:
		p.advance()
	case tRBrace, tEOF, tElse:
	default:
		p.unexpected()
	}
}

func (p *parser) forStatement() {
	p.advance()
	open := p.expect(tLParen)
	if p.tok.Kind != tSemicolon {
		if p.simple() == membership && p.tok.Kind == tRParen {
			p.advance()
			p.body()
			return
		}
	}
	p.expect(tSemicolon)
	p.newlines()
	if p.tok.Kind != tSemicolon {
		p.expr(0)
	}
	p.expect(tSemicolon)
	p.newlines()
	if p.tok.Kind != tRParen {
		p.simple()
	}
	p.closing(tRParen, open)
	p.body()
}

func (p *parser) switchStatement() {
	p.contextual()
	p.condition()
	p.newlines()
	open := p.expect(tLBrace)
	base := p.open.Depth()
	for !p.attempt(base, func() { p.blockBody(open, p.switchItem) }) {
	}
}

func (p *parser) switchItem() {
	switch {
	case p.is("case"):
		p.contextual()
		if p.tok.Kind == tSub || p.tok.Kind == tAdd {
			p.advance()
		}
		switch p.tok.Kind {
		case tNumber, tString, tRegexp:
			p.advance()
		default:
			p.unexpected()
		}
		p.expect(tColon)
	case p.is("default"):
		p.contextual()
		p.expect(tColon)
	default:
		p.statement()
	}
}

func (p *parser) simple() value {
	switch p.tok.Kind {
	case tPrint, tPrintf:
		p.print()
	case tDelete:
		p.advance()
		if p.tok.Kind == tLParen {
			open := p.tok
			p.advance()
			p.arrayElement()
			p.closing(tRParen, open)
			return plain
		}
		p.arrayElement()
	case tExit, tReturn:
		p.advance()
		if startsOperand(p.tok.Kind) {
			p.expr(0)
		}
	case tNext, tNextFile, tBreak, tContinue:
		p.advance()
	default:
		return p.expr(0)
	}
	return plain
}

func (p *parser) arrayElement() {
	if p.tok.Kind != tName {
		p.unexpected()
	}
	p.name()
	if p.tok.Kind == tLBracket {
		p.subscript()
	}
}

func (p *parser) print() {
	p.advance()
	if startsOperand(p.tok.Kind) {
		p.list(inPrint)
	}
	switch p.tok.Kind {
	case tGreater, tAppend, tPipe, tPipeBoth:
		p.advance()
		p.concatenation(inPrint)
	}
}

func (p *parser) list(ctx context) {
	p.expr(ctx)
	for p.tok.Kind == tComma {
		p.advance()
		p.newlines()
		p.expr(ctx)
	}
}

func startsOperand(k kind) bool {
	switch k {
	case tName, tFuncName, tBuiltinFunc, tNumber, tString, tRegexp, tDollar, tNot, tSub, tAdd,
		tLParen, tIncr, tDecr, tGetline, tAt:
		return true
	}
	return false
}

func startsConcatenated(k kind) bool {
	switch k {
	case tName, tFuncName, tBuiltinFunc, tNumber, tString, tRegexp, tDollar, tNot, tLParen, tIncr, tDecr, tAt:
		return true
	}
	return false
}

func isAssignment(k kind) bool {
	switch k {
	case tAssign, tAddAssign, tSubAssign, tMulAssign, tDivAssign, tModAssign, tPowAssign:
		return true
	}
	return false
}

func (p *parser) expr(ctx context) value {
	left := p.ternary(ctx)
	if !isAssignment(p.tok.Kind) {
		return left
	}
	if left != assignable {
		p.failAt(p.tok, "the left side of "+lang.Quote(p.lex.src, p.tok.Start, p.tok.End)+" is not a variable, field or array element")
	}
	p.advance()
	p.newlines()
	p.expr(ctx)
	return plain
}

func (p *parser) ternary(ctx context) value {
	cond := p.or(ctx)
	if p.tok.Kind != tQuestion {
		return cond
	}
	p.advance()
	p.newlines()
	p.expr(ctx)
	p.newlines()
	p.expect(tColon)
	p.newlines()
	p.expr(ctx)
	return plain
}

func (p *parser) or(ctx context) value {
	left := p.and(ctx)
	for p.tok.Kind == tOr {
		p.advance()
		p.newlines()
		p.and(ctx)
		left = plain
	}
	return left
}

func (p *parser) and(ctx context) value {
	left := p.in(ctx)
	for p.tok.Kind == tAnd {
		p.advance()
		p.newlines()
		p.in(ctx)
		left = plain
	}
	return left
}

func (p *parser) in(ctx context) value {
	left := p.match(ctx)
	for p.tok.Kind == tIn {
		p.advance()
		if p.tok.Kind != tName {
			p.unexpected()
		}
		p.name()
		left = membership
	}
	return left
}

func (p *parser) match(ctx context) value {
	left := p.comparison(ctx)
	for p.tok.Kind == tMatch || p.tok.Kind == tNoMatch {
		p.advance()
		operand := p.tok
		p.comparison(ctx)
		if operand.Kind == tString && p.last == operand {
			p.dynamicRegex(operand)
		}
		left = plain
	}
	return left
}

var stringEscapes = map[byte]string{
	'\\': `\`, '"': `"`, '/': "/", 'a': "\a", 'b': "\b", 'f': "\f", 'n': "\n", 'r': "\r", 't': "\t", 'v': "\v",
}

func (p *parser) dynamicRegex(t token) {
	src, start, end := p.lex.src, t.Start+1, t.End-1
	if !strings.Contains(src[start:end], `\`) {
		p.lex.res.CheckRegex(lang.Verbatim(src, start, end), regex.Awk)
		return
	}
	b := lang.NewExcerptBuilder(end - start)
	for i := start; i < end; i++ {
		if src[i] != '\\' {
			b.Add(src[i:i+1], i, i+1)
			continue
		}
		decoded, known := stringEscapes[src[i+1]]
		if !known {
			return
		}
		b.Add(decoded, i, i+2)
		i++
	}
	p.lex.res.CheckRegex(b.Excerpt(), regex.Awk)
}

func (p *parser) comparison(ctx context) value {
	left := p.piped(ctx)
	for {
		switch p.tok.Kind {
		case tLess, tLessEq, tEqual, tNotEqual, tGreaterEq:
		case tGreater:
			if ctx&inPrint != 0 {
				return left
			}
		default:
			return left
		}
		p.advance()
		p.piped(ctx)
		left = plain
	}
}

func (p *parser) piped(ctx context) value {
	left := p.concatenation(ctx)
	for ctx&inPrint == 0 && (p.tok.Kind == tPipe || p.tok.Kind == tPipeBoth) {
		p.advance()
		if p.tok.Kind != tGetline {
			p.failAt(p.tok, "expected `getline` after `|`, found "+lang.Quote(p.lex.src, p.tok.Start, p.tok.End))
		}
		p.advance()
		p.getlineTarget()
		left = plain
	}
	return left
}

func (p *parser) concatenation(ctx context) value {
	left := p.additive(ctx)
	for startsConcatenated(p.tok.Kind) {
		p.additive(ctx)
		left = plain
	}
	return left
}

func (p *parser) additive(ctx context) value {
	left := p.multiplicative(ctx)
	for p.tok.Kind == tAdd || p.tok.Kind == tSub {
		p.advance()
		p.multiplicative(ctx)
		left = plain
	}
	return left
}

func (p *parser) multiplicative(ctx context) value {
	left := p.unary(ctx)
	for p.tok.Kind == tMul || p.tok.Kind == tDiv || p.tok.Kind == tMod {
		p.advance()
		p.unary(ctx)
		left = plain
	}
	return left
}

func (p *parser) unary(ctx context) value {
	switch p.tok.Kind {
	case tNot, tSub, tAdd:
		p.advance()
		p.unary(ctx)
		return plain
	}
	return p.power(ctx)
}

func (p *parser) power(ctx context) value {
	left := p.postfix(ctx)
	if p.tok.Kind != tPow {
		return left
	}
	p.advance()
	p.unary(ctx)
	return plain
}

func (p *parser) postfix(ctx context) value {
	if p.tok.Kind == tIncr || p.tok.Kind == tDecr {
		op := p.tok
		p.advance()
		if p.postfix(ctx) != assignable {
			p.failAt(op, lang.Quote(p.lex.src, op.Start, op.End)+" needs a variable, field or array element")
		}
		return plain
	}
	left := p.primary(ctx)
	if left == assignable && (p.tok.Kind == tIncr || p.tok.Kind == tDecr) {
		p.advance()
		return plain
	}
	return left
}

func (p *parser) primary(ctx context) value {
	switch p.tok.Kind {
	case tNumber, tString, tRegexp:
		p.advance()
		return plain
	case tLParen:
		open := p.tok
		p.advance()
		p.newlines()
		p.list(0)
		p.newlines()
		p.closing(tRParen, open)
		return plain
	case tDollar:
		p.advance()
		switch p.tok.Kind {
		case tIncr, tDecr, tSub, tAdd, tNot:
			p.unary(ctx)
		default:
			p.primary(ctx)
		}
		return assignable
	case tName:
		p.name()
		if p.tok.Kind == tLBracket {
			p.subscript()
		}
		return assignable
	case tFuncName, tBuiltinFunc:
		p.advance()
		if p.tok.Kind == tLParen {
			p.arguments()
		}
		return plain
	case tGetline:
		p.advance()
		p.getlineTarget()
		if p.tok.Kind == tLess {
			p.advance()
			p.postfix(ctx)
		}
		return plain
	case tAt:
		p.advance()
		switch p.tok.Kind {
		case tRegexp:
			p.advance()
		case tFuncName:
			p.advance()
			p.arguments()
		default:
			p.unexpected()
		}
		return plain
	case tNot, tSub, tAdd, tIncr, tDecr:
		return p.unary(ctx)
	}
	p.unexpected()
	return plain
}

func (p *parser) getlineTarget() {
	switch p.tok.Kind {
	case tName, tDollar:
		p.primary(0)
	}
}

func (p *parser) name() {
	if slices.Contains(p.params, p.lex.src[p.tok.Start:p.tok.End]) {
		p.reclassify(p.tok, lang.Parameter, 0)
	}
	p.advance()
}

func (p *parser) subscript() {
	open := p.tok
	p.advance()
	p.newlines()
	p.list(0)
	p.closing(tRBracket, open)
}

func (p *parser) arguments() {
	open := p.expect(tLParen)
	p.newlines()
	if p.tok.Kind != tRParen {
		p.list(0)
		p.newlines()
	}
	p.closing(tRParen, open)
}
