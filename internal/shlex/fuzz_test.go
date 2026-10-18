package shlex

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func FuzzLex(f *testing.F) {
	for _, seed := range []string{
		"echo hi; ls -l | wc -l && true || false &",
		"if [[ $a =~ ^(x|y)$ && -f b ]]; then echo \"${c:-$(d)}\"; fi",
		"for ((i=0; i<3; i++)); do echo $((i * 2)); done",
		"case $x in (a|b) y;; c) z;& *) w;;& esac",
		"cat <<-EOF <<'E2'\n\tbody $x\n\tEOF\nraw\nE2\n",
		"x=(a 'b c' $(d)) y+=([k]=v) declare -a z=(1)",
		"diff <(sort a) >(tee b) `echo \\`nested\\``",
		"echo $'a\\'b' $\"c\" \\\n d # comment",
		"f() { (cd x && ((n++))); } ; ((a) b) $((a)+(b))",
		"echo 'open \"open $(open ${open `open",
		"\xff\xfe\\\xc3 $\xc3\xa9",
		"ls *.(c|h)(N) @(a|b) +(x) !(y) ~(E)re {2}(ab) *(e:'(x)':)",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for _, d := range []dialect.Dialect{dialect.Bash, dialect.Zsh, dialect.Ksh, dialect.Posix} {
			lexChecked(t, src, d)
		}
	})
}

func lexChecked(t *testing.T, src string, d dialect.Dialect) {
	t.Helper()
	tokens, err := Lex(src, d)
	if err != nil {
		if !strings.Contains(err.Error(), "nested deeper") {
			t.Fatalf("Lex(%q, %s): unexpected error %v", src, d, err)
		}
		return
	}
	end := checkSequence(t, src, tokens, 0, len(src), true)
	if gap := src[end:]; !blank(gap) {
		t.Fatalf("Lex(%q, %s): bytes %q after the last token are not covered", src, d, gap)
	}
}

func blank(gap string) bool {
	return strings.Trim(strings.ReplaceAll(gap, "\\\n", ""), " \t") == ""
}

func checkSequence(t *testing.T, src string, tokens []Token, from, to int, top bool) int {
	t.Helper()
	at := from
	for i, tok := range tokens {
		if tok.Start < at || tok.End <= tok.Start || tok.End > to {
			t.Fatalf("Lex(%q): token %v [%d,%d) is empty, overlaps or leaves [%d,%d)", src, tok.Kind, tok.Start, tok.End, at, to)
		}
		if gap := src[at:tok.Start]; (top || i > 0) && !blank(gap) {
			t.Fatalf("Lex(%q): bytes %q before token %v at %d are not covered", src, gap, tok.Kind, tok.Start)
		}
		if tok.Kind == Word {
			checkParts(t, src, tok.Parts, tok.Start, tok.End, true)
		} else if tok.Parts != nil {
			t.Fatalf("Lex(%q): %v token at %d has parts", src, tok.Kind, tok.Start)
		}
		at = tok.End
	}
	return at
}

func checkParts(t *testing.T, src string, parts []Part, from, to int, tile bool) {
	t.Helper()
	if tile && (len(parts) == 0 || parts[0].Start != from || parts[len(parts)-1].End != to) {
		t.Fatalf("Lex(%q): parts %v do not tile the word [%d,%d)", src, parts, from, to)
	}
	at := from
	for _, p := range parts {
		if p.Start < at || p.End <= p.Start || p.End > to || tile && p.Start != at {
			t.Fatalf("Lex(%q): part %v [%d,%d) is empty, overlaps or leaves [%d,%d)", src, p.Kind, p.Start, p.End, at, to)
		}
		checkParts(t, src, p.Parts, p.Start, p.End, false)
		checkSequence(t, src, p.Tokens, p.Start, p.End, false)
		at = p.End
	}
}
