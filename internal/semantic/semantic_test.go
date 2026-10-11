package semantic

import (
	"cmp"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/structure"
)

func describe(text string, tokens []Token) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		var mods []string
		for bit, name := range ModifierNames {
			if t.Modifiers&(1<<bit) != 0 {
				mods = append(mods, name)
			}
		}
		out[i] = fmt.Sprintf("%s:%s%s", text[t.Span.Start:t.Span.End], TypeNames[t.Type], strings.Join(append([]string{""}, mods...), "+"))
	}
	return out
}

func TestTokens(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\nreadonly MAX=3\ngreet() {\n\tlocal who=\"$1\"\n\techo \"hi $who\" 'raw' >&2\n}\nif (( MAX > 2 )); then greet -v \"$HOME\" | grep x; fi\ncat <<EOF\nbody $MAX\nEOF\n"
	got := describe(text, Tokens(parser.Parse(text, dialect.Bash), nil, nil))
	want := []string{
		"#!/bin/bash:comment",
		"readonly:function+defaultLibrary",
		"MAX:variable+declaration+readonly",
		"greet:function+declaration",
		"local:function+defaultLibrary",
		"who:variable+declaration",
		"\":string",
		"1:parameter",
		"\":string",
		"echo:function+defaultLibrary",
		"\":string",
		"hi :string",
		"who:variable",
		"\":string",
		"'raw':string",
		">&:operator",
		"if:keyword",
		"MAX:variable+readonly",
		">:operator",
		"2:number",
		"then:keyword",
		"greet:function",
		"-v:property",
		"\":string",
		"HOME:variable+defaultLibrary",
		"\":string",
		"|:operator",
		"grep:function+external",
		"fi:keyword",
		"cat:function+external",
		"<<:operator",
		"EOF:string",
		"body :string",
		"MAX:variable+readonly",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tokens:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEmbeddedProgramTokensReplaceTheirString(t *testing.T) {
	t.Parallel()

	text := "awk '{ print $1 }' f\njq \".a | length\" x\nsed -f - <<'EOF'\ns/a/b/\nEOF\n"
	tree := parser.Parse(text, dialect.Bash)
	got := describe(text, Tokens(tree, nil, embedded.Find(tree)))
	want := []string{
		"awk:function+external", "':string", "print:keyword", "$:operator", "1:number", "':string",
		"jq:function+external", "\":string", ".a:property", "|:operator", "length:function+defaultLibrary", "\":string",
		"sed:function+external", "-f:property", "<<:operator", "'EOF':string",
		"s:keyword+defaultLibrary", "/a/:regexp", "b/:string",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tokens:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEmbeddedTokensAcrossEscapesAndScripts(t *testing.T) {
	t.Parallel()

	text := "awk \"{ print \\\"a\\\" }\"\nsed -e 'a\\' -e 'appended' f\n"
	tree := parser.Parse(text, dialect.Bash)
	got := describe(text, Tokens(tree, nil, embedded.Find(tree)))
	want := []string{
		"awk:function+external", "\":string", "print:keyword", "\\\"a\\\":string", "\":string",
		"sed:function+external", "-e:property", "':string", "a:keyword+defaultLibrary", "':string",
		"-e:property", "':string", "appended:string", "':string",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tokens:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTokensAreSingleLineAndOrdered(t *testing.T) {
	t.Parallel()

	text := "echo 'multi\nline\nstring' \"a\nb\"\n"
	tokens := Tokens(parser.Parse(text, dialect.Bash), nil, nil)
	last := -1
	for _, tok := range tokens {
		if strings.Contains(text[tok.Span.Start:tok.Span.End], "\n") {
			t.Errorf("token %q spans lines", text[tok.Span.Start:tok.Span.End])
		}
		if tok.Span.Start < last {
			t.Errorf("tokens overlap or are unordered at %d", tok.Span.Start)
		}
		last = tok.Span.End
	}
	if got := describe(text, tokens); len(got) < 5 {
		t.Errorf("multi-line string tokens = %v", got)
	}
}

func TestDirectiveTokens(t *testing.T) {
	t.Parallel()

	text := "  # shellcheck disable=SC2086,SC2034 source=lib.sh # quoting is deliberate\necho $1 # just a comment\n"
	var got []string
	for _, d := range describe(text, Tokens(parser.Parse(text, dialect.Bash), nil, nil)) {
		if !strings.HasPrefix(d, "echo") && !strings.HasPrefix(d, "1:") {
			got = append(got, d)
		}
	}
	want := []string{
		"# :comment",
		"shellcheck:keyword",
		"disable:property",
		"=:operator",
		"SC2086,SC2034:string",
		"source:property",
		"=:operator",
		"lib.sh:string",
		" # quoting is deliberate:comment",
		"# just a comment:comment",
	}
	if !slices.Equal(got, want) {
		t.Errorf("tokens:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestNumbersInZshSubscriptFlagsAreNumbers(t *testing.T) {
	t.Parallel()

	text := "echo ${words[(w)42]}\n"
	got := strings.Join(describe(text, Tokens(parser.Parse(text, dialect.Zsh), nil, nil)), " ")
	if want := "42:number"; !strings.Contains(got, want) {
		t.Errorf("tokens = %s, want %s", got, want)
	}
}

func TestPatternsInZshSubscriptFlagsAreNotNumbers(t *testing.T) {
	t.Parallel()

	text := "echo ${items[(r)42]}\n"
	got := strings.Join(describe(text, Tokens(parser.Parse(text, dialect.Zsh), nil, nil)), " ")
	if strings.Contains(got, "42:number") {
		t.Errorf("tokens = %s, want the pattern 42 not to be a number", got)
	}
}

func TestZshAndKsh93KeywordsAreKeywords(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		d    dialect.Dialect
		text string
		want []string
	}{
		{dialect.Zsh, "repeat 2 { :; }\nforeach f (a b)\n:\nend\n{ : } always { : }\nif [[ -n $1 ]] { : }\n", []string{"repeat", "foreach", "end", "always", "if", "[[", "]]"}},
		{dialect.Ksh, "#!/bin/ksh\nnamespace geo { :; }\nfunction .sh.math.f x { :; }\n", []string{"namespace", "function"}},
	} {
		var keywords []string
		tree := parser.Parse(tc.text, tc.d)
		for _, tok := range Tokens(tree, nil, nil) {
			if TypeNames[tok.Type] == "keyword" {
				keywords = append(keywords, tc.text[tok.Span.Start:tok.Span.End])
			}
		}
		if !slices.Equal(keywords, tc.want) {
			t.Errorf("%s keywords in %q = %q, want %q", tc.d, tc.text, keywords, tc.want)
		}
	}
}

func TestTokensUseImports(t *testing.T) {
	t.Parallel()

	text := "test\necho \"$LIMIT\"\n"
	tree := parser.Parse(text, dialect.Bash)
	lib := parser.Import{Name: "lib.sh", Tree: parser.Parse("readonly LIMIT=3\ntest() { :; }\n", dialect.Bash)}
	without := strings.Join(describe(text, Tokens(tree, nil, nil)), " ")
	with := strings.Join(describe(text, Tokens(tree, []parser.Import{lib}, nil)), " ")
	if want := "test:function+defaultLibrary"; !strings.Contains(without, want) {
		t.Errorf("tokens without imports = %s, want %s", without, want)
	}
	for _, want := range []string{"test:function ", "LIMIT:variable+readonly"} {
		if !strings.Contains(with+" ", want) {
			t.Errorf("tokens with imports = %s, want %s", with, want)
		}
	}
}

func TestSelectionPath(t *testing.T) {
	t.Parallel()

	text := "if true; then\n\techo \"$HOME/x\" | grep y\nfi\n"
	offset := strings.Index(text, "HOME")
	var got []string
	for _, s := range structure.SelectionPath(parser.Parse(text, dialect.Bash), offset) {
		got = append(got, text[s.Start:s.End])
	}
	want := []string{"HOME", "$HOME", "\"$HOME/x\"", "echo \"$HOME/x\"", "echo \"$HOME/x\" | grep y", "if true; then\n\techo \"$HOME/x\" | grep y\nfi", text}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("selection path:\n%q\nwant\n%q", got, want)
	}
}

func TestOrderByStartIsAStableSortByStart(t *testing.T) {
	t.Parallel()

	for seed, maxStart := range []int{0, 1, 255, 256, 70000, 1 << 24} {
		rng := rand.New(rand.NewPCG(uint64(seed), 7))
		tokens := make([]Token, 5000)
		for i := range tokens {
			tokens[i].Span.Start = rng.IntN(maxStart + 1)
		}
		want := make([]int, len(tokens))
		for i := range want {
			want[i] = i
		}
		slices.SortStableFunc(want, func(a, b int) int { return cmp.Compare(tokens[a].Span.Start, tokens[b].Span.Start) })
		got := orderByStart(tokens, maxStart)
		for i, key := range got {
			if int(uint32(key)) != want[i] {
				t.Fatalf("max start %d: position %d holds token %d, want %d", maxStart, i, uint32(key), want[i])
			}
		}
	}
}

func TestResultKeepsTheFirstOfOverlappingTokensInStartOrder(t *testing.T) {
	t.Parallel()

	for seed, maxStart := range []int{0, 1, 255, 256, 70000, 1 << 24} {
		rng := rand.New(rand.NewPCG(uint64(seed), 11))
		tokens := make([]Token, 5000)
		for i := range tokens {
			start := rng.IntN(maxStart + 1)
			tokens[i] = Token{Span: parser.Span{Start: start, End: start + rng.IntN(3)}, Type: Type(i % 7)}
		}
		order := make([]int, len(tokens))
		for i := range order {
			order[i] = i
		}
		slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(tokens[a].Span.Start, tokens[b].Span.Start) })
		var want []Token
		end := -1
		for _, i := range order {
			if tokens[i].Span.Start >= end {
				want = append(want, tokens[i])
				end = tokens[i].Span.End
			}
		}
		g := generator{tokens: slices.Clone(tokens), text: strings.Repeat(" ", maxStart+3)}
		if got := g.result(); !slices.Equal(got, want) {
			t.Fatalf("max start %d: result differs from a stable sort by start that drops overlaps", maxStart)
		}
	}
}
