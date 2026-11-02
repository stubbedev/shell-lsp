package syntax

func (p *treeParser) attrSet(start int, rec bool) *AttrSet {
	open := p.tok
	p.advance()
	p.open[inBraces]++
	bindings := p.bindings(false)
	p.open[inBraces]--
	closed := p.closing(TokRBrace, "}", open)
	return p.arena.sets.put(p.chunk, AttrSet{exprNode: exprAt(p.span(start)), Rec: rec, Bindings: bindings, Closed: closed})
}

func (p *treeParser) bindings(isLet bool) []Binding {
	mark := len(p.arena.bindings.stack)
	for !p.atCloser() {
		switch p.tok.kind {
		case TokInherit:
			stmt := p.inherit()
			p.arena.bindings.stack = append(p.arena.bindings.stack, stmt)
		case TokIdent, TokOrKeyword, TokQuote, TokInterp:
			stmt := p.assign(isLet)
			p.arena.bindings.stack = append(p.arena.bindings.stack, stmt)
		default:
			p.errorHere("expected an attribute, found " + p.describe(p.tok))
			p.syncBinding()
		}
	}
	return p.arena.bindings.since(mark, p.chunk)
}

func (p *treeParser) assign(isLet bool) *Assign {
	start := p.tok.start
	path := p.attrPath()
	p.binders += len(path)
	if isLet && !StaticAttr(path[0]) {
		p.report(path[0].Span(), "a `let` cannot bind a dynamic attribute")
	}
	var value Expr
	if p.expect(TokAssign, "=") {
		value = p.expr()
	} else {
		value = p.bad()
	}
	p.endBinding()
	return p.arena.assigns.put(p.chunk, Assign{bindingNode: bindingAt(p.span(start)), Path: path, Value: value})
}

func (p *treeParser) inherit() *Inherit {
	start := p.tok.start
	p.advance()
	var from *Paren
	if p.tok.kind == TokLParen {
		from = p.paren()
	}
	mark := len(p.arena.attrs.stack)
	for p.atAttr() && !p.startsBinding() {
		a := p.attribute()
		if !StaticAttr(a) {
			p.report(a.Span(), "`inherit` cannot name a dynamic attribute")
		}
		p.arena.attrs.stack = append(p.arena.attrs.stack, a)
	}
	attrs := p.arena.attrs.since(mark, p.chunk)
	p.reads += len(attrs)
	p.binders += len(attrs)
	p.endBinding()
	return p.arena.inherits.put(p.chunk, Inherit{bindingNode: bindingAt(p.span(start)), From: from, Attrs: attrs})
}

func (p *treeParser) endBinding() {
	if p.accept(TokSemicolon) {
		return
	}
	p.errorHere("expected `;`, found " + p.describe(p.tok))
	if p.atCloser() || p.atAttr() || p.tok.kind == TokInherit {
		return
	}
	p.syncBinding()
}

func (p *treeParser) atAttr() bool {
	switch p.tok.kind {
	case TokIdent, TokOrKeyword, TokQuote, TokInterp:
		return true
	default:
		return false
	}
}

func (p *treeParser) attrPath() AttrPath {
	mark := len(p.arena.attrs.stack)
	first := p.attribute()
	p.arena.attrs.stack = append(p.arena.attrs.stack, first)
	for p.accept(TokDot) {
		next := p.attribute()
		p.arena.attrs.stack = append(p.arena.attrs.stack, next)
	}
	return p.arena.attrs.since(mark, p.chunk)
}

func (p *treeParser) attribute() Attr {
	here := Span{Start: p.tok.start, End: p.tok.end}
	switch p.tok.kind {
	case TokIdent, TokOrKeyword:
		name := p.src[here.Start:here.End]
		p.advance()
		return p.arena.identAttrs.put(p.chunk, IdentAttr{attrNode: p.attrAt(here), Name: name})
	case TokQuote:
		s := p.quoted()
		return &StringAttr{attrNode: p.attrAt(s.Span()), Value: s}
	case TokInterp:
		in := p.antiquote()
		if in.Closed {
			p.advance()
		}
		return &DynamicAttr{attrNode: p.attrAt(in.Span()), Value: in.Value, Closed: in.Closed}
	default:
		p.errorHere("expected an attribute name, found " + p.describe(p.tok))
		return &BadAttr{p.attrAt(Span{Start: p.prevEnd, End: p.prevEnd})}
	}
}

func (p *treeParser) attrAt(s Span) attrNode {
	slot := p.slots
	p.slots++
	return attrNode{node: node{s}, slot: int32(slot)} //nolint:gosec // bounded by the source length, which the transport caps well below 2^31
}
