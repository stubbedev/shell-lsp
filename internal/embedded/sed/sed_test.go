package sed

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
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
	"b end # skip the rest\np\n:end",
	":loop  \nN\nb loop\t",
}

func parseBasic(src string) lang.Result { return Parse(src, Basic) }

func parseExtended(src string) lang.Result { return Parse(src, Extended) }

func TestRegularExpressions(t *testing.T) {
	t.Parallel()

	langtest.CheckValid(t, parseBasic, []string{
		`s/\(a\)\(b\)/\2\1/`, `s/(a)/x/;s/x/\\1/`, `/^*/d`, `s/a)/x/`, `s|a\|b|x|`, `s,a\,b,x,`,
		`s//\1/`, `/[[:space:]]*$/d`, `s/[/]/x/`, `/[/]/d`, `s^.*/env[ ]*\(py\|pypy\)[^ ]*^#!@x@^`, `s/[]/]/x/`, `s/x\{2,3\}/y/`, `\,a\(b\),s,,\1,`, `sxa\(b\)x\1x`, `s1a\(b\)1\21`,
	})
	langtest.CheckValid(t, parseExtended, []string{
		`s/(a)(b)/\2\1/`, `s/a)/x/`, `s/a{2,3}/b/`, `s/(x|y)+/z/g`, `s|a\|b|x|`,
	})
	langtest.CheckInvalid(t, parseBasic, []langtest.Case{
		{`s/\(a/x/`, `\(`, "`\\(` is never closed in the regular expression"},
		{`s/a\)/x/`, `\)`, "`\\)` has no matching opening parenthesis in the regular expression"},
		{`/[abc/d`, `[`, "`[` is never closed in the regular expression"},
		{`/[[:alhpa:]]/d`, `[:`, "unknown character class `[:alhpa:]`"},
		{`s/[z-a]//`, `z-a`, "the range `z-a` is empty because `z` sorts after `a`"},
		{`s/a\{3,1\}//`, `\{`, "the interval `\\{3,1\\}` has a minimum above its maximum"},
		{`s/\(a\)/\2/`, `\2`, "`\\2` refers to group 2, but the regular expression has only 1 group"},
		{`s/a/\1/`, `\1`, "`\\1` refers to group 1, but the regular expression has no groups"},
		{`s/a\1/x/`, `\1`, "`\\1` refers to group 1, but the regular expression has no groups"},
		{`\,a\(,d`, `\(`, "`\\(` is never closed in the regular expression"},
		{`s|\(a\|b|x|`, `\(`, "`\\(` is never closed in the regular expression"},
	})
	langtest.CheckInvalid(t, parseExtended, []langtest.Case{
		{`s/(a/x/`, `(`, "`(` is never closed in the regular expression"},
		{`s/*a/x/`, `*`, "`*` has nothing to repeat"},
		{`s/(a)/\2/`, `\2`, "`\\2` refers to group 2, but the regular expression has only 1 group"},
		{`s(a\(b(x(`, `\(`, "`(` is never closed in the regular expression"},
	})
	langtest.CheckInvalid(t, parseBasic, []langtest.Case{
		{`/(/s/\(//q`, `q`, "unknown option to `s`"},
		{`s/[/x/`, `[`, "`[` is never closed in the regular expression"},
	})
	langtest.CheckErrors(t, parseBasic, `s/\(a/\1\2/`,
		[2]string{`\(`, "`\\(` is never closed in the regular expression"})
	langtest.CheckErrors(t, parseBasic, `s/a/\1\2/`,
		[2]string{`\1`, "`\\1` refers to group 1, but the regular expression has no groups"},
		[2]string{`\2`, "`\\2` refers to group 2, but the regular expression has no groups"})
}

func TestValidScripts(t *testing.T) {
	t.Parallel()

	langtest.CheckValid(t, parseBasic, validScripts)
}

func TestInvalidScripts(t *testing.T) {
	t.Parallel()

	langtest.CheckInvalid(t, parseBasic, []langtest.Case{
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

func TestReportsAnErrorPerCommand(t *testing.T) {
	t.Parallel()

	langtest.CheckErrors(t, parseBasic, "s/a/b/x\nk\np;y/ab/c/",
		[2]string{"x", "unknown option to `s`"}, [2]string{"k", "unknown command `k`"},
		[2]string{"y/", "the strings of `y` have different lengths"})
	langtest.CheckErrors(t, parseBasic, "p;s/a/b/x;q\nb nowhere",
		[2]string{"x;", "unknown option to `s`"}, [2]string{"nowhere", "no label named `nowhere`"})
	langtest.CheckErrors(t, parseBasic, "1,2q\n0p\n$!!d\n/x/",
		[2]string{"q", "`q` takes at most one address"}, [2]string{"0", "line address 0 is only valid as `0,/regexp/`"},
		[2]string{"!d", "multiple `!`s"}, [2]string{"/x/", "missing command"})
}

func TestStopsWhereRecoveryIsUnreliable(t *testing.T) {
	t.Parallel()

	langtest.CheckErrors(t, parseBasic, "/x/{ s/a/b/q }\np", [2]string{"q", "unknown option to `s`"})
	langtest.CheckErrors(t, parseBasic, "1:a\nb a", [2]string{":", "`:` doesn't take an address"})
	langtest.CheckErrors(t, parseBasic, "}\n}\np", [2]string{"}", "unexpected `}`"})
	langtest.CheckErrors(t, parseBasic, "s/a/b\n/;k", [2]string{"s", "unterminated `s` command"})
	langtest.CheckErrors(t, parseBasic, "y/ab/\nk", [2]string{"y", "unterminated `y` command"})
	langtest.CheckErrors(t, parseBasic, "/ab\nk", [2]string{"/ab", "unterminated address regex"})
}

func TestCapsErrors(t *testing.T) {
	t.Parallel()

	if got := len(parseBasic(strings.Repeat("k\n", lang.MaxErrors+3)).Errors); got != lang.MaxErrors {
		t.Errorf("%d errors, want %d", got, lang.MaxErrors)
	}
}

func TestTokens(t *testing.T) {
	t.Parallel()

	langtest.CheckTokens(t, parseBasic, "/^#/!s/a/b/g;1,$y/ab/cd/ # done\n:top\nb top\n$a\\\nbye",
		"/^#/:regexp", "!:operator", "s:keyword+builtin", "/a/:regexp", "b/:string", "g:keyword+builtin+option",
		"1:number", ",:operator", "$:operator", "y:keyword+builtin", "ab:string", "cd:string",
		"# done:comment", "::keyword+builtin", "top:property", "b:keyword+builtin", "top:property",
		"$:operator", "a:keyword+builtin", "bye:string",
	)
}

func TestOptionTokens(t *testing.T) {
	t.Parallel()

	langtest.CheckTokens(t, parseBasic, "/x/I,/y/Ms/a/b/2gpw out",
		"/x/:regexp", "I:keyword+builtin+option", ",:operator", "/y/:regexp", "M:keyword+builtin+option",
		"s:keyword+builtin", "/a/:regexp", "b/:string", "2:number+builtin+option", "g:keyword+builtin+option",
		"p:keyword+builtin+option", "w:keyword+builtin+option", "out:string",
	)
}

func TestDocs(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"s", "y", "p", "d", "a", "i", "c", "n", "N", "P", "D", "h", "H", "g", "G", "x", "b", "t", "T", "q", "Q", "r", "R", "w", "W", "e", "F", "z", "l", "=", ":", "v"} {
		if doc, ok := Doc(name, lang.Builtin); !ok || doc.Signature == "" || doc.Summary == "" {
			t.Errorf("Doc(%q) = %+v, %t, want documentation", name, doc, ok)
		}
	}
	for _, name := range []string{"g", "p", "w", "e", "i", "I", "m", "M", "3", "12"} {
		doc, ok := Doc(name, lang.Builtin|lang.Option)
		if !ok || !strings.Contains(doc.Signature, "regexp") || doc.Summary == "" {
			t.Errorf("Doc(%q, option) = %+v, %t, want documentation", name, doc, ok)
		}
	}
	command, _ := Doc("p", lang.Builtin)
	if flag, _ := Doc("p", lang.Builtin|lang.Option); command == flag {
		t.Error("the p command and the p flag share documentation")
	}
	for _, name := range []string{"k", "L", "", "}"} {
		if _, ok := Doc(name, lang.Builtin); ok {
			t.Errorf("Doc(%q) found documentation", name)
		}
	}
}

func FuzzParse(f *testing.F) {
	langtest.Fuzz(f, parseBasic, validScripts)
}

func BenchmarkParse(b *testing.B) {
	src := strings.Join(validScripts, "\n")
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		parseBasic(src)
	}
}
