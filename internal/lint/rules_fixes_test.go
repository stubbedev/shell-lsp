package lint

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestAutofixes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		code   Code
		script string
		want   string
	}{
		{2281, "$target=release\n", "target=release\n"},
		{2281, "${ports[42]}=8080\n", "ports[42]=8080\n"},
		{2281, "$flags+=-O2\n", "flags+=-O2\n"},
		{2277, "#!/bin/bash\n0='worker'\n", "#!/bin/bash\nBASH_ARGV0='worker'\n"},
		{2277, "#!/bin/bash\n${0}='worker'\n", "#!/bin/bash\nBASH_ARGV0='worker'\n"},
		{2268, "[ x$answer = xyes ]\n", "[ $answer = yes ]\n"},
		{2268, "[ \"x$mode\" = 'xdebug' ]\n", "[ \"$mode\" = 'debug' ]\n"},
		{2268, "[ x = x$mode ]\n", "[ \"\" = $mode ]\n"},
		{2331, "[ ! -a foo ]\n", "[ ! -e foo ]\n"},
		{2292, "[ -x ./configure ]\n", "[[ -x ./configure ]]\n"},
		{2243, "[[ $(pending) ]]\n", "[[ -n $(pending) ]]\n"},
		{2244, "[ \"$name\" ]\n", "[ -n \"$name\" ]\n"},
		{2265, "[ -f a ] & [ -f b ]\n", "[ -f a ] && [ -f b ]\n"},
		{2266, "[ -f a ] | [ -f b ]\n", "[ -f a ] || [ -f b ]\n"},
		{2247, "echo $\"(date)\"\n", "echo \"$(date)\"\n"},
		{2256, "dir=x\necho $\"dir\"\n", "dir=x\necho \"$dir\"\n"},
		{2295, "echo \"${path#$root}\"\n", "echo \"${path#\"$root\"}\"\n"},
		{2321, "slots[$((1+1))]=x\n", "slots[1+1]=x\n"},
		{2322, "echo $(( ((1+1)) + 2 ))\n", "echo $(( (1+1) + 2 ))\n"},
		{2323, "(( (count) ))\n", "(( count ))\n"},
		{2314, "#!/usr/bin/env bats\n@test \"t\" {\n  ! true\n}\n", "#!/usr/bin/env bats\n@test \"t\" {\n  run ! true\n}\n"},
	} {
		t.Run(tc.code.String()+" "+tc.script, func(t *testing.T) {
			t.Parallel()
			if got := applyFix(t, tc.script, tc.code); got != tc.want {
				t.Errorf("fixed script = %q, want %q", got, tc.want)
			}
		})
	}
}

func applyFix(t *testing.T, script string, code Code) string {
	t.Helper()
	d := dialect.Detect(nil, dialect.Input{URI: "file:///fix", Text: script})
	for _, f := range Check(parser.Parse(script, d), nil, Options{Optional: []string{allCodes}}) {
		if f.Code != code {
			continue
		}
		if len(f.Fix) == 0 {
			t.Fatalf("%s at %d has no fix", code, f.Span.Start)
		}
		return textedit.Apply(script, f.Fix)
	}
	t.Fatalf("no %s finding in %q", code, script)
	return ""
}
