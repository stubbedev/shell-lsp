package lang

import "testing"

const (
	kEOF testKind = iota + 100
	kOpen
	kClose
	kWord
)

var testBrackets = NewBrackets(map[testKind]testKind{kOpen: kClose})

func cursorOver(src string, res *Result, kinds ...testKind) *Cursor[testKind] {
	at := 0
	next := func() Lexeme[testKind] {
		if at == len(kinds) {
			return Lexeme[testKind]{Kind: kEOF, Start: len(src), End: len(src)}
		}
		l := Lexeme[testKind]{Kind: kinds[at], Start: at, End: at + 1}
		at++
		return l
	}
	c := NewCursor(src, res, kEOF, testBrackets, next)
	c.Skip()
	return &c
}

func TestCursorTracksBrackets(t *testing.T) {
	t.Parallel()

	var res Result
	c := cursorOver("(a)", &res, kOpen, kWord, kClose)
	c.Advance()
	if c.Open.Depth() != 1 || c.Last.Kind != kOpen || c.Tok.Kind != kWord {
		t.Fatalf("after ( depth %d, last %v, token %v", c.Open.Depth(), c.Last.Kind, c.Tok.Kind)
	}
	c.Advance()
	c.Closing(kClose, c.Last)
	if c.Open.Depth() != 0 || c.Tok.Kind != kEOF || len(res.Errors) != 0 {
		t.Errorf("after ) depth %d, token %v, errors %v", c.Open.Depth(), c.Tok.Kind, res.Errors)
	}
}

func TestCursorAbortsOnAStrayCloser(t *testing.T) {
	t.Parallel()

	var res Result
	c := cursorOver(")", &res, kClose)
	finished := false
	Run(func() {
		c.Advance()
		finished = true
	})
	if finished {
		t.Error("a closer with nothing open did not abort")
	}
}

func TestCursorReportsAnUnclosedOpenerAtTheOpener(t *testing.T) {
	t.Parallel()

	var res Result
	c := cursorOver("(a", &res, kOpen, kWord)
	open := c.Expect(kOpen)
	c.Advance()
	if Attempt(func() { c.Closing(kClose, open) }) {
		t.Fatal("closing at the end of input did not bail out")
	}
	if len(res.Errors) != 1 || res.Errors[0].Start != 0 || res.Errors[0].Message != "`(` is never closed" {
		t.Errorf("errors = %+v", res.Errors)
	}
}

func TestBracketsRefuseACloserThatOpens(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("a bracket that both opens and closes was accepted")
		}
	}()
	NewBrackets(map[testKind]testKind{kOpen: kClose, kClose: kWord})
}
