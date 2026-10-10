package lint

import (
	"slices"
	"testing"
)

func TestQuoteTaintUsesUnescapedText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		src    string
		origin string
	}{
		{"a=foo\\ bar\nrm $a\n", ""},
		{"b=\\\"x\\\"\nrm $b\n", `\"x\"`},
		{"c=\"it\\\"s\"\nrm $c\n", `it\"s`},
		{"g='a'\\''b'\nrm $g\n", `\'`},
		{"h=x\nh+=\"'y z'\"\nrm $h\n", `'y z'`},
		{"for d in \"a\\\"b\" c; do rm $d; done\n", `a\"b`},
		{": \"${e:=\"--x=\\\"y\\\"\"}\"\nrm $e\n", `--x=\"y\"`},
		{"f='--a=\"b\"'\nf=plain\nrm $f\n", ""},
	}
	for _, c := range cases {
		var origins []string
		for _, f := range findingsWith(t, c.src, 2089) {
			origins = append(origins, c.src[f.Span.Start:f.Span.End])
		}
		var want []string
		if c.origin != "" {
			want = append(want, c.origin)
		}
		if !slices.Equal(origins, want) {
			t.Errorf("%q: SC2089 origins = %q, want %q", c.src, origins, want)
		}
	}
}
