package lang

import "testing"

func TestFailKeepsTheFirstError(t *testing.T) {
	t.Parallel()

	var r Result
	r.Fail(5, 6, "first")
	r.Fail(1, 2, "second")
	if r.Error == nil || r.Error.Message != "first" || r.Error.Start != 5 {
		t.Errorf("error = %+v, want the first one reported", r.Error)
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

func TestDocs(t *testing.T) {
	t.Parallel()

	docs := Docs{"f": {"f(x)", "Does f."}}
	doc, ok := docs.Lookup("f")
	if !ok || doc.Markdown("awk") != "```awk\nf(x)\n```\n\nDoes f." {
		t.Errorf("Lookup(f) = %+v, %v", doc, ok)
	}
	if _, ok := docs.Lookup("g"); ok {
		t.Error("Lookup(g) found an entry")
	}
}
