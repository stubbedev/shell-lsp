package lint

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var forkRewrites = []struct {
	code   Code
	script string
	want   string
}{
	{2001, "y=$(echo \"$x\" | sed 's/a/b/g')\n", "y=${x//a/b}\n"},
	{2001, "y=\"$(echo \"$x\" | sed -e 's/\\./_/')\"\n", "y=\"${x/./_}\"\n"},
	{2001, "y=$(sed 's/^foo/bar/' <<< \"$x\")\n", "y=${x/#foo/bar}\n"},
	{2001, "y=$(sed 's/c$/C/' <<< \"$x\")\n", "y=${x/%c/C}\n"},
	{2001, "y=$(echo \"$x\" | sed 's|/|-|g')\n", "y=${x//\\//-}\n"},
	{2001, "y=$(echo \"$x\" | sed 's/&/ and /g')\n", "y=${x//\\&/ and }\n"},
	{2001, "y=$(echo \"$x\" | sed 's/ //g')\n", "y=${x// }\n"},
	{2001, "y=$(echo \"$x\" | sed 's/b/x\\&y/g')\n", "y=${x//b/x\\&y}\n"},
	{2001, "#!/bin/zsh\ny=$(echo \"$x\" | sed 's/b/x\\&y/g')\n", "#!/bin/zsh\ny=${x//b/x&y}\n"},
	{2001, "#!/bin/zsh\ny=$(echo \"$x\" | sed 's/+/(/g')\n", "#!/bin/zsh\ny=${x//\\+/\\(}\n"},
	{codeShellDoesIt, "n=$(basename \"$f\")\n", "n=${f##*/}\n"},
	{codeShellDoesIt, "#!/bin/zsh\nn=$(basename \"$f\")\n", "#!/bin/zsh\nn=${f:t}\n"},
	{codeShellDoesIt, "#!/bin/zsh\nd=$(dirname \"$f\")\n", "#!/bin/zsh\nd=${f:h}\n"},
	{codeShellDoesIt, "p=\"$(pwd)/x\"\n", "p=\"$PWD/x\"\n"},
	{codeShellDoesIt, "u=$(echo \"$x\" | tr a-z A-Z)\n", "u=${x^^}\n"},
	{codeShellDoesIt, "#!/bin/zsh\nu=$(echo \"$x\" | tr a-z A-Z)\n", "#!/bin/zsh\nu=${(U)x}\n"},
	{codeShellDoesIt, "l=$(tr '[:upper:]' '[:lower:]' <<< \"$x\")\n", "l=${x,,}\n"},
	{codeShellDoesIt, "s=$(echo \"$x\" | tr ' ' '_')\n", "s=${x// /_}\n"},
	{codeShellDoesIt, "s=$(echo \"$x\" | tr '/' '&')\n", "s=${x//\\//\\&}\n"},
	{codeShellDoesIt, "t=$(echo \"$x\" | tr -d '\\n')\n", "t=${x//$'\\n'}\n"},
	{codeShellDoesIt, "t=$(echo \"$x\" | tr -d .)\n", "t=${x//.}\n"},
	{codeShellDoesIt, "k=$(echo \"$x\" | cut -d: -f1)\n", "k=${x%%:*}\n"},
	{codeShellDoesIt, "k=$(echo \"$x\" | cut -f 1 -d '/')\n", "k=${x%%\\/*}\n"},
	{codeShellDoesIt, "v=$(cut -d= -f2- <<< \"$x\")\n", "v=${x#*=}\n"},
	{codeShellDoesIt, "if echo \"$x\" | grep -q ' c'; then echo y; fi\n", "if [[ $x == *\" c\"* ]]; then echo y; fi\n"},
	{codeShellDoesIt, "printf '%s\\n' \"$x\" | grep -qF '.b' && echo y\n", "[[ $x == *\".b\"* ]] && echo y\n"},
	{codeShellDoesIt, "grep -q b <<< \"$x\" && echo y\n", "[[ $x == *\"b\"* ]] && echo y\n"},
	{codeCatSubstitution, "c=$(cat \"$f\")\n", "c=$(<\"$f\")\n"},
}

func TestForkRewrites(t *testing.T) {
	t.Parallel()

	for _, tc := range forkRewrites {
		got, safety := applyFix(t, tc.script, tc.code)
		if got != tc.want || safety != Review {
			t.Errorf("%s %q fixed to %q (%d), want %q (review)", tc.code, tc.script, got, safety, tc.want)
		}
	}
}

func TestForkRewritesAreNotOfferedWhenTheyWouldDiffer(t *testing.T) {
	t.Parallel()

	for _, script := range []string{
		"y=$(echo \"$x\" | sed 's/ *$//')\n",
		"y=$(echo \"$x\" | sed 's/a/&&/g')\n",
		"y=$(echo \"$x\" | sed 's/a/b/2')\n",
		"y=$(echo \"$x\" | sed 's/^a$/b/')\n",
		"y=$(echo $x | sed 's/a/b/')\n",
		"y=$(echo \"$x\" | tr -s ' ')\n",
		"y=$(echo \"$x\" | cut -d: -f2)\n",
		"y=$(echo \"$x\" | cut -c1-3)\n",
		"y=$(basename \"$f\" .txt)\n",
		"y=$(dirname \"$f\")\n",
		"y=$(basename -a \"$f\")\n",
		"y=$(pwd -P)\n",
		"y=$(cat $f)\n",
		"y=$(cat a b)\n",
		"echo \"$x\" | grep -q 'a.b'\n",
		"echo \"$x\" | grep 'ab'\n",
		"echo \"$x\" | grep -qi 'ab'\n",
		"#!/bin/sh\necho \"$x\" | grep -q ab\n",
		"#!/bin/sh\ny=$(echo \"$x\" | tr a-z A-Z)\n",
	} {
		d := findingsFor(script)
		for _, f := range d {
			if (f.Code == codeShellDoesIt || f.Code == codeCatSubstitution || f.Code == 2001) && len(f.Fixes) > 0 {
				t.Errorf("%q: %s offers %q", script, f.Code, f.Fixes[0].Title)
			}
		}
	}
}

func findingsFor(script string) []Finding {
	d := dialect.Detect(nil, dialect.Input{URI: "file:///fix", Text: script})
	return Check(parser.Parse(script, d), nil, Options{Optional: []string{directive.All}})
}

func TestForkRewritesMatchTheShells(t *testing.T) {
	values := []string{"a.b c/d:e=f&g", "abc", "", "path/to/file.tar.gz", "/root", "Hello World", "x=y=z"}
	shells := map[bool]struct {
		name string
		args []string
	}{false: {"bash", []string{"--norc", "--noprofile", "-c"}}, true: {"zsh", []string{"-f", "-c"}}}
	for _, tc := range forkRewrites {
		zsh := strings.HasPrefix(tc.script, "#!/bin/zsh")
		sh := shells[zsh]
		if _, err := exec.LookPath(sh.name); err != nil {
			if os.Getenv("REQUIRE_SHELLS") == "1" {
				t.Fatalf("%s is not installed", sh.name)
			}
			t.Skipf("%s is not installed", sh.name)
		}
		if strings.Contains(tc.script, "basename") || strings.Contains(tc.script, "dirname") || strings.Contains(tc.script, "$(cat") {
			continue
		}
		for _, value := range values {
			run := func(script string) string {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				program := "x=" + shellQuote(value) + "\n" + strings.TrimPrefix(script, "#!/bin/zsh\n") + "printf '<%s>' \"$y\" \"$u\" \"$l\" \"$s\" \"$t\" \"$k\" \"$v\" \"$p\"\n"
				cmd := exec.CommandContext(ctx, sh.name, append(sh.args, program)...)
				cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s %q: %v\n%s", sh.name, program, err, out)
				}
				return strings.ReplaceAll(string(out), "\n", `\n`)
			}
			if before, after := run(tc.script), run(tc.want); before != after {
				t.Errorf("%s with x=%q: %q prints %s, the rewrite %q prints %s", sh.name, value, tc.script, before, tc.want, after)
			}
		}
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func TestPatternOptionsInSourcedFilesAreInformational(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		script   string
		severity Severity
		says     string
	}{
		{"#!/bin/zsh\n[[ $1 == ^x ]] && print y\n", SeverityWarning, "so zsh matches it literally"},
		{"# shellcheck shell=zsh\n[[ $1 == ^x ]] && print y\n", SeverityInfo, "unless whatever sources or autoloads this file"},
		{"# shellcheck shell=bash\nrm -- @(a|b)\n", SeverityInfo, "here or in the script that sources this file"},
		{"# shellcheck shell=zsh\nsetopt $opts\n[[ $1 == ^x ]] && print y\n", 0, ""},
	} {
		f, found := Finding{}, false
		for _, g := range findingsFor(tc.script) {
			if g.Code == codePatternNeedsOption {
				f, found = g, true
			}
		}
		if tc.says == "" {
			if found {
				t.Errorf("%q: unexpected %s", tc.script, f.Message)
			}
			continue
		}
		if !found || f.Severity != tc.severity || !strings.Contains(f.Message, tc.says) {
			t.Errorf("%q: got %v %q, want severity %v saying %q", tc.script, f.Severity, f.Message, tc.severity, tc.says)
		}
	}
}
