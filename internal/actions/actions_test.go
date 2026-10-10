package actions

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func selection(t *testing.T, src string) (string, parser.Span) {
	t.Helper()

	start := strings.Index(src, "«")
	end := strings.Index(src, "»")
	if start < 0 || end < start {
		t.Fatalf("source %q lacks a «selection»", src)
	}
	text := src[:start] + src[start+len("«"):end] + src[end+len("»"):]
	return text, parser.Span{Start: start, End: end - len("«")}
}

func refactor(t *testing.T, kind RefactorKind, src string) (string, string, bool) {
	t.Helper()

	text, sel := selection(t, src)
	edits, title, ok := Compute(parser.Parse(text, dialect.Bash), Refactor{Kind: kind, Start: sel.Start, End: sel.End})
	if !ok {
		return "", "", false
	}
	return textedit.Apply(text, edits), title, true
}

func TestExtractVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src, want string
	}{
		{src: "if true; then\n\tcp «/etc/hosts» /tmp\nfi\n", want: "if true; then\n\tvalue=/etc/hosts\n\tcp \"$value\" /tmp\nfi\n"},
		{src: "echo \"hello «$USER» there\"\n", want: "value=\"$USER\"\necho \"hello ${value} there\"\n"},
		{src: "ls «/var»/log\n", want: "value=/var\nls \"${value}\"/log\n"},
	}
	for _, tc := range tests {
		got, title, ok := refactor(t, ExtractVariable, tc.src)
		if !ok || got != tc.want || title != "Extract to variable value" {
			t.Errorf("extract %q = %q (%q, %t), want %q", tc.src, got, title, ok, tc.want)
		}
	}
	for _, src := range []string{"echo a«b c»d\n", "for f in *; do echo «$f»; done\n", "echo «a\nb»\n", "echo \"«$x\" \"$y»\"\n", "cat <<EOF\nbody\n«EOF»\n", "cat <<«EOF»\nbody\nEOF\n", "cat <<EOF\n«body»\nEOF\n"} {
		if got, _, ok := refactor(t, ExtractVariable, src); ok {
			t.Errorf("extract %q offered: %q", src, got)
		}
	}
	got, title, _ := refactor(t, ExtractVariable, "value=1\necho «x»\n")
	if title != "Extract to variable value2" || !strings.Contains(got, "value2=x") {
		t.Errorf("name collision handling = %q (%q)", got, title)
	}
}

func TestExtractFunction(t *testing.T) {
	t.Parallel()

	src := "main() {\n\techo start\n«\tmkdir -p \"$1\"\n\tcd \"$1\" || exit»\n\techo done\n}\n"
	got, title, ok := refactor(t, ExtractFunction, src)
	want := "extracted() {\n\tmkdir -p \"$1\"\n\tcd \"$1\" || exit\n}\n\nmain() {\n\techo start\n\textracted \"$@\"\n\techo done\n}\n"
	if !ok || got != want || title != "Extract to function extracted" {
		t.Errorf("extract function = %q (%t)\nwant %q", got, ok, want)
	}
	for _, src := range []string{
		"f() {\n«\techo a\n\treturn 1»\n}\n",
		"echo «a\necho» b\n",
		"if true; then\n«\techo a\nfi»\n",
	} {
		if got, _, ok := refactor(t, ExtractFunction, src); ok {
			t.Errorf("extract function %q offered: %q", src, got)
		}
	}
	top, _, ok := refactor(t, ExtractFunction, "«echo a\necho b»\necho c\n")
	if !ok || top != "extracted() {\n\techo a\n\techo b\n}\n\nextracted\necho c\n" {
		t.Errorf("top-level extract = %q", top)
	}
}

func TestToggleQuotes(t *testing.T) {
	t.Parallel()

	got, title, ok := refactor(t, ToggleQuotes, "echo 'a \"b\" «$c»'\n")
	if !ok || got != "echo \"a \\\"b\\\" \\$c\"\n" || title != "Convert to double quotes" {
		t.Errorf("single to double = %q (%q)", got, title)
	}
	got, _, ok = refactor(t, ToggleQuotes, "echo \"it's «a» \\$1\"\n")
	if !ok || got != "echo 'it'\\''s a $1'\n" {
		t.Errorf("double to single = %q", got)
	}
	if got, _, ok := refactor(t, ToggleQuotes, "echo \"«$HOME»\"\n"); ok {
		t.Errorf("double quotes with expansion converted: %q", got)
	}
}

func TestDeclareLocal(t *testing.T) {
	t.Parallel()

	got, title, ok := refactor(t, DeclareLocal, "f() {\n\t«count»=1\n}\n")
	if !ok || got != "f() {\n\tlocal count=1\n}\n" || title != "Declare count as local" {
		t.Errorf("declare local = %q (%q)", got, title)
	}
	for _, src := range []string{"«x»=1\n", "f() {\n\tlocal «x»=1\n}\n", "f() {\n\t«y»=1 cmd\n}\n"} {
		if got, _, ok := refactor(t, DeclareLocal, src); ok {
			t.Errorf("declare local %q offered: %q", src, got)
		}
	}
}

func TestQuickFixes(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\necho $x\n"
	tree := parser.Parse(text, dialect.Bash)
	cd := strings.Index(text, "cd /tmp")
	echo := strings.Index(text, "$x")
	diags := []Diagnostic{
		{Index: 0, Code: "SC2164", Message: "Use cd ... || exit", Span: parser.Span{Start: cd, End: cd + 7}},
		{Index: 1, Code: "SC2086", Message: "Double quote", Span: parser.Span{Start: echo, End: echo + 2}, Fix: []textedit.Edit{{Start: echo, End: echo, NewText: `"`}, {Start: echo + 2, End: echo + 2, NewText: `"`}}},
	}
	byTitle := map[string]Action{}
	for _, a := range Actions(Input{Tree: tree, Selection: parser.Span{Start: cd, End: cd}, Diagnostics: diags, AllDiagnostics: diags}) {
		byTitle[a.Title] = a
	}
	expect := map[string]string{
		"Exit when cd fails":             "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp || exit\n}\necho $x\n",
		"Ignore SC2164 for this command": "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034,SC2164\n\tcd /tmp\n}\necho $x\n",
		"Ignore SC2164 in this file":     "#!/bin/bash\n# shellcheck disable=SC2164\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\necho $x\n",
		"Fix SC2086: Double quote":       "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\necho \"$x\"\n",
		"Ignore SC2086 for this command": "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\n# shellcheck disable=SC2086\necho $x\n",
		"Apply all automatic fixes":      "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\necho \"$x\"\n",
	}
	for title, want := range expect {
		a, ok := byTitle[title]
		if !ok {
			t.Errorf("missing action %q (have %v)", title, keys(byTitle))
			continue
		}
		if got := textedit.Apply(text, a.Edits); got != want {
			t.Errorf("%s:\n got %q\nwant %q", title, got, want)
		}
	}
	if a := byTitle["Fix SC2086: Double quote"]; !a.Preferred || a.Kind != QuickFix {
		t.Errorf("lint fix = %+v", a)
	}
	if a := byTitle["Apply all automatic fixes"]; a.Kind != SourceFixAll {
		t.Errorf("fix all kind = %q", a.Kind)
	}
}

func TestDisableForFileWithoutShebang(t *testing.T) {
	t.Parallel()

	edit := disableForFile(parser.Parse("echo $x", dialect.Bash), "SC2086")
	if got := textedit.Apply("echo $x", []textedit.Edit{edit}); got != "# shellcheck disable=SC2086\necho $x" {
		t.Errorf("disable for file = %q", got)
	}
}

func keys(m map[string]Action) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
