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
		{3014, "#!/bin/sh\n[ \"$a\" == b ]\n", "#!/bin/sh\n[ \"$a\" = b ]\n", Safe},
		{3017, "#!/bin/sh\n[ -a /etc/passwd ]\n", "#!/bin/sh\n[ -e /etc/passwd ]\n", Safe},
		{3046, "#!/bin/sh\nsource ./lib.sh\n", "#!/bin/sh\n. ./lib.sh\n", Safe},
		{3048, "#!/bin/sh\ntrap cleanup SIGINT\n", "#!/bin/sh\ntrap cleanup INT\n", Safe},
		{3049, "#!/bin/sh\ntrap cleanup exit\n", "#!/bin/sh\ntrap cleanup EXIT\n", Safe},
		{3020, "#!/bin/sh\nmake &> build.log\n", "#!/bin/sh\nmake > build.log 2>&1\n", Safe},
		{3026, "#!/bin/sh\nls [^.]*\n", "#!/bin/sh\nls [!.]*\n", Safe},
		{3034, "#!/bin/sh\nx=$(< /etc/hostname)\n", "#!/bin/sh\nx=$(cat /etc/hostname)\n", Safe},
		{3034, "#!/bin/sh\nx=$(</etc/hostname)\n", "#!/bin/sh\nx=$(cat /etc/hostname)\n", Safe},
		{2112, "#!/bin/sh\nfunction f() { :; }\n", "#!/bin/sh\nf() { :; }\n", Safe},
		{2113, "#!/bin/sh\nfunction f { :; }\n", "#!/bin/sh\nf() { :; }\n", Safe},
		{2196, "egrep 'a|b' f\n", "grep -E 'a|b' f\n", Safe},
		{2219, "let i=i+1 j++\n", "(( i=i+1, j++ ))\n", Safe},
		{2219, "let \"n = 3 * 2\"\n", "(( n = 3 * 2 ))\n", Safe},
		{1020, "[ \"$a\" = b]\n", "[ \"$a\" = b ]\n", Review},
		{2166, "#!/bin/sh\nif [ -f \"$a\" -a -r \"$a\" ]; then :; fi\n", "#!/bin/sh\nif [ -f \"$a\" ] && [ -r \"$a\" ]; then :; fi\n", Safe},
		{2166, "#!/bin/sh\n[ \"$a\" = x -o \"$a\" = y -o -z \"$b\" ] || exit\n", "#!/bin/sh\n[ \"$a\" = x ] || [ \"$a\" = y ] || [ -z \"$b\" ] || exit\n", Safe},
		{2166, "#!/bin/sh\n[ -n $a -a -n \"$b\" ]\n", "#!/bin/sh\n[ -n $a ] && [ -n \"$b\" ]\n", Review},
		{2077, "[ $a=y ]\n", "[ $a = y ]\n", Review},
		{2077, "[ $a!=y ]\n", "[ $a != y ]\n", Review},
		{2219, "let 'n = 2'\n", "(( n = 2 ))\n", Safe},
		{2070, "#!/bin/sh\n[ -n $1 ]\n", "#!/bin/sh\n[ -n \"$1\" ]\n", Review},
		{2004, "echo $(( $count + 1 ))\n", "echo $(( count + 1 ))\n", Review},
		{2162, "read line < f\n", "read -r line < f\n", Review},
		{2230, "which git\n", "command -v git\n", Review},
		{2059, "printf \"$msg\"\n", "printf '%s' \"$msg\"\n", Review},
		{2064, "trap \"rm -f $tmp \\\"x\\\"\" EXIT\n", "trap 'rm -f $tmp \"x\"' EXIT\n", Review},
		{2069, "make 2>&1 > log\n", "make > log 2>&1\n", Review},
		{2155, "f() {\n\tlocal now=$(date)\n}\n", "f() {\n\tlocal now\n\tnow=$(date)\n}\n", Review},
		{2155, "export PATH=$(dirname \"$0\"):$PATH\n", "PATH=$(dirname \"$0\"):$PATH\nexport PATH\n", Review},
		{2207, "#!/bin/bash\nfiles=( $(ls) )\n", "#!/bin/bash\nmapfile -t files < <(ls)\n", Review},
		{2207, "#!/bin/zsh\nsetopt sh_word_split\nfiles=( $(ls) )\n", "#!/bin/zsh\nsetopt sh_word_split\nfiles=( ${(f)\"$(ls)\"} )\n", Review},
		{2002, "cat log | grep -c x\n", "grep -c x < log\n", Review},
		{2002, "cat \"$f\" | while read -r l; do :; done\n", "while read -r l; do :; done < \"$f\"\n", Review},
		{2048, "printf '%s\\n' ${list[*]}\n", "printf '%s\\n' \"${list[@]}\"\n", Review},
		{2231, "for f in $dir/*; do :; done\n", "for f in \"$dir\"/*; do :; done\n", Review},
		{2254, "case $a in $b) ;; esac\n", "case $a in \"$b\") ;; esac\n", Review},
		{2115, "rm -rf \"$dir/\"*\n", "rm -rf \"${dir:?}/\"*\n", Review},
		{2005, "echo \"$(date)\"\n", "date\n", Review},
		{3037, "#!/bin/sh\necho -n \"$x\"\n", "#!/bin/sh\nprintf '%s' \"$x\"\n", Review},
		{1113, "#/bin/sh\necho\n", "#!/bin/sh\necho\n", Review},
		{1115, "# !/bin/sh\necho\n", "#!/bin/sh\necho\n", Review},
		{2185, "find -name '*.go'\n", "find . -name '*.go'\n", Safe},
		{2185, "find -L\n", "find -L .\n", Safe},
		{2188, "> log\n", ": > log\n", Safe},
		{1001, "echo a\\:b\n", "echo a:b\n", Safe},
		{2007, "echo $[1 + 2]\n", "echo $((1 + 2))\n", Safe},
		{2003, "n=$(expr $n + 1)\n", "n=$(($n + 1))\n", Review},
		{2003, "n=`expr \"$a\" \\* 3 - -2`\n", "n=$(($a * 3 - -2))\n", Review},
		{2197, "/usr/bin/fgrep -x a f\n", "/usr/bin/grep -F -x a f\n", Safe},
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
		{codeLeakedTempFile, "#!/bin/bash\nwork=$(mktemp -d)\ncp a \"$work\"\n", "#!/bin/bash\nwork=$(mktemp -d)\ntrap 'rm -rf -- \"$work\"' EXIT\ncp a \"$work\"\n", Review},
		{codeLeakedTempFile, "#!/bin/sh\nlog=`mktemp`\ndate > \"$log\"\n", "#!/bin/sh\nlog=`mktemp`\ntrap 'rm -f -- \"$log\"' EXIT\ndate > \"$log\"\n", Review},
		{codeUnusedDirective, "#!/bin/bash\n# shellcheck disable=SC2086\necho \"$1\"\n", "#!/bin/bash\necho \"$1\"\n", Safe},
		{codeUnusedDirective, "#!/bin/bash\n  # shellcheck disable=SC2086\necho \"$1\"\n", "#!/bin/bash\necho \"$1\"\n", Safe},
		{codeUnusedDirective, "#!/bin/bash\n# shellcheck disable=SC2086,SC2034\nx=1\n", "#!/bin/bash\n# shellcheck disable=SC2034\nx=1\n", Safe},
		{codeUnusedDirective, "#!/bin/bash\n# shellcheck disable=SC2034,SC2086\nx=1\n", "#!/bin/bash\n# shellcheck disable=SC2034\nx=1\n", Safe},
		{codeUnusedDirective, "#!/bin/bash\n# shellcheck shell=bash disable=SC2086\necho \"$1\"\n", "#!/bin/bash\n# shellcheck shell=bash\necho \"$1\"\n", Safe},
		{codeUnusedDirective, "#!/bin/bash\n# shellcheck disable=SC2034 disable=SC2086\nx=1\n", "#!/bin/bash\n# shellcheck disable=SC2034\nx=1\n", Safe},
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
		{2219, "let \"${name} |= 1\" 1\n"},
		{2003, "n=$(expr $n + 1 2>/dev/null)\n"},
		{2003, "expr $n + 1\n"},
		{2003, "n=$(expr $n + $(id -u))\n"},
		{2265, "[ -f a ] & # [ -f b ]\n"},
		{codeLeakedTempFile, "#!/bin/bash\ntrap 'echo bye' EXIT\nt=$(mktemp)\ndate > \"$t\"\n"},
		{codeLeakedTempFile, "#!/bin/bash\nf() { t=$(mktemp); date > \"$t\"; }\nf\ndate\n"},
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
