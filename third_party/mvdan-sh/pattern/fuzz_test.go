// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package pattern

import (
	"regexp"
	"testing"
)

func FuzzRegexp(f *testing.F) {
	for _, seed := range []string{"foo*bar?", "[a-z]**/x", "@(a|b)*(c)", "[!]]", `\[*`, "+(x", "[[:alpha:]]", "**/"} {
		f.Add(seed, uint(EntireString|ExtendedOperators))
	}
	f.Fuzz(func(t *testing.T, pat string, mode uint) {
		mode &= uint(Shortest | Filenames | EntireString | NoGlobCase | NoGlobStar | GlobLeadingDot | ExtendedOperators)
		expr, err := Regexp(pat, Mode(mode))
		if err != nil {
			return
		}
		if len(expr) > 16*len(pat)+64 {
			t.Fatalf("Regexp(%q, %d) = %d bytes, out of proportion", pat, mode, len(expr))
		}
		if _, err := regexp.Compile(expr); err != nil {
			t.Fatalf("Regexp(%q, %d) = %q, which does not compile: %v", pat, mode, expr, err)
		}
	})
}
