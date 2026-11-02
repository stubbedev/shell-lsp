package syntax

import (
	"strings"
	"unicode/utf8"
)

func (p *treeParser) antiquote() *Interpolation {
	start := p.tok.start
	p.advance()
	p.open[inBraces]++
	value := p.expr()
	p.open[inBraces]--
	if p.tok.kind != TokRBrace {
		p.errorHere("expected `}` to close the antiquotation, found " + p.describe(p.tok))
		p.skipToBrace()
	}
	if p.tok.kind != TokRBrace {
		return &Interpolation{partNode: partAt(p.span(start)), Value: value}
	}
	return &Interpolation{partNode: partAt(Span{Start: start, End: p.tok.end}), Value: value, Closed: true}
}

func (p *treeParser) skipToBrace() {
	depth := 0
	for p.tok.kind != TokEOF {
		switch p.tok.kind {
		case TokLBrace, TokInterp:
			depth++
		case TokRBrace:
			if depth == 0 {
				return
			}
			depth--
		default:
		}
		p.skip()
	}
}

func decodeEscape(c string) string {
	switch c {
	case "n":
		return "\n"
	case "r":
		return "\r"
	case "t":
		return "\t"
	}
	return c
}

type stringBody struct {
	parts   []Part
	literal int
	text    TokenKind
}

func (p *treeParser) flush(b *stringBody, end int) {
	if end > b.literal {
		b.parts = append(b.parts, p.arena.literals.put(p.chunk, Literal{partNode: partAt(Span{Start: b.literal, End: end}), Text: p.src[b.literal:end]}))
		p.emit(b.text, b.literal, end)
	}
}

func (p *treeParser) escape(b *stringBody, at, end int, text string) int {
	p.flush(b, at)
	p.emit(TokEscape, at, end)
	b.parts = append(b.parts, &Escape{partNode: partAt(Span{Start: at, End: end}), Text: text})
	b.literal = end
	return end
}

func (p *treeParser) interpolate(b *stringBody, at int) (int, bool) {
	p.flush(b, at)
	p.emit(TokInterp, at, at+len("${"))
	p.tok = token{kind: TokInterp, start: at, end: at + len("${")}
	in := p.antiquote()
	b.parts = append(b.parts, in)
	if !in.Closed {
		return 0, false
	}
	b.literal = p.tok.end
	return p.tok.end, true
}

func (p *treeParser) closeAt(end int) {
	p.tok = token{kind: TokQuote, start: end, end: end}
	p.advance()
}

func (p *treeParser) unterminated(open token, kind StringKind, b *stringBody, end int) *String {
	p.report(Span{Start: open.start, End: open.end}, "missing `"+p.src[open.start:open.end]+"` to match `"+p.src[open.start:open.end]+"`")
	if !p.fatal {
		p.closeAt(end)
	}
	stop := p.prevEnd
	return p.arena.strings.put(p.chunk, String{exprNode: exprAt(Span{Start: open.start, End: stop}), Kind: kind, Content: Span{Start: open.end, End: stop}, Parts: b.parts})
}

func (p *treeParser) quoted() *String {
	src := p.src
	open := p.tok
	b := &stringBody{literal: open.end, text: TokStringText}
	i := open.end
	for i < len(src) {
		c := src[i]
		if c != '"' && c != '\\' && c != '$' {
			i++
			continue
		}
		switch {
		case c == '"':
			p.flush(b, i)
			p.emit(TokQuote, i, i+1)
			p.closeAt(i + 1)
			return p.arena.strings.put(p.chunk, String{exprNode: exprAt(Span{Start: open.start, End: i + 1}), Kind: Quoted, Content: Span{Start: open.end, End: i}, Parts: b.parts, Closed: true})
		case c == '\\' && i+1 < len(src):
			_, width := utf8.DecodeRuneInString(src[i+1:])
			i = p.escape(b, i, i+1+width, decodeEscape(src[i+1:i+1+width]))
		case strings.HasPrefix(src[i:], "${"):
			next, ok := p.interpolate(b, i)
			if !ok {
				return p.unterminated(open, Quoted, b, len(src))
			}
			i = next
		case c == '$' && i+1 < len(src) && src[i+1] != '"' && src[i+1] != '\\':
			i += 2
		default:
			i++
		}
	}
	p.flush(b, len(src))
	return p.unterminated(open, Quoted, b, len(src))
}

func (p *treeParser) indented() *String {
	src := p.src
	open := p.tok
	b := &stringBody{literal: open.end, text: TokStringText}
	i := open.end
	for i < len(src) {
		if src[i] != '\'' && src[i] != '$' {
			i++
			continue
		}
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "'''"):
			i = p.escape(b, i, i+3, "''")
		case strings.HasPrefix(rest, "''$"):
			i = p.escape(b, i, i+3, "$")
		case strings.HasPrefix(rest, "''\\") && len(rest) > 3:
			_, width := utf8.DecodeRuneInString(rest[3:])
			i = p.escape(b, i, i+3+width, decodeEscape(rest[3:3+width]))
		case strings.HasPrefix(rest, "''"):
			p.flush(b, i)
			p.emit(TokIndentQuote, i, i+2)
			p.closeAt(i + 2)
			return p.arena.strings.put(p.chunk, String{exprNode: exprAt(Span{Start: open.start, End: i + 2}), Kind: Indented, Content: Span{Start: open.end, End: i}, Parts: b.parts, Closed: true})
		case strings.HasPrefix(rest, "${"):
			next, ok := p.interpolate(b, i)
			if !ok {
				return p.unterminated(open, Indented, b, len(src))
			}
			i = next
		case rest[0] == '$' && len(rest) > 1 && rest[1] != '\'':
			i += 2
		default:
			i++
		}
	}
	p.flush(b, len(src))
	return p.unterminated(open, Indented, b, len(src))
}

func (p *treeParser) path() *Path {
	t := p.tok
	if t.kind == TokSearchPath {
		p.advance()
		inner := Span{Start: t.start + 1, End: t.end - 1}
		literal := p.arena.literals.put(p.chunk, Literal{partNode: partAt(inner), Text: p.src[inner.Start:inner.End]})
		return p.arena.paths.put(p.chunk, Path{exprNode: exprAt(Span{Start: t.start, End: t.end}), Kind: SearchPath, Parts: []Part{literal}})
	}
	kind := RelativePath
	switch {
	case t.kind == TokHomePath:
		kind = HomePath
	case p.src[t.start] == '/':
		kind = AbsolutePath
	}
	first := p.arena.literals.put(p.chunk, Literal{partNode: partAt(Span{Start: t.start, End: t.end}), Text: p.src[t.start:t.end]})
	b := &stringBody{parts: []Part{first}, literal: t.end, text: TokPathText}
	end, closed := p.pathBody(b, t.end)
	if closed {
		p.closeAt(end)
	}
	if closed && p.src[end-1] == '/' {
		p.report(Span{Start: t.start, End: end}, "a path cannot end in `/`")
	}
	return p.arena.paths.put(p.chunk, Path{exprNode: exprAt(p.span(t.start)), Kind: kind, Parts: b.parts})
}

func (p *treeParser) pathBody(b *stringBody, at int) (int, bool) {
	src := p.src
	for strings.HasPrefix(src[at:], "${") {
		next, ok := p.interpolate(b, at)
		if !ok {
			return 0, false
		}
		at = next
		for at < len(src) && (isPathByte(src[at]) || src[at] == '/') {
			at++
		}
	}
	p.flush(b, at)
	return at, true
}
