package format

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func TestEditsReproduceTarget(t *testing.T) {
	t.Parallel()

	words := []string{"a\n", "b\n", "c\n", "\tif x\n", "fi\n", "echo\n", "", "z"}
	for seed := range 500 {
		rng := rand.New(rand.NewPCG(uint64(seed), 7))
		build := func() string {
			var sb strings.Builder
			for range rng.IntN(30) {
				sb.WriteString(words[rng.IntN(len(words)-2)])
			}
			if rng.IntN(3) == 0 {
				sb.WriteString(words[len(words)-1])
			}
			return sb.String()
		}
		before, after := build(), build()
		edits := Edits(before, after)
		if got := textedit.Apply(before, edits); got != after {
			t.Fatalf("seed %d: textedit.Apply(Edits) = %q, want %q (edits %+v)", seed, got, after, edits)
		}
		for i := 1; i < len(edits); i++ {
			if edits[i].Start < edits[i-1].End {
				t.Fatalf("seed %d: overlapping edits %+v", seed, edits)
			}
		}
	}
}

func TestEditsAreMinimalForLocalChanges(t *testing.T) {
	t.Parallel()

	before := "a\nb\nc\nd\ne\n"
	after := "a\nB\nc\nd\nE\n"
	edits := Edits(before, after)
	if len(edits) != 2 || before[edits[0].Start:edits[0].End] != "b\n" || edits[1].NewText != "E\n" {
		t.Errorf("Edits = %+v, want two single-line replacements", edits)
	}
	if len(Edits(before, before)) != 0 {
		t.Error("identical texts produced edits")
	}
}

func TestUnified(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ before, after, want string }{
		"identical": {"a\nb\n", "a\nb\n", ""},
		"one hunk": {
			"#!/bin/bash\necho $1\nfoo=bar\ncd /tmp\nif [ $x == y ]; then echo a; fi\n",
			"#!/bin/bash\necho \"$1\"\nfoo=bar\ncd /tmp || exit\nif [ \"$x\" == y ]; then echo a; fi\n",
			"--- a/x\n+++ b/x\n@@ -1,5 +1,5 @@\n #!/bin/bash\n-echo $1\n+echo \"$1\"\n foo=bar\n-cd /tmp\n-if [ $x == y ]; then echo a; fi\n+cd /tmp || exit\n+if [ \"$x\" == y ]; then echo a; fi\n",
		},
		"two hunks": {
			"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n",
			"one\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\ntwelve\n",
			"--- a/x\n+++ b/x\n@@ -1,4 +1,4 @@\n-1\n+one\n 2\n 3\n 4\n@@ -9,4 +9,4 @@\n 9\n 10\n 11\n-12\n+twelve\n",
		},
		"insertion into empty":  {"", "echo\n", "--- a/x\n+++ b/x\n@@ -0,0 +1,1 @@\n+echo\n"},
		"missing final newline": {"a\nb", "a\nc", "--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n"},
	} {
		if got := Unified("a/x", "b/x", c.before, c.after); got != c.want {
			t.Errorf("%s: Unified =\n%s\nwant\n%s", name, got, c.want)
		}
	}
}

func TestUnifiedHunksReproduceBothSides(t *testing.T) {
	t.Parallel()

	words := []string{"a\n", "b\n", "c\n", "d\n", "e\n"}
	for seed := range 300 {
		rng := rand.New(rand.NewPCG(uint64(seed), 11))
		build := func() []string {
			out := make([]string, rng.IntN(40))
			for i := range out {
				out[i] = words[rng.IntN(len(words))]
			}
			return out
		}
		a, b := build(), build()
		patch := Unified("a", "b", strings.Join(a, ""), strings.Join(b, ""))
		if got, want := applyUnified(t, patch, a), strings.Join(b, ""); got != want {
			t.Fatalf("seed %d: applying\n%s\nto %q gave %q, want %q", seed, patch, a, got, want)
		}
	}
}

func applyUnified(t *testing.T, patch string, a []string) string {
	t.Helper()
	var out strings.Builder
	at := 0
	for line := range strings.SplitSeq(strings.TrimSuffix(patch, "\n"), "\n") {
		switch {
		case line == "" || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
		case strings.HasPrefix(line, "@@"):
			var from, count, to, toCount int
			if _, err := fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &from, &count, &to, &toCount); err != nil {
				t.Fatalf("hunk header %q: %v", line, err)
			}
			start := max(from-1, 0)
			if count == 0 {
				start = from
			}
			for ; at < start; at++ {
				out.WriteString(a[at])
			}
		case line[0] == ' ' || line[0] == '-':
			if a[at] != line[1:]+"\n" {
				t.Fatalf("line %d is %q, the patch expects %q", at+1, a[at], line[1:])
			}
			if line[0] == ' ' {
				out.WriteString(a[at])
			}
			at++
		case line[0] == '+':
			out.WriteString(line[1:] + "\n")
		default:
			t.Fatalf("unexpected patch line %q", line)
		}
	}
	for ; at < len(a); at++ {
		out.WriteString(a[at])
	}
	return out.String()
}

func TestFormat(t *testing.T) {
	t.Parallel()

	src := "if true;then\necho  hi\nfi\nf(){\nx=1\n}\n"
	got, err := Format(parser.Parse(src, dialect.Bash), Options{})
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	want := "if true; then\n\techo hi\nfi\nf() {\n\tx=1\n}\n"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
	spaces, err := Format(parser.Parse(src, dialect.Bash), Options{Indent: 2, FunctionNextLine: true})
	if err != nil {
		t.Fatalf("Format(spaces) error = %v", err)
	}
	if !strings.Contains(spaces, "  echo hi") || !strings.Contains(spaces, "f()\n{") {
		t.Errorf("Format(spaces) = %q", spaces)
	}
}

func TestFormatOptions(t *testing.T) {
	t.Parallel()

	src := "case $x in\na) ls >out && echo `date` ;;\nesac\n"
	got, err := Format(parser.Parse(src, dialect.Bash), Options{SwitchCaseIndent: true, SpaceRedirects: true, Simplify: true})
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	for _, want := range []string{"\ta)", "> out"} {
		if !strings.Contains(got, want) {
			t.Errorf("Format() = %q, lacks %q", got, want)
		}
	}
	minified, err := Format(parser.Parse("f() {\n\techo hi\n}\n", dialect.Bash), Options{Minify: true})
	if err != nil || strings.Contains(minified, "\t") {
		t.Errorf("Format(minify) = %q, %v", minified, err)
	}
}

func TestFormatLeavesTheTreeUntouched(t *testing.T) {
	t.Parallel()

	tree := parser.Parse("echo $(( $x + 1 ))\n", dialect.Bash)
	simplified, err := Format(tree, Options{Simplify: true})
	if err != nil || simplified != "echo $((x + 1))\n" {
		t.Fatalf("Format(simplify) = %q, %v", simplified, err)
	}
	plain, err := Format(tree, Options{})
	if err != nil || plain != "echo $(($x + 1))\n" {
		t.Errorf("Format after a simplifying format = %q, %v", plain, err)
	}
}

func TestFormatRefusesBrokenInput(t *testing.T) {
	t.Parallel()

	if _, err := Format(parser.Parse("if true; then\n", dialect.Bash), Options{}); err == nil || !strings.Contains(err.Error(), "syntax errors") {
		t.Errorf("Format(broken) error = %v", err)
	}
	if _, err := Format(parser.Parse("echo ${name@Q}\n", dialect.Ksh), Options{}); err == nil {
		t.Error("Format accepted a bash expansion operator in ksh")
	}
}

func TestFormatZsh(t *testing.T) {
	t.Parallel()

	got, err := Format(parser.Parse("for f in **/*(.); do\nprint -r -- ${(U)f:t}\ndone\n", dialect.Zsh), Options{})
	if err != nil {
		t.Fatalf("Format(zsh) error = %v", err)
	}
	if !strings.Contains(got, "\tprint -r -- ${(U)f:t}") {
		t.Errorf("Format(zsh) = %q", got)
	}
}

func TestCorpusFormatsIdempotently(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob(filepath.Join("..", "parser", "testdata", "corpus", "*", "*"))
	if err != nil {
		t.Fatalf("listing the parser corpus: %v", err)
	}
	styles := []Options{
		{},
		{Simplify: true},
		{Minify: true},
		{Indent: 2, BinaryNextLine: true, SwitchCaseIndent: true, SpaceRedirects: true, FunctionNextLine: true},
	}
	for _, fixture := range fixtures {
		if filepath.Ext(fixture) == ".golden" {
			continue
		}
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()

			d, ok := dialect.FromName(filepath.Base(filepath.Dir(fixture)))
			if !ok {
				t.Fatalf("corpus directory %s is not a dialect name", filepath.Dir(fixture))
			}
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}
			tree := parser.Parse(string(src), d)
			if name := strings.TrimSuffix(filepath.Base(fixture), filepath.Ext(fixture)); name == "errors" || name == "broken" {
				return
			}
			if len(tree.Errors) > 0 {
				t.Fatalf("syntax errors %v", tree.Errors)
			}
			for _, opts := range styles {
				got, err := Format(tree, opts)
				if err != nil {
					t.Fatalf("Format(%+v) error = %v", opts, err)
				}
				if reparsed := parser.Parse(got, d); len(reparsed.Errors) > 0 {
					t.Errorf("Format(%+v) output has syntax errors %v:\n%s", opts, reparsed.Errors, got)
				}
			}
		})
	}
}

func TestRangeEdits(t *testing.T) {
	t.Parallel()

	edits := []textedit.Edit{{Start: 0, End: 4}, {Start: 10, End: 14}, {Start: 20, End: 24}}
	got := RangeEdits(edits, 11, 12)
	if len(got) != 1 || got[0].Start != 10 {
		t.Errorf("RangeEdits = %+v", got)
	}
}

func TestEditsScaleToLargeReformats(t *testing.T) {
	t.Parallel()

	var before, after strings.Builder
	for i := range 20000 {
		line := fmt.Sprintf("echo %d\n", i%50)
		before.WriteString(line)
		if i%3 == 0 {
			after.WriteString("\t" + line)
		} else {
			after.WriteString(line)
		}
	}
	edits := Edits(before.String(), after.String())
	if got := textedit.Apply(before.String(), edits); got != after.String() {
		t.Fatal("large diff does not reproduce the target")
	}
}
