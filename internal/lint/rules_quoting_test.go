package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestQuoteSafeVariables(t *testing.T) {
	t.Parallel()

	src := "region=eu-west-1\necho $region\ndirty=$(date)\necho $dirty\n"
	got := findingsWith(t, src, 2248, "quote-safe-variables")
	if len(got) != 1 {
		t.Fatalf("SC2248 findings = %+v, want one for the clean variable", got)
	}
	if fixed, want := textedit.Apply(src, fixEdits(got[0])), "region=eu-west-1\necho \"$region\"\ndirty=$(date)\necho $dirty\n"; fixed != want {
		t.Errorf("fixed = %q, want %q", fixed, want)
	}
	if off := findingsWith(t, src, 2248); len(off) != 0 {
		t.Errorf("SC2248 reported without being enabled: %+v", off)
	}
	bats := "#!/usr/bin/env bats\n@test \"ok\" {\n  run true\n  [ $status -eq 0 ]\n}\n"
	if got := findingsWith(t, bats, 2248, "quote-safe-variables"); len(got) != 1 {
		t.Errorf("SC2248 on the Bats status = %+v, want one finding", got)
	}
}

func TestCommandSubstitutionMessagesFollowTheDialect(t *testing.T) {
	t.Parallel()

	src := "print -r -- $(ls)\nfiles=( $(ls) )\nprint -r -- $files\n"
	for _, tc := range []struct {
		d    dialect.Dialect
		src  string
		want []string
	}{
		{dialect.Zsh, src, []string{
			"The output of this command substitution is split into words; quote it.",
			`Command output is split on whitespace when building this array; split it explicitly instead, such as into lines with ${(f)"$(...)"}.`,
		}},
		{dialect.Zsh, "setopt glob_subst\n" + src, []string{
			"The output of this command substitution is split into words and globbed; quote it.",
			`Command output is split on whitespace and globbed when building this array; split it explicitly instead, such as into lines with ${(f)"$(...)"}.`,
		}},
		{dialect.Bash, "echo $(ls)\nfiles=( $(ls) )\necho \"${files[@]}\"\n", []string{
			"The output of this command substitution is split into words and globbed; quote it.",
			"Command output is split on whitespace and globbed when building this array; read it with mapfile or read -a instead.",
		}},
	} {
		var got []string
		for _, f := range Check(parser.Parse(tc.src, tc.d), nil, Options{}) {
			if f.Code == 2046 || f.Code == 2207 {
				got = append(got, f.Message)
			}
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s %q:\ngot  %q\nwant %q", tc.d, tc.src, got, tc.want)
		}
	}
}
