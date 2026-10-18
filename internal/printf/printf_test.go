package printf

import "testing"

func TestConversions(t *testing.T) {
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
		"%5y%s":                         "5s",
		"%0-5d":                         "d",
		"%+-#5x":                        "x",
		"%  d":                          "d",
		"trailing %":                    "",
		"%s %-":                         "s",
		"%s %l":                         "s",
	} {
		if got := string(Conversions(format)); got != want {
			t.Errorf("Conversions(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestPending(t *testing.T) {
	t.Parallel()

	for format, want := range map[string]bool{
		"%":         true,
		"%-10":      true,
		"%0*":       true,
		"%5.":       true,
		"%5.3":      true,
		"%-+ #0*.*": true,
		"a %s %":    true,
		"%%%":       true,
		"":          false,
		"x":         false,
		"%%":        false,
		"%s":        false,
		"%l":        false,
		"%5.3.":     false,
		"%(%":       false,
		"%(%F)":     false,
		"%(%F)T %":  true,
	} {
		if got := Pending(format); got != want {
			t.Errorf("Pending(%q) = %t, want %t", format, got, want)
		}
	}
}
