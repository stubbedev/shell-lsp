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
		safety Safety
	}{
		{2006, "now=`date`\n", "now=$(date)\n", Safe},
		{2006, "x=`echo \\$HOME \\\\ \\`id -u\\``\n", "x=$(echo $HOME \\ `id -u`)\n", Safe},
		{2006, "echo \"`echo \\\"hi\\\" \\n`\"\n", "echo \"$(echo \"hi\" \\n)\"\n", Safe},
		{2006, "x=`(cd /tmp; pwd)`\n", "x=$( (cd /tmp; pwd))\n", Safe},
		{2281, "$target=release\n", "target=release\n", Review},
		{2281, "${ports[42]}=8080\n", "ports[42]=8080\n", Review},
		{2281, "$flags+=-O2\n", "flags+=-O2\n", Review},
		{2277, "#!/bin/bash\n0='worker'\n", "#!/bin/bash\nBASH_ARGV0='worker'\n", Review},
		{2277, "#!/bin/bash\n${0}='worker'\n", "#!/bin/bash\nBASH_ARGV0='worker'\n", Review},
		{2268, "[ x$answer = xyes ]\n", "[ $answer = yes ]\n", Review},
		{2268, "[ \"x$mode\" = 'xdebug' ]\n", "[ \"$mode\" = 'debug' ]\n", Safe},
		{2268, "[ x = x$mode ]\n", "[ \"\" = $mode ]\n", Review},
		{2331, "[ ! -a foo ]\n", "[ ! -e foo ]\n", Safe},
		{2292, "[ -x ./configure ]\n", "[[ -x ./configure ]]\n", Safe},
		{2292, "[ -n \"$a\" -a \\( \"$b\" \\< c -o ! -f x \\) ]\n", "[[ -n \"$a\" && ( \"$b\" < c || ! -f x ) ]]\n", Safe},
		{2243, "[[ $(pending) ]]\n", "[[ -n $(pending) ]]\n", Safe},
		{2244, "[ \"$name\" ]\n", "[ -n \"$name\" ]\n", Safe},
		{2265, "[ -f a ] & [ -f b ]\n", "[ -f a ] && [ -f b ]\n", Review},
		{2266, "[ -f a ] | [ -f b ]\n", "[ -f a ] || [ -f b ]\n", Review},
		{2247, "echo $\"(date)\"\n", "echo \"$(date)\"\n", Review},
		{2256, "dir=x\necho $\"dir\"\n", "dir=x\necho \"$dir\"\n", Review},
		{2295, "echo \"${path#$root}\"\n", "echo \"${path#\"$root\"}\"\n", Review},
		{2321, "slots[$((1+1))]=x\n", "slots[1+1]=x\n", Safe},
		{2322, "echo $(( ((1+1)) + 2 ))\n", "echo $(( (1+1) + 2 ))\n", Safe},
		{2323, "(( (count) ))\n", "(( count ))\n", Safe},
		{2251, "set -e\n! grep -q x log\necho done\n", "set -e\ngrep -q x log && exit 1\necho done\n", Review},
		{2264, "ls() { ls --color \"$@\"; }\n", "ls() { command ls --color \"$@\"; }\n", Review},
		{2314, "#!/usr/bin/env bats\n@test \"t\" {\n  ! true\n}\n", "#!/usr/bin/env bats\n@test \"t\" {\n  run ! true\n}\n", Review},
		{2268, "[[ x$answer = xyes ]]\n", "[[ $answer = yes ]]\n", Safe},
		{2250, "echo $dir\r\n", "echo ${dir}\r\n", Safe},
		{2250, "x=`echo \"s%$libdir\\$%%\"`\n", "x=`echo \"s%${libdir}\\$%%\"`\n", Safe},
		{2250, "echo ${list[1]}$list[2]\n", "echo ${list[1]}${list}[2]\n", Safe},
		{2338, "#!/bin/bash\n[ \"$a\" = b ]\n", "#!/bin/bash\n[ \"$a\" == b ]\n", Safe},
		{2236, "[ ! -z \"$a\" ]\n", "[ -n \"$a\" ]\n", Safe},
		{2236, "[[ ! -n $a ]]\n", "[[ -z $a ]]\n", Safe},
		{2237, "! [ -z \"$a\" ]\n", "[ -n \"$a\" ]\n", Safe},
		{2335, "[[ ! $a == b ]]\n", "[[ $a != b ]]\n", Safe},
		{2335, "! [ \"$a\" -lt 3 ]\n", "[ \"$a\" -ge 3 ]\n", Safe},
		{2164, "cd /tmp 2>/dev/null\nls\n", "cd /tmp 2>/dev/null || exit\nls\n", Review},
		{2164, "f() {\n\tcd /tmp; ls\n}\n", "f() {\n\tcd /tmp || return; ls\n}\n", Review},
	} {
		t.Run(tc.code.String()+" "+tc.script, func(t *testing.T) {
			t.Parallel()
			got, safety := applyFix(t, tc.script, tc.code)
			if got != tc.want {
				t.Errorf("fixed script = %q, want %q", got, tc.want)
			}
			if safety != tc.safety {
				t.Errorf("fix safety = %d, want %d", safety, tc.safety)
			}
		})
	}
}

func applyFix(t *testing.T, script string, code Code) (string, Safety) {
	t.Helper()
	f := findingOf(t, script, code)
	if len(f.Fixes) == 0 {
		t.Fatalf("%s at %d has no fix", code, f.Span.Start)
	}
	return textedit.Apply(script, f.Fixes[0].Edits), f.Fixes[0].Safety
}

func findingOf(t *testing.T, script string, code Code) Finding {
	t.Helper()
	d := dialect.Detect(nil, dialect.Input{URI: "file:///fix", Text: script})
	for _, f := range Check(parser.Parse(script, d), nil, Options{Optional: []string{allCodes}}) {
		if f.Code == code {
			return f
		}
	}
	t.Fatalf("no %s finding in %q", code, script)
	return Finding{}
}

func TestUnsafeRewritesOfferNoFix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		code   Code
		script string
	}{
		{2236, "[ ! -z $a ]\n"},
		{2237, "! [ -n $(cmd) ]\n"},
		{2335, "[ ! $a = b ]\n"},
		{2164, "cd /tmp &\nls\n"},
		{2164, "! cd /tmp\nls\n"},
		{2265, "make || [ -d build ] &\nls\n"},
		{2265, "[ -f a ] & # [ -f b ]\n"},
	} {
		t.Run(tc.code.String()+" "+tc.script, func(t *testing.T) {
			t.Parallel()
			if f := findingOf(t, tc.script, tc.code); len(f.Fixes) > 0 {
				t.Errorf("fixes = %+v, want none", f.Fixes)
			}
		})
	}
}

func TestFixTitlesShortenLongCode(t *testing.T) {
	t.Parallel()

	f := findingOf(t, "echo ${a_rather_long_variable_name_that_goes_on:-and on and on}\n", 2086)
	if got, want := f.Fixes[0].Title, "Double-quote ${a_rather_long_variable_name_that_goes_…"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func fixEdits(f Finding) []textedit.Edit {
	if len(f.Fixes) == 0 {
		return nil
	}
	return f.Fixes[0].Edits
}
