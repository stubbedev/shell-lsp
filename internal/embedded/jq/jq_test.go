package jq

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/embedded/lang/langtest"
)

var validFilters = []string{
	``,
	`.`,
	`$ENV.HOME`,
	`.foo`,
	`.foo.bar`,
	`.foo?`,
	`.foo[]`,
	`.[]`,
	`.[]?`,
	`.[0]`,
	`.[-1]`,
	`.[1:3]`,
	`.[:-1]`,
	`.[2:]`,
	`.["a-b"]`,
	`."a-b"`,
	`.a."b-c".d`,
	`.a.[0]`,
	`..`,
	`..|numbers`,
	`..|.a?`,
	`.items[] | select(.name == "x") | .id`,
	`{name, id}`,
	`{name: .n, "id": .i, (.k): .v, $x, "a\(.b)": 1, @base64: 2}`,
	`{if: 1, then: 2, and: 3}`,
	`[.[] | {a: .b}]`,
	`[.[] | numbers]`,
	`.a as $x | $x + 1`,
	`.[] as [$a, $b] | {a: $a, b: $b}`,
	`. as {a: $x, $y, "c": [$z]} | $x`,
	`. as [$a] ?// $a | $a`,
	`reduce .[] as $x (0; . + $x)`,
	`reduce .[] as {a: $a} (0; . + $a)`,
	`foreach .[] as $item (0; . + 1)`,
	`foreach .[] as $item (0; . + 1; [$item, .])`,
	`def addone: . + 1; map(addone)`,
	`def f(x): x | x; f(. + 1)`,
	`def f($a; $b): $a + $b; f(1; 2)`,
	`def f: def g: 3; g; f`,
	`label $out | foreach .[] as $item (0; . + 1; if . > 3 then ., break $out else . end)`,
	`try error("x") catch .`,
	`try .a`,
	`.a // "default"`,
	`.a? // empty`,
	`.a?//1`,
	`.a |= . + 1`,
	`.a += 1 | .b -= 1 | .c *= 2 | .d /= 2 | .e %= 2 | .f //= 3 | .g = 4`,
	`del(.a)`,
	`to_entries[]`,
	`to_entries | map(select(.value > 1)) | from_entries`,
	`with_entries(.value |= tostring)`,
	`input_line_number`,
	`$__loc__`,
	`"\(.a) and \(.b)"`,
	`"nested \("inner \(1 + 2) x") y"`,
	`"\(f(1; 2))"`,
	`@csv`,
	`@sh "echo \(.)"`,
	`@json "x: \(.)"`,
	`@base64d`,
	`.[] | tostring`,
	`env.HOME`,
	`splits(", ")`,
	`ascii_downcase | test("a"; "i")`,
	`capture("(?<x>a)")`,
	`not`,
	`if . == null then "null" elif . == true then "t" else "o" end`,
	`if . then 1 end`,
	`if . then 1 end | .x`,
	`.a and .b or (.c | not)`,
	`input`,
	`[inputs]`,
	`$ENV | keys`,
	`[paths]`,
	`{"a": 1} + {"b": 2}`,
	`-.a`,
	`-1`,
	`1 - -1`,
	`1 as $x | 2 as $y | [$x, $y]`,
	`import "foo" as foo; foo::bar`,
	`import "data" as $data; $data`,
	`include "lib"; f`,
	`include "lib" {search: "./"}; f`,
	`limit(3; .[])`,
	`first(.[]), last(.[]), nth(1; .[])`,
	`path(..)`,
	`getpath(["a", "b"])`,
	`.[] |= (. as $x | $x * 2)`,
	`# a comment
.a # trailing
| .b`,
	`.a, .b, .c`,
	`[.[] | select(.a > 1 and .b < 2)]`,
	`map(.price * .qty) | add`,
	`group_by(.k) | map({k: .[0].k, n: length})`,
	`.[] | "\(.name)\t\(.value)"`,
	`.a[1:2][0]`,
	`.a | .[0:2]`,
	`1.5e3, .5, 0, 10`,
	`true, false, null`,
	`{a: true, b: null}`,
	`.a as [$x, {b: $y}] | $x`,
	`[.[] | select(type == "object")] | length`,
	`.users | map(.name) | join(", ")`,
	`def f: .; `,
	`.a | if . then "y" else "n" end`,
	`try (.a | tonumber) catch "bad"`,
	`[limit(2; repeat(1))]`,
	`.. |= (if type == "string" then ascii_upcase else . end)`,
	`{a: .x | .y}`,
	`{a: 1, b: 2,}`,
	`{(.a, .b): 1}`,
	`"é\n\t\"\\\/"`,
	`walk(if type == "object" then del(.x) else . end)`,
	`.a as $x | label $f | .b`,
	`reduce range(5) as $i ([]; . + [$i])`,
	`.[] | select(.tags | index("x"))`,
	`$ENV.PATH | split(":")`,
	`input_filename`,
	`{"a":{"b":1}} | .a.b`,
	`.foo.end`,
	`.a.if`,
	`[.[] | .x?]`,
	`.x as $v | {$v}`,
	`{$__loc__}`,
	`.[] as {name: $n} | $n`,
	`@text "\(.)"`,
	`(.a | length) > 2`,
	`.a == "x" | not`,
}

func TestValidFilters(t *testing.T) {
	t.Parallel()

	langtest.CheckValid(t, Parse, validFilters)
}

func TestInvalidFilters(t *testing.T) {
	t.Parallel()

	langtest.CheckInvalid(t, Parse, []langtest.Case{
		{`.a |`, ``, "unexpected end of program"},
		{`.[0`, `[`, "`[` is never closed"},
		{`.[`, ``, "unexpected end of program"},
		{`map(.a`, `(`, "`(` is never closed"},
		{`{a: 1`, `{`, "`{` is never closed"},
		{`{a:}`, `}`, "unexpected `}`"},
		{`if . then 1`, `if`, "`if` is never closed"},
		{`"abc`, `"abc`, "unterminated string"},
		{`"abc \(.a`, `"abc \(`, "unterminated string"},
		{`.a | | .b`, `| .b`, "unexpected `|`"},
		{`[1,]`, `]`, "unexpected `]`"},
		{`.a )`, `)`, "unexpected `)`"},
		{`.a then`, `then`, "unexpected `then`"},
		{`"\q"`, `\q`, "invalid escape `\\q`"},
		{`"\u12"`, `\u12`, "invalid escape `\\u12` needs four hexadecimal digits"},
		{`.a & .b`, `&`, "unexpected character `&`"},
		{`$`, `$`, "`$` needs a name"},
		{`. as x | x`, `x |`, "unexpected `x`"},
		{`reduce .[] (0; .)`, `(`, "unexpected `(`"},
		{`def f .;`, `.;`, "unexpected `.`"},
		{`.a.`, ``, "unexpected end of program"},
		{`{1: 2}`, `1`, "unexpected `1`"},
		{`.a = `, ``, "unexpected end of program"},
		{`..a`, `a`, "unexpected `a`"},
		{`try`, ``, "unexpected end of program"},
	})
}

func TestReportsAnErrorPerPipeStage(t *testing.T) {
	t.Parallel()

	langtest.CheckErrors(t, Parse, `.a & .b | .c | .d ) | .e`,
		[2]string{"&", "unexpected character `&`"}, [2]string{")", "unexpected `)`"})
	langtest.CheckErrors(t, Parse, `if . then & else 1 end | .x | map(&)`,
		[2]string{"&", "unexpected character `&`"}, [2]string{"&)", "unexpected character `&`"})
	langtest.CheckErrors(t, Parse, `def f: & ; .a | .b &`,
		[2]string{"&", "unexpected character `&`"}, [2]string{"&", "unexpected character `&`"})
	langtest.CheckErrors(t, Parse, `{a: "x\q", b: "\w"}`,
		[2]string{`\q`, "invalid escape `\\q`"}, [2]string{`\w`, "invalid escape `\\w`"})
	langtest.CheckErrors(t, Parse, `import "a" as ; .a | .b |`,
		[2]string{";", "unexpected `;`"}, [2]string{"", "unexpected end of program"})
	langtest.CheckErrors(t, Parse, `"\(1 +)" | [1,] | {a:}`,
		[2]string{"\" |", "unexpected `\"`"}, [2]string{"]", "unexpected `]`"}, [2]string{"}", "unexpected `}`"})
}

func TestStopsWhereRecoveryIsUnreliable(t *testing.T) {
	t.Parallel()

	langtest.CheckErrors(t, Parse, `.a | "abc | &`, [2]string{`"abc`, "unterminated string"})
	langtest.CheckErrors(t, Parse, `[.a | & | .b]`, [2]string{"&", "unexpected character `&`"})
	langtest.CheckErrors(t, Parse, `{a: &, if: 1} | .b &`, [2]string{"&", "unexpected character `&`"})
	langtest.CheckErrors(t, Parse, `(.a | & ] | .b &`, [2]string{"&", "unexpected character `&`"})
	langtest.CheckErrors(t, Parse, `if . then 1 | .b`, [2]string{"if", "`if` is never closed"})
}

func TestCapsErrors(t *testing.T) {
	t.Parallel()

	if got := len(Parse(strings.Repeat(".a & | ", lang.MaxErrors+3)).Errors); got != lang.MaxErrors {
		t.Errorf("%d errors, want %d", got, lang.MaxErrors)
	}
}

func TestTokens(t *testing.T) {
	t.Parallel()

	langtest.CheckTokens(t, Parse, `def f($a): $a + 1; .items[] as $x | {name: "n\($x)", $ENV} # c`,
		"def:keyword", "f:function+declaration", "$a:parameter+declaration", "$a:variable", "+:operator", "1:number",
		".items:property", "as:keyword", "$x:variable+declaration", "|:operator", "name:property",
		`"n:string`, `\(:operator`, "$x:variable", `):operator`, `":string`, "$ENV:variable+builtin", "# c:comment",
	)
}

func TestTokensAfterAnError(t *testing.T) {
	t.Parallel()

	src := `.a | ) | map(.b)`
	res := Parse(src)
	if len(res.Errors) == 0 {
		t.Fatal("want an error")
	}
	if got := langtest.Describe(src, res.Tokens); !strings.HasSuffix(got, ".b:property") {
		t.Errorf("tokens after the error = %s", got)
	}
}

func TestDocs(t *testing.T) {
	t.Parallel()

	for name, kind := range map[string]lang.Kind{"map": lang.Function, "@base64": lang.Function, "$ENV": lang.Variable} {
		doc, ok := Doc(name, kind)
		if !ok || !strings.Contains(doc.Signature, name) || doc.Summary == "" {
			t.Errorf("Doc(%q) = %+v, %v", name, doc, ok)
		}
	}
	if _, ok := Doc("nosuchbuiltin", lang.Function); ok {
		t.Error("unknown names have no documentation")
	}
}

func FuzzParse(f *testing.F) {
	langtest.Fuzz(f, Parse, validFilters)
}

func BenchmarkParse(b *testing.B) {
	src := strings.Join(validFilters[1:], " | ")
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		Parse(src)
	}
}
