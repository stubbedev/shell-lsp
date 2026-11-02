package syntax

func (p *treeParser) expr() Expr {
	if !p.enter() {
		return p.bad()
	}
	defer func() { p.depth-- }()
	start := p.tok.start
	switch p.tok.kind {
	case TokLet:
		if p.peek().kind != TokLBrace {
			return p.let(start)
		}
	case TokWith:
		p.advance()
		scope := p.expr()
		p.expect(TokSemicolon, ";")
		body := p.expr()
		return &With{exprNode: exprAt(p.span(start)), Scope: scope, Body: body}
	case TokAssert:
		p.advance()
		cond := p.expr()
		p.expect(TokSemicolon, ";")
		body := p.expr()
		return &Assert{exprNode: exprAt(p.span(start)), Cond: cond, Body: body}
	case TokIf:
		return p.ifExpr(start)
	case TokIdent:
		next := p.peek().kind
		if next == TokColon {
			return p.identLambda(start)
		}
		if next == TokAt {
			return p.aliasedLambda(start)
		}
	case TokLBrace:
		if p.startsPattern() {
			return p.patternLambda(start)
		}
	default:
	}
	return p.binary(precLowest)
}

func (p *treeParser) ifExpr(start int) Expr {
	p.advance()
	cond := p.expr()
	p.expect(TokThen, "then")
	then := p.expr()
	p.expect(TokElse, "else")
	otherwise := p.expr()
	return &If{exprNode: exprAt(p.span(start)), Cond: cond, Then: then, Else: otherwise}
}

func (p *treeParser) let(start int) Expr {
	p.advance()
	p.open[inLet]++
	bindings := p.bindings(true)
	p.open[inLet]--
	p.expect(TokIn, "in")
	body := p.expr()
	return &Let{exprNode: exprAt(p.span(start)), Bindings: bindings, Body: body}
}

type precedence uint8

const (
	precLowest precedence = iota
	precImplies
	precOr
	precAnd
	precEquality
	precComparison
	precUpdate
	precNot
	precAdditive
	precMultiplicative
	precConcat
	precHasAttr
	precNegate
)

type associativity uint8

const (
	leftAssociative associativity = iota
	rightAssociative
	nonAssociative
)

type operator struct {
	op    Op
	prec  precedence
	assoc associativity
	known bool
}

var operators = [tokenKinds]operator{
	TokPipeInto: {OpPipeInto, precLowest, leftAssociative, true},
	TokPipeFrom: {OpPipeFrom, precLowest, rightAssociative, true},
	TokImplies:  {OpImplies, precImplies, rightAssociative, true},
	TokOr:       {OpOr, precOr, leftAssociative, true},
	TokAnd:      {OpAnd, precAnd, leftAssociative, true},
	TokEq:       {OpEq, precEquality, nonAssociative, true},
	TokNe:       {OpNe, precEquality, nonAssociative, true},
	TokLt:       {OpLt, precComparison, nonAssociative, true},
	TokLe:       {OpLe, precComparison, nonAssociative, true},
	TokGt:       {OpGt, precComparison, nonAssociative, true},
	TokGe:       {OpGe, precComparison, nonAssociative, true},
	TokUpdate:   {OpUpdate, precUpdate, rightAssociative, true},
	TokPlus:     {OpAdd, precAdditive, leftAssociative, true},
	TokMinus:    {OpSub, precAdditive, leftAssociative, true},
	TokStar:     {OpMul, precMultiplicative, leftAssociative, true},
	TokSlash:    {OpDiv, precMultiplicative, leftAssociative, true},
	TokConcat:   {OpConcat, precConcat, rightAssociative, true},
}

func (p *treeParser) binary(lowest precedence) Expr {
	if !p.enter() {
		return p.bad()
	}
	base := p.depth
	defer func() { p.depth = base - 1 }()
	left := p.prefix()
	for {
		if p.tok.kind == TokQuestion && lowest <= precHasAttr {
			p.advance()
			path := p.attrPath()
			left = &HasAttr{exprNode: exprAt(p.span(left.Span().Start)), Target: left, Path: path}
		} else {
			o := operators[p.tok.kind]
			if !o.known || o.prec < lowest {
				return left
			}
			left = p.infix(left, o)
		}
		if !p.enter() {
			return left
		}
	}
}

func (p *treeParser) infix(left Expr, o operator) Expr {
	opSpan := Span{Start: p.tok.start, End: p.tok.end}
	p.advance()
	next := o.prec
	if o.assoc != rightAssociative {
		next++
	}
	right := p.binary(next)
	p.mixedPipes(left, o, opSpan)
	p.mixedPipes(right, o, opSpan)
	if after := operators[p.tok.kind]; after.known && o.assoc == nonAssociative && after.prec == o.prec {
		p.errorHere("`" + after.op.String() + "` cannot follow `" + o.op.String() + "` without parentheses")
	}
	return &Binary{exprNode: exprAt(p.span(left.Span().Start)), Op: o.op, OpSpan: opSpan, Left: left, Right: right}
}

func (p *treeParser) mixedPipes(operand Expr, o operator, at Span) {
	if b, ok := operand.(*Binary); ok && o.prec == precLowest && (b.Op == OpPipeInto || b.Op == OpPipeFrom) && b.Op != o.op {
		p.report(at, "`|>` and `<|` cannot be mixed without parentheses")
	}
}

func (p *treeParser) prefix() Expr {
	start := p.tok.start
	switch p.tok.kind {
	case TokNot:
		p.advance()
		operand := p.binary(precNot)
		return &Unary{exprNode: exprAt(p.span(start)), Op: OpNot, Operand: operand}
	case TokMinus:
		p.advance()
		operand := p.binary(precNegate)
		return &Unary{exprNode: exprAt(p.span(start)), Op: OpNegate, Operand: operand}
	default:
		return p.application()
	}
}

func (p *treeParser) atAtom() bool {
	switch p.tok.kind {
	case TokIdent:
		return !p.startsBinding()
	case TokInt, TokFloat, TokURI, TokPath, TokHomePath, TokSearchPath, TokQuote, TokIndentQuote, TokLParen, TokLBracket, TokLBrace, TokRec:
		return true
	case TokLet:
		return p.peek().kind == TokLBrace
	default:
		return false
	}
}

func (p *treeParser) application() Expr {
	fn := p.selection()
	if !p.atAtom() {
		return fn
	}
	mark := len(p.arena.exprs.stack)
	for p.atAtom() {
		arg := p.selection()
		p.arena.exprs.stack = append(p.arena.exprs.stack, arg)
	}
	args := p.arena.exprs.since(mark, p.chunk)
	return p.arena.applies.put(p.chunk, Apply{exprNode: exprAt(p.span(fn.Span().Start)), Fn: fn, Args: args})
}

func (p *treeParser) selection() Expr {
	if !p.enter() {
		return p.bad()
	}
	defer func() { p.depth-- }()
	target := p.simple()
	start := target.Span().Start
	switch p.tok.kind {
	case TokDot:
		p.advance()
		path := p.attrPath()
		if !p.accept(TokOrKeyword) {
			return p.arena.selects.put(p.chunk, Select{exprNode: exprAt(p.span(start)), Target: target, Path: path})
		}
		fallback := p.selection()
		return p.arena.selects.put(p.chunk, Select{exprNode: exprAt(p.span(start)), Target: target, Path: path, Default: fallback})
	case TokOrKeyword:
		or := p.identifier()
		return &Apply{exprNode: exprAt(p.span(start)), Fn: target, Args: []Expr{or}}
	default:
		return target
	}
}

func (p *treeParser) simple() Expr {
	t := p.tok
	here := Span{Start: t.start, End: t.end}
	text := p.src[t.start:t.end]
	switch t.kind {
	case TokIdent:
		if p.startsBinding() {
			return p.notAnExpression(t)
		}
		return p.identifier()
	case TokInt:
		p.advance()
		return &Int{exprNode: exprAt(here), Text: text}
	case TokFloat:
		p.advance()
		return &Float{exprNode: exprAt(here), Text: text}
	case TokURI:
		p.advance()
		return &URI{exprNode: exprAt(here), Text: text}
	case TokQuote:
		return p.quoted()
	case TokIndentQuote:
		return p.indented()
	case TokPath, TokHomePath, TokSearchPath:
		return p.path()
	case TokLParen:
		return p.paren()
	case TokLBracket:
		return p.list()
	case TokLBrace:
		return p.attrSet(t.start, false)
	case TokRec:
		p.advance()
		if p.tok.kind != TokLBrace {
			p.errorHere("expected `{` after `rec`, found " + p.describe(p.tok))
			return p.bad()
		}
		return p.attrSet(t.start, true)
	case TokLet:
		if p.peek().kind == TokLBrace {
			p.advance()
			set := p.attrSet(p.tok.start, false)
			return &LegacyLet{exprNode: exprAt(p.span(t.start)), Set: set}
		}
		return p.misplaced()
	case TokIf, TokWith, TokAssert:
		return p.misplaced()
	case TokOrKeyword:
		p.errorHere("`or` cannot stand alone as an expression")
		p.bareOr++
		return p.identifier()
	default:
		return p.notAnExpression(t)
	}
}

func (p *treeParser) notAnExpression(t token) Expr {
	p.errorHere("expected an expression, found " + p.describe(t))
	return p.bad()
}

func (p *treeParser) misplaced() Expr {
	p.errorHere("`" + p.src[p.tok.start:p.tok.end] + "` needs parentheses here")
	return p.expr()
}

func (p *treeParser) paren() *Paren {
	open := p.tok
	p.advance()
	p.open[inParens]++
	inner := p.expr()
	p.open[inParens]--
	closed := p.closing(TokRParen, ")", open)
	return p.arena.parens.put(p.chunk, Paren{exprNode: exprAt(p.span(open.start)), Inner: inner, Closed: closed})
}

func (p *treeParser) atListItem() bool {
	switch p.tok.kind {
	case TokIf, TokWith, TokAssert, TokLet:
		return true
	default:
		return p.atAtom()
	}
}

func (p *treeParser) list() *List {
	open := p.tok
	p.advance()
	p.open[inBrackets]++
	mark := len(p.arena.exprs.stack)
	for !p.atCloser() && p.tok.kind != TokSemicolon && !p.startsBinding() {
		if p.atListItem() {
			item := p.selection()
			p.arena.exprs.stack = append(p.arena.exprs.stack, item)
			continue
		}
		p.errorHere("expected a list element, found " + p.describe(p.tok))
		p.skip()
	}
	items := p.arena.exprs.since(mark, p.chunk)
	p.open[inBrackets]--
	closed := p.closing(TokRBracket, "]", open)
	return &List{exprNode: exprAt(p.span(open.start)), Items: items, Closed: closed}
}
