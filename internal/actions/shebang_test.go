package actions

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestAddShebang(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d         dialect.Dialect
		src, want string
	}{
		{dialect.Bash, "«»echo hi\n", "#!/usr/bin/env bash\necho hi\n"},
		{dialect.Zsh, "# shellcheck shell=zsh\n«»print hi\n", "#!/usr/bin/env zsh\n# shellcheck shell=zsh\nprint hi\n"},
		{dialect.Posix, "«»", "#!/bin/sh\n"},
		{dialect.Dash, "echo «»\n", "#!/usr/bin/env dash\necho \n"},
		{dialect.Ksh, "«»print\n", "#!/usr/bin/env ksh\nprint\n"},
		{dialect.Busybox, "«»ls\n", "#!/bin/busybox sh\nls\n"},
	}
	for _, tc := range tests {
		got, title, ok := refactorIn(t, tc.d, AddShebang, tc.src)
		if !ok || got != tc.want || title != "Add "+shebangs[tc.d] {
			t.Errorf("add shebang to %q in %s = %q (%q, %v), want %q", tc.src, tc.d, got, title, ok, tc.want)
		}
	}
	for _, src := range []string{"#!/bin/bash\n«»echo\n", "#compdef git\n«»_git\n", "#autoload\n«»"} {
		if got, _, ok := refactorIn(t, dialect.Zsh, AddShebang, src); ok {
			t.Errorf("add shebang offered for %q: %q", src, got)
		}
	}
}

func TestRemoveQuotes(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"echo «'hello'»\n":                  "echo hello\n",
		"cp \"«src/a.txt»\" dst\n":          "cp src/a.txt dst\n",
		"x=\"«-v»\"\n":                      "x=-v\n",
		"case $a in \"«start»\") ;; esac\n": "case $a in start) ;; esac\n",
		"echo pre'«fix»'\n":                 "echo prefix\n",
	}
	for src, want := range tests {
		got, title, ok := refactor(t, RemoveQuotes, src)
		if !ok || got != want || title != "Remove the quotes" {
			t.Errorf("remove quotes in %q = %q (%q, %v), want %q", src, got, title, ok, want)
		}
	}
	for _, src := range []string{
		"echo «'a b'»\n",
		"echo «'*.txt'»\n",
		"echo «\"$x\"»\n",
		"echo «'~/x'»\n",
		"echo «'a=b'»\n",
		"echo «''»\n",
		"«'ls'» -l\n",
		"cat <<«'EOF'»\n$x\nEOF\n",
		"echo «$'a'»\n",
		"echo «\"a\\\"b\"»\n",
	} {
		if got, _, ok := refactor(t, RemoveQuotes, src); ok {
			t.Errorf("remove quotes offered for %q: %q", src, got)
		}
	}
}

func TestKindsCoverEveryAction(t *testing.T) {
	t.Parallel()

	kinds := Kinds()
	for _, r := range refactorings {
		if !slices.Contains(kinds, r.action) {
			t.Errorf("the %s refactoring's kind %s is not advertised", r.kind, r.action)
		}
	}
	for _, k := range []Kind{QuickFix, SourceFixAll, SourceShebang} {
		if !slices.Contains(kinds, k) {
			t.Errorf("kind %s is not advertised", k)
		}
	}
}
