package actions

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/format"
	"github.com/stubbedev/shell-lsp/internal/lint"
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

	return refactorIn(t, dialect.Bash, kind, src)
}

func refactorIn(t *testing.T, d dialect.Dialect, kind RefactorKind, src string) (string, string, bool) {
	t.Helper()

	text, sel := selection(t, src)
	change, ok := Compute(Document{Tree: parser.Parse(text, d)}, Refactor{Kind: kind, Start: sel.Start, End: sel.End})
	if !ok {
		return "", "", false
	}
	return textedit.Apply(text, change.Edits), change.Title, true
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
	for _, src := range []string{"(( «n» += 1 ))\n", "echo $(( «n» * 2 ))\n", "echo a«b c»d\n", "for f in *; do echo «$f»; done\n", "echo «a\nb»\n", "echo \"«$x\" \"$y»\"\n", "cat <<EOF\nbody\n«EOF»\n", "cat <<«EOF»\nbody\nEOF\n", "cat <<EOF\n«body»\nEOF\n"} {
		if got, _, ok := refactor(t, ExtractVariable, src); ok {
			t.Errorf("extract %q offered: %q", src, got)
		}
	}
	for _, tc := range []struct {
		d   dialect.Dialect
		src string
	}{
		{dialect.Bash, "[[ $x =~ ^f«(ir)s»t$ ]]\n"},
		{dialect.Zsh, "print -l *.zsh«(N.om»[1,5])\n"},
		{dialect.Zsh, "print -l «((a»|b)|c)*\n"},
		{dialect.Zsh, "print -l <«1-10»>.log\n"},
		{dialect.Zsh, "case $1 in\n«(a|b)*») ;;\nesac\n"},
		{dialect.Ksh, "#!/bin/ksh\nprint -r -- ~(i:rea«dme)»\n"},
	} {
		if got, _, ok := refactorIn(t, tc.d, ExtractVariable, tc.src); ok {
			t.Errorf("extract %q offered: %q", tc.src, got)
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
	if got, _, ok := refactorIn(t, dialect.Ksh, ExtractFunction, "#!/bin/ksh\ntypeset -T P_t=(\n«\tfloat x=0»\n)\n"); ok {
		t.Errorf("extract function from a ksh93 type body offered: %q", got)
	}
	heredoc, _, ok := refactor(t, ExtractFunction, "f() {\n«\tcat <<EOF\n  kept\nEOF\n\techo \"a\n  b\"»\n}\n")
	if want := "extracted() {\n\tcat <<EOF\n  kept\nEOF\n\techo \"a\n  b\"\n}\n\nf() {\n\textracted\n}\n"; !ok || heredoc != want {
		t.Errorf("extract around a heredoc and a multi-line string = %q (%t)\nwant %q", heredoc, ok, want)
	}
	spaced, _, ok := refactor(t, ExtractFunction, "main() {\n  echo start\n«  if true; then\n    echo yes\n  fi»\n}\n")
	if want := "extracted() {\n  if true; then\n    echo yes\n  fi\n}\n\nmain() {\n  echo start\n  extracted\n}\n"; !ok || spaced != want {
		t.Errorf("extract in a two-space document = %q (%t)\nwant %q", spaced, ok, want)
	}
	text, sel := selection(t, "«echo a»\n")
	change, ok := Compute(Document{Tree: parser.Parse(text, dialect.Bash), Format: format.Options{Indent: 3}}, Refactor{Kind: ExtractFunction, Start: sel.Start, End: sel.End})
	if got, want := textedit.Apply(text, change.Edits), "extracted() {\n   echo a\n}\n\nextracted\n"; !ok || got != want {
		t.Errorf("extract in an unindented document with indent 3 = %q (%t), want %q", got, ok, want)
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
	if got, _, ok := refactorIn(t, dialect.Posix, DeclareLocal, "f() {\n\t«count»=1\n}\n"); ok {
		t.Errorf("declare local offered in POSIX sh, which has no local: %q", got)
	}
}

func lintDiagnostics(tree *parser.Tree) []Diagnostic {
	return FindingDiagnostics(lint.Check(tree, nil, lint.Options{Optional: []string{"all"}}))
}

func TestQuickFixes(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\necho $1\n"
	tree := parser.Parse(text, dialect.Bash)
	all := slices.DeleteFunc(lintDiagnostics(tree), func(d Diagnostic) bool { return d.Code.String() == "SL2009" })
	var diags []Diagnostic
	for _, d := range all {
		if d.Code.String() == "SC2164" || d.Code.String() == "SC2086" {
			diags = append(diags, d)
		}
	}
	byTitle := map[string]Action{}
	for _, a := range Actions(Input{Tree: tree, Diagnostics: diags, AllDiagnostics: all}) {
		byTitle[a.Title] = a
	}
	expect := map[string]string{
		"Add || return":                  "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp || return\n}\necho $1\n",
		"Add || exit":                    "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp || exit\n}\necho $1\n",
		"Ignore SC2164 for this command": "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034,SC2164\n\tcd /tmp\n}\necho $1\n",
		"Ignore SC2164 in this file":     "#!/bin/bash\n# shellcheck disable=SC2164\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\necho $1\n",
		"Double-quote $1":                "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\necho \"$1\"\n",
		"Ignore SC2086 for this command": "#!/bin/bash\nf() {\n\t# shellcheck disable=SC2034\n\tcd /tmp\n}\n# shellcheck disable=SC2086\necho $1\n",
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
		if a.Kind != QuickFix || len(a.Diagnostics) != 1 {
			t.Errorf("%s = %+v, want a quick fix for one diagnostic", title, a)
		}
	}
	for title, preferred := range map[string]bool{"Add || return": true, "Add || exit": false, "Double-quote $1": true, "Ignore SC2164 in this file": false} {
		if byTitle[title].Preferred != preferred {
			t.Errorf("%s preferred = %v, want %v", title, byTitle[title].Preferred, preferred)
		}
	}
	if _, ok := byTitle["Apply all automatic fixes"]; ok {
		t.Errorf("fix all offered although no fix is safe to apply unattended: %v", keys(byTitle))
	}
}

func TestQuickFixesForNativeCodes(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\nname=$1\neval \"echo hello $name\"\n"
	tree := parser.Parse(text, dialect.Bash)
	var titles []string
	for _, a := range Actions(Input{Tree: tree, Diagnostics: lintDiagnostics(tree)}) {
		titles = append(titles, a.Title)
	}
	if !slices.Contains(titles, "Ignore SL1001 for this command") || !slices.Contains(titles, "Ignore SL1001 in this file") {
		t.Errorf("titles = %q, want the SL1001 disable actions", titles)
	}
}

func TestFixAllAppliesOnlySafeFixes(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\nname=x\necho $name `date` $1\n"
	tree := parser.Parse(text, dialect.Bash)
	got := textedit.Apply(text, FixAll(lintDiagnostics(tree)))
	if want := "#!/bin/bash\nname=x\necho \"$name\" $(date) $1\n"; got != want {
		t.Errorf("fix all = %q, want %q", got, want)
	}
}

func TestDisableForFileWithoutShebang(t *testing.T) {
	t.Parallel()

	edit, _ := disableForFile(parser.Parse("echo $x", dialect.Bash), 2086)
	if got := textedit.Apply("echo $x", []textedit.Edit{edit}); got != "# shellcheck disable=SC2086\necho $x" {
		t.Errorf("disable for file = %q", got)
	}
}

func actionsFor(t *testing.T, text, code string, settings func() (SettingsFile, bool)) (map[string]Action, []string) {
	t.Helper()

	tree := parser.Parse(text, dialect.Bash)
	all := lintDiagnostics(tree)
	i := slices.IndexFunc(all, func(d Diagnostic) bool { return d.Code.String() == code })
	if i < 0 {
		t.Fatalf("no %s in %q", code, text)
	}
	byTitle := map[string]Action{}
	var titles []string
	for _, a := range Actions(Input{Tree: tree, Diagnostics: all[i : i+1], AllDiagnostics: all, Settings: settings}) {
		byTitle[a.Title] = a
		titles = append(titles, a.Title)
	}
	return byTitle, titles
}

func TestFixAllOfACode(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\na=`date`\nb=`id -u`\nc=`pwd`\necho \"$a$b$c\"\n"
	byTitle, _ := actionsFor(t, text, "SC2006", nil)
	safe, ok := byTitle["Fix all SC2006 in this file (3)"]
	if !ok {
		t.Fatalf("missing the SC2006 fix-all (have %v)", keys(byTitle))
	}
	if got, want := textedit.Apply(text, safe.Edits), "#!/bin/bash\na=$(date)\nb=$(id -u)\nc=$(pwd)\necho \"$a$b$c\"\n"; got != want || safe.FixAllCode == nil || *safe.FixAllCode != 2006 || safe.Kind != QuickFix || safe.Preferred {
		t.Errorf("fix all = %q (%+v), want %q", got, safe, want)
	}

	review := "#!/bin/bash\necho $1 $2\n"
	byTitle, _ = actionsFor(t, review, "SC2086", nil)
	a, ok := byTitle["Fix all SC2086 in this file (2, review)"]
	if !ok || textedit.Apply(review, a.Edits) != "#!/bin/bash\necho \"$1\" \"$2\"\n" {
		t.Errorf("review fix-all = %+v (have %v)", a, keys(byTitle))
	}

	single, _ := actionsFor(t, "#!/bin/bash\necho $1\n", "SC2086", nil)
	for title := range single {
		if strings.HasPrefix(title, "Fix all") {
			t.Errorf("fix-all offered for a single finding: %q", title)
		}
	}
}

func TestIgnoreInFileUsesTheHeader(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ text, want string }{
		{
			"#!/bin/bash\n# shellcheck disable=SC2034 # unused on purpose\n# shellcheck source=lib.sh\n. ./lib.sh\necho $1\n",
			"#!/bin/bash\n# shellcheck disable=SC2034,SC2086 # unused on purpose\n# shellcheck source=lib.sh\n. ./lib.sh\necho $1\n",
		},
		{
			"#!/bin/bash\n# shellcheck shell=bash\n# Copyright someone\necho $1\n",
			"#!/bin/bash\n# shellcheck shell=bash\n# shellcheck disable=SC2086\n# Copyright someone\necho $1\n",
		},
		{
			"# Copyright someone\necho $1\n",
			"# shellcheck disable=SC2086\n# Copyright someone\necho $1\n",
		},
	} {
		byTitle, _ := actionsFor(t, tc.text, "SC2086", nil)
		a, ok := byTitle["Ignore SC2086 in this file"]
		if !ok {
			t.Errorf("%q: no file ignore (have %v)", tc.text, keys(byTitle))
			continue
		}
		got := textedit.Apply(tc.text, a.Edits)
		if got != tc.want {
			t.Errorf("ignore in file %q = %q, want %q", tc.text, got, tc.want)
		}
		if _, again := disableForFile(parser.Parse(got, dialect.Bash), 2086); again {
			t.Errorf("ignoring SC2086 twice would edit %q again", got)
		}
	}
	if _, ok := disableForFile(parser.Parse("#!/bin/bash\n# shellcheck disable=SC2000-SC2100\necho $1\n", dialect.Bash), 2086); ok {
		t.Error("offered to ignore a code the header already disables through a range")
	}
}

func TestIgnoreInWorkspaceEditsTheSettingsFile(t *testing.T) {
	t.Parallel()

	text := "#!/bin/bash\necho $1\n"
	existing := func() (SettingsFile, bool) {
		return SettingsFile{URI: "file:///w/.shellcheckrc", Text: "shell=bash\ndisable=SC2034\n", Exists: true}, true
	}
	byTitle, _ := actionsFor(t, text, "SC2086", existing)
	a, ok := byTitle["Ignore SC2086 in this workspace"]
	if !ok || a.Settings == nil || a.Settings.Create || a.Settings.URI != "file:///w/.shellcheckrc" {
		t.Fatalf("workspace ignore = %+v (have %v)", a, keys(byTitle))
	}
	if got := textedit.Apply("shell=bash\ndisable=SC2034\n", a.Settings.Edits); got != "shell=bash\ndisable=SC2034,SC2086\n" {
		t.Errorf("rc after the edit = %q", got)
	}

	inherited := func() (SettingsFile, bool) {
		return SettingsFile{URI: "file:///w/.shellcheckrc", Text: "enable=require-variable-braces"}, true
	}
	byTitle, _ = actionsFor(t, text, "SC2086", inherited)
	a = byTitle["Ignore SC2086 in this workspace"]
	if a.Settings == nil || !a.Settings.Create || textedit.Apply("", a.Settings.Edits) != "enable=require-variable-braces\ndisable=SC2086\n" {
		t.Errorf("new rc = %+v, want the inherited settings plus the disable", a.Settings)
	}

	covered := func() (SettingsFile, bool) {
		return SettingsFile{URI: "file:///w/.shellcheckrc", Text: "disable=all\n", Exists: true}, true
	}
	if byTitle, _ := actionsFor(t, text, "SC2086", covered); byTitle["Ignore SC2086 in this workspace"].Settings != nil {
		t.Error("offered to ignore a code the settings file already disables")
	}
}

func TestDiagnosticActionOrder(t *testing.T) {
	t.Parallel()

	settings := func() (SettingsFile, bool) { return SettingsFile{URI: "file:///w/.shellcheckrc"}, true }
	_, titles := actionsFor(t, "#!/bin/bash\necho $1 $2\n", "SC2086", settings)
	want := []string{
		"Double-quote $1",
		"Fix all SC2086 in this file (2, review)",
		"Ignore SC2086 for this command",
		"Ignore SC2086 in this file",
		"Ignore SC2086 in this workspace",
	}
	if len(titles) < len(want) || !slices.Equal(titles[:len(want)], want) {
		t.Errorf("titles = %q, want them to start with %q", titles, want)
	}
}

func keys(m map[string]Action) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
