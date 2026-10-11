package regex

import (
	"strings"
	"testing"
)

var flavorNames = map[Flavor]string{Basic: "basic", Extended: "extended", Awk: "awk", Oniguruma: "oniguruma"}

var validPatterns = map[Flavor][]string{
	Basic: {
		``, `abc`, `^a.*b$`, `*a`, `\(*a\)`, `^*`, `a\{2\}`, `a\{1,3\}`, `a\{2,\}`, `\(a\)\(b\)\2\1`,
		`[]a]`, `[^]a]`, `[a-z0-9]`, `[[:alpha:][:digit:]]`, `[[.hyphen.]]`, `[[=e=]]`, `[\]`, `a\|b`, `a\+\?`,
		`(a)`, `a)`, `a{3,2}`, `x{`, `[a-]`, `[-z]`, `[a-Z]`, `\<word\>`, `\n\t`, `[[:alpha]`, `\.\*`,
		`[ \t]*$`, `^[^#]*#`, `.\{1,78\}`, `\(x\)`, `[/]`, `a**`,
	},
	Extended: {
		``, `(a|b)+`, `^(foo|bar)?$`, `a{2,3}`, `a{,3}`, `a{x}`, `a{`, `a)`, `(a)\1`, `[(]`, `\(`, `^*`,
		`[[:space:]]+`, `a+?`, `a**`, `x|y|z`, `(^a)`, `$*`, `[a-z]{1,2}`,
	},
	Awk: {
		``, `^[0-9]+$`, `+`, `*x`, `(a|b)`, `a)`, `[\]]`, `[\/]`, `[^\n]`, `a{3,2}`, `\1`, `[]a]`, `/`, `\/`,
		`[[:upper:]]`, `\.(gz|bz2)$`, `\(`,
	},
	Oniguruma: {
		``, `^\d+$`, `(?<year>\d{4})-(?<month>\d\d)`, `(?i)abc`, `(?:a|b)*`, `a{,3}`, `a{x}`, `[\]]`, `[a-z&&[^aeiou]]`,
		`\p{Alpha}+`, `(?#comment (`, `\Qa(b\E`, `[[:word:]]`, `\\`, `a*?`, `\x{41}`, `^*`, `[^]a]`,
	},
}

func TestValidPatterns(t *testing.T) {
	t.Parallel()

	for flavor, patterns := range validPatterns {
		for _, pattern := range patterns {
			if _, err := Check(pattern, flavor); err != nil {
				t.Errorf("Check(%q, %s) = %q at %d, want valid", pattern, flavorNames[flavor], err.Message, err.Start)
			}
		}
	}
}

func TestInvalidPatterns(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		flavor  Flavor
		pattern string
		at      string
		width   int
		message string
	}{
		{Basic, `\(a`, `\(`, 2, "`\\(` is never closed in the regular expression"},
		{Basic, `\(a\)\(b`, `\(b`, 2, "`\\(` is never closed in the regular expression"},
		{Basic, `a\)`, `\)`, 2, "`\\)` has no matching opening parenthesis in the regular expression"},
		{Basic, `[abc`, `[`, 1, "`[` is never closed in the regular expression"},
		{Basic, `[]`, `[`, 1, "`[` is never closed in the regular expression"},
		{Basic, `[[:alpah:]]`, `[:`, 9, "unknown character class `[:alpah:]`"},
		{Basic, `[z-a]`, `z-a`, 3, "the range `z-a` is empty because `z` sorts after `a`"},
		{Basic, `[0-9Z-A]`, `Z-A`, 3, "the range `Z-A` is empty because `Z` sorts after `A`"},
		{Basic, `a\{3,2\}`, `\{`, 7, "the interval `\\{3,2\\}` has a minimum above its maximum"},
		{Basic, `a\{x\}`, `\{`, 5, "invalid interval `\\{x\\}`: it must hold a count such as 2, 1,3 or 2,"},
		{Basic, `a\{2`, `\{`, 2, "`\\{` is never closed in the regular expression"},
		{Basic, `\(a\)\2`, `\2`, 2, "`\\2` refers to group 2, but the regular expression has only 1 group"},
		{Basic, `a\1`, `\1`, 2, "`\\1` refers to group 1, but the regular expression has no groups"},
		{Extended, `(a|b`, `(`, 1, "`(` is never closed in the regular expression"},
		{Extended, `*a`, `*`, 1, "`*` has nothing to repeat"},
		{Extended, `a|+b`, `+`, 1, "`+` has nothing to repeat"},
		{Extended, `(?a)`, `?`, 1, "`?` has nothing to repeat"},
		{Extended, `a{3,2}`, `{`, 5, "the interval `{3,2}` has a minimum above its maximum"},
		{Extended, `(a)(b)\3`, `\3`, 2, "`\\3` refers to group 3, but the regular expression has only 2 groups"},
		{Extended, `[[:foo:]]`, `[:`, 7, "unknown character class `[:foo:]`"},
		{Awk, `(a`, `(`, 1, "`(` is never closed in the regular expression"},
		{Awk, `[a`, `[`, 1, "`[` is never closed in the regular expression"},
		{Awk, `[9-0]`, `9-0`, 3, "the range `9-0` is empty because `9` sorts after `0`"},
		{Awk, `ab\`, `\`, 1, "the regular expression ends with a lone `\\`"},
		{Oniguruma, `(a`, `(`, 1, "`(` is never closed in the regular expression"},
		{Oniguruma, `a)`, `)`, 1, "`)` has no matching opening parenthesis in the regular expression"},
		{Oniguruma, `*a`, `*`, 1, "`*` has nothing to repeat"},
		{Oniguruma, `[a-z`, `[`, 1, "`[` is never closed in the regular expression"},
		{Oniguruma, `[z-a]`, `z-a`, 3, "the range `z-a` is empty because `z` sorts after `a`"},
		{Oniguruma, `a{3,2}`, `{`, 5, "the interval `{3,2}` has a minimum above its maximum"},
		{Oniguruma, `a\`, `\`, 1, "the regular expression ends with a lone `\\`"},
	} {
		_, err := Check(tt.pattern, tt.flavor)
		at := strings.LastIndex(tt.pattern, tt.at)
		if err == nil || err.Message != tt.message || err.Start != at || err.End != at+tt.width {
			t.Errorf("Check(%q, %s) = %+v, want %q at %d-%d", tt.pattern, flavorNames[tt.flavor], err, tt.message, at, at+tt.width)
		}
	}
}

func TestAmbiguousAwkBracketsAreNotJudged(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{`[\](`, `[a\]b](`} {
		if p, err := Check(pattern, Awk); err != nil {
			t.Errorf("Check(%q, awk) = %q, want no verdict", pattern, err.Message)
		} else if _, known := p.Groups(); known {
			t.Errorf("Check(%q, awk) knows its groups, want unknown", pattern)
		}
	}
}

func TestGroups(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		pattern string
		flavor  Flavor
		groups  int
		known   bool
	}{
		{`\(a\)\(b\)`, Basic, 2, true},
		{`(a)(b)`, Basic, 0, true},
		{`(a)([(])`, Extended, 2, true},
		{`(a)\(`, Extended, 1, true},
		{`(?<n>a)`, Oniguruma, 0, true},
		{`\Q(\E`, Oniguruma, 0, false},
	} {
		p, err := Check(tt.pattern, tt.flavor)
		if err != nil {
			t.Fatalf("Check(%q) = %q", tt.pattern, err.Message)
		}
		if groups, known := p.Groups(); groups != tt.groups || known != tt.known {
			t.Errorf("Check(%q, %s).Groups() = %d, %t, want %d, %t", tt.pattern, flavorNames[tt.flavor], groups, known, tt.groups, tt.known)
		}
	}
}

func FuzzCheck(f *testing.F) {
	for _, patterns := range validPatterns {
		for _, pattern := range patterns {
			f.Add(pattern)
		}
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		for flavor := range flavorNames {
			if _, err := Check(pattern, flavor); err != nil && (err.Start < 0 || err.Start >= err.End || err.End > len(pattern) || err.Message == "") {
				t.Fatalf("Check(%q, %s) = %+v out of bounds", pattern, flavorNames[flavor], err)
			}
		}
	})
}
