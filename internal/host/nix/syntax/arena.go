package syntax

const (
	minChunk = 4
	maxChunk = 32
)

type Chunks[T any] struct {
	free []T
	next int
}

func (c *Chunks[T]) grow(atLeast int) {
	c.next = min(max(2*c.next, minChunk), maxChunk)
	c.free = make([]T, max(c.next, atLeast))
}

func (c *Chunks[T]) New() *T {
	if len(c.free) == 0 {
		c.grow(1)
	}
	at := &c.free[0]
	c.free = c.free[1:]
	return at
}

func (c *Chunks[T]) Take(n int) []T {
	if n > len(c.free) {
		c.grow(n)
	}
	out := c.free[:n:n]
	c.free = c.free[n:]
	return out
}

func (c *Chunks[T]) put(value T) *T {
	at := c.New()
	*at = value
	return at
}

type pile[T any] struct {
	stack []T
	kept  Chunks[T]
}

func (p *pile[T]) since(mark int) []T {
	n := len(p.stack) - mark
	if n == 0 {
		return nil
	}
	out := p.kept.Take(n)
	copy(out, p.stack[mark:])
	clear(p.stack[mark:])
	p.stack = p.stack[:mark]
	return out
}

type nodeArena struct {
	idents     Chunks[Ident]
	identAttrs Chunks[IdentAttr]
	selects    Chunks[Select]
	applies    Chunks[Apply]
	assigns    Chunks[Assign]
	parens     Chunks[Paren]
	sets       Chunks[AttrSet]
	inherits   Chunks[Inherit]
	literals   Chunks[Literal]
	strings    Chunks[String]
	paths      Chunks[Path]
	exprs      pile[Expr]
	bindings   pile[Binding]
	attrs      pile[Attr]
}

const maxTokensPerFourBytes = 3

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
