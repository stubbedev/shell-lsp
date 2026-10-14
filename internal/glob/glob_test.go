package glob

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func pattern(t *testing.T, d dialect.Dialect, text string) Pattern {
	t.Helper()
	return Parse([]Segment{{Span: parser.Span{End: len(text)}, Kind: SegmentPattern, Text: text}}, Syntax{Dialect: d})
}

func matcher(t *testing.T, d dialect.Dialect, text string) Matcher {
	t.Helper()
	m, err := pattern(t, d, text).Matcher()
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return m
}

func TestWholeMatches(t *testing.T) {
	tests := []struct {
		dialect dialect.Dialect
		pattern string
		yes     []string
		no      []string
	}{
		{dialect.Zsh, "a^b", []string{"ac", "a^b", "a"}, []string{"ab"}},
		{dialect.Zsh, "^a*", []string{"ba", ""}, []string{"ab", "a"}},
		{dialect.Zsh, "*.c~f*", []string{"bar.c"}, []string{"foo.c", "bar.o"}},
		{dialect.Zsh, "*.c~f*~b*", []string{"x.c"}, []string{"foo.c", "bar.c"}},
		{dialect.Zsh, "<1-10>5", []string{"105", "15"}, []string{"5", "115"}},
		{dialect.Zsh, "<->", []string{"0", "18446744073709551616"}, []string{"", "x"}},
		{dialect.Zsh, "<5->", []string{"5", "99"}, []string{"4"}},
		{dialect.Zsh, "(#i)X", []string{"x", "X"}, []string{"y"}},
		{dialect.Zsh, "a(#i)b(#I)c", []string{"aBc"}, []string{"ABc", "aBC"}},
		{dialect.Zsh, "(#l)ab", []string{"ab", "AB", "aB"}, []string{"ac"}},
		{dialect.Zsh, "(#l)AB", []string{"AB"}, []string{"Ab", "ab"}},
		{dialect.Zsh, "a#", []string{"", "a", "aaa"}, []string{"b"}},
		{dialect.Zsh, "a##", []string{"a", "aaa"}, []string{""}},
		{dialect.Zsh, "(ab)#c", []string{"c", "ababc"}, []string{"abac"}},
		{dialect.Zsh, "a(#c2,3)", []string{"aa", "aaa"}, []string{"a", "aaaa"}},
		{dialect.Zsh, "x(#c2)", []string{"xx"}, []string{"x", "xxx"}},
		{dialect.Zsh, "(foo|ba[rz])-*", []string{"foo-1", "baz-"}, []string{"bat-1"}},
		{dialect.Zsh, "[^=]##=*", []string{"k=v", "k=="}, []string{"=v"}},
		{dialect.Zsh, "(#s)a*", []string{"ab"}, []string{"ba"}},
		{dialect.Bash, "+([0-9])", []string{"1", "123"}, []string{"", "1a"}},
		{dialect.Bash, "!(abc)", []string{"abd", "", "abcd"}, []string{"abc"}},
		{dialect.Bash, "@(a|b)*.c", []string{"bx.c", "a.c"}, []string{"c.c"}},
		{dialect.Bash, "?(x)y", []string{"y", "xy"}, []string{"xxy"}},
		{dialect.Bash, "*(ab)", []string{"", "abab"}, []string{"aba"}},
		{dialect.Bash, "[[:alpha:]_]*", []string{"Q", "_x"}, []string{"1"}},
		{dialect.Bash, "[!a-c]", []string{"d"}, []string{"b", "dd"}},
		{dialect.Bash, "[]a]", []string{"]", "a"}, []string{"b"}},
		{dialect.Bash, `\*`, []string{"*"}, []string{"x"}},
		{dialect.Bash, "a|b", []string{"a|b"}, []string{"a", "b"}},
		{dialect.Bash, "(a)", []string{"(a)"}, []string{"a"}},
		{dialect.Bash, "[a", []string{"[a"}, []string{"a"}},
		{dialect.Bash, "é?", []string{"éa", "éé"}, []string{"é"}},
		{dialect.Posix, "@(a)", []string{"@(a)"}, []string{"a"}},
		{dialect.Ksh, "{2}(ab)c", []string{"ababc"}, []string{"abc"}},
		{dialect.Ksh, "{1,}(x)", []string{"x", "xxx"}, []string{""}},
	}
	for _, tt := range tests {
		m := matcher(t, tt.dialect, tt.pattern)
		for _, s := range tt.yes {
			if _, ok, err := m.Whole(s); !ok || err != nil {
				t.Errorf("%s %q should match %q (err %v)", tt.dialect, tt.pattern, s, err)
			}
		}
		for _, s := range tt.no {
			if _, ok, err := m.Whole(s); ok || err != nil {
				t.Errorf("%s %q should not match %q (err %v)", tt.dialect, tt.pattern, s, err)
			}
		}
	}
}

func TestRemoveAndReplace(t *testing.T) {
	literal := func(r string) func(string, Match) string {
		return func(string, Match) string { return r }
	}
	tests := []struct {
		dialect dialect.Dialect
		pattern string
		run     func(Matcher, string) (string, error)
		in      string
		want    string
	}{
		{dialect.Bash, "*.", func(m Matcher, s string) (string, error) { return m.RemovePrefix(s, false) }, "foo.tar.gz", "tar.gz"},
		{dialect.Bash, "*.", func(m Matcher, s string) (string, error) { return m.RemovePrefix(s, true) }, "foo.tar.gz", "gz"},
		{dialect.Bash, ".*", func(m Matcher, s string) (string, error) { return m.RemoveSuffix(s, false) }, "foo.tar.gz", "foo.tar"},
		{dialect.Bash, ".*", func(m Matcher, s string) (string, error) { return m.RemoveSuffix(s, true) }, "foo.tar.gz", "foo"},
		{dialect.Bash, "x", func(m Matcher, s string) (string, error) { return m.RemoveSuffix(s, true) }, "abc", "abc"},
		{dialect.Bash, "+([0-9])", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, true, literal("N")) }, "a1b22c333", "aNbNcN"},
		{dialect.Bash, "*([0-9])", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, true, literal("-")) }, "abc", "-a-b-c"},
		{dialect.Bash, "*([0-9])", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, false, literal("-")) }, "abc", "-abc"},
		{dialect.Bash, "*([0-9])", func(m Matcher, s string) (string, error) { return m.Replace(s, AtEnd, false, literal("-")) }, "abc", "abc-"},
		{dialect.Bash, "a*", func(m Matcher, s string) (string, error) { return m.Replace(s, AtEnd, false, literal("X")) }, "aaa", "X"},
		{dialect.Bash, "a", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, true, literal("")) }, "aaa", ""},
		{dialect.Bash, "*", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, true, literal("X")) }, "", "X"},
		{dialect.Bash, "a", func(m Matcher, s string) (string, error) { return m.Replace(s, AtStart, false, literal("X")) }, "a1b", "X1b"},
		{dialect.Bash, "b", func(m Matcher, s string) (string, error) { return m.Replace(s, AtStart, false, literal("X")) }, "a1b", "a1b"},
		{dialect.Zsh, "*.c~*.bak", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, true, literal("X")) }, "foo.c.bak", "X.bak"},
		{dialect.Zsh, ".*~.c*", func(m Matcher, s string) (string, error) { return m.RemoveSuffix(s, true) }, "foo.c.bak", "foo.c"},
		{dialect.Zsh, "(#s)k", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, true, literal("X")) }, "k1=v1 k2=v2", "X1=v1 k2=v2"},
		{dialect.Zsh, "2(#e)", func(m Matcher, s string) (string, error) { return m.Replace(s, Anywhere, true, literal("E")) }, "k1=v1 k2=v2", "k1=v1 k2=vE"},
		{dialect.Zsh, "(#b)([^= ]##)=([^ ]#)", func(m Matcher, s string) (string, error) {
			return m.Replace(s, Anywhere, true, func(text string, found Match) string {
				c := found.Captures
				return text[c[1][0]:c[1][1]] + ":" + text[c[0][0]:c[0][1]]
			})
		}, "k1=v1 k2=v2", "v1:k1 v2:k2"},
	}
	for _, tt := range tests {
		got, err := tt.run(matcher(t, tt.dialect, tt.pattern), tt.in)
		if err != nil || got != tt.want {
			t.Errorf("%s %q on %q = %q, %v; want %q", tt.dialect, tt.pattern, tt.in, got, err, tt.want)
		}
	}
}

func TestUnsupportedPatternsAreRefused(t *testing.T) {
	for _, text := range []string{"(#a1)foo", "[[:WORD:]]"} {
		if _, err := pattern(t, dialect.Zsh, text).Matcher(); err == nil {
			t.Errorf("%q: want an error", text)
		}
	}
	p := Parse([]Segment{{Kind: SegmentPattern, Text: "a"}, {Kind: SegmentUnknown, Text: "$x", Glob: true}}, Syntax{Dialect: dialect.Bash})
	if _, err := p.Matcher(); err == nil {
		t.Error("an unknown expansion: want an error")
	}
}

func TestStepBudget(t *testing.T) {
	m := matcher(t, dialect.Bash, strings.Repeat("*a", 30)+"b")
	_, ok, err := m.Whole(strings.Repeat("a", 60))
	if ok || !errors.Is(err, ErrTooComplex) {
		t.Fatalf("got %v, %v; want ErrTooComplex", ok, err)
	}
}

func TestNeeds(t *testing.T) {
	tests := []struct {
		dialect dialect.Dialect
		pattern string
		want    []string
	}{
		{dialect.Zsh, "^*.o", []string{"extendedglob ^*.o"}},
		{dialect.Zsh, "*.c~x*", []string{"extendedglob ~"}},
		{dialect.Zsh, "a##b", []string{"extendedglob ##"}},
		{dialect.Zsh, "(#i)x", []string{"extendedglob (#i)"}},
		{dialect.Zsh, "#foo", nil},
		{dialect.Zsh, "~/.cache~", nil},
		{dialect.Zsh, "(a|b)<1-3>", nil},
		{dialect.Bash, "@(a|b)", []string{"extglob @(a|b)"}},
		{dialect.Ksh, "@(a|b)", nil},
	}
	for _, tt := range tests {
		p := pattern(t, tt.dialect, tt.pattern)
		var got []string
		for _, n := range p.Needs {
			got = append(got, n.Option.String()+" "+tt.pattern[n.Span.Start:n.Span.End])
		}
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%q needs %q, want %q", tt.pattern, got, tt.want)
		}
	}
}

func TestErrors(t *testing.T) {
	for _, text := range []string{"(a|b", "(#x)a", "a(#c3,1)"} {
		if p := pattern(t, dialect.Zsh, text); len(p.Errors) == 0 {
			t.Errorf("%q: want a pattern error", text)
		}
	}
}

func TestPieces(t *testing.T) {
	tests := []struct {
		dialect dialect.Dialect
		pattern string
		want    []string
	}{
		{dialect.Zsh, "(#b)([^=]##)=(*)", []string{
			"(#b): Backreferences: every parenthesised group after this flag stores what it matched in `$match`, with offsets in `$mbegin` and `$mend`",
			"([^=]##): one or more of `[^=]` (one character other than `=`); captured in `$match[1]`",
			"=: the text `=`",
			"(*): any text, including none; captured in `$match[2]`",
		}},
		{dialect.Bash, "@(a|b)*.[ch]", []string{
			"@(a|b): either `a` or `b`",
			"*: any text, including none",
			".: the text `.`",
			"[ch]: one character: `c` or `h`",
		}},
		{dialect.Zsh, "<1-10>~5", []string{"<1-10>~5: a number from 1 to 10, except what matches `5`"}},
		{dialect.Zsh, "^*.o", []string{"^*.o: anything that does not match `*.o`"}},
	}
	for _, tt := range tests {
		var got []string
		for _, piece := range pattern(t, tt.dialect, tt.pattern).Pieces(tt.pattern) {
			got = append(got, piece.Text+": "+piece.Doc)
		}
		if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
			t.Errorf("%q:\n%s\nwant:\n%s", tt.pattern, strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
		}
	}
}

func TestQualifiers(t *testing.T) {
	text := "*.txt(.om[1,3]NLk+10e:'[[ -s $REPLY ]]':mh-1:t)"
	p := Parse([]Segment{
		{Span: parser.Span{End: 5}, Kind: SegmentPattern, Text: "*.txt"},
		{Span: parser.Span{Start: 5, End: len(text)}, Kind: SegmentQualifiers, Text: text[5:]},
	}, Syntax{Dialect: dialect.Zsh, Filenames: true})
	pieces := p.Pieces(text)
	last := pieces[len(pieces)-1].Doc
	for _, want := range []string{
		"`.`: Regular files", "`om`: sort ascending by modification time", "`[1,3]`: only matches 1 to 3",
		"`N`: Null glob", "`Lk+10`: files by size: more than 10 KiB", "`e:'[[ -s $REPLY ]]':`",
		"`mh-1`: files modified within the last 1 hours", "`:t`: apply modifiers to every match: `:t` Tail",
	} {
		if !strings.Contains(last, want) {
			t.Errorf("qualifier description lacks %q:\n%s", want, last)
		}
	}
}

func TestQuotesInsidePatternText(t *testing.T) {
	m := matcher(t, dialect.Zsh, `*('`+"`"+`'|['<>=$']'(')*`)
	for _, s := range []string{"a`b", "x$(y", "<(z"} {
		if _, ok, err := m.Whole(s); !ok || err != nil {
			t.Errorf("should match %q (err %v)", s, err)
		}
	}
	if _, ok, _ := m.Whole("$y"); ok {
		t.Error("should not match $y")
	}
	if p := pattern(t, dialect.Zsh, `'(*)'`); !p.Static() {
		t.Error("a quoted group is literal text")
	}
}

func regex(t *testing.T, d dialect.Dialect, flavor RegexFlavor, text string) Pattern {
	t.Helper()
	return ParseRegex([]Segment{{Span: parser.Span{End: len(text)}, Kind: SegmentText, Text: text}}, Syntax{Dialect: d, Regex: flavor})
}

func TestRegexSearch(t *testing.T) {
	tests := []struct {
		flavor  RegexFlavor
		re      string
		in      string
		want    string
		groups  []string
		matches bool
	}{
		{ExtendedRegex, `^v([0-9]+)\.([0-9]+)$`, "v1.22", "v1.22", []string{"1", "22"}, true},
		{ExtendedRegex, `b+`, "abbbc", "bbb", nil, true},
		{ExtendedRegex, `(a|ab)(c|bcd)(d*)`, "abcd", "abcd", []string{"a", "bcd", ""}, true},
		{ExtendedRegex, `x{2,}`, "axxxb", "xxx", nil, true},
		{ExtendedRegex, `(a)\1`, "baab", "aa", []string{"a"}, true},
		{ExtendedRegex, `\bfoo\b`, "a foo b", "foo", nil, true},
		{ExtendedRegex, `\bfoo\b`, "afoo", "", nil, false},
		{ExtendedRegex, `[^[:space:]]+`, "  ab c", "ab", nil, true},
		{ExtendedRegex, `[\.]`, `a\b`, `\`, nil, true},
		{ExtendedRegex, `colou?r`, "my color", "color", nil, true},
		{BasicRegex, `a\(b*\)c`, "xabbc", "abbc", []string{"bb"}, true},
		{BasicRegex, `a+`, "aa+", "a+", nil, true},
		{BasicRegex, `x\{2\}`, "xxx", "xx", nil, true},
	}
	for _, tt := range tests {
		m, err := regex(t, dialect.Bash, tt.flavor, tt.re).Matcher()
		if err != nil {
			t.Fatalf("%q: %v", tt.re, err)
		}
		found, ok, err := m.Search(tt.in)
		if err != nil || ok != tt.matches {
			t.Errorf("%q on %q: matched %v, err %v", tt.re, tt.in, ok, err)
			continue
		}
		if !ok {
			continue
		}
		if got := tt.in[found.Start:found.End]; got != tt.want {
			t.Errorf("%q on %q found %q, want %q", tt.re, tt.in, got, tt.want)
		}
		for i, want := range tt.groups {
			c := found.Captures[i]
			if got := tt.in[c[0]:c[1]]; got != want {
				t.Errorf("%q on %q group %d = %q, want %q", tt.re, tt.in, i+1, got, want)
			}
		}
	}
}

func TestRegexPieces(t *testing.T) {
	text := `^v([0-9]+)\.x?\w$`
	var got []string
	for _, piece := range regex(t, dialect.Bash, ExtendedRegex, text).Pieces(text) {
		got = append(got, piece.Text+": "+piece.Doc)
	}
	want := []string{
		"^: the start of the value",
		"v: the text `v`",
		"([0-9]+): one or more of `[0-9]` (one character: `0` to `9`); captured in `${BASH_REMATCH[1]}`",
		`\.: the text ` + "`.`",
		"x?: optionally `x`",
		`\w: a word character: a letter, digit or underscore (` + "`\\w`" + ` is a GNU extension; BSD and macOS read it differently)`,
		"$: the end of the value",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("pieces:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestKshPatterns(t *testing.T) {
	tests := []struct {
		pattern string
		yes, no []string
	}{
		{"~(i)abc", []string{"ABC", "abc"}, []string{"abd"}},
		{"a~(i:b)C", []string{"aBC", "abC"}, []string{"abc"}},
		{"~(-i)ABC", []string{"ABC"}, []string{"abc"}},
		{"~(E)abc", []string{"xabcx", "abc"}, []string{"ab"}},
		{"~(E)^a.c$", []string{"abc"}, []string{"xabc"}},
		{"~(F)a.c", []string{"xa.cx"}, []string{"abc"}},
		{"~(El)abc", []string{"abcx"}, []string{"xabc"}},
		{"~(Er)abc", []string{"xabc"}, []string{"abcx"}},
		{"~(Ei)A+", []string{"xaay"}, []string{"xy"}},
		{"x~(E:b+)y", []string{"xbby", "xabcy"}, []string{"xy"}},
		{"*-(a)b", []string{"aab", "b"}, []string{"aa"}},
		{"{2,}-(a)", []string{"aa", "aaa"}, []string{"a"}},
	}
	for _, tt := range tests {
		m := matcher(t, dialect.Ksh, tt.pattern)
		for _, s := range tt.yes {
			if _, ok, err := m.Whole(s); !ok || err != nil {
				t.Errorf("%q should match %q (err %v)", tt.pattern, s, err)
			}
		}
		for _, s := range tt.no {
			if _, ok, err := m.Whole(s); ok || err != nil {
				t.Errorf("%q should not match %q (err %v)", tt.pattern, s, err)
			}
		}
	}
	if _, err := pattern(t, dialect.Ksh, "~(P)a+").Matcher(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a perl pattern: got %v, want ErrUnsupported", err)
	}
	m := matcher(t, dialect.Ksh, "*-(a)")
	if _, err := m.Replace("aaab", Anywhere, false, func(string, Match) string { return "X" }); !errors.Is(err, ErrUnsupported) {
		t.Errorf("replacing with a lazy group: got %v, want ErrUnsupported", err)
	}
	var docs []string
	for _, piece := range pattern(t, dialect.Ksh, "~(Ei)^a+").Pieces("~(Ei)^a+") {
		docs = append(docs, piece.Doc)
	}
	if want := "ksh93 pattern options: `E` the rest is a POSIX extended regular expression; `i` letters match in either case; found anywhere in the rest of the value: the start of the value, then one or more of `a`"; strings.Join(docs, "|") != want {
		t.Errorf("ksh docs = %q", docs)
	}
}
