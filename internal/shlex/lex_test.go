package shlex

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

var partNames = map[PartKind]string{
	Literal:             "lit",
	Escaped:             "esc",
	Continuation:        "cont",
	SingleQuoted:        "sq",
	DollarSingleQuoted:  "dsq",
	DoubleQuoted:        "dq",
	DollarDoubleQuoted:  "ddq",
	Param:               "param",
	CommandSubst:        "cmd",
	ArithmeticExpansion: "arith",
	Backquote:           "bq",
	ProcessSubst:        "proc",
	Array:               "array",
	ExtGlob:             "extglob",
	GlobGroup:           "group",
}

func describe(src string, tokens []Token) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		var b strings.Builder
		switch t.Kind {
		case Word, Comment:
			b.WriteString(t.Kind.String() + ":" + t.Text(src))
		case HeredocBody:
			b.WriteString("body:" + t.Text(src))
		default:
			b.WriteString(t.Kind.String())
		}
		for _, flag := range []struct {
			flag Flags
			name string
		}{{CommandStart, "/cmd"}, {Keyword, "/kw"}, {Arithmetic, "/arith"}, {Unterminated, "/open"}} {
			if t.Has(flag.flag) {
				b.WriteString(flag.name)
			}
		}
		out = append(out, b.String())
	}
	return out
}

func describeParts(src string, parts []Part) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := partNames[p.Kind] + ":" + p.Text(src)
		if p.Unterminated {
			s += "/open"
		}
		out = append(out, s)
	}
	return out
}

func lex(t *testing.T, src string) []Token {
	t.Helper()
	return lexAs(t, src, dialect.Bash)
}

func lexAs(t *testing.T, src string, d dialect.Dialect) []Token {
	t.Helper()
	tokens, err := Lex(src, d)
	if err != nil {
		t.Fatalf("Lex(%q): %v", src, err)
	}
	return tokens
}

func TestLexTokens(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want []string
	}{
		{"", []string{}},
		{"echo hi", []string{"word:echo/cmd", "word:hi"}},
		{"  echo\thi  ", []string{"word:echo/cmd", "word:hi"}},
		{"a && b || c | d |& e", []string{"word:a/cmd", "&&", "word:b/cmd", "||", "word:c/cmd", "|", "word:d/cmd", "|&", "word:e/cmd"}},
		{"a; b & c\nd", []string{"word:a/cmd", ";", "word:b/cmd", "&", "word:c/cmd", "newline", "word:d/cmd"}},
		{"a&&b||c", []string{"word:a/cmd", "&&", "word:b/cmd", "||", "word:c/cmd"}},
		{"echo a#b # c", []string{"word:echo/cmd", "word:a#b", "comment:# c"}},
		{"# only\n", []string{"comment:# only", "newline"}},
		{"echo 'a b' \"c d\" $'e\\'f' $\"g\"", []string{"word:echo/cmd", "word:'a b'", "word:\"c d\"", "word:$'e\\'f'", "word:$\"g\""}},
		{"echo a\\ b \\#c", []string{"word:echo/cmd", "word:a\\ b", "word:\\#c"}},
		{"echo a\\\nb", []string{"word:echo/cmd", "word:a\\\nb"}},
		{"echo \\\n b", []string{"word:echo/cmd", "word:b"}},
		{"echo $(a b) `c d` ${e:-f g}", []string{"word:echo/cmd", "word:$(a b)", "word:`c d`", "word:${e:-f g}"}},
		{"echo $((1 + (2) ))x", []string{"word:echo/cmd", "word:$((1 + (2) ))x"}},
		{"echo \"$(echo \")\")\" x", []string{"word:echo/cmd", "word:\"$(echo \")\")\"", "word:x"}},
		{"diff <(a) >(b) x<(c)", []string{"word:diff/cmd", "word:<(a)", "word:>(b)", "word:x<(c)"}},
		{"a >f 2>&1 <g >>h <&0 &>i &>>j >|k <>l <<<m", []string{"word:a/cmd", ">", "word:f", "word:2", ">&", "word:1", "<", "word:g", ">>", "word:h", "<&", "word:0", "&>", "word:i", "&>>", "word:j", ">|", "word:k", "<>", "word:l", "<<<", "word:m"}},
		{"case x in a) b;; c) d;& e) f;;& g) h;| esac", []string{"word:case/cmd/kw", "word:x", "word:in/kw", "word:a", ")", "word:b/cmd", ";;", "word:c", ")", "word:d/cmd", ";&", "word:e", ")", "word:f/cmd", ";;&", "word:g", ")", "word:h/cmd", ";|", "word:esac/kw"}},
		{"case x in (a|b) c;; esac; d", []string{"word:case/cmd/kw", "word:x", "word:in/kw", "(", "word:a", "|", "word:b", ")", "word:c/cmd", ";;", "word:esac/kw", ";", "word:d/cmd"}},
		{"case x in\n a) b\nesac", []string{"word:case/cmd/kw", "word:x", "word:in/kw", "newline", "word:a", ")", "word:b/cmd", "newline", "word:esac/cmd/kw"}},
		{"if a; then b; elif c; then d; else e; fi", []string{"word:if/cmd/kw", "word:a/cmd", ";", "word:then/cmd/kw", "word:b/cmd", ";", "word:elif/cmd/kw", "word:c/cmd", ";", "word:then/cmd/kw", "word:d/cmd", ";", "word:else/cmd/kw", "word:e/cmd", ";", "word:fi/cmd/kw"}},
		{"echo if then", []string{"word:echo/cmd", "word:if", "word:then"}},
		{"'if' \\if i\\\nf", []string{"word:'if'/cmd", "word:\\if", "word:i\\\nf"}},
		{"for x in a b; do c; done", []string{"word:for/cmd/kw", "word:x", "word:in/kw", "word:a", "word:b", ";", "word:do/cmd/kw", "word:c/cmd", ";", "word:done/cmd/kw"}},
		{"for x do y; done", []string{"word:for/cmd/kw", "word:x", "word:do/kw", "word:y/cmd", ";", "word:done/cmd/kw"}},
		{"for ((i=0; i<3; i++)) do x; done", []string{"word:for/cmd/kw", "((", "word:i=0/arith", ";/arith", "word:i<3/arith", ";/arith", "word:i++/arith", "))", "word:do/cmd/kw", "word:x/cmd", ";", "word:done/cmd/kw"}},
		{"(( a << 2 )) && x", []string{"((", "word:a/arith", "word:<</arith", "word:2/arith", "))", "&&", "word:x/cmd"}},
		{"echo ((", []string{"word:echo/cmd", "(", "("}},
		{"((a)", []string{"((/open", "word:a/arith", ")/arith"}},
		{"((a) b)", []string{"((/open", "word:a/arith", ")/arith", "word:b/arith", ")"}},
		{"( (a) )", []string{"(", "(", "word:a/cmd", ")", ")"}},
		{"{ a; } b", []string{"{", "word:a/cmd", ";", "}", "word:b/cmd"}},
		{"echo { }", []string{"word:echo/cmd", "word:{", "word:}"}},
		{"{a; }", []string{"word:{a/cmd", ";", "}"}},
		{"f() { :; }", []string{"word:f/cmd", "(", ")", "{", "word::/cmd", ";", "}"}},
		{"function f { :; }", []string{"word:function/cmd/kw", "word:f", "{", "word::/cmd", ";", "}"}},
		{"! a | time b", []string{"word:!/cmd/kw", "word:a/cmd", "|", "word:time/cmd/kw", "word:b/cmd"}},
		{"x=1 y=(a b) if", []string{"word:x=1/cmd", "word:y=(a b)", "word:if"}},
		{"declare -a x=(1 2) y+=(3)", []string{"word:declare/cmd", "word:-a", "word:x=(1 2)", "word:y+=(3)"}},
		{"a[i+1]=(x)", []string{"word:a[i+1]=(x)/cmd"}},
		{"eval x=(a)", []string{"word:eval/cmd", "word:x=", "(", "word:a/cmd", ")"}},
		{"x=(a\n# c\nb)", []string{"word:x=(a\n# c\nb)/cmd"}},
		{"[[ a < b && ( c > d ) ]] || e", []string{"word:[[/cmd/kw", "word:a", "word:<", "word:b", "&&", "(", "word:c", "word:>", "word:d", ")", "word:]]/kw", "||", "word:e/cmd"}},
		{"[[ $x =~ ^(a|b c)$ ]]; y", []string{"word:[[/cmd/kw", "word:$x", "word:=~", "word:^(a|b c)$", "word:]]/kw", ";", "word:y/cmd"}},
		{"[[ a ; b", []string{"word:[[/cmd/kw", "word:a", ";", "word:b/cmd"}},
		{"cat <<EOF\nhello\nEOF\nx", []string{"word:cat/cmd", "<<", "word:EOF", "newline", "body:hello\nEOF\n", "word:x/cmd"}},
		{"cat <<-'E F' <<\\G\n\tbody\n\tE F\nmore\nG\n", []string{"word:cat/cmd", "<<-", "word:'E F'", "<<", "word:\\G", "newline", "body:\tbody\n\tE F\n", "body:more\nG\n"}},
		{"cat <<E\nnever closed", []string{"word:cat/cmd", "<<", "word:E", "newline", "body:never closed/open"}},
		{"cat <<E\n", []string{"word:cat/cmd", "<<", "word:E", "newline"}},
		{"cat <<E; echo $(cat <<F\nf\nF\n)\ne\nE\n", []string{"word:cat/cmd", "<<", "word:E", ";", "word:echo/cmd", "word:$(cat <<F\nf\nF\n)", "newline", "body:e\nE\n"}},
		{"x <<< y\nz", []string{"word:x/cmd", "<<<", "word:y", "newline", "word:z/cmd"}},
		{"> f if", []string{">", "word:f", "word:if"}},
		{"echo 'open", []string{"word:echo/cmd", "word:'open/open"}},
		{"echo \"open $x", []string{"word:echo/cmd", "word:\"open $x/open"}},
		{"echo $(open", []string{"word:echo/cmd", "word:$(open/open"}},
		{"echo ${open", []string{"word:echo/cmd", "word:${open/open"}},
		{"echo $((open", []string{"word:echo/cmd", "word:$((open/open"}},
		{"echo `open", []string{"word:echo/cmd", "word:`open/open"}},
		{"echo \\", []string{"word:echo/cmd", "word:\\/open"}},
		{"echo $'open\\'", []string{"word:echo/cmd", "word:$'open\\'/open"}},
		{"x=(open", []string{"word:x=(open/cmd/open"}},
		{"echo \xff\xfe 'é\xc3'", []string{"word:echo/cmd", "word:\xff\xfe", "word:'é\xc3'"}},
		{"echo \\\xc3\xa9x", []string{"word:echo/cmd", "word:\\\xc3\xa9x"}},
		{")", []string{")"}},
		{"a ) b", []string{"word:a/cmd", ")", "word:b/cmd"}},
	} {
		got := describe(tt.src, lex(t, tt.src))
		if diff := cmp.Diff(tt.want, got); diff != "" {
			t.Errorf("Lex(%q) mismatch (-want +got):\n%s", tt.src, diff)
		}
	}
}

func TestLexParts(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want []string
	}{
		{"abc", []string{"lit:abc"}},
		{"a'b'\"c\"d", []string{"lit:a", "sq:'b'", "dq:\"c\"", "lit:d"}},
		{"\\a\\\nb", []string{"esc:\\a", "cont:\\\n", "lit:b"}},
		{"$x$1$@${y}$", []string{"param:$x", "param:$1", "param:$@", "param:${y}", "lit:$"}},
		{"$'a'$\"b\"", []string{"dsq:$'a'", "ddq:$\"b\""}},
		{"$(a)$((b))`c`", []string{"cmd:$(a)", "arith:$((b))", "bq:`c`"}},
		{"<(a)>(b)", []string{"proc:<(a)", "proc:>(b)"}},
		{"x=(a)", []string{"lit:x=", "array:(a)"}},
		{"${a:-'}'}", []string{"param:${a:-'}'}"}},
		{"${a:-{b}}", []string{"param:${a:-{b}}"}},
		{"$((a) b)", []string{"arith:$((a) b)/open"}},
		{"`a \\` b`", []string{"bq:`a \\` b`"}},
		{"$(case x in a) b;; esac)", []string{"cmd:$(case x in a) b;; esac)"}},
		{"$( (a) )", []string{"cmd:$( (a) )"}},
	} {
		tokens := lex(t, tt.src)
		if len(tokens) != 1 {
			t.Errorf("Lex(%q) = %q, want one word", tt.src, describe(tt.src, tokens))
			continue
		}
		if diff := cmp.Diff(tt.want, describeParts(tt.src, tokens[0].Parts)); diff != "" {
			t.Errorf("parts of %q mismatch (-want +got):\n%s", tt.src, diff)
		}
	}
}

func TestLexPatternGroups(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		dialect dialect.Dialect
		src     string
		tokens  []string
		parts   [][]string
	}{
		{
			dialect.Bash, "ls @(a|b)x +(y) !(z)",
			[]string{"word:ls/cmd", "word:@(a|b)x", "word:+(y)", "word:!(z)"},
			[][]string{{"lit:ls"}, {"extglob:@(a|b)", "lit:x"}, {"extglob:+(y)"}, {"extglob:!(z)"}},
		},
		{dialect.Bash, "ls *(a(b)c)", []string{"word:ls/cmd", "word:*(a(b)c)"}, [][]string{{"lit:ls"}, {"extglob:*(a(b)c)"}}},
		{dialect.Bash, "f@()", []string{"word:f@/cmd", "(", ")"}, nil},
		{dialect.Bash, "ls *.(c|h)", []string{"word:ls/cmd", "word:*.", "(", "word:c/cmd", "|", "word:h/cmd", ")"}, nil},
		{dialect.Bash, "ls ?(a", []string{"word:ls/cmd", "word:?(a/open"}, [][]string{{"lit:ls"}, {"extglob:?(a/open"}}},
		{dialect.Zsh, "ls *.(c|h)(N)", []string{"word:ls/cmd", "word:*.(c|h)(N)"}, [][]string{{"lit:ls"}, {"lit:*.", "group:(c|h)", "group:(N)"}}},
		{dialect.Zsh, "ls *(e:'(x)':)", []string{"word:ls/cmd", "word:*(e:'(x)':)"}, [][]string{{"lit:ls"}, {"lit:*", "group:(e:'(x)':)"}}},
		{dialect.Zsh, "echo @(a)", []string{"word:echo/cmd", "word:@(a)"}, [][]string{{"lit:echo"}, {"lit:@", "group:(a)"}}},
		{dialect.Zsh, "f() { :; }", []string{"word:f/cmd", "(", ")", "{", "word::/cmd", ";", "}"}, nil},
		{dialect.Zsh, "ls x(N", []string{"word:ls/cmd", "word:x(N/open"}, [][]string{{"lit:ls"}, {"lit:x", "group:(N/open"}}},
		{dialect.Ksh, "ls ~(E)re {2}(ab) a(b)", []string{"word:ls/cmd", "word:~(E)re", "word:{2}(ab)", "word:a", "(", "word:b/cmd", ")"}, nil},
		{dialect.Ksh, "ls @(a)", []string{"word:ls/cmd", "word:@(a)"}, [][]string{{"lit:ls"}, {"extglob:@(a)"}}},
	} {
		tokens := lexAs(t, tt.src, tt.dialect)
		if diff := cmp.Diff(tt.tokens, describe(tt.src, tokens)); diff != "" {
			t.Errorf("Lex(%q, %s) mismatch (-want +got):\n%s", tt.src, tt.dialect, diff)
			continue
		}
		for i, want := range tt.parts {
			if diff := cmp.Diff(want, describeParts(tt.src, tokens[i].Parts)); diff != "" {
				t.Errorf("parts of word %d of %q in %s mismatch (-want +got):\n%s", i, tt.src, tt.dialect, diff)
			}
		}
	}
}

func TestLexNestedSequences(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		src  string
		want [][]string
	}{
		{"echo $(If x; then y; fi)", [][]string{{"word:If/cmd", "word:x", ";", "word:then/cmd/kw", "word:y/cmd", ";", "word:fi/cmd/kw"}}},
		{"x=$(( a -lt (b) ))", [][]string{{"word:a/arith", "word:-lt/arith", "(/arith", "word:b/arith", ")/arith"}}},
		{"echo \"${a:-$(b)}\" `c`", [][]string{{"word:b/cmd"}, {"word:c/cmd"}}},
		{"cat <(a | b) x=(c d)", [][]string{{"word:a/cmd", "|", "word:b/cmd"}}},
		{"x=(If $(c) d)", [][]string{{"word:If", "word:$(c)", "word:d"}, {"word:c/cmd"}}},
		{"echo $(echo $(echo deep))", [][]string{{"word:echo/cmd", "word:$(echo deep)"}, {"word:echo/cmd", "word:deep"}}},
	} {
		tokens := lex(t, tt.src)
		var got [][]string
		seen := map[*Token]bool{&tokens[0]: true}
		for seq := range All(tokens) {
			if !seen[&seq[0]] {
				seen[&seq[0]] = true
				got = append(got, describe(tt.src, seq))
			}
		}
		if diff := cmp.Diff(tt.want, got); diff != "" {
			t.Errorf("nested sequences of %q mismatch (-want +got):\n%s", tt.src, diff)
		}
	}
}

func TestLexRejectsTooDeepNesting(t *testing.T) {
	t.Parallel()

	src := strings.Repeat("$(", maxDepth+1)
	if _, err := Lex(src, dialect.Bash); err == nil || !strings.Contains(err.Error(), "nested deeper") {
		t.Errorf("Lex of %d nested substitutions: err %v, want a nesting error", maxDepth+1, err)
	}
	if _, err := Lex(strings.Repeat("$(", maxDepth/2), dialect.Bash); err != nil {
		t.Errorf("Lex of %d nested substitutions: %v", maxDepth/2, err)
	}
}

func TestAssignmentPrefix(t *testing.T) {
	t.Parallel()

	for word, want := range map[string]int{
		"x=1":          2,
		"x+=1":         3,
		"_a1=":         4,
		"a[i]=(":       5,
		"a[b[1]]+=":    9,
		"a[$(x)]=y":    8,
		"1x=":          -1,
		"=x":           -1,
		"x":            -1,
		"a[1=":         -1,
		"\"x\"=1":      -1,
		"x-y=1":        -1,
		"name=a=b=c":   5,
		"PATH+=:/bin":  6,
		"a[1]x=":       -1,
		"a[1]+x=":      -1,
		"x[\"]\"]=1":   -1,
		"abc=\"$x\"":   4,
		"tail=((3+4))": 5,
	} {
		n, ok := AssignmentPrefix(word)
		if !ok {
			n = -1
		}
		if n != want {
			t.Errorf("AssignmentPrefix(%q) = %d, want %d", word, n, want)
		}
	}
}
