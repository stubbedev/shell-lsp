package hostnav

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

func (tr *tree) fixes(name string) []string {
	g := tr.group(name)
	text := g.Self.Doc.Source().Text()
	var out []string
	for _, f := range g.Fixes(parser.Span{End: len(text)}) {
		switch {
		case f.Create != "":
			rel, _ := filepath.Rel(tr.dir, f.Create)
			out = append(out, f.Title+" -> "+filepath.ToSlash(rel))
		default:
			out = append(out, f.Title+" -> "+textedit.Apply(text, f.Edits))
		}
	}
	return out
}

func TestJustfileFixes(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"justfile":  "build: compil\n    echo {{uppercse(\"a\")}}\ncompile:\n    cc\n",
		"main.just": "import 'ci.just'\n",
	})
	want := []string{
		"Change to `compile` -> build: compile\n    echo {{uppercse(\"a\")}}\ncompile:\n    cc\n",
		"Add recipe `compil` -> build: compil\n    echo {{uppercse(\"a\")}}\ncompile:\n    cc\n\ncompil:\n",
		"Change to `uppercase` -> build: compil\n    echo {{uppercase(\"a\")}}\ncompile:\n    cc\n",
	}
	if got := tr.fixes("justfile"); !slices.Equal(got, want) {
		t.Errorf("fixes =\n%q\nwant\n%q", got, want)
	}
	if got, want := tr.fixes("main.just"), []string{"Create `ci.just` -> ci.just"}; !slices.Equal(got, want) {
		t.Errorf("fixes of main.just = %q, want %q", got, want)
	}
}

func TestMakefileFixes(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{
		"Makefile": ".PHONY: tesst\nall:\n\t@echo $(subt a,b,c)\n\ntest:\n\ttrue\n",
		"rules.mk": "all:\n\ttrue\n    echo spaces\n",
	})
	want := []string{
		"Change to `test` -> .PHONY: test\nall:\n\t@echo $(subt a,b,c)\n\ntest:\n\ttrue\n",
		"Add target `tesst` -> .PHONY: tesst\nall:\n\t@echo $(subt a,b,c)\n\ntest:\n\ttrue\n\ntesst:\n",
		"Change to `subst` -> .PHONY: tesst\nall:\n\t@echo $(subst a,b,c)\n\ntest:\n\ttrue\n",
	}
	if got := tr.fixes("Makefile"); !slices.Equal(got, want) {
		t.Errorf("fixes =\n%q\nwant\n%q", got, want)
	}
	if got, want := tr.fixes("rules.mk"), []string{"Start the recipe line with a tab -> all:\n\ttrue\n\techo spaces\n"}; !slices.Equal(got, want) {
		t.Errorf("fixes of rules.mk = %q, want %q", got, want)
	}
	phony := newTree(t, map[string]string{"Makefile": "all:\n\ttrue\n\nclean:\n\trm -f app\n"})
	if got, want := phony.fixes("Makefile"), []string{"Add `clean` to .PHONY -> all:\n\ttrue\n\n.PHONY: clean\nclean:\n\trm -f app\n"}; !slices.Equal(got, want) {
		t.Errorf("fixes for a target missing from .PHONY = %q, want %q", got, want)
	}
}

func TestNixFixes(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"default.nix": "let\n  a = 1;\n  b = 2;\nin { b = b; c = (b); }\n"})
	want := []string{
		"Remove `a` -> let\n  b = 2;\nin { b = b; c = (b); }\n",
		"Use `inherit b;` -> let\n  a = 1;\n  b = 2;\nin { inherit b; c = (b); }\n",
		"Remove the parentheses -> let\n  a = 1;\n  b = 2;\nin { b = b; c = b; }\n",
	}
	if got := tr.fixes("default.nix"); !slices.Equal(got, want) {
		t.Errorf("fixes =\n%q\nwant\n%q", got, want)
	}
	g := tr.group("default.nix")
	text := g.Self.Doc.Source().Text()
	if got, want := textedit.Apply(text, lint.FindingsSafeEdits(g.Findings())), "let\n  b = 2;\nin { inherit b; c = b; }\n"; got != want {
		t.Errorf("fix-all gives %q, want %q", got, want)
	}
}

func TestNixFixAllLeavesReviewFixesAlone(t *testing.T) {
	t.Parallel()

	tr := newTree(t, map[string]string{"default.nix": "{ a, b }: x: a\n"})
	g := tr.group("default.nix")
	if edits := lint.FindingsSafeEdits(g.Findings()); len(edits) != 0 {
		t.Errorf("fix-all edits a function's interface: %+v", edits)
	}
	if got := tr.fixes("default.nix"); len(got) != 2 {
		t.Errorf("quick fixes = %q", got)
	}
	for _, fix := range g.Fixes(parser.Span{End: len(g.Self.Doc.Source().Text())}) {
		if fix.Preferred {
			t.Errorf("%q is preferred, but needs a review", fix.Title)
		}
	}
}
