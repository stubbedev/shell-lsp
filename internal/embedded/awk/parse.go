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

var brackets = lang.NewBrackets(map[kind]kind{tLBrace: tRBrace, tLParen: tRParen, tLBracket: tRBracket})

type parser struct {
	lang.Cursor[kind]
	lex    lexer
	peeked *token
	params []string
}

func Parse(src string) lang.Result {
	res := lang.NewResult(src)
	p := &parser{lex: lexer{src: src, prev: tNewline, res: &res}}
	p.Cursor = lang.NewCursor(src, &res, tEOF, brackets, p.next)
	lang.Run(func() {
		p.Advance()
		p.program()
	})
	p.lex.quiet = true
	p.Drain()
	return res
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
		k := p.Tok.Kind
		switch {
		case k == tEOF || k == tUnterminated || !p.Open.Tracked():
			lang.Abort()
		case p.Open.Depth() == base && (k == tNewline || k == tSemicolon || (k == tRBrace && base > 0)):
			return
		case p.Open.Depth() == base && p.Closes(k):
			p.Skip()
			continue
		}
		p.Advance()
	}
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

func (p *parser) newlines() {
	for p.Tok.Kind == tNewline {
		p.Advance()
	}
}

func (p *parser) terminators() {
	for p.Tok.Kind == tNewline || p.Tok.Kind == tSemicolon {
		p.Advance()
	}
}

func (p *parser) program() {
	for !p.attempt(0, p.items) {
		p.params = p.params[:0]
	}
}

func (p *parser) items() {
	p.terminators()
	for p.Tok.Kind != tEOF {
		p.item()
		p.terminators()
	}
}

func (p *parser) is(word string) bool {
	return (p.Tok.Kind == tName || p.Tok.Kind == tFuncName) && p.lex.src[p.Tok.Start:p.Tok.End] == word
}

func (p *parser) contextual() {
	p.Reclassify(p.Tok, lang.Keyword, 0)
	if p.peeked == nil {
		p.lex.prev = tContextual
	}
	p.Advance()
}

func (p *parser) item() {
	switch {
	case p.Tok.Kind == tAt:
		if p.directive() {
			return
		}
	case p.Tok.Kind == tFunction, p.is("func") && startsName(p.peek().Kind):
		p.function()
		return
	case p.Tok.Kind == tBegin, p.Tok.Kind == tEnd, (p.is("BEGINFILE") || p.is("ENDFILE")) && p.peek().Kind == tLBrace:
		special := p.Tok
		p.contextual()
		if p.Tok.Kind != tLBrace {
			p.FailAt(special, lang.Quote(p.lex.src, special.Start, special.End)+" needs an action in braces on the same line")
		}
		p.block()
		return
	case p.Tok.Kind == tLBrace:
		p.block()
		return
	}
	p.expr(0)
	if p.Tok.Kind == tComma {
		p.Advance()
		p.newlines()
		p.expr(0)
	}
	if p.Tok.Kind == tLBrace {
		p.block()
		return
	}
	switch p.Tok.Kind {
	case tNewline, tSemicolon, tEOF:
	default:
		p.Unexpected()
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
	p.Advance()
	p.Reclassify(p.Tok, lang.Keyword, 0)
	p.Advance()
	p.Expect(tString)
	return true
}

func startsName(k kind) bool { return k == tName || k == tFuncName }

func (p *parser) function() {
	p.contextual()
	name := p.Tok
	switch name.Kind {
	case tName, tFuncName:
		p.Reclassify(name, lang.Function, lang.Declaration)
	case tBuiltinFunc:
		p.FailAt(name, lang.Quote(p.lex.src, name.Start, name.End)+" is a built-in function and cannot be redefined")
	default:
		p.Unexpected()
	}
	p.Advance()
	open := p.Expect(tLParen)
	p.params = p.params[:0]
	for p.Tok.Kind == tName {
		p.Reclassify(p.Tok, lang.Parameter, lang.Declaration)
		p.params = append(p.params, p.lex.src[p.Tok.Start:p.Tok.End])
		p.Advance()
		if p.Tok.Kind != tComma {
			break
		}
		p.Advance()
		p.newlines()
	}
	p.Closing(tRParen, open)
	p.newlines()
	if p.Tok.Kind != tLBrace {
		p.Unexpected()
	}
	p.block()
	p.params = p.params[:0]
}

func (p *parser) block() {
	open := p.Expect(tLBrace)
	base := p.Open.Depth()
	for !p.attempt(base, func() { p.blockBody(open, p.statement) }) {
	}
}

func (p *parser) blockBody(open token, parse func()) {
	for {
		p.terminators()
		switch p.Tok.Kind {
		case tRBrace:
			p.Advance()
			return
		case tEOF:
			p.FailAt(open, lang.Unclosed(p.lex.src, open.Start, open.End))
		}
		parse()
	}
}

func (p *parser) statement() {
	if p.is("switch") && (p.Tok.Kind == tFuncName || p.peek().Kind == tLParen) {
		p.switchStatement()
		return
	}
	switch p.Tok.Kind {
	case tLBrace:
		p.block()
	case tIf:
		p.Advance()
		p.condition()
		p.body()
		for p.Tok.Kind == tNewline || p.Tok.Kind == tSemicolon {
			p.Advance()
		}
		if p.Tok.Kind == tElse {
			p.Advance()
			p.body()
		}
	case tWhile:
		p.Advance()
		p.condition()
		p.body()
	case tDo:
		p.Advance()
		p.body()
		p.terminators()
		p.Expect(tWhile)
		p.condition()
		p.endSimple()
	case tFor:
		p.forStatement()
	case tSemicolon:
		p.Advance()
	default:
		p.simple()
		p.endSimple()
	}
}

func (p *parser) body() {
	p.newlines()
	if p.Tok.Kind == tSemicolon {
		p.Advance()
		return
	}
	p.statement()
}

func (p *parser) condition() {
	open := p.Expect(tLParen)
	p.expr(0)
	p.Closing(tRParen, open)
}

func (p *parser) endSimple() {
	switch p.Tok.Kind {
	case tSemicolon, tNewline:
		p.Advance()
	case tRBrace, tEOF, tElse:
	default:
		p.Unexpected()
	}
}

func (p *parser) forStatement() {
	p.Advance()
	open := p.Expect(tLParen)
	if p.Tok.Kind != tSemicolon {
		if p.simple() == membership && p.Tok.Kind == tRParen {
			p.Advance()
			p.body()
			return
		}
	}
	p.Expect(tSemicolon)
	p.newlines()
	if p.Tok.Kind != tSemicolon {
		p.expr(0)
	}
	p.Expect(tSemicolon)
	p.newlines()
	if p.Tok.Kind != tRParen {
		p.simple()
	}
	p.Closing(tRParen, open)
	p.body()
}

func (p *parser) switchStatement() {
	p.contextual()
	p.condition()
	p.newlines()
	open := p.Expect(tLBrace)
	base := p.Open.Depth()
	for !p.attempt(base, func() { p.blockBody(open, p.switchItem) }) {
	}
}

func (p *parser) switchItem() {
	switch {
	case p.is("case"):
		p.contextual()
		if p.Tok.Kind == tSub || p.Tok.Kind == tAdd {
			p.Advance()
		}
		switch p.Tok.Kind {
		case tNumber, tString, tRegexp:
			p.Advance()
		default:
			p.Unexpected()
		}
		p.Expect(tColon)
	case p.is("default"):
		p.contextual()
		p.Expect(tColon)
	default:
		p.statement()
	}
}

func (p *parser) simple() value {
	switch p.Tok.Kind {
	case tPrint, tPrintf:
		p.print()
	case tDelete:
		p.Advance()
		if p.Tok.Kind == tLParen {
			open := p.Tok
			p.Advance()
			p.arrayElement()
			p.Closing(tRParen, open)
			return plain
		}
		p.arrayElement()
	case tExit, tReturn:
		p.Advance()
		if startsOperand(p.Tok.Kind) {
			p.expr(0)
		}
	case tNext, tNextFile, tBreak, tContinue:
		p.Advance()
	default:
		return p.expr(0)
	}
	return plain
}

func (p *parser) arrayElement() {
	if p.Tok.Kind != tName {
		p.Unexpected()
	}
	p.name()
	if p.Tok.Kind == tLBracket {
		p.subscript()
	}
}

func (p *parser) print() {
	p.Advance()
	if startsOperand(p.Tok.Kind) {
		p.list(inPrint)
	}
	switch p.Tok.Kind {
	case tGreater, tAppend, tPipe, tPipeBoth:
		p.Advance()
		p.concatenation(inPrint)
	}
}

func (p *parser) list(ctx context) {
	p.expr(ctx)
	for p.Tok.Kind == tComma {
		p.Advance()
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
	if !isAssignment(p.Tok.Kind) {
		return left
	}
	if left != assignable {
		p.FailAt(p.Tok, "the left side of "+lang.Quote(p.lex.src, p.Tok.Start, p.Tok.End)+" is not a variable, field or array element")
	}
	p.Advance()
	p.newlines()
	p.expr(ctx)
	return plain
}

func (p *parser) ternary(ctx context) value {
	cond := p.or(ctx)
	if p.Tok.Kind != tQuestion {
		return cond
	}
	p.Advance()
	p.newlines()
	p.expr(ctx)
	p.newlines()
	p.Expect(tColon)
	p.newlines()
	p.expr(ctx)
	return plain
}

func (p *parser) or(ctx context) value {
	left := p.and(ctx)
	for p.Tok.Kind == tOr {
		p.Advance()
		p.newlines()
		p.and(ctx)
		left = plain
	}
	return left
}

func (p *parser) and(ctx context) value {
	left := p.in(ctx)
	for p.Tok.Kind == tAnd {
		p.Advance()
		p.newlines()
		p.in(ctx)
		left = plain
	}
	return left
}

func (p *parser) in(ctx context) value {
	left := p.match(ctx)
	for p.Tok.Kind == tIn {
		p.Advance()
		if p.Tok.Kind != tName {
			p.Unexpected()
		}
		p.name()
		left = membership
	}
	return left
}

func (p *parser) match(ctx context) value {
	left := p.comparison(ctx)
	for p.Tok.Kind == tMatch || p.Tok.Kind == tNoMatch {
		p.Advance()
		operand := p.Tok
		p.comparison(ctx)
		if operand.Kind == tString && p.Last == operand {
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
		switch p.Tok.Kind {
		case tLess, tLessEq, tEqual, tNotEqual, tGreaterEq:
		case tGreater:
			if ctx&inPrint != 0 {
				return left
			}
		default:
			return left
		}
		p.Advance()
		p.piped(ctx)
		left = plain
	}
}

func (p *parser) piped(ctx context) value {
	left := p.concatenation(ctx)
	for ctx&inPrint == 0 && (p.Tok.Kind == tPipe || p.Tok.Kind == tPipeBoth) {
		p.Advance()
		if p.Tok.Kind != tGetline {
			p.FailAt(p.Tok, "expected `getline` after `|`, found "+lang.Quote(p.lex.src, p.Tok.Start, p.Tok.End))
		}
		p.Advance()
		p.getlineTarget()
		left = plain
	}
	return left
}

func (p *parser) concatenation(ctx context) value {
	left := p.additive(ctx)
	for startsConcatenated(p.Tok.Kind) {
		p.additive(ctx)
		left = plain
	}
	return left
}

func (p *parser) additive(ctx context) value {
	left := p.multiplicative(ctx)
	for p.Tok.Kind == tAdd || p.Tok.Kind == tSub {
		p.Advance()
		p.multiplicative(ctx)
		left = plain
	}
	return left
}

func (p *parser) multiplicative(ctx context) value {
	left := p.unary(ctx)
	for p.Tok.Kind == tMul || p.Tok.Kind == tDiv || p.Tok.Kind == tMod {
		p.Advance()
		p.unary(ctx)
		left = plain
	}
	return left
}

func (p *parser) unary(ctx context) value {
	switch p.Tok.Kind {
	case tNot, tSub, tAdd:
		p.Advance()
		p.unary(ctx)
		return plain
	}
	return p.power(ctx)
}

func (p *parser) power(ctx context) value {
	left := p.postfix(ctx)
	if p.Tok.Kind != tPow {
		return left
	}
	p.Advance()
	p.unary(ctx)
	return plain
}

func (p *parser) postfix(ctx context) value {
	if p.Tok.Kind == tIncr || p.Tok.Kind == tDecr {
		op := p.Tok
		p.Advance()
		if p.postfix(ctx) != assignable {
			p.FailAt(op, lang.Quote(p.lex.src, op.Start, op.End)+" needs a variable, field or array element")
		}
		return plain
	}
	left := p.primary(ctx)
	if left == assignable && (p.Tok.Kind == tIncr || p.Tok.Kind == tDecr) {
		p.Advance()
		return plain
	}
	return left
}

func (p *parser) primary(ctx context) value {
	switch p.Tok.Kind {
	case tNumber, tString, tRegexp:
		p.Advance()
		return plain
	case tLParen:
		open := p.Tok
		p.Advance()
		p.newlines()
		p.list(0)
		p.newlines()
		p.Closing(tRParen, open)
		return plain
	case tDollar:
		p.Advance()
		switch p.Tok.Kind {
		case tIncr, tDecr, tSub, tAdd, tNot:
			p.unary(ctx)
		default:
			p.primary(ctx)
		}
		return assignable
	case tName:
		p.name()
		if p.Tok.Kind == tLBracket {
			p.subscript()
		}
		return assignable
	case tFuncName, tBuiltinFunc:
		p.Advance()
		if p.Tok.Kind == tLParen {
			p.arguments()
		}
		return plain
	case tGetline:
		p.Advance()
		p.getlineTarget()
		if p.Tok.Kind == tLess {
			p.Advance()
			p.postfix(ctx)
		}
		return plain
	case tAt:
		p.Advance()
		switch p.Tok.Kind {
		case tRegexp:
			p.Advance()
		case tFuncName:
			p.Advance()
			p.arguments()
		default:
			p.Unexpected()
		}
		return plain
	case tNot, tSub, tAdd, tIncr, tDecr:
		return p.unary(ctx)
	}
	p.Unexpected()
	return plain
}

func (p *parser) getlineTarget() {
	switch p.Tok.Kind {
	case tName, tDollar:
		p.primary(0)
	}
}

func (p *parser) name() {
	if slices.Contains(p.params, p.lex.src[p.Tok.Start:p.Tok.End]) {
		p.Reclassify(p.Tok, lang.Parameter, 0)
	}
	p.Advance()
}

func (p *parser) subscript() {
	open := p.Tok
	p.Advance()
	p.newlines()
	p.list(0)
	p.Closing(tRBracket, open)
}

func (p *parser) arguments() {
	open := p.Expect(tLParen)
	p.newlines()
	if p.Tok.Kind != tRParen {
		p.list(0)
		p.newlines()
	}
	p.Closing(tRParen, open)
}
