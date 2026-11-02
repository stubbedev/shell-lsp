package syntax

import (
	"cmp"
	"slices"
	"unicode/utf8"
)

const (
	maxDepth         = 4096
	maxDescribedText = 32
)

type enclosure uint8

const (
	inParens enclosure = iota
	inBrackets
	inBraces
	inLet
	enclosures
)

const lookaheadMask = 3

type peeked struct {
	from  int
	tok   token
	end   triviaEnd
	known bool
}

type treeParser struct {
	lexer
	tok       token
	prevEnd   int
	depth     int
	fatal     bool
	open      [enclosures]int
	errors    []Error
	lookahead [lookaheadMask + 1]peeked
	bareOr    int
}

func Parse(src string) *File {
	p := &treeParser{src: src, tokens: make([]Token, 0, len(src)/4+1)}
	p.advance()
	root := p.expr()
	stray := p.strayExprs()
	slices.SortStableFunc(p.errors, func(a, b Error) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	return &File{Text: src, Root: root, Stray: stray, Comments: p.comments, Tokens: p.tokens, Errors: p.errors, BareOr: p.bareOr}
}

func (p *treeParser) strayExprs() []Expr {
	var out []Expr
	reported := false
	for p.tok.kind != TokEOF {
		if !reported {
			p.errorHere("expected the end of the file, found " + p.describe(p.tok))
			reported = true
		}
		if !p.startsExpr() {
			p.skip()
			continue
		}
		out = append(out, p.expr())
		reported = false
	}
	return out
}

func (p *treeParser) startsExpr() bool {
	return p.tok.kind == TokNot || p.tok.kind == TokMinus || p.atListItem()
}

func (p *treeParser) advance() {
	from := p.tok.end
	p.prevEnd = from
	if m := p.lookahead[from&lookaheadMask]; m.known && m.from == from && m.end == triviaPlain {
		p.emit(TokWhitespace, from, m.tok.start)
		p.take(m.tok)
		return
	}
	at, end := p.trivia(from, true)
	if end == triviaUnclosed {
		p.report(Span{Start: at, End: at + len("/*")}, "missing `*/` to match `/*`")
		p.tok = p.eof()
		return
	}
	p.take(p.scan(at))
}

func (p *treeParser) take(t token) {
	p.tok = t
	p.emit(t.kind, t.start, t.end)
}

func (p *treeParser) eof() token { return token{kind: TokEOF, start: len(p.src), end: len(p.src)} }

func (p *treeParser) peekAfter(t token) token {
	m := &p.lookahead[t.end&lookaheadMask]
	if m.known && m.from == t.end {
		return m.tok
	}
	at, end := p.trivia(t.end, false)
	next := p.eof()
	if end != triviaUnclosed {
		next = p.scan(at)
	}
	*m = peeked{from: t.end, tok: next, end: end, known: true}
	return next
}

func (p *treeParser) peek() token { return p.peekAfter(p.tok) }

func (p *treeParser) report(span Span, message string) {
	if p.fatal {
		return
	}
	if n := len(p.errors); n > 0 && p.errors[n-1].Span.Start == span.Start {
		return
	}
	p.errors = append(p.errors, Error{Span: span, Message: message})
}

func (p *treeParser) errorHere(message string) {
	p.report(Span{Start: p.tok.start, End: p.tok.end}, message)
}

func (p *treeParser) describe(t token) string {
	text := p.src[t.start:t.end]
	switch {
	case t.kind == TokEOF:
		return "the end of the file"
	case len(text) > maxDescribedText || !utf8.ValidString(text):
		return "something else"
	}
	return "`" + text + "`"
}

func (p *treeParser) span(start int) Span { return Span{Start: start, End: p.prevEnd} }

func (p *treeParser) bad() *Bad {
	return &Bad{exprAt(Span{Start: p.prevEnd, End: p.prevEnd})}
}

func (p *treeParser) enter() bool {
	p.depth++
	if p.depth <= maxDepth {
		return true
	}
	p.errorHere("the expression is nested too deeply to analyse")
	p.fatal = true
	p.emit(TokUnparsed, p.emitted, len(p.src))
	p.tok = p.eof()
	return false
}

func (p *treeParser) accept(kind TokenKind) bool {
	if p.tok.kind != kind {
		return false
	}
	p.advance()
	return true
}

func (p *treeParser) expect(kind TokenKind, text string) bool {
	if p.accept(kind) {
		return true
	}
	p.errorHere("expected `" + text + "`, found " + p.describe(p.tok))
	return false
}

func (p *treeParser) closing(kind TokenKind, text string, open token) bool {
	if p.accept(kind) {
		return true
	}
	if p.tok.kind == TokEOF {
		p.report(Span{Start: open.start, End: open.end}, "missing `"+text+"` to match `"+p.src[open.start:open.end]+"`")
		return false
	}
	p.errorHere("expected `" + text + "` to match the `" + p.src[open.start:open.end] + "`, found " + p.describe(p.tok))
	return false
}

func (p *treeParser) atCloser() bool {
	switch p.tok.kind {
	case TokEOF:
		return true
	case TokRParen:
		return p.open[inParens] > 0
	case TokRBracket:
		return p.open[inBrackets] > 0
	case TokRBrace:
		return p.open[inBraces] > 0
	case TokIn:
		return p.open[inLet] > 0
	default:
		return false
	}
}

func (p *treeParser) startsBinding() bool {
	t := p.tok
	for t.kind == TokIdent || t.kind == TokOrKeyword {
		next := p.peekAfter(t)
		switch next.kind {
		case TokAssign:
			return true
		case TokDot:
			t = p.peekAfter(next)
		default:
			return false
		}
	}
	return false
}

func (p *treeParser) skip() {
	switch p.tok.kind {
	case TokQuote:
		p.quoted()
	case TokIndentQuote:
		p.indented()
	default:
		p.advance()
	}
}

func (p *treeParser) syncBinding() {
	depth := 0
	for p.tok.kind != TokEOF {
		switch p.tok.kind {
		case TokSemicolon:
			if depth == 0 {
				p.advance()
				return
			}
		case TokLBrace, TokLParen, TokLBracket, TokInterp:
			depth++
		case TokRBrace, TokRParen, TokRBracket:
			if depth == 0 && p.atCloser() {
				return
			}
			depth = max(depth-1, 0)
		case TokIn:
			if depth == 0 && p.atCloser() {
				return
			}
		case TokIdent:
			if depth == 0 && p.peek().kind == TokAssign {
				return
			}
		default:
		}
		p.skip()
	}
}
