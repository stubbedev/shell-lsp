package hover

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestHoverPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dialect dialect.Dialect
		src     string
		needle  string
		want    []string
		absent  []string
		span    string
	}{
		{
			dialect: dialect.Zsh,
			src:     "setopt extendedglob\nline='k1=v1 k2=v2'\nprint ${line//(#b)([^= ]##)=([^ ]#)/$match[2]:$match[1]}\n",
			needle:  "(#b)",
			want: []string{
				"**Pattern**: every match, each as long as possible, is replaced.",
				"- `([^= ]##)`: one or more of `[^= ]` (one character other than `=` or a space); captured in `$match[1]`",
				"**Result**: `v1:k1 v2:k2`",
				"Given `line` = `k1=v1 k2=v2` (line 2).",
			},
			absent: []string{"needs `setopt extendedglob`"},
			span:   "${line//(#b)([^= ]##)=([^ ]#)/$match[2]:$match[1]}",
		},
		{
			dialect: dialect.Zsh,
			src:     "print ${line//(#b)(*)/x}\n",
			needle:  "(#b)",
			want:    []string{"`(#b)` needs `setopt extendedglob`; this file never enables it"},
			absent:  []string{"**Result**"},
			span:    "${line//(#b)(*)/x}",
		},
		{
			dialect: dialect.Zsh,
			src:     "f=archive.tar.gz\nprint ${f:t:r}\n",
			needle:  ":t",
			want:    []string{"`:t`: Tail", "**Result**: `archive.tar`"},
			span:    "${f:t:r}",
		},
		{
			dialect: dialect.Bash,
			src:     "f=archive.tar.gz\necho \"${f%%.*}\"\n",
			needle:  "%%",
			want:    []string{"the longest suffix that matches is removed", "- `*`: any text, including none", "**Result**: `archive`"},
			span:    "${f%%.*}",
		},
		{
			dialect: dialect.Bash,
			src:     "a=(one two three)\necho \"${a[@]/o/0}\"\n",
			needle:  "/o",
			want:    []string{"**Result**: `(0ne tw0 three)`", "Given `a` = `(one two three)` (line 1)."},
			span:    "${a[@]/o/0}",
		},
		{
			dialect: dialect.Bash,
			src:     "echo \"${f%%.*}\"\n",
			needle:  "%%",
			want:    []string{"the longest suffix that matches is removed"},
			absent:  []string{"**Result**"},
			span:    "${f%%.*}",
		},
		{
			dialect: dialect.Zsh,
			src:     "for f in **/*.go(.om[1,5]N); do :; done\n",
			needle:  "om",
			want: []string{
				"expands to the names of the existing files that match",
				"- `**/`: any number of directories, recursively",
				"`om`: sort ascending by modification time",
				"`[1,5]`: only matches 1 to 5 after sorting",
				"`N`: Null glob",
			},
			span: "**/*.go(.om[1,5]N)",
		},
		{
			dialect: dialect.Zsh,
			src:     "setopt extendedglob\nv=Release-2.4\n[[ $v == (#i)release-<->.<-> ]] && print yes\n",
			needle:  "<->.",
			want:    []string{"**Result**: `Release-2.4` matches.", "Given `v` = `Release-2.4` (line 2)."},
			span:    "(#i)release-<->.<->",
		},
		{
			dialect: dialect.Bash,
			src:     "v=main\n[[ $v != feature/* ]]\n",
			needle:  "feature",
			want:    []string{"**Result**: `main` does not match, so the test is true."},
			span:    "feature/*",
		},
		{
			dialect: dialect.Bash,
			src:     "v=x.tar.gz\ncase $v in\n  *.zip) ;;\n  *.tar.*) ;;\nesac\n",
			needle:  "*.zip",
			want:    []string{"**Result**: `x.tar.gz` does not match."},
			span:    "*.zip",
		},
		{
			dialect: dialect.Bash,
			src:     "v=v1.22\nif [[ $v =~ ^v([0-9]+)\\.([0-9]+)$ ]]; then echo ok; fi\n",
			needle:  "([0-9]+)",
			want: []string{
				"a POSIX extended regular expression, searched for anywhere in the left-hand side",
				"- `([0-9]+)`: one or more of `[0-9]` (one character: `0` to `9`); captured in `${BASH_REMATCH[1]}`",
				"- `\\.`: the text `.`",
				"**Result**: `v1.22` matches. `BASH_REMATCH` is `(v1.22 1 22)`.",
			},
			span: "^v([0-9]+)\\.([0-9]+)$",
		},
		{
			dialect: dialect.Zsh,
			src:     "v=v1.22\n[[ $v =~ 'v([0-9]+)' ]] && print $match[1]\n",
			needle:  "[0-9]",
			want:    []string{"captured in `$match[1]`", "`$MATCH` is `v1` and `$match` is `(1)`."},
			span:    "'v([0-9]+)'",
		},
		{
			dialect: dialect.Ksh,
			src:     "v=Release\n[[ $v == ~(i)release ]] && print ok\n",
			needle:  "release",
			want:    []string{"ksh93 pattern options: `i` letters match in either case, for the rest of the pattern", "**Result**: `Release` matches."},
			span:    "~(i)release",
		},
		{
			dialect: dialect.Zsh,
			src:     "print -l *.c\n",
			needle:  "*.c",
			want:    []string{"zsh stops with \"no matches found\"; the `(N)` qualifier"},
			span:    "*.c",
		},
		{
			dialect: dialect.Bash,
			src:     "shopt -s nullglob nocaseglob\nls *.c\n",
			needle:  "*.c",
			want:    []string{"expands to nothing (`nullglob` is set in this file). This file sets `nocaseglob`"},
			span:    "*.c",
		},
		{
			dialect: dialect.Bash,
			src:     "shopt -s nocasematch\n[[ $x == y* ]]\n",
			needle:  "y*",
			want:    []string{"This file sets `nocasematch`, so letters match in either case."},
			span:    "y*",
		},
		{
			dialect: dialect.Bash,
			src:     "case $x in @(a|b)*.[ch]) ;; esac\n",
			needle:  "[ch]",
			want:    []string{"matched against the whole `case` word", "- `@(a|b)`: either `a` or `b`", "- `[ch]`: one character: `c` or `h`"},
			span:    "@(a|b)*.[ch]",
		},
		{
			dialect: dialect.Bash,
			src:     "[[ $x == +([0-9]) ]]\n",
			needle:  "[0-9]",
			want:    []string{"matched against the whole left-hand side", "one or more repetitions of one character: `0` to `9`"},
			absent:  []string{"needs `shopt -s extglob`"},
			span:    "+([0-9])",
		},
		{
			dialect: dialect.Bash,
			src:     "rm -- @(a|b).log\n",
			needle:  "@(",
			want:    []string{"`@(a|b)` needs `shopt -s extglob`"},
			span:    "@(a|b).log",
		},
		{
			dialect: dialect.Zsh,
			src:     "[[ $x == <1-10>~5 ]]\n",
			needle:  "<1",
			want:    []string{"a number from 1 to 10, except what matches `5`", "`~` needs `setopt extendedglob`"},
			span:    "<1-10>~5",
		},
		{
			dialect: dialect.Zsh,
			src:     "#compdef foo\n[[ $x == ^*.o ]]\n",
			needle:  "*.o",
			want:    []string{"anything that does not match `*.o`"},
			absent:  []string{"needs `setopt extendedglob`"},
			span:    "^*.o",
		},
		{
			dialect: dialect.Zsh,
			src:     "a=(foo.c bar.o)\nprint ${(M)a:#*.c}\n",
			needle:  "*.c",
			want:    []string{"`M`: With `:#`, keep the elements that match", "**Result**: `(foo.c)`"},
			span:    "${(M)a:#*.c}",
		},
	}
	for _, tc := range tests {
		got := hoverAt(t, tc.src, tc.dialect, tc.needle, 0)
		for _, want := range tc.want {
			if !strings.Contains(got.Markdown, want) {
				t.Errorf("hover %q in %q: markdown lacks %q:\n%s", tc.needle, tc.src, want, got.Markdown)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(got.Markdown, absent) {
				t.Errorf("hover %q in %q: markdown has %q:\n%s", tc.needle, tc.src, absent, got.Markdown)
			}
		}
		if span := tc.src[got.Span.Start:got.Span.End]; span != tc.span {
			t.Errorf("hover %q in %q: span = %q, want %q", tc.needle, tc.src, span, tc.span)
		}
	}
}

func TestHoverShowsNoResultForAPlainParameter(t *testing.T) {
	t.Parallel()

	got := hoverAt(t, "f=a.b\necho \"$f\"\n", dialect.Bash, "$f", 0)
	if strings.Contains(got.Markdown, "**Result**") {
		t.Errorf("plain $f hover shows a result:\n%s", got.Markdown)
	}
}

func TestHoverPlainArgumentsAreNotPatterns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ src, needle string }{
		{"echo '*.c'\n", "*.c"},
		{"echo \"$x\"\n", "x"},
		{"[ -f x ]\n", "]"},
		{"noglob echo *.c\n", "*.c"},
	} {
		got, _, err := Hover(t.Context(), Input{Tree: parser.Parse(tc.src, dialect.Zsh), Offset: strings.Index(tc.src, tc.needle)})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got.Markdown, "**Pattern**") {
			t.Errorf("%q: hover explains a pattern:\n%s", tc.src, got.Markdown)
		}
	}
}
