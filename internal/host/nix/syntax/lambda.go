package syntax

func (p *treeParser) identLambda(start int) Expr {
	name := p.identifier()
	p.advance()
	param := &IdentParam{paramNode: paramAt(name.Span()), Name: name}
	body := p.expr()
	return &Lambda{exprNode: exprAt(p.span(start)), Param: param, Body: body}
}

func (p *treeParser) identifier() *Ident {
	slot := p.slots
	p.slots++
	p.reads++
	id := p.arena.idents.put(p.chunk, Ident{
		exprNode: exprAt(Span{Start: p.tok.start, End: p.tok.end}),
		Name:     p.src[p.tok.start:p.tok.end],
		slot:     int32(slot), //nolint:gosec // bounded by the source length, which the transport caps well below 2^31
	})
	p.advance()
	return id
}

func (p *treeParser) aliasedLambda(start int) Expr {
	alias := p.identifier()
	p.advance()
	if p.tok.kind != TokLBrace {
		p.errorHere("expected `{` after `@`, found " + p.describe(p.tok))
		param := &IdentParam{paramNode: paramAt(alias.Span()), Name: alias}
		return &Lambda{exprNode: exprAt(p.span(start)), Param: param, Body: p.bad()}
	}
	param := p.pattern(start)
	param.Alias, param.AliasFirst = alias, true
	return p.patternBody(start, param)
}

func (p *treeParser) patternLambda(start int) Expr {
	param := p.pattern(start)
	if p.accept(TokAt) {
		if p.tok.kind == TokIdent {
			param.Alias = p.identifier()
			param.span = p.span(start)
		} else {
			p.errorHere("expected a name after `@`, found " + p.describe(p.tok))
		}
	}
	return p.patternBody(start, param)
}

func (p *treeParser) patternBody(start int, param *PatternParam) Expr {
	p.expect(TokColon, ":")
	body := p.expr()
	return &Lambda{exprNode: exprAt(p.span(start)), Param: param, Body: body}
}

func (p *treeParser) startsPattern() bool {
	first := p.peek()
	second := p.peekAfter(first)
	switch first.kind {
	case TokEllipsis:
		return true
	case TokRBrace:
		return second.kind == TokColon || second.kind == TokAt
	case TokIdent:
		return second.kind == TokComma || second.kind == TokQuestion || second.kind == TokRBrace
	default:
		return false
	}
}

func (p *treeParser) pattern(start int) *PatternParam {
	open := p.tok
	p.advance()
	p.open[inBraces]++
	param := &PatternParam{}
	separated := true
	for separated && p.tok.kind == TokIdent {
		param.Formals = append(param.Formals, p.formal())
		p.binders++
		separated = p.accept(TokComma)
	}
	param.Ellipsis = separated && p.accept(TokEllipsis)
	p.open[inBraces]--
	param.Closed = p.closing(TokRBrace, "}", open)
	param.span = p.span(start)
	return param
}

func (p *treeParser) formal() *Formal {
	start := p.tok.start
	name := p.identifier()
	f := &Formal{Name: name}
	if p.accept(TokQuestion) {
		f.Default = p.expr()
	}
	f.span = p.span(start)
	return f
}
