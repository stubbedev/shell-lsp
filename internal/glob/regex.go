package glob

import (
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func ParseRegex(segments []Segment, syn Syntax) Pattern {
	out := Pattern{Syntax: syn}
	p := &parse{items: lex(segments), syn: syn, out: &out}
	out.Root = p.regexAlternation(0)
	return out
}

const basicEscapedOperators = "(){}|+?"

func (p *parse) regexOp(offset int, c byte) bool {
	it, ok := p.peek(offset)
	if !ok || it.unknown != nil || it.quals != nil || it.c != c {
		return false
	}
	if p.syn.Regex == BasicRegex && strings.IndexByte(basicEscapedOperators, c) >= 0 {
		return it.escaped
	}
	return it.active
}

func (p *parse) regexAlternation(depth int) Node {
	start := p.pos
	choices := []Node{p.regexSequence(depth)}
	for p.regexOp(0, '|') {
		p.pos++
		choices = append(choices, p.regexSequence(depth))
	}
	if len(choices) == 1 {
		return choices[0]
	}
	return &Alternatives{At: p.span(start), Choices: choices}
}

func (p *parse) regexSequence(depth int) Node {
	start := p.pos
	var items []Node
	for p.pos < len(p.items) && !p.regexOp(0, '|') && (depth == 0 || !p.regexOp(0, ')')) {
		atom := p.regexAtom(depth, len(items) == 0)
		items = append(items, p.regexQuantifiers(atom))
	}
	return &Seq{At: p.span(start), Items: mergeLiterals(items)}
}

const (
	regexEscapes = "wWsSbB<>`'"
	kshEscapes   = regexEscapes + "dD"
)

func (p *parse) escapes() string {
	if p.syn.Dialect == dialect.Ksh {
		return kshEscapes
	}
	return regexEscapes
}

func (p *parse) regexAtom(depth int, first bool) Node {
	it := p.items[p.pos]
	switch {
	case it.unknown != nil:
		p.pos++
		return &Unknown{At: it.at, Source: it.unknown.Text, Glob: it.unknown.Glob}
	case p.regexOp(0, '('):
		return p.regexGroup(depth)
	case p.regexOp(0, '.'):
		p.pos++
		return &AnyChar{At: it.at}
	case p.regexOp(0, '['):
		if n, ok := p.bracket(); ok {
			return n
		}
	case p.regexOp(0, '^') && (p.syn.Regex == ExtendedRegex || first):
		p.pos++
		return &Anchor{At: it.at, Start: true}
	case p.regexOp(0, '$') && (p.syn.Regex == ExtendedRegex || p.regexEnds(1, depth)):
		p.pos++
		return &Anchor{At: it.at}
	case it.escaped && strings.IndexByte(p.escapes(), it.c) >= 0:
		p.pos++
		return &Escape{At: it.at, Kind: it.c}
	case it.escaped && it.c >= '1' && it.c <= '9':
		p.pos++
		return &Backref{At: it.at, N: int(it.c - '0')}
	}
	return p.literal()
}

func (p *parse) regexEnds(offset, depth int) bool {
	_, more := p.peek(offset)
	if !more {
		return true
	}
	p.pos += offset
	defer func() { p.pos -= offset }()
	return p.regexOp(0, '|') || depth > 0 && p.regexOp(0, ')')
}

func (p *parse) regexGroup(depth int) Node {
	start := p.pos
	p.pos++
	p.out.Captures++
	capture := p.out.Captures
	x := p.regexAlternation(depth + 1)
	if !p.regexOp(0, ')') {
		p.fail(p.items[start].at, "unclosed group")
		return x
	}
	p.pos++
	return &Group{At: p.span(start), Op: GroupPlain, X: x, Capture: capture}
}

func (p *parse) regexQuantifiers(atom Node) Node {
	if _, ok := atom.(*Anchor); ok {
		return atom
	}
	for {
		lo, hi, width, ok := p.regexQuantifier()
		if !ok {
			return atom
		}
		p.pos += width
		atom = &Repeat{At: p.joined(atom.Span(), p.items[p.pos-1].at), X: atom, Min: lo, Max: hi}
	}
}

func (p *parse) joined(a, b parser.Span) parser.Span {
	return parser.Span{Start: min(a.Start, b.Start), End: max(a.End, b.End)}
}

func (p *parse) regexQuantifier() (lo, hi, width int, ok bool) {
	switch {
	case p.regexOp(0, '*'):
		return 0, unbounded, 1, true
	case p.regexOp(0, '+'):
		return 1, unbounded, 1, true
	case p.regexOp(0, '?'):
		return 0, 1, 1, true
	case p.regexOp(0, '{'):
		var text strings.Builder
		for i := 1; ; i++ {
			if p.regexOp(i, '}') {
				lo, hi, ok := repeatCount(text.String())
				return lo, hi, i + 1, ok
			}
			it, more := p.peek(i)
			if !more || it.unknown != nil || it.c != ',' && (it.c < '0' || it.c > '9') {
				return 0, 0, 0, false
			}
			text.WriteByte(it.c)
		}
	}
	return 0, 0, 0, false
}

func regexEscapeText(kind byte, d dialect.Dialect) string {
	text := map[byte]string{
		'w': "a word character: a letter, digit or underscore", 'W': "a character that is not a letter, digit or underscore",
		's': "whitespace", 'S': "a character that is not whitespace",
		'b': "a word boundary", 'B': "a position inside a word or between two non-word characters",
		'<': "the start of a word", '>': "the end of a word",
		'`': "the start of the value", '\'': "the end of the value",
		'd': "a digit", 'D': "a character that is not a digit",
	}[kind]
	if d == dialect.Ksh {
		return text
	}
	return text + " (`\\" + string(kind) + "` is a GNU extension; BSD and macOS read it differently)"
}

func backrefText(n int) string {
	return "the same text that group " + strconv.Itoa(n) + " matched"
}
