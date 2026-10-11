package sed

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang/langtest"
)

var validScripts = []string{
	``,
	`p`,
	`s/a/b/`,
	`s/a/b/g`,
	`s/VERSION/${version}/`,
	`s/DATE/$(date)/`,
	`s/N/$((n + 1))/`,
	`s/foo$/bar/`,
	`$p`,
	`$ { p; }`,
	"${ a\\\n  appended\n}",
	"/start/,${\np\n}",
	`${/^$/d}`,
	`${p}`,
	`s/ *$//`,
	`s/-/_/g`,
	`s,\.bak$,,`,
	`s/\(x\)/\1/`,
	`/^$/d`,
	`s/[^[:alnum:]]//g`,
	`1~3d`,
	`0~4p`,
	`0,/foo/s//bar/`,
	`/start/,+2d`,
	`/start/,~4d`,
	`s/x/\n/g`,
	`s|/usr|/opt|`,
	`s/(a|b)+/X/g`,
	`s/a/b/I`,
	`s/a/b/gI`,
	`s/a/b/2g`,
	`s/a/b/3`,
	`s/a/b/w out.txt`,
	`s/a/b/gpw out.txt`,
	`s/a/b/e`,
	`s/a/b/M`,
	`y/abc/xyz/`,
	`y/a\/b/x\/y/`,
	`y,a\,b,x\,y,`,
	`y/äb/xy/`,
	`1i #!/bin/sh`,
	`/x/a hello world`,
	`/x/a\hello`,
	"/x/i\\\nline one\\\nline two",
	"/x/c\\\nreplacement",
	`$a\`,
	`/^#/!s/foo/bar/`,
	`/^#/ !s/foo/bar/`,
	`/^#/! s/foo/bar/`,
	`2q`,
	`q5`,
	`Q`,
	`5Q 1`,
	`/re/{=;p}`,
	`s/.*/"&"/`,
	`/a/,/b/{/a/n;/b/!d}`,
	`N;s/\n/\t/`,
	`1!G;h;$!d`,
	`1!G;h;$p`,
	`s/^[ \t]*//;s/[ \t]*$//`,
	`/^$/N;/\n$/D`,
	`:a;s/^.\{1,78\}$/ &/;ta`,
	`:a;N;$!ba;s/\n/ /g`,
	":a\n$!N;s/\\n//;ta",
	`n;d`,
	`$!d`,
	`$=`,
	`r file.txt`,
	`/x/w out.txt`,
	`/x/R other.txt`,
	`/x/W first.txt`,
	`e echo hi`,
	`e`,
	`1d;$d`,
	`s/a/b/;s/c/d/`,
	`/foo/ s//bar/g`,
	`\,foo,d`,
	`\|^/usr|d`,
	`/x/Id`,
	`/x/I,/y/Mp`,
	`l`,
	`l 0`,
	`l;L`,
	`z;s/^$/x/`,
	`F`,
	`{p}`,
	`{p};p`,
	`/a/ { p }`,
	"# a comment\np # trailing\n",
	"#n\np",
	`;;p;;`,
	"/a/{\n  s/a/b/\n  /b/{\n    p\n  }\n}",
	`b end; s/a/b/; :end`,
	`/x/b;p`,
	`/x/{s/a/b/;b};p`,
	`t;T;b`,
	`h;x;g;G;H;D;P;N;n;z;=`,
	`v`,
	`v 4.2`,
	`1,3p`,
	`1 , 3p`,
	`$!N`,
	`s/\//|/g`,
	`s/a\
b/x/`,
	`s/x/a\
b/`,
	`s/[\/]/x/`,
	`s#/#\\#g`,
	`1~2{N;s/\n/ /}`,
	`/a/,/b/!{/^$/d}`,
	`5,$d`,
}

func TestValidScripts(t *testing.T) {
	t.Parallel()

	langtest.CheckValid(t, Parse, validScripts)
}

func TestInvalidScripts(t *testing.T) {
	t.Parallel()

	langtest.CheckInvalid(t, Parse, []langtest.Case{
		{`$prefix`, `refix`, "extra characters after command"},
		{`s/a/b`, `s`, "unterminated `s` command"},
		{`s/a`, `s`, "unterminated `s` command"},
		{`s`, `s`, "unterminated `s` command"},
		{"s/a/b\n/", `s`, "unterminated `s` command"},
		{`p;s/a/b/x`, `x`, "unknown option to `s`"},
		{`s/a/b/ g`, `g`, "unknown option to `s`"},
		{`s/a/b/gg`, `g`, "multiple `g` options to `s`"},
		{`s/a/b/pp`, `p`, "multiple `p` options to `s`"},
		{`s/a/b/1g2`, `2`, "multiple number options to `s`"},
		{`s/a/b/0`, `0`, "the number option to `s` may not be zero"},
		{`k`, `k`, "unknown command `k`"},
		{`/x/ü`, `ü`, "unknown command `ü`"},
		{`/x/`, `/x/`, "missing command"},
		{`1,3`, `1,3`, "missing command"},
		{`/x/!`, `/x/!`, "missing command"},
		{`/abc`, `/abc`, "unterminated address regex"},
		{`\`, `\`, "unterminated address regex"},
		{`{p`, `{`, "`{` is never closed"},
		{`p}`, `}`, "unexpected `}`"},
		{`1}`, `}`, "`}` doesn't take an address"},
		{`1:a`, `:`, "`:` doesn't take an address"},
		{`:`, `:`, "`:` needs a label"},
		{`b nowhere`, `nowhere`, "no label named `nowhere`"},
		{`y/abc/de/`, `y`, "the strings of `y` have different lengths"},
		{`y/abc/def`, `y`, "unterminated `y` command"},
		{`/x/a`, `a`, "`a` needs text"},
		{`w`, `w`, "`w` needs a file name"},
		{`1,2q`, `q`, "`q` takes at most one address"},
		{`0p`, `0`, "line address 0 is only valid as `0,/regexp/`"},
		{`0,5p`, `0`, "line address 0 is only valid as `0,/regexp/`"},
		{`1,p`, `,`, "missing address after `,`"},
		{`pq`, `q`, "extra characters after command"},
		{`/x/!!p`, `!p`, "multiple `!`s"},
		{`1~d`, `~`, "expected a number after `~`"},
	})
}

func TestTokens(t *testing.T) {
	t.Parallel()

	langtest.CheckTokens(t, Parse, "/^#/!s/a/b/g;1,$y/ab/cd/ # done\n:top\nb top\n$a\\\nbye",
		"/^#/:regexp", "!:operator", "s:keyword", "/a/:regexp", "b/:string", "g:keyword",
		"1:number", ",:operator", "$:operator", "y:keyword", "ab:string", "cd:string",
		"# done:comment", "::keyword", "top:property", "b:keyword", "top:property",
		"$:operator", "a:keyword", "bye:string",
	)
}

func BenchmarkParse(b *testing.B) {
	src := strings.Join(validScripts, "\n")
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		Parse(src)
	}
}
