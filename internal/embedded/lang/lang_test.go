package lang

import (
	"slices"
	"testing"
)

func TestFailKeepsDistinctErrorsInOrder(t *testing.T) {
	t.Parallel()

	var r Result
	r.Fail(5, 6, "second")
	r.Fail(1, 2, "first")
	r.Fail(5, 5, "same place")
	r.Fail(1, 4, "overlapping")
	r.Fail(9, 9, "at the end")
	r.Fail(9, 12, "starting at the end")
	r.Fail(6, 8, "after the second")
	var got []string
	for _, e := range r.Errors {
		got = append(got, e.Message)
	}
	if want := []string{"first", "second", "after the second", "at the end"}; !slices.Equal(got, want) {
		t.Errorf("errors = %q, want %q", got, want)
	}
}

func TestFailStopsAtMaxErrors(t *testing.T) {
	t.Parallel()

	var r Result
	for i := range MaxErrors + 5 {
		r.Fail(i*2, i*2+1, "error")
	}
	if len(r.Errors) != MaxErrors || r.Errors[MaxErrors-1].Start != (MaxErrors-1)*2 {
		t.Errorf("errors = %+v, want the first %d", r.Errors, MaxErrors)
	}
}

func TestEmitSkipsEmptySpans(t *testing.T) {
	t.Parallel()

	var r Result
	if i := r.Emit(3, 3, String, 0); i != -1 || len(r.Tokens) != 0 {
		t.Errorf("Emit of an empty span = %d with tokens %v", i, r.Tokens)
	}
}

func TestTokenAt(t *testing.T) {
	t.Parallel()

	r := Result{Tokens: []Token{{Start: 0, End: 2}, {Start: 4, End: 7, Kind: Number}}}
	for offset, want := range map[int]bool{0: true, 1: true, 2: false, 3: false, 4: true, 6: true, 7: false} {
		if _, ok := r.TokenAt(offset); ok != want {
			t.Errorf("TokenAt(%d) found = %v, want %v", offset, ok, want)
		}
	}
	if tok, _ := r.TokenAt(5); tok.Kind != Number {
		t.Errorf("TokenAt(5) = %+v", tok)
	}
}

func TestQuote(t *testing.T) {
	t.Parallel()

	src := "ab`c\nlong-long-long-long-long-long-token"
	for _, tt := range []struct {
		start, end int
		want       string
	}{
		{0, 2, "`ab`"},
		{0, 4, "\"ab`c\""},
		{4, 5, "end of line"},
		{len(src), len(src), "end of program"},
		{5, len(src), "`long-long-long-long-long…`"},
	} {
		if got := Quote(src, tt.start, tt.end); got != tt.want {
			t.Errorf("Quote(%d, %d) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestDocMarkdown(t *testing.T) {
	t.Parallel()

	doc := Doc{Signature: "f(x)", Summary: "Does f."}
	if got := doc.Markdown("awk"); got != "```awk\nf(x)\n```\n\nDoes f." {
		t.Errorf("Markdown = %q", got)
	}
}
