package syntax

// compact specifies whether we allow spaces between expressions.
// This is true for let
func (p *Parser) arithmExpr(compact bool) ArithmExpr {
	p.nest()
	defer p.unnest(1)
	return p.arithmExprComma(compact)
}

// These function names are inspired by Bash's expr.c

func (p *Parser) arithmExprComma(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprAssign, Comma)
}

func (p *Parser) arithmExprAssign(compact bool) ArithmExpr {
	// Assign is different from the other binary operators because it's
	// right-associative and needs to check that it's placed after a name
	value := p.arithmExprTernary(compact)
	switch BinAritOperator(p.tok) {
	case AddAssgn, SubAssgn, MulAssgn, QuoAssgn, RemAssgn, AndAssgn,
		OrAssgn, XorAssgn, ShlAssgn, ShrAssgn, Assgn,
		AndBoolAssgn, OrBoolAssgn, XorBoolAssgn, PowAssgn:
		if compact && p.spaced {
			return value
		}
		if !p.isArithName(value) {
			p.posErr(p.pos, "%#q must follow a name", p.tok)
		}
		pos := p.pos
		tok := p.tok
		p.nextArithOp(compact)
		p.nest()
		y := p.arithmExprAssign(compact)
		p.unnest(1)
		if y == nil {
			p.followErrExp(pos, tok)
		}
		return &BinaryArithm{
			OpPos: pos,
			Op:    BinAritOperator(tok),
			X:     value,
			Y:     y,
		}
	}
	return value
}

func (p *Parser) arithmExprTernary(compact bool) ArithmExpr {
	value := p.arithmExprLor(compact)
	if BinAritOperator(p.tok) != TernQuest || (compact && p.spaced) {
		return value
	}

	if value == nil {
		p.curErr("%#q must follow an expression", p.tok)
	}
	questPos := p.pos
	p.nextArithOp(compact)
	if BinAritOperator(p.tok) == TernColon {
		p.followErrExp(questPos, TernQuest)
	}
	trueExpr := p.arithmExpr(compact)
	if trueExpr == nil {
		p.followErrExp(questPos, TernQuest)
	}
	if BinAritOperator(p.tok) != TernColon {
		p.posErr(questPos, "ternary operator missing %#q after %#q", colon, quest)
	}
	colonPos := p.pos
	p.nextArithOp(compact)
	falseExpr := p.arithmExprTernary(compact)
	if falseExpr == nil {
		p.followErrExp(colonPos, TernColon)
	}
	return &BinaryArithm{
		OpPos: questPos,
		Op:    TernQuest,
		X:     value,
		Y: &BinaryArithm{
			OpPos: colonPos,
			Op:    TernColon,
			X:     trueExpr,
			Y:     falseExpr,
		},
	}
}

func (p *Parser) arithmExprLor(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprLand, OrArit, XorBool)
}

func (p *Parser) arithmExprLand(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprBor, AndArit)
}

func (p *Parser) arithmExprBor(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprBxor, Or)
}

func (p *Parser) arithmExprBxor(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprBand, Xor)
}

func (p *Parser) arithmExprBand(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprEquality, And)
}

func (p *Parser) arithmExprEquality(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprComparison, Eql, Neq)
}

func (p *Parser) arithmExprComparison(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprShift, Lss, Gtr, Leq, Geq)
}

func (p *Parser) arithmExprShift(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprAddition, Shl, Shr)
}

func (p *Parser) arithmExprAddition(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprMultiplication, Add, Sub)
}

func (p *Parser) arithmExprMultiplication(compact bool) ArithmExpr {
	return p.arithmExprBinary(compact, p.arithmExprPower, Mul, Quo, Rem)
}

func (p *Parser) arithmExprPower(compact bool) ArithmExpr {
	// Power is different from the other binary operators because it's right-associative
	value := p.arithmExprUnary(compact)
	if BinAritOperator(p.tok) != Pow || (compact && p.spaced) {
		return value
	}

	if value == nil {
		p.curErr("%#q must follow an expression", p.tok)
	}

	op := p.tok
	pos := p.pos
	p.nextArithOp(compact)
	y := p.arithmExprPower(compact)
	if y == nil {
		p.followErrExp(pos, op)
	}
	return &BinaryArithm{
		OpPos: pos,
		Op:    BinAritOperator(op),
		X:     value,
		Y:     y,
	}
}

func (p *Parser) arithmExprUnary(compact bool) ArithmExpr {
	if !compact {
		p.got(_Newl)
	}

	switch op := UnAritOperator(p.tok); op {
	case CharCode:
		if !p.lang.in(LangZsh) {
			break
		}
		// Zsh's #name is the code of the first character of the value of name.
		ue := &UnaryArithm{OpPos: p.pos, Op: op}
		p.nextArith(compact)
		if ue.X = p.arithmExprValue(compact); ue.X == nil {
			p.followErr(ue.OpPos, ue.Op, noQuote("a name"))
		}
		return ue
	case Not, BitNegation, Plus, Minus:
		ue := &UnaryArithm{OpPos: p.pos, Op: UnAritOperator(p.tok)}
		p.nextArithOp(compact)
		p.nest()
		ue.X = p.arithmExprUnary(compact)
		p.unnest(1)
		if ue.X == nil {
			p.followErrExp(ue.OpPos, ue.Op)
		}
		return ue
	}
	return p.arithmExprValue(compact)
}

func (p *Parser) arithmExprValue(compact bool) ArithmExpr {
	var x ArithmExpr
	switch p.tok {
	case addAdd, subSub:
		ue := &UnaryArithm{OpPos: p.pos, Op: UnAritOperator(p.tok)}
		p.nextArith(compact)
		if p.tok != _LitWord {
			p.followErr(ue.OpPos, ue.Op, noQuote("a literal"))
		}
		ue.X = p.arithmExprValue(compact)
		return ue
	case leftParen:
		if p.quote == paramExpArithm && p.lang.in(LangZsh) {
			x = p.zshSubFlags()
			break
		}
		pe := &ParenArithm{Lparen: p.pos}
		p.nextArithOp(compact)
		pe.X = p.followArithm(leftParen, pe.Lparen)
		pe.Rparen = p.matched(pe.Lparen, leftParen, rightParen)
		if p.quote == paramExpArithm && p.tok == _LitWord {
			p.checkLang(pe.Lparen, LangZsh, "subscript flags")
		}
		x = pe
	case leftBrack:
		if p.lang.in(LangZsh) && p.quote&(arithmExpr|arithmExprLet|arithmExprCmd) != 0 {
			return p.zshOutputBase(compact)
		}
		p.curErr("%#q must follow a name like a[i]", p.tok)
	case colon:
		p.curErr("ternary operator missing %#q before %#q", quest, colon)
	case _LitWord:
		l := p.getLit()
		if p.lang.in(LangZsh|LangKsh93) && p.tok == leftParen && !p.spaced && ValidName(l.Value) {
			x = p.arithmCall(l)
			break
		}
		if p.tok != leftBrack {
			x = p.wordOne(l)
			break
		}
		pe := &ParamExp{Short: true, Param: l}
		pe.Index = p.eitherIndex()
		x = p.wordOne(pe)
	case bckQuote:
		if p.quote == arithmExprLet && p.openBquotes > 0 {
			return nil
		}
		fallthrough
	default:
		if w := p.getWord(); w != nil {
			x = w
		} else {
			return nil
		}
	}

	if compact && p.spaced {
		return x
	}
	if !compact {
		p.got(_Newl)
	}

	// we want real nil, not (*Word)(nil) as that
	// sets the type to non-nil and then x != nil
	if p.tok == addAdd || p.tok == subSub {
		if !p.isArithName(x) {
			p.curErr("%#q must follow a name", p.tok)
		}
		u := &UnaryArithm{
			Post:  true,
			OpPos: p.pos,
			Op:    UnAritOperator(p.tok),
			X:     x,
		}
		p.nextArith(compact)
		return u
	}
	return x
}

// zshOutputBase parses zsh's output base for an arithmetic expression, like
// `[#16]` in `$(( [#16] x + 1 ))`, followed by the expression it applies to.
func (p *Parser) zshOutputBase(compact bool) ArithmExpr {
	fa := &FlagsArithm{Brackets: true}
	lbrack := p.pos
	old := p.quote
	p.quote = runeByRune
	p.pos = p.nextPos()
	for p.newLit(p.r); p.r != runeEOF && p.r != ']'; p.rune() {
	}
	p.val = p.endLit()
	if p.r != ']' {
		p.tok = _EOF // we can only get here due to EOF
		p.matchingErr(lbrack, leftBrack, rightBrack)
	}
	fa.Flags = p.lit(p.pos, p.val)
	p.rune()
	p.quote = old
	p.nextArith(compact)
	if fa.X = p.arithmExpr(compact); fa.X == nil {
		p.followErrExp(lbrack, leftBrack)
	}
	return fa
}

// arithmCall parses a call to a math function, like `sqrt(x)` or
// `atan2(y, x)`, after its name.
func (p *Parser) arithmCall(name *Lit) *FuncArithm {
	fa := &FuncArithm{Name: name, Lparen: p.pos}
	p.nextArith(false)
	if p.tok != rightParen {
		fa.X = p.followArithm(leftParen, fa.Lparen)
	}
	fa.Rparen = p.matched(fa.Lparen, leftParen, rightParen)
	return fa
}

// nextArith consumes a token.
// It returns true if compact and the token was followed by spaces
func (p *Parser) nextArith(compact bool) bool {
	p.next()
	if compact && p.spaced {
		return true
	}
	if !compact {
		p.got(_Newl)
	}
	return false
}

func (p *Parser) nextArithOp(compact bool) {
	pos := p.pos
	tok := p.tok
	if p.nextArith(compact) {
		p.followErrExp(pos, tok)
	}
}

// arithmExprBinary is used for all left-associative binary operators
func (p *Parser) arithmExprBinary(compact bool, nextOp func(bool) ArithmExpr, operators ...BinAritOperator) ArithmExpr {
	value := nextOp(compact)
	levels := 0
	defer func() { p.unnest(levels) }()
	for {
		var foundOp BinAritOperator
		for _, op := range operators {
			if p.tok == token(op) {
				foundOp = op
				break
			}
		}

		if token(foundOp) == illegalTok || (compact && p.spaced) {
			return value
		}

		if value == nil {
			p.curErr("%#q must follow an expression", p.tok)
		}

		pos := p.pos
		levels++
		p.nest()
		p.nextArithOp(compact)
		y := nextOp(compact)
		if y == nil {
			p.followErrExp(pos, foundOp)
		}

		value = &BinaryArithm{
			OpPos: pos,
			Op:    foundOp,
			X:     value,
			Y:     y,
		}
	}
}

func (p *Parser) isArithName(left ArithmExpr) bool {
	w, ok := left.(*Word)
	if !ok || len(w.Parts) != 1 {
		return false
	}
	switch wp := w.Parts[0].(type) {
	case *Lit:
		return p.validVarName(wp.Value)
	case *ParamExp:
		return wp.nakedIndex()
	default:
		return false
	}
}

func (p *Parser) followArithm(ftok token, fpos Pos) ArithmExpr {
	x := p.arithmExpr(false)
	if x == nil {
		p.followErrExp(fpos, ftok)
	}
	return x
}

func (p *Parser) peekArithmEnd() bool {
	return p.tok == rightParen && p.r == ')'
}

func (p *Parser) arithmMatchingErr(pos Pos, left, right token) {
	switch p.tok {
	case _Lit, _LitWord:
		p.curErr("not a valid arithmetic operator: %#q", p.val)
	case leftBrack:
		p.curErr("%#q must follow a name like a[i]", leftBrack)
	case colon:
		p.curErr("ternary operator missing %#q before %#q", quest, colon)
	case rightParen, _EOF:
		p.matchingErr(pos, left, right)
	case period:
		p.checkLang(p.pos, LangZsh|LangKsh93, `floating point arithmetic`)
	default:
		if p.quote&allArithmExpr != 0 {
			p.curErr("not a valid arithmetic operator: %#q", p.tok)
		}
		p.matchingErr(pos, left, right)
	}
}

func (p *Parser) matchedArithm(lpos Pos, left, right token) {
	if !p.got(right) {
		p.arithmMatchingErr(lpos, left, right)
	}
}

func (p *Parser) arithmEnd(ltok token, lpos Pos, old saveState) Pos {
	if !p.peekArithmEnd() {
		if p.recoverError() {
			return recoveredPos
		}
		p.arithmMatchingErr(lpos, ltok, dblRightParen)
	}
	p.rune()
	p.postNested(old)
	pos := p.pos
	p.next()
	return pos
}
