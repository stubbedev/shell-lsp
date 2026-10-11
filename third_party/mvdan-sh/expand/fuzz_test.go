// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

func FuzzBracesSeq(f *testing.F) {
	for _, seed := range []string{"a{b,c}d", "{1..10..3}", "{z..a}", "{9223372036854775806..9223372036854775807}", "{1..3..-9223372036854775808}", "{a,{b,{c,d}}}", "{01..-05}"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, lit string) {
		word := &syntax.Word{Parts: []syntax.WordPart{&syntax.Lit{Value: lit}}}
		syntax.SplitBraces(word)
		count := 0
		for w, err := range BracesSeq(nil, word) {
			if err != nil {
				return
			}
			if count++; count > 16<<10 {
				t.Fatalf("BracesSeq(%q) yielded more than its element limit", lit)
			}
			if size := len(w.Lit()); size > 2*len(lit)+40 {
				t.Fatalf("BracesSeq(%q) yielded a %d byte word", lit, size)
			}
		}
	})
}

func FuzzFormat(f *testing.F) {
	for _, seed := range []string{`%s\n`, `\x41é\U0001F600`, `%5.2d %*s`, `\0777\c`, `%b`, `%`} {
		f.Add(seed, "arg")
	}
	f.Fuzz(func(t *testing.T, format, arg string) {
		if len(format) > 1<<10 {
			return
		}
		out, _, err := Format(nil, format, strings.Fields(arg))
		if err == nil && len(out) > 1<<20 {
			t.Fatalf("Format(%q) produced %d bytes", format, len(out))
		}
	})
}
