package format

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func apply(text string, edits []Edit) string {
	var sb strings.Builder
	last := 0
	for _, e := range edits {
		sb.WriteString(text[last:e.Start])
		sb.WriteString(e.NewText)
		last = e.End
	}
	sb.WriteString(text[last:])
	return sb.String()
}

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
		if got := apply(before, edits); got != after {
			t.Fatalf("seed %d: apply(Edits) = %q, want %q (edits %+v)", seed, got, after, edits)
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

func TestFormat(t *testing.T) {
	t.Parallel()

	src := "if true;then\necho  hi\nfi\nf(){\nx=1\n}\n"
	got, err := Format(src, dialect.Bash, Options{})
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	want := "if true; then\n\techo hi\nfi\nf() {\n\tx=1\n}\n"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
	spaces, err := Format(src, dialect.Bash, Options{Indent: 2, FunctionNextLine: true})
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
	got, err := Format(src, dialect.Bash, Options{SwitchCaseIndent: true, SpaceRedirects: true, Simplify: true})
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	for _, want := range []string{"\ta)", "> out"} {
		if !strings.Contains(got, want) {
			t.Errorf("Format() = %q, lacks %q", got, want)
		}
	}
	minified, err := Format("f() {\n\techo hi\n}\n", dialect.Bash, Options{Minify: true})
	if err != nil || strings.Contains(minified, "\t") {
		t.Errorf("Format(minify) = %q, %v", minified, err)
	}
}

func TestFormatRefusesBrokenInput(t *testing.T) {
	t.Parallel()

	if _, err := Format("if true; then\n", dialect.Bash, Options{}); err == nil || !strings.Contains(err.Error(), "syntax errors") {
		t.Errorf("Format(broken) error = %v", err)
	}
	if _, err := Format("arr=(1 2)\n", dialect.Posix, Options{}); err == nil {
		t.Error("Format accepted bash arrays as POSIX sh")
	}
}

func TestFormatZsh(t *testing.T) {
	t.Parallel()

	got, err := Format("for f in **/*(.); do\nprint -r -- ${(U)f:t}\ndone\n", dialect.Zsh, Options{})
	if errors.Is(err, ErrNotIdempotent) {
		t.Skipf("zsh printing is not stable upstream yet: %v", err)
	}
	if err != nil {
		t.Fatalf("Format(zsh) error = %v", err)
	}
	if !strings.Contains(got, "\tprint -r -- ${(U)f:t}") {
		t.Errorf("Format(zsh) = %q", got)
	}
}

func TestRangeEdits(t *testing.T) {
	t.Parallel()

	edits := []Edit{{Start: 0, End: 4}, {Start: 10, End: 14}, {Start: 20, End: 24}}
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
	if got := apply(before.String(), edits); got != after.String() {
		t.Fatal("large diff does not reproduce the target")
	}
}
