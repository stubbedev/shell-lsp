package lint

import "testing"

func TestPrintfConversions(t *testing.T) {
	t.Parallel()

	for format, want := range map[string]string{
		"%s":                            "s",
		"%0*s":                          "*s",
		"%(%F)T":                        "T",
		"%d%%%(%H)T":                    "dT",
		"%b %d %b %d %b %d %b %d %b\\n": "bdbdbdbdb",
		"%s%s":                          "ss",
		"%s\n%s":                        "ss",
		"%ld":                           "d",
		"%lld":                          "d",
		"%Q":                            "Q",
		"%-*.*f":                        "**f",
		"%(unterminated":                "",
		"%y":                            "y",
	} {
		if got := string(printfConversions(format)); got != want {
			t.Errorf("printfConversions(%q) = %q, want %q", format, got, want)
		}
	}
}
