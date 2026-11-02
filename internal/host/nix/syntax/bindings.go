package syntax

func (p *treeParser) attrSet(start int, rec bool) *AttrSet {
	open := p.tok
	p.advance()
	p.open[inBraces]++
	bindings := p.bindings(false)
	p.open[inBraces]--
	closed := p.closing(TokRBrace, "}", open)
	return &AttrSet{exprNode: exprAt(p.span(start)), Rec: rec, Bindings: bindings, Closed: closed}
}

func (p *treeParser) bindings(isLet bool) []Binding {
	var out []Binding
	for !p.atCloser() {
		switch p.tok.kind {
		case TokInherit:
			out = append(out, p.inherit())
		case TokIdent, TokOrKeyword, TokQuote, TokInterp:
			out = append(out, p.assign(isLet))
		default:
			p.errorHere("expected an attribute, found " + p.describe(p.tok))
			p.syncBinding()
		}
	}
	return out
}

func (p *treeParser) assign(isLet bool) *Assign {
	start := p.tok.start
	path := p.attrPath()
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
	return &Assign{bindingNode: bindingAt(p.span(start)), Path: path, Value: value}
}

func (p *treeParser) inherit() *Inherit {
	start := p.tok.start
	p.advance()
	var from *Paren
	if p.tok.kind == TokLParen {
		from = p.paren()
	}
	var attrs []Attr
	for p.atAttr() && !p.startsBinding() {
		a := p.attribute()
		if !StaticAttr(a) {
			p.report(a.Span(), "`inherit` cannot name a dynamic attribute")
		}
		attrs = append(attrs, a)
	}
	p.endBinding()
	return &Inherit{bindingNode: bindingAt(p.span(start)), From: from, Attrs: attrs}
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
	path := AttrPath{p.attribute()}
	for p.accept(TokDot) {
		path = append(path, p.attribute())
	}
	return path
}

func (p *treeParser) attribute() Attr {
	here := Span{Start: p.tok.start, End: p.tok.end}
	switch p.tok.kind {
	case TokIdent, TokOrKeyword:
		name := p.src[here.Start:here.End]
		p.advance()
		return &IdentAttr{attrNode: attrAt(here), Name: name}
	case TokQuote:
		s := p.quoted()
		return &StringAttr{attrNode: attrAt(s.Span()), Value: s}
	case TokInterp:
		in := p.antiquote()
		if in.Closed {
			p.advance()
		}
		return &DynamicAttr{attrNode: attrAt(in.Span()), Value: in.Value, Closed: in.Closed}
	default:
		p.errorHere("expected an attribute name, found " + p.describe(p.tok))
		return &BadAttr{attrAt(Span{Start: p.prevEnd, End: p.prevEnd})}
	}
}
