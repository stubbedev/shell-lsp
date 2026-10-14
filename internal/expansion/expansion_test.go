package expansion

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/glob"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
	"github.com/stubbedev/shell-lsp/internal/shelltest"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type evalCase struct {
	setup string
	expr  string
}

var bashCases = []evalCase{
	{"x=foo.tar.gz", `"${x%%.*}"`},
	{"x=foo.tar.gz", `"${x%.*}"`},
	{"x=foo.tar.gz", `"${x#*.}"`},
	{"x=foo.tar.gz", `"${x##*.}"`},
	{"x=foo.tar.gz", `"${x/./_}"`},
	{"x=foo.tar.gz", `"${x//./_}"`},
	{"x=foo.tar.gz", `"${x/#foo/bar}"`},
	{"x=foo.tar.gz", `"${x/%gz/xz}"`},
	{"x=foo.tar.gz", `"${x/#tar/bar}"`},
	{"x=foo.tar.gz", `"${x^^}"`},
	{"x=foo.tar.gz", `"${x^}"`},
	{"x=FOO.Tar", `"${x,,}"`},
	{"x=FOO.Tar", `"${x,}"`},
	{"x=foo.tar.gz", `"${x^^[ao]}"`},
	{"x=foo.tar.gz", `"${x:1:3}"`},
	{"x=foo.tar.gz", `"${x: -2}"`},
	{"x=foo.tar.gz", `"${x:2}"`},
	{"x=foo.tar.gz", `"${x:2:-3}"`},
	{"x=foo.tar.gz", `"${#x}"`},
	{"x=foo.tar.gz", `"${x@U}"`},
	{"x=foo.tar.gz", `"${x@u}"`},
	{"x=FOO", `"${x@L}"`},
	{"x=foo.tar.gz", `"${x//[[:alpha:]]/c}"`},
	{"x=foo.tar.gz", `"${x//[!.]/-}"`},
	{"x=foo.tar.gz", `"${x//o}"`},
	{"y=k=v", `"${y//=/&&}"`},
	{"y=k=v", `"${y//=/\&}"`},
	{"y=k=v", `"${y/k/[&]}"`},
	{"y=k=v", `"${y/k/"&"}"`},
	{"a=(one two three '')", `"${a[@]}"`},
	{"a=(one two three '')", `"${a[*]}"`},
	{"a=(one two three '')", `"${a[1]}"`},
	{"a=(one two three '')", `"${a[-2]}"`},
	{"a=(one two three '')", `"${#a[@]}"`},
	{"a=(one two three '')", `"${a[@]:1:2}"`},
	{"a=(one two three '')", `"${a[@]%e}"`},
	{"a=(one two three '')", `"${a[@]/o/0}"`},
	{"a=(one two three '')", `"${a[*]%e}"`},
	{"a=(one two three '')", `"${!a[@]}"`},
	{"a=(one two three '')", `"${a}"`},
	{"a=(one two three '')", `"${#a}"`},
	{"a=(one two three '')", `"${a[@]^}"`},
	{"x=y; y=deep", `"${!x}"`},
	{"x=a1b22c333; shopt -s extglob", `"${x//+([0-9])/N}"`},
	{"x=foo.tar.gz; shopt -s extglob", `"${x//@(foo|gz)/Q}"`},
	{"x=foo.tar.gz; shopt -s extglob", `"${x//*(o)/-}"`},
	{"x=foo.tar.gz; shopt -s extglob", `"${x/!(f*)/N}"`},
	{"z=", `"${z:-def}"`},
	{"z=", `"${z-def}"`},
	{"z=", `"${z:+alt}"`},
	{"z=", `"${z+alt}"`},
	{"z=", `"${z:=set}"`},
	{"x=val", `"${x:-def}"`},
	{"x=val", `"${x:+alt$x}"`},
	{"x=a.b; s=.; p='*.'", `"${x%"$s"*}"`},
	{"x=a.b; s=.; p='*.'", `"${x#$p}"`},
	{"x=a.b; s=.; p='*.'", `"${x#"$p"}"`},
	{"base=/srv/app; dir=$base/logs", `"${dir##*/}"`},
	{"f='my file.txt'", `"${f%.txt}"`},
	{"x=héllo", `"${x//?/.}"`},
	{"x=héllo", `"${#x}"`},
}

var zshCases = []evalCase{
	{"x=foo.tar.gz", "${x:t}"},
	{"x=foo.tar.gz", "${x:r}"},
	{"x=foo.tar.gz", "${x:e}"},
	{"x=foo.tar.gz", "${x:h}"},
	{"x=foo.tar.gz", "${x:r:r}"},
	{"x=foo.tar.gz", "${x%%.*}"},
	{"x=foo.tar.gz", "${x#*.}"},
	{"x=foo.tar.gz", "${x/./_}"},
	{"x=foo.tar.gz", "${x//./_}"},
	{"x=foo.tar.gz", "${x:s/o/0/}"},
	{"x=foo.tar.gz", "${x:gs/o/0/}"},
	{"x=foo.tar.gz", "${x:s/o/=&=/}"},
	{"x=foo.tar.gz", "${x:u}"},
	{"x=FOO.tar", "${x:l}"},
	{"x=foo.tar.gz", "${(U)x}"},
	{"x='foo bar-baz qux1'", "${(C)x}"},
	{"x=foo.tar.gz", "${#x}"},
	{"x=foo.tar.gz", "${x[2,4]}"},
	{"x=foo.tar.gz", "${x[-2,-1]}"},
	{"x=foo.tar.gz", "${x[3]}"},
	{"x=foo.tar.gz", "${x/#foo/bar}"},
	{"x=foo.tar.gz", "${x/%gz/xz}"},
	{"x=foo.tar.gz", "${x:/foo.tar.gz/whole}"},
	{"x=foo.tar.gz", "${x:/foo/whole}"},
	{"p=/usr/local/bin/", "${p:h}"},
	{"p=/usr/local/bin/", "${p:t}"},
	{"p=/usr/local/bin", "${p:h2}"},
	{"p=/usr/local/bin", "${p:t2}"},
	{"p=usr/local/bin", "${p:h2}"},
	{"p=a//b", "${p:h}"},
	{"p=a//b", "${p:t}"},
	{"p=/", "${p:h}"},
	{"p=/", "${p:t}"},
	{"p=/a", "${p:h}"},
	{"p=//a", "${p:h}"},
	{"p=//", "${p:h}"},
	{"p=a/", "${p:h}"},
	{"p=''", "${p:h}"},
	{"p=//", "${p:t}"},
	{"p=b", "${p:h}"},
	{"p=.bashrc", "${p:r}"},
	{"p=.bashrc", "${p:e}"},
	{"p=a.b/c", "${p:r}"},
	{"p=a.b/c", "${p:e}"},
	{"p=a.", "${p:r}"},
	{"a=(b a c a '')", "${a}"},
	{"a=(b a c a '')", `"${a}"`},
	{"a=(b a c a '')", `"${(@)a}"`},
	{"a=(b a c a '')", "${(o)a}"},
	{"a=(b a c a '')", "${(ou)a}"},
	{"a=(b a c a '')", "${(O)a}"},
	{"a=(b a c a '')", "${(j:,:)a}"},
	{"a=(b a c a '')", `"${(j:,:)a}"`},
	{"a=(b a c a '')", "${#a}"},
	{"a=(b a c a '')", "${a[2]}"},
	{"a=(b a c a '')", "${a[2,3]}"},
	{"a=(b a c a '')", "${a[-2]}"},
	{"a=(b a c a '')", "${a%a}"},
	{"a=(b a c a '')", "${a:#a}"},
	{"a=(b a c a '')", "${(M)a:#a}"},
	{"a=(b a c a '')", "${(U)a}"},
	{"a=(b a c a '')", "${a/a/x}"},
	{"a=(b a c a '')", "${a[2,3]:u}"},
	{"c=(1 10 2 b a)", "${(on)c}"},
	{"c=(1 10 2 b a)", "${(On)c}"},
	{"c=(a10 a2 b1 a)", "${(on)c}"},
	{"c=(1 10 2 b a)", "${(o)c}"},
	{"c=(B a C)", "${(oi)c}"},
	{"a=(1 2 3); b=(2 3 4)", "${a:|b}"},
	{"a=(1 2 3); b=(2 3 4)", "${a:*b}"},
	{"a=(1 2 3); b=(x y)", "${a:^b}"},
	{"a=(1 2 3); b=(x y)", "${a:^^b}"},
	{"s='k1=v1 k2=v2'; setopt extendedglob", "${s//(#b)([^= ]##)=([^ ]#)/$match[2]:$match[1]}"},
	{"s='k1=v1 k2=v2'; setopt extendedglob", "${s//(#m)k?/<$MATCH>}"},
	{"s='k1=v1 k2=v2'; setopt extendedglob", "${s//(#s)k/X}"},
	{"s='k1=v1 k2=v2'; setopt extendedglob", "${s//2(#e)/E}"},
	{"s='k1=v1 k2=v2'; setopt extendedglob", "${s//(#b)k(<->)/$mbegin[1]-$mend[1]}"},
	{"s='k1=v1 k2=v2'", "${(s: :)s}"},
	{"s='k1=v1 k2=v2'", `"${(s: :)s}"`},
	{"s='k1=v1 k2=v2'", "${(s:=:)s}"},
	{"s='k1=v1 k2=v2'", "${${s%% *}#*=}"},
	{"s='a,,b'", "${(s:,:)s}"},
	{"s='a,,b'", "${(@s:,:)s}"},
	{"s='l1\nl2'; s=$'l1\\nl2'", "${(f)s}"},
	{"a=(x y); s='k1=v1 k2=v2'", "${(F)a}"},
	{"n=s; s=hello", "${(P)n}"},
	{"x=FooBAR; setopt extendedglob", "${x//(#i)b/Z}"},
	{"x=FooBAR; setopt extendedglob", "${x//(#l)o/0}"},
	{"v=a10b5c99", "${v//<1-9>/N}"},
	{"v=a10b5c99", "${v//<->/N}"},
	{"v=foo.c.bak; setopt extendedglob", "${v//*.c~*.bak/X}"},
	{"v=foo.c.bak; setopt extendedglob", "${v%%.*~.c*}"},
	{"v=aaab; setopt extendedglob", "${v/a##/X}"},
	{"v=aaab; setopt extendedglob", "${v/a(#c2)/X}"},
	{"a=(foo.c bar.o baz.c); setopt extendedglob", "${a:#^*.c}"},
	{"a=(foo.c bar.o baz.c)", "${a:#(foo|bar).*}"},
	{"x='a b'; y=$x", `"${y}"`},
	{"x=1; y=2", "${:-$x$y}"},
	{"f=archive.tar.gz", "${${f%.gz}:e}"},
}

var kshCases = []evalCase{
	{"x=foo.tar.gz", `"${x%%.*}"`},
	{"x=foo.tar.gz", `"${x##*.}"`},
	{"x=foo.tar.gz", `"${x//./_}"`},
	{"x=foo.tar.gz", `"${x/#foo/bar}"`},
	{"x=foo.tar.gz", `"${x:1:3}"`},
	{"x=foo.tar.gz", `"${#x}"`},
	{"x=aaab", `"${x//@(a|b)/-}"`},
	{"x=ABC", `"${x/~(i)b/x}"`},
	{"a=(one two three)", `"${a[@]%e}"`},
	{"a=(one two three)", `"${a[1]}"`},
	{"a=(one two three)", `"${!a[@]}"`},
	{"a=(one two three)", `"${#a[@]}"`},
}

type regexCase struct {
	setup   string
	subject string
	re      string
}

var bashRegexCases = []regexCase{
	{"v=v1.22", `$v`, `^v([0-9]+)\.([0-9]+)$`},
	{"v=v1.22 re='^v([0-9]+)'", `$v`, `$re`},
	{"v=abbbc", `$v`, `b+`},
	{"v=abcd re='(a|ab)(c|bcd)(d*)'", `$v`, `$re`},
	{"v=5", `$v`, `\d`},
	{"v=d", `$v`, `\d`},
	{"v=a.b", `$v`, `a"."b`},
	{"v=axb", `$v`, `a"."b`},
	{"v=axb", `$v`, `a.b`},
	{"v='key = value' re='^([^=]+)= *(.*)$'", `$v`, `$re`},
	{"v=xyz", `$v`, `^(a)?xyz$`},
}

var zshRegexCases = []regexCase{
	{"v=v1.22", `$v`, `'^v([0-9]+)\.([0-9]+)$'`},
	{"v=axb", `$v`, `"a.b"`},
	{"v=a.b", `$v`, `a\.b`},
	{"v=abcd", `$v`, `"(a|ab)(c|bcd)(d*)"`},
	{"v=xaby", `$v`, `"a(b)"`},
}

func TestRegexEvaluationMatchesTheShells(t *testing.T) {
	shells := []struct {
		dialect dialect.Dialect
		shell   shelltest.Shell
		cases   []regexCase
		report  string
	}{
		{dialect.Bash, shelltest.Bash, bashRegexCases, `IFS='|'; printf '%s' "${BASH_REMATCH[*]}"`},
		{dialect.Zsh, shelltest.Zsh, zshRegexCases, `print -rn -- "$MATCH|${(j:|:)match}"`},
	}
	for _, sh := range shells {
		t.Run(sh.shell.Name, func(t *testing.T) {
			sh.shell.Require(t)
			for _, c := range sh.cases {
				script := c.setup + "\nif [[ " + c.subject + " =~ " + c.re + " ]]; then " + sh.report + "; else printf none; fi\n"
				got, err := regexOutcome(t, sh.dialect, script)
				if err != nil {
					t.Errorf("%q: %v", script, err)
					continue
				}
				want := sh.shell.Run(t, script)
				printed := "none"
				switch {
				case got.Holds && got.GroupsUncertain:
					printed, want = got.Groups[0], strings.SplitN(want, "|", 2)[0]
				case got.Holds && sh.dialect == dialect.Zsh:
					printed = got.Groups[0] + "|" + strings.Join(got.Groups[1:], "|")
				case got.Holds:
					printed = strings.Join(got.Groups, "|")
				}
				if printed != want {
					t.Errorf("%s =~ %s with %s: we say %s, the shell says %s", c.subject, c.re, c.setup, printed, want)
				}
			}
		})
	}
}

func regexOutcome(t *testing.T, d dialect.Dialect, script string) (Outcome, error) {
	t.Helper()
	tree := parser.Parse(script, d)
	site, ok := siteOfRegex(tree)
	if !ok {
		t.Fatalf("%q: no regex site", script)
	}
	return New(tree, shelloptions.Scan(tree)).Matches(site)
}

func TestRegexEvaluationLeavesOutWhatDependsOnTheLibrary(t *testing.T) {
	for _, script := range []string{
		"v='a foo b' re='\\bfoo\\b'\n[[ $v =~ $re ]]\n",
		"v=a re='\\w'\n[[ $v =~ $re ]]\n",
	} {
		if _, err := regexOutcome(t, dialect.Bash, script); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: got %v, want ErrUnsupported", script, err)
		}
	}
	got, err := regexOutcome(t, dialect.Bash, "v=abcd re='(a|ab)(c|bcd)(d*)'\n[[ $v =~ $re ]]\n")
	if err != nil || !got.GroupsUncertain || got.Groups[0] != "abcd" {
		t.Errorf("ambiguous groups: got %+v, %v", got, err)
	}
	got, err = regexOutcome(t, dialect.Bash, "v=v1.2 re='^v([0-9]+)[.]([0-9]+)$'\n[[ $v =~ $re ]]\n")
	if err != nil || got.GroupsUncertain || strings.Join(got.Groups, "|") != "v1.2|1|2" {
		t.Errorf("unambiguous groups: got %+v, %v", got, err)
	}
}

func siteOfRegex(tree *parser.Tree) (glob.Site, bool) {
	for _, site := range glob.For(tree, shelloptions.Scan(tree)).All() {
		if site.Role == glob.RoleRegex {
			return site, true
		}
	}
	return glob.Site{}, false
}

func ours(t *testing.T, d dialect.Dialect, script string) (string, error) {
	t.Helper()
	tree := parser.Parse(script, d)
	stmts := tree.File.Stmts
	call := stmts[len(stmts)-1].Cmd.(*syntax.CallExpr)
	part := call.Args[2].Parts[0]
	quoted := false
	if dq, ok := part.(*syntax.DblQuoted); ok {
		part, quoted = dq.Parts[0], true
	}
	got, err := New(tree, shelloptions.Scan(tree)).Expansion(part.(*syntax.ParamExp), quoted)
	if err != nil {
		return "", err
	}
	if len(got.Value.Words) == 0 {
		return "<>", nil
	}
	var sb strings.Builder
	for _, w := range got.Value.Words {
		sb.WriteString("<" + w + ">")
	}
	return sb.String(), nil
}

func TestEvaluationMatchesTheShells(t *testing.T) {
	shells := []struct {
		dialect dialect.Dialect
		shell   shelltest.Shell
		cases   []evalCase
	}{
		{dialect.Bash, shelltest.Bash, bashCases},
		{dialect.Zsh, shelltest.Zsh, zshCases},
		{dialect.Ksh, shelltest.Ksh, kshCases},
	}
	for _, sh := range shells {
		t.Run(sh.shell.Name, func(t *testing.T) {
			sh.shell.Require(t)
			for _, c := range sh.cases {
				script := c.setup + "\nprintf '<%s>' " + c.expr + "\n"
				got, err := ours(t, sh.dialect, script)
				if err != nil {
					t.Errorf("%s; %s: %v", c.setup, c.expr, err)
					continue
				}
				if want := sh.shell.Run(t, script); got != want {
					t.Errorf("%s; %s = %s, the shell says %s", c.setup, c.expr, got, want)
				}
			}
		})
	}
}

func TestEvaluationRefusesWhatItCannotKnow(t *testing.T) {
	tests := []struct {
		dialect dialect.Dialect
		script  string
		want    error
	}{
		{dialect.Bash, "printf '<%s>' \"${x%.*}\"\n", ErrUnknown},
		{dialect.Bash, "x=$(date)\nprintf '<%s>' \"${x%.*}\"\n", ErrUnknown},
		{dialect.Bash, "for x in a b; do :; done\nprintf '<%s>' \"${x%.*}\"\n", ErrUnknown},
		{dialect.Bash, "declare -i x=1+2\nprintf '<%s>' \"${x}\"\n", ErrUnknown},
		{dialect.Bash, "declare -u x=abc\nprintf '<%s>' \"${x}\"\n", ErrUnknown},
		{dialect.Bash, "declare -u x\nx=abc\nprintf '<%s>' \"${x}\"\n", ErrUnknown},
		{dialect.Zsh, "typeset -U a\na=(x x)\nprintf '<%s>' ${a}\n", ErrUnknown},
		{dialect.Bash, "x=abc\nx+=d\nprintf '<%s>' \"${x}\"\n", ErrUnknown},
		{dialect.Bash, "a=(*.c)\nprintf '<%s>' \"${a[@]}\"\n", ErrUnknown},
		{dialect.Bash, "x=abc\nprintf '<%s>' \"${x//+(a)/b}\"\n", ErrUnsupported},
		{dialect.Bash, "x=abc; shopt -s nocasematch\nprintf '<%s>' \"${x//A/b}\"\n", ErrUnsupported},
		{dialect.Bash, "x=abc\nprintf '<%s>' \"${x@Q}\"\n", ErrUnsupported},
		{dialect.Zsh, "x=abc\nprintf '<%s>' ${x//a#/b}\n", ErrUnsupported},
		{dialect.Zsh, "x=abc\nprintf '<%s>' ${(q)x}\n", ErrUnsupported},
		{dialect.Zsh, "x=abc\nprintf '<%s>' ${x:a}\n", ErrUnsupported},
		{dialect.Ksh, "x=aaab\nprintf '<%s>' \"${x/~(E)a+/X}\"\n", glob.ErrUnsupported},
		{dialect.Ksh, "x=aaab\nprintf '<%s>' \"${x/*-(a)/X}\"\n", glob.ErrUnsupported},
		{dialect.Ksh, "x=ab\nprintf '<%s>' \"${x/@(a)/\\1}\"\n", ErrUnsupported},
		{dialect.Ksh, "x=y\nprintf '<%s>' \"${!x}\"\n", ErrUnsupported},
		{dialect.Zsh, "c=(B a)\nprintf '<%s>' ${(o)c}\n", ErrUnsupported},
		{dialect.Zsh, "c=(a-b ab)\nprintf '<%s>' ${(o)c}\n", ErrUnsupported},
	}
	for _, tt := range tests {
		_, err := ours(t, tt.dialect, tt.script)
		if !errors.Is(err, tt.want) {
			t.Errorf("%q: got %v, want %v", tt.script, err, tt.want)
		}
	}
}

func TestAssumptionsNameTheirLines(t *testing.T) {
	tree := parser.Parse("f=archive.tar.gz\nsuffix=.gz\necho \"${f%\"$suffix\"}\"\n", dialect.Bash)
	pe := tree.File.Stmts[2].Cmd.(*syntax.CallExpr).Args[1].Parts[0].(*syntax.DblQuoted).Parts[0].(*syntax.ParamExp)
	got, err := New(tree, shelloptions.Scan(tree)).Expansion(pe, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assumptions) != 2 || got.Assumptions[0].Name != "f" || got.Assumptions[0].Line != 1 || got.Assumptions[1].Name != "suffix" || got.Assumptions[1].Line != 2 {
		t.Fatalf("assumptions = %+v", got.Assumptions)
	}
	if got.Value.Words[0] != "archive.tar" {
		t.Fatalf("value = %q", got.Value.Words)
	}
}
