package lang

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/embedded/regex"
)

func TestExcerptSpans(t *testing.T) {
	t.Parallel()

	src := `x "a\n(b" y`
	verbatim := Verbatim(src, 3, 9)
	if start, end := verbatim.Span(2, 3); start != 5 || end != 6 {
		t.Errorf("verbatim Span(2, 3) = %d-%d, want 5-6", start, end)
	}
	b := NewExcerptBuilder(6)
	b.Add("a", 3, 4)
	b.Add("\n", 4, 6)
	b.Add("(", 6, 7)
	b.Add("b", 7, 8)
	decoded := b.Excerpt()
	if decoded.Text != "a\n(b" {
		t.Fatalf("decoded text = %q", decoded.Text)
	}
	for _, tt := range [][4]int{{1, 2, 4, 6}, {2, 3, 6, 7}, {0, 4, 3, 8}, {4, 4, 8, 8}} {
		if start, end := decoded.Span(tt[0], tt[1]); start != tt[2] || end != tt[3] {
			t.Errorf("decoded Span(%d, %d) = %d-%d, want %d-%d", tt[0], tt[1], start, end, tt[2], tt[3])
		}
	}
}

func TestCheckRegexReportsAtTheSource(t *testing.T) {
	t.Parallel()

	src := `x "a\n(b" y`
	b := NewExcerptBuilder(6)
	b.Add("a", 3, 4)
	b.Add("\n", 4, 6)
	b.Add("(", 6, 7)
	b.Add("b", 7, 8)
	var r Result
	if _, known := r.CheckRegex(b.Excerpt(), regex.Extended).Groups(); known {
		t.Error("an invalid pattern has a known group count")
	}
	if len(r.Errors) != 1 || r.Errors[0].Start != 6 || r.Errors[0].End != 7 {
		t.Errorf("errors = %+v, want the `(` at 6-7 of %q", r.Errors, src)
	}
}
