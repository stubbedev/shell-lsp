package langtest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
)

func Describe(src string, tokens []lang.Token) string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		name := src[t.Start:t.End] + ":" + t.Kind.String()
		if t.Modifiers&lang.Builtin != 0 {
			name += "+builtin"
		}
		if t.Modifiers&lang.Declaration != 0 {
			name += "+declaration"
		}
		if t.Modifiers&lang.Option != 0 {
			name += "+option"
		}
		out[i] = name
	}
	return strings.Join(out, " ")
}

type Case [3]string

func (c Case) Src() string { return c[0] }

func (c Case) Message() string { return c[2] }

func (c Case) Offset() int {
	if c[1] == "" {
		return len(c[0])
	}
	return strings.LastIndex(c[0], c[1])
}

func CheckValid(t *testing.T, parse func(string) lang.Result, sources []string) {
	t.Helper()

	for _, src := range sources {
		if res := parse(src); len(res.Errors) > 0 {
			t.Errorf("Parse(%q) = errors %+v", src, res.Errors)
		}
	}
}

func CheckInvalid(t *testing.T, parse func(string) lang.Result, cases []Case) {
	t.Helper()

	for _, c := range cases {
		res := parse(c.Src())
		want := lang.Error{Start: c.Offset(), Message: c.Message()}
		if len(res.Errors) != 1 || res.Errors[0].Start != want.Start || res.Errors[0].Message != want.Message {
			t.Errorf("Parse(%q) = errors %+v, want only %q at %d", c.Src(), res.Errors, want.Message, want.Start)
		}
	}
}

func CheckErrors(t *testing.T, parse func(string) lang.Result, src string, want ...[2]string) {
	t.Helper()

	var got, expected []string
	for _, e := range parse(src).Errors {
		got = append(got, fmt.Sprintf("%d %s", e.Start, e.Message))
	}
	from := 0
	for _, w := range want {
		at := strings.Index(src[from:], w[0])
		switch {
		case w[0] == "":
			from = len(src)
		case at < 0:
			t.Fatalf("%q does not occur in %q after offset %d", w[0], src, from)
		default:
			from += at
		}
		expected = append(expected, fmt.Sprintf("%d %s", from, w[1]))
		from++
	}
	if !slices.Equal(got, expected) {
		t.Errorf("errors of %q\n got %q\nwant %q", src, got, expected)
	}
}

func CheckTokens(t *testing.T, parse func(string) lang.Result, src string, want ...string) {
	t.Helper()

	if got := Describe(src, parse(src).Tokens); got != strings.Join(want, " ") {
		t.Errorf("tokens of %q\n got %s\nwant %s", src, got, strings.Join(want, " "))
	}
}

func CheckInvariants(t *testing.T, src string, res lang.Result) {
	t.Helper()

	end := 0
	for _, tok := range res.Tokens {
		if tok.Start < end || tok.End <= tok.Start || tok.End > len(src) {
			t.Fatalf("token %+v of %q overlaps the previous one, is empty or out of bounds", tok, src)
		}
		end = tok.End
	}
	if len(res.Errors) > lang.MaxErrors {
		t.Fatalf("%d errors in %q, want at most %d", len(res.Errors), src, lang.MaxErrors)
	}
	for i, e := range res.Errors {
		if e.Start < 0 || e.Start > e.End || e.End > len(src) || e.Message == "" {
			t.Fatalf("error %+v of %q is out of bounds or empty", e, src)
		}
		if i > 0 && res.Errors[i-1].Start >= e.Start {
			t.Fatalf("errors %+v of %q are not ordered by position", res.Errors, src)
		}
	}
}

func Fuzz(f *testing.F, parse func(string) lang.Result, seeds []string) {
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		CheckInvariants(t, src, parse(src))
	})
}
