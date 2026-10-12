// Copyright (c) 2018, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"math"
	"strconv"
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
		args := strings.Fields(arg)
		bound := formatBound(format, args)
		if len(format) > 1<<10 || bound > 1<<24 {
			return // printing what it asks for would take too much memory
		}
		out, _, err := Format(nil, format, args)
		if err == nil && len(out) > bound {
			t.Fatalf("Format(%q, %q) produced %d bytes, more than the %d it can ask for", format, args, len(out), bound)
		}
	})
}

// formatBound is the most output that Format may produce for a format and
// its arguments: every byte of both, each number in the format as the width
// or precision of a field, and the sign, prefix and digits of each number
// that a directive formats.
func formatBound(format string, args []string) int {
	bound := len(format) + 24*strings.Count(format, "%")
	for _, arg := range args {
		bound += len(arg)
	}
	for _, digits := range strings.FieldsFunc(format, func(r rune) bool { return r < '0' || r > '9' }) {
		n, err := strconv.Atoi(digits)
		if err != nil || n > math.MaxInt-bound {
			return math.MaxInt
		}
		bound += n
	}
	return bound
}
