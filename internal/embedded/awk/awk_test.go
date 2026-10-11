package awk

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/embedded/lang"
	"github.com/stubbedev/shell-lsp/internal/embedded/lang/langtest"
)

var validPrograms = []string{
	``,
	`{print $1}`,
	`{ print $1 }`,
	`NR>1 {print $6}`,
	`$1 ~ /error/ { print }`,
	`$1 !~ /^#/`,
	`!seen[$0]++`,
	`NF`,
	`1`,
	`END { print NR }`,
	`BEGIN { FS = ":" } { print $1 }`,
	`BEGIN{FS=":"}{print $1}END{print NR}`,
	`BEGINFILE { n = 0 } ENDFILE { print FILENAME, n }`,
	`{ $1 = ""; print substr($0, 2) }`,
	`function add(a,   b) { return a+b }`,
	"function max(a, b)\n{\n\treturn a > b ? a : b\n}\n{ print max($1, $2) }",
	`{ for (i = NF; i > 0; i--) printf "%s ", $i; print "" }`,
	`{ if ($1 > max) max = $1 } END { print max }`,
	`{ x = a > b ? a : b }`,
	`{ print (a > b ? "y" : "n") }`,
	`BEGIN { while ((getline line < "/etc/passwd") > 0) n++; print n }`,
	`BEGIN { "date" | getline d; close("date"); print d }`,
	`BEGIN { while (("ls" | getline f) > 0) files[f]; for (f in files) print f }`,
	`{ n = split($0, parts, /[ \t]+/) }`,
	`{ sub(/x/, "y"); gsub(/^[ \t]+|[ \t]+$/, "") }`,
	`$1 == "foo", $1 == "bar"`,
	`/start/,/end/`,
	`/start/, /end/ { print NR": "$0 }`,
	`{ printf("%-10s %s\n", $1, $2) }`,
	`{ a[NR] = $0 } END { for (i = NR; i >= 1; i--) print a[i] }`,
	`{ $2 = ""; print }`,
	`NR==FNR { a[$1]; next } $1 in a`,
	`BEGIN { OFS = "\t" } { $1 = $1 } 1`,
	`{ printf "%s%s", $0, (NR % 2 ? OFS : ORS) }`,
	`{ print > ($1 ".txt") }`,
	`{ print $1, $2 > $3 ".txt" }`,
	`{ print $0 >> "log.txt" }`,
	`{ getline; print }`,
	`{ print | "sort -u" }`,
	`{ print $1 | "sort" } END { close("sort") }`,
	`{ print "x" |& "cat" }`,
	`BEGIN { printf "%d\n", 2^10; print 2**3 }`,
	`{ s += $NF } END { print s / NR }`,
	`{ s /= 2; t ^= 2; u **= 2; v %= 3 }`,
	"# leading comment\n{ print } # trailing comment\n",
	"BEGIN {\n  x = 1 \\\n    + 2\n}",
	`{ print $1 }; { print $2 }`,
	`{ delete a; delete a[i]; delete b[i, j] }`,
	`{ exit 1 } END { exit }`,
	`{ sum[$1] += $2 } END { for (k in sum) print k, sum[k] | "sort" }`,
	`{ if (!(x in a)) a[x] = 1 }`,
	`{ if ((i, j) in arr) print "yes" }`,
	`{ print -1, +2, !3 }`,
	`{ printf("x") > "/dev/stderr" }`,
	`{ system("rm " f) }`,
	`{ x = y = z; a[i] = b[i] = 0 }`,
	`{ print $(NF-1), $NF, $i++, ++$i, $++i }`,
	`{ print $1" "$2 }`,
	`/^#/ || NF == 0 { next }`,
	`!/^$/`,
	`NR > 1 && NR < 10`,
	`length > 72`,
	`length($0) > 72 { print length }`,
	`{ if ((getline x < file) <= 0) exit }`,
	"{\n\tif ($1 == \"a\")\n\t\tprint \"A\"\n\telse if ($1 == \"b\")\n\t\tprint \"B\"\n\telse\n\t\tprint \"other\"\n}",
	`{ if (x) print "a"; else print "b" }`,
	`{ if (x) { print "a" } else { print "b" } }`,
	`{ while (i < 3) i++ }`,
	`{ do { i++ } while (i < 3) }`,
	"{ do\n\ti++\nwhile (i < 3)\n}",
	`{ for (;;) break }`,
	`{ for (i = 0; i < 3; i++) continue }`,
	`{ while (x) ; }`,
	`{ nextfile }`,
	`BEGIN { srand(); print int(rand() * 10) }`,
	`BEGIN { print toupper(substr("abc", 1, 1)) tolower("X") }`,
	`BEGIN { print index("abc", "b"), match("abc", /b/), RSTART, RLENGTH }`,
	`BEGIN { print sprintf("%05.2f", 3.14159), atan2(0, -1), exp(1), log(10), sqrt(2), sin(0), cos(0) }`,
	`BEGIN { print length("x"), length() }`,
	`BEGIN { print ENVIRON["HOME"], ARGV[0], ARGC, SUBSEP, CONVFMT, OFMT }`,
	`{ print gensub(/(a)(b)/, "\\2\\1", "g") }`,
	`BEGIN { print strftime("%Y-%m-%d", systime()) }`,
	`BEGIN { n = asort(a, b); m = asorti(a, c); print and(5, 3), or(5, 3), xor(5, 3), lshift(1, 2), rshift(4, 1), compl(0) }`,
	`BEGIN { print strtonum("0x1F"), 0x1F, 1e3, .5, 1., 1.5e-3 }`,
	`BEGIN { switch (x) { case 1: print "one"; break; case "two": print 2; break; case /re/: break; default: print "?" } }`,
	`@include "lib.awk"
BEGIN { print f() }`,
	`BEGIN { fn = "f"; @fn(1) }`,
	`BEGIN { if (x ~ @/re/) print }`,
	`BEGIN { FS = "," } NR == 1 { for (i = 1; i <= NF; i++) h[$i] = i; next } { print $h["name"] }`,
	`{ lines[NR] = $0 }
END {
	for (i = 1; i <= NR; i++) {
		if (lines[i] ~ /^[[:space:]]*$/)
			continue
		print lines[i]
	}
}`,
	`$3 > 0 { pay = pay + $2 * $3 }
END { print "total pay is", pay
      print "average pay is", pay/NR
}`,
	`{ $1 = $1; print }`,
	`BEGIN { x["a"] = 1; if ("a" in x) delete x["a"] }`,
	`{ print > "/dev/stderr" }`,
	`{ print $0 > "out_" NR ".txt" }`,
	`{ printf "%s\n", $0 | "cat 1>&2" }`,
	`BEGIN {
	cmd = "ls"
	while ((cmd | getline line) > 0) {
		print line
	}
	close(cmd)
}`,
	`{ x = $1 ~ /a/ ? "a" : "b" }`,
	`/[/]/ { print "slash" }`,
	`/a\/b/`,
	`$0 ~ "a" "b"`,
	`BEGIN { a = "x" (1 + 2) }`,
	`{ print a[$1, $2] }`,
	`BEGIN { getline line < "-"; print line }`,
	`BEGIN { printf "a" "b" "\n" }`,
	`function f(arr, n,    i) {
	for (i in arr)
		n++
	return n
}`,
	`{ print NR ": " $0 }`,
	`{ gsub(/[[:punct:]]/, "") }`,
	`{ if (/x/) print }`,
	`func f() { return }`,
	`{ total += $1 }; END { print total }`,
	`{ print $1, $2,
	$3 }`,
	`{ if (a &&
	b) print }`,
	`BEGIN { x = 1; y = x++ + ++x; z = -x^2; w = 2^-1 }`,
	`{ c = substr ($0, 1, 1) }`,
	`BEGIN { and = 1; print and }`,
	`{ case = 1; default = 2; switch = 3; func = 4; print case + default + switch + func }`,
	`BEGINFILE == 1 { print BEGINFILE, ENDFILE }`,
	`{ case = $1 } case ~ /x/ { print case / 2 }`,
	"BEGIN { switch (x) {\ncase 1:\n\tprint\n\tbreak\ndefault:\n\tprint 0\n} }",
	`BEGIN { switch(x) { case /a/: print; case -1: print } }`,
}

func TestValidPrograms(t *testing.T) {
	t.Parallel()

	langtest.CheckValid(t, Parse, validPrograms)
}

func TestInvalidPrograms(t *testing.T) {
	t.Parallel()

	langtest.CheckInvalid(t, Parse, []langtest.Case{
		{`{ print "unterminated }`, `"unterminated }`, "unterminated string"},
		{`{ print $1`, `{`, "`{` is never closed"},
		{`{ print $1 }}`, `}`, "unexpected `}`"},
		{`$1 ~ /abc { print }`, `/abc { print }`, "unterminated regular expression"},
		{`BEGIN`, `BEGIN`, "`BEGIN` needs an action in braces on the same line"},
		{"BEGIN\n{ x = 1 }", "BEGIN", "`BEGIN` needs an action in braces on the same line"},
		{`{ if (x { print } }`, `{ print`, "unexpected `{`"},
		{`{ x = (1 + 2 }`, `}`, "unexpected `}`"},
		{`{ x = (1 + 2`, `(`, "`(` is never closed"},
		{`{ a[1 }`, `}`, "unexpected `}`"},
		{`{ a[1`, `[`, "`[` is never closed"},
		{`{ 1 = 2 }`, `= 2`, "the left side of `=` is not a variable, field or array element"},
		{`{ ++1 }`, `++`, "`++` needs a variable, field or array element"},
		{`{ print $1 & $2 }`, `&`, "unexpected character `&`"},
		{`{ print 'x' }`, `'x'`, "unexpected character `'`"},
		{`{ x = 1 +* 2 }`, `* 2`, "unexpected `*`"},
		{`function length(s) { return 1 }`, `length`, "`length` is a built-in function and cannot be redefined"},
		{`{ "cmd" | sort }`, `sort`, "expected `getline` after `|`, found `sort`"},
		{`{ for (i = 0; i < 3) print }`, `)`, "unexpected `)`"},
		{`{ print } x y = 1`, `= 1`, "the left side of `=` is not a variable, field or array element"},
		{`NR == 1 print`, `print`, "unexpected `print`"},
		{`{ x = . }`, `.`, "unexpected character `.`"},
		{`{ f(1, 2 }`, `}`, "unexpected `}`"},
		{"{ f(1,\n2", `(`, "`(` is never closed"},
		{`{ x = 1 ? 2 }`, `}`, "unexpected `}`"},
		{`{ print a b c d e f g h i j k l m n o p q r s t u v w x y z ( }`, `}`, "unexpected `}`"},
		{`{ x = "abc`, `"abc`, "unterminated string"},
		{`{ x = 1 }  }`, `}`, "unexpected `}`"},
	})
}

func TestRegularExpressions(t *testing.T) {
	t.Parallel()

	langtest.CheckValid(t, Parse, []string{
		`/+/`, `/*x/`, `/a)/`, `/[\]]/`, `/[/]/`, `/a{3,2}/`, `$1 ~ "^[0-9]+$"`, `$0 ~ "\\("`, `$0 ~ "(" "x)"`,
		`$0 ~ "\q("`, `{ n = a / b / c }`, `$0 ~ x "("`, `$0 !~ /^#/`, `/\.(gz|bz2)$/`, `{ sub("(", "x") }`,
	})
	langtest.CheckInvalid(t, Parse, []langtest.Case{
		{`/(a/ { print }`, `(`, "`(` is never closed in the regular expression"},
		{`/[[:alhpa:]]/`, `[:`, "unknown character class `[:alhpa:]`"},
		{`$1 ~ /[9-0]/`, `9-0`, "the range `9-0` is empty because `9` sorts after `0`"},
		{`$0 ~ "[abc"`, `[`, "`[` is never closed in the regular expression"},
		{`$0 !~ "a\n(b"`, `(`, "`(` is never closed in the regular expression"},
		{`$0 ~ "a\\"`, `\\`, "the regular expression ends with a lone `\\`"},
	})
}

func TestReportsEveryStatementError(t *testing.T) {
	t.Parallel()

	langtest.CheckErrors(t, Parse, "{ x = 1 +* 2; y = 3; z = (4 & 5) }",
		[2]string{"*", "unexpected `*`"}, [2]string{"&", "unexpected character `&`"})
	langtest.CheckErrors(t, Parse, "{ print ) }\n{ x = = 1 }\nBEGIN { y = ( }\nEND { z = 1 + }",
		[2]string{") }", "unexpected `)`"}, [2]string{"= 1", "unexpected `=`"}, [2]string{"}\nEND", "unexpected `}`"})
	langtest.CheckErrors(t, Parse, "{ if (x) { a = ) } ; b = * }",
		[2]string{") }", "unexpected `)`"}, [2]string{"*", "unexpected `*`"})
	langtest.CheckErrors(t, Parse, "function length(s) { return 1 }\nfunction f(a) { return a }\n{ x = ) }",
		[2]string{"length", "`length` is a built-in function and cannot be redefined"}, [2]string{") }", "unexpected `)`"})
	langtest.CheckErrors(t, Parse, "{ print 'x' }\n{ y = ) }",
		[2]string{"'", "unexpected character `'`"}, [2]string{") }", "unexpected `)`"})
	langtest.CheckErrors(t, Parse, "{ switch (x) { case 1: y = ); case 2: z = * } }",
		[2]string{");", "unexpected `)`"}, [2]string{"*", "unexpected `*`"})
	langtest.CheckErrors(t, Parse, "$1 == & { print }\n/x/ { print \"a\" }\nNR > { n++ }",
		[2]string{"&", "unexpected character `&`"}, [2]string{"{ n++", "unexpected `{`"})
	langtest.CheckErrors(t, Parse, "{ x = 1 } }\n{ y = ) }",
		[2]string{"}\n", "unexpected `}`"}, [2]string{") }", "unexpected `)`"})
	langtest.CheckErrors(t, Parse, "{ x = (1 + 2)) ; y = ) }",
		[2]string{") ;", "unexpected `)`"}, [2]string{") }", "unexpected `)`"})
}

func TestStopsWhereRecoveryIsUnreliable(t *testing.T) {
	t.Parallel()

	langtest.CheckErrors(t, Parse, "{ {\n { z", [2]string{"{ z", "`{` is never closed"})
	langtest.CheckErrors(t, Parse, "{ if (x &) print 1; y = 2 }", [2]string{"&", "unexpected character `&`"})
	langtest.CheckErrors(t, Parse, "{ print \"x }\n{ y = ) }", [2]string{`"x }`, "unterminated string"})
	langtest.CheckErrors(t, Parse, "{ x = /a\n{ y = ) }", [2]string{"/a", "unterminated regular expression"})
	langtest.CheckErrors(t, Parse, "{ f(a]; b = ) }", [2]string{"]", "unexpected `]`"})
}

func TestCapsErrors(t *testing.T) {
	t.Parallel()

	if got := len(Parse(strings.Repeat("{ x = ) }\n", lang.MaxErrors+3)).Errors); got != lang.MaxErrors {
		t.Errorf("%d errors, want %d", got, lang.MaxErrors)
	}
}

func TestTokens(t *testing.T) {
	t.Parallel()

	langtest.CheckTokens(t, Parse, "function f(a) { return a + NR } # note\n$1 ~ /x/ { print f(2), length, \"s\" > \"out\" }",
		"function:keyword", "f:function+declaration", "a:parameter+declaration", "return:keyword", "a:parameter",
		"+:operator", "NR:variable+builtin", "# note:comment",
		"$:operator", "1:number", "~:operator", "/x/:regexp", "print:keyword", "f:function", "2:number",
		"length:function+builtin", "\"s\":string", ">:operator", "\"out\":string",
	)
	langtest.CheckTokens(t, Parse, "func g() { switch (x) { case /r/: default: } } BEGINFILE { case = 1 }",
		"func:keyword", "g:function+declaration", "switch:keyword", "x:variable", "case:keyword", "/r/:regexp",
		"::operator", "default:keyword", "::operator", "BEGINFILE:keyword", "case:variable", "=:operator", "1:number",
	)
}

func TestTokensAfterAnError(t *testing.T) {
	t.Parallel()

	src := `{ print ) } { x = "s" }`
	res := Parse(src)
	if len(res.Errors) == 0 {
		t.Fatal("want an error")
	}
	if got := langtest.Describe(src, res.Tokens); !strings.HasSuffix(got, `"s":string`) {
		t.Errorf("tokens after the error = %s", got)
	}
}

func TestDocs(t *testing.T) {
	t.Parallel()

	for name, kind := range map[string]lang.Kind{"substr": lang.Function, "NR": lang.Variable, "gensub": lang.Function} {
		doc, ok := Doc(name, kind)
		if !ok || !strings.Contains(doc.Signature, name) || doc.Summary == "" {
			t.Errorf("Doc(%q) = %+v, %v", name, doc, ok)
		}
	}
	if _, ok := Doc("NR", lang.Function); ok {
		t.Error("NR is not a function")
	}
}

func FuzzParse(f *testing.F) {
	langtest.Fuzz(f, Parse, validPrograms)
}

func BenchmarkParse(b *testing.B) {
	src := strings.Join(validPrograms, "\n")
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		Parse(src)
	}
}
