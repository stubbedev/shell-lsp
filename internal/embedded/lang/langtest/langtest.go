package langtest

import (
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
		if res := parse(src); res.Error != nil {
			t.Errorf("Parse(%q) = error %q at %d", src, res.Error.Message, res.Error.Start)
		}
	}
}

func CheckInvalid(t *testing.T, parse func(string) lang.Result, cases []Case) {
	t.Helper()

	for _, c := range cases {
		res := parse(c.Src())
		if res.Error == nil {
			t.Errorf("Parse(%q) = no error, want %q", c.Src(), c.Message())
			continue
		}
		if at := c.Offset(); res.Error.Start != at || res.Error.Message != c.Message() {
			t.Errorf("Parse(%q) = %q at %d, want %q at %d", c.Src(), res.Error.Message, res.Error.Start, c.Message(), at)
		}
	}
}

func CheckTokens(t *testing.T, parse func(string) lang.Result, src string, want ...string) {
	t.Helper()

	if got := Describe(src, parse(src).Tokens); got != strings.Join(want, " ") {
		t.Errorf("tokens of %q\n got %s\nwant %s", src, got, strings.Join(want, " "))
	}
}
