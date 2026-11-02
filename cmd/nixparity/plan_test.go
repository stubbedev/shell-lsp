package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
)

func tokenTexts(text string) []string {
	var out []string
	for _, t := range nixTokens(text) {
		out = append(out, text[t.start:t.end])
	}
	return out
}

func TestNixTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, text string
		want       []string
	}{
		{"keywords are left out", "let a = b; in a", []string{"a", "b", "a"}},
		{"names keep dashes and primes", "self' foo-bar_2", []string{"self'", "foo-bar_2"}},
		{"line comments end at the newline", "a # b c\nd", []string{"a", "d"}},
		{"a hash inside a word is no comment", "x#y", []string{"x", "y"}},
		{"block comments are skipped", "a /* b */ c", []string{"a", "c"}},
		{"an unclosed block opener stays text", "$out/lib/* a", []string{"out", "lib", "a"}},
		{"digits do not start a name", "2a b", []string{"b"}},
		{"shell strings are sampled", "x = ''echo $pname'';", []string{"x", "echo", "pname"}},
	}
	for _, tt := range tests {
		if got := tokenTexts(tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("%s: tokens of %q = %q, want %q", tt.name, tt.text, got, tt.want)
		}
	}
}

func manyTokens(n int) []token {
	out := make([]token, n)
	for i := range out {
		out[i] = token{start: i * 2, end: i*2 + 1}
	}
	return out
}

func TestSampleTokensIsDeterministicAndSorted(t *testing.T) {
	t.Parallel()

	tokens := manyTokens(100)
	first := sampleTokens(7, "a.nix", tokens, 10)
	if !slices.Equal(first, sampleTokens(7, "a.nix", tokens, 10)) {
		t.Error("the same seed and file sampled different tokens")
	}
	if slices.Equal(first, sampleTokens(8, "a.nix", tokens, 10)) {
		t.Error("another seed sampled the same tokens")
	}
	if slices.Equal(first, sampleTokens(7, "b.nix", tokens, 10)) {
		t.Error("another file sampled the same tokens")
	}
	if len(first) != 10 || !slices.IsSortedFunc(first, func(a, b token) int { return a.start - b.start }) {
		t.Errorf("sample = %v, want 10 tokens in source order", first)
	}
	if got := sampleTokens(7, "a.nix", tokens[:3], 10); !slices.Equal(got, tokens[:3]) {
		t.Errorf("a short file sampled %v, want every token", got)
	}
}

func TestSampleFilesIsDeterministicAndSorted(t *testing.T) {
	t.Parallel()

	rels := []string{"a.nix", "b.nix", "c.nix", "d.nix", "e.nix", "f.nix"}
	first := sampleFiles(3, rels, 3)
	if !slices.Equal(first, sampleFiles(3, rels, 3)) {
		t.Error("the same seed sampled different files")
	}
	if len(first) != 3 || !slices.IsSorted(first) {
		t.Errorf("sample = %q, want three sorted files", first)
	}
	if got := sampleFiles(3, rels, 6); !slices.Equal(got, rels) {
		t.Errorf("a limit covering every file sampled %q", got)
	}
}

func TestPlanQueriesAskDocumentFeaturesOnceAndProbeInsideTokens(t *testing.T) {
	t.Parallel()

	doc := docstore.NewDocument("file:///w/a.nix", languageID, 1, "é = pname;\n")
	queries := planQueries([]document{{rel: "a.nix", doc: doc}}, settings{seed: 1, positions: 5, fileLimit: 1})
	perFeature := map[feature]int{}
	for _, q := range queries {
		perFeature[q.feature]++
	}
	for _, f := range allFeatures() {
		if perFeature[f] != 1 {
			t.Errorf("%s was asked %d times, want once for the one token of the file", f, perFeature[f])
		}
	}
	for _, q := range queries {
		switch q.feature {
		case featureDefinition:
			if q.position.Character != 4 {
				t.Errorf("definition probes UTF-16 column %d, want 4", q.position.Character)
			}
		case featureCompletion:
			if q.position.Character != 6 {
				t.Errorf("completion probes UTF-16 column %d, want 6", q.position.Character)
			}
		}
	}
}

func TestBuildPlanSkipsUnusableFilesAndHiddenDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel string, data []byte) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("sub/b.nix", []byte("b"))
	write("a.nix", []byte("a"))
	write("notes.txt", []byte("text"))
	write(".direnv/c.nix", []byte("c"))
	write("binary.nix", []byte{0xff, 0xfe})
	p, err := buildPlan(root, settings{seed: 1, positions: 1, fileLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, d := range p.docs {
		rels = append(rels, d.rel)
	}
	if want := []string{"a.nix", "sub/b.nix"}; !slices.Equal(rels, want) {
		t.Errorf("documents = %q, want %q", rels, want)
	}
	if want := []string{"binary.nix"}; !slices.Equal(p.skipped, want) {
		t.Errorf("skipped = %q, want %q", p.skipped, want)
	}
	if _, err := buildPlan(t.TempDir(), settings{seed: 1, positions: 1, fileLimit: 10}); err == nil {
		t.Error("a directory without Nix files gave a plan")
	}
}
