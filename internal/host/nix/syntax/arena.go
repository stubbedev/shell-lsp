package syntax

import "slices"

const (
	srcBytesPerChunkNode = 512
	maxChunkNodes        = 256
)

func chunkFor(src string) int {
	return min(max(len(src)/srcBytesPerChunkNode, 1), maxChunkNodes)
}

type nodes[T any] struct{ free []T }

func (n *nodes[T]) put(chunk int, value T) *T {
	if len(n.free) == 0 {
		n.free = make([]T, chunk)
	}
	at := &n.free[0]
	n.free = n.free[1:]
	*at = value
	return at
}

type pile[T any] struct {
	stack []T
	free  []T
}

func (p *pile[T]) since(mark, chunk int) []T {
	n := len(p.stack) - mark
	if n == 0 {
		return nil
	}
	if n > len(p.free) {
		p.free = make([]T, max(n, chunk))
	}
	out := p.free[:n:n]
	p.free = p.free[n:]
	copy(out, p.stack[mark:])
	clear(p.stack[mark:])
	p.stack = p.stack[:mark]
	return out
}

type nodeArena struct {
	idents     nodes[Ident]
	identAttrs nodes[IdentAttr]
	selects    nodes[Select]
	applies    nodes[Apply]
	assigns    nodes[Assign]
	parens     nodes[Paren]
	sets       nodes[AttrSet]
	inherits   nodes[Inherit]
	literals   nodes[Literal]
	strings    nodes[String]
	paths      nodes[Path]
	exprs      pile[Expr]
	bindings   pile[Binding]
	attrs      pile[Attr]
}

const (
	wastefulSlack         = 4
	maxTokensPerFourBytes = 3
)

func estimatedTokens(src string) int {
	count := 1
	inWord := false
	inSpace := false
	for i := range len(src) {
		c := src[i]
		switch {
		case isIdentByte(c):
			if !inWord {
				count++
			}
			inWord, inSpace = true, false
		case c == ' ' || c == '\n' || c == '\t' || c == '\r':
			if !inSpace {
				count++
			}
			inWord, inSpace = false, true
		default:
			count++
			inWord, inSpace = false, false
		}
	}
	return min(count, len(src)*maxTokensPerFourBytes/4+1)
}

func Tightened[T any](s []T) []T {
	if cap(s)-len(s) <= len(s)/wastefulSlack {
		return s
	}
	return slices.Clone(s)
}
