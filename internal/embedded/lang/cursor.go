package lang

import "fmt"

type Brackets[K ~uint8] struct {
	closerOf [256]K
	opens    [256]bool
	closes   [256]bool
}

func NewBrackets[K ~uint8](pairs map[K]K) *Brackets[K] {
	b := &Brackets[K]{}
	for opener, closer := range pairs {
		if b.closes[opener] || b.opens[closer] || opener == closer {
			panic(fmt.Sprintf("lang: bracket %d both opens and closes", max(opener, closer)))
		}
		b.closerOf[opener], b.opens[opener], b.closes[closer] = closer, true, true
	}
	return b
}

type Cursor[K ~uint8] struct {
	Tok, Last Lexeme[K]
	Open      Nesting[K]
	src       string
	res       *Result
	eof       K
	brackets  *Brackets[K]
	next      func() Lexeme[K]
}

func NewCursor[K ~uint8](src string, res *Result, eof K, brackets *Brackets[K], next func() Lexeme[K]) Cursor[K] {
	return Cursor[K]{src: src, res: res, eof: eof, brackets: brackets, next: next}
}

func (c *Cursor[K]) Closes(k K) bool { return c.brackets.closes[k] }

func (c *Cursor[K]) Push(closer K) { c.Open.Push(closer) }

func (c *Cursor[K]) Pop(closer K) {
	if !c.Open.Pop(closer) {
		Abort()
	}
}

func (c *Cursor[K]) Advance() {
	switch k := c.Tok.Kind; {
	case c.brackets.opens[k]:
		c.Push(c.brackets.closerOf[k])
	case c.brackets.closes[k]:
		c.Pop(k)
	}
	c.Last = c.Tok
	c.Tok = c.next()
}

func (c *Cursor[K]) Skip() { c.Tok = c.next() }

func (c *Cursor[K]) Drain() {
	for c.Tok.Kind != c.eof {
		c.Skip()
	}
}

func (c *Cursor[K]) FailAt(t Lexeme[K], message string) {
	c.res.Fail(t.Start, t.End, message)
	Bail()
}

func (c *Cursor[K]) Unexpected() {
	c.FailAt(c.Tok, Unexpected(c.src, c.Tok.Start, c.Tok.End))
}

func (c *Cursor[K]) Expect(k K) Lexeme[K] {
	if c.Tok.Kind != k {
		c.Unexpected()
	}
	t := c.Tok
	c.Advance()
	return t
}

func (c *Cursor[K]) Closing(k K, open Lexeme[K]) {
	if c.Tok.Kind == c.eof {
		c.FailAt(open, Unclosed(c.src, open.Start, open.End))
	}
	c.Expect(k)
}

func (c *Cursor[K]) Reclassify(t Lexeme[K], kind Kind, mods Modifier) {
	c.res.Reclassify(t.Sem, kind, mods)
}
