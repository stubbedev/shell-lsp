package embedded

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var knownInvalid = map[string]string{
	"$prefix": "extra characters after command",
}

const (
	ownCorpus             = "realworld.bash"
	minimumCorpusPrograms = 140
)

type corpusScript struct {
	name    string
	text    string
	dialect dialect.Dialect
}

func lintFixtureScripts(t *testing.T) []corpusScript {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("..", "lint", "testdata", "fixtures", "*", "*.fixture"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("listing lint fixtures: %v (%d found)", err, len(paths))
	}
	var out []corpusScript
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var current *corpusScript
		for line := range strings.Lines(string(raw)) {
			switch {
			case strings.HasPrefix(line, "### "):
				out = append(out, corpusScript{name: path + ": " + strings.TrimSpace(line[4:]), dialect: dialect.Bash})
				current = &out[len(out)-1]
			case strings.HasPrefix(line, ">>"):
				current = nil
			case current != nil:
				current.text += line
			}
		}
	}
	return out
}

func parserCorpusScripts(t *testing.T) []corpusScript {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("..", "parser", "testdata", "corpus", "*", "*"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("listing the parser corpus: %v (%d found)", err, len(paths))
	}
	var out []corpusScript
	for _, path := range paths {
		if filepath.Ext(path) == ".golden" {
			continue
		}
		d, ok := dialect.FromName(filepath.Base(filepath.Dir(path)))
		if !ok {
			t.Fatalf("corpus directory of %s is not a dialect name", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		out = append(out, corpusScript{name: path, text: string(raw), dialect: d})
	}
	return out
}

func ownScripts(t *testing.T) []corpusScript {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", ownCorpus))
	if err != nil {
		t.Fatalf("reading the embedded-language corpus: %v", err)
	}
	return []corpusScript{{name: ownCorpus, text: string(raw), dialect: dialect.Bash}}
}

func FuzzFind(f *testing.F) {
	raw, err := os.ReadFile(filepath.Join("testdata", ownCorpus))
	if err != nil {
		f.Fatalf("reading the embedded-language corpus: %v", err)
	}
	for line := range strings.Lines(string(raw)) {
		f.Add(line)
	}
	f.Add("awk -f - <<'EOF'\n{ print\nEOF\n")
	f.Add("sed -f - <<-'EOF'\n\t\ts/a/b\n\tEOF\n")
	f.Add("sed -e 'a\\' -e \"x\\\"y\" -es/a/b/ f\n")
	f.Fuzz(func(t *testing.T, text string) {
		for _, p := range Find(parser.Parse(text, dialect.Bash)) {
			inBounds := func(what string, s parser.Span) {
				if s.Start < 0 || s.Start > s.End || s.End > len(text) {
					t.Fatalf("%s span %+v out of bounds in %q", what, s, text)
				}
			}
			p.Regions(func(s parser.Span) { inBounds("region", s) })
			for _, tok := range p.Result.Tokens {
				p.Spans(tok.Start, tok.End, func(s parser.Span) { inBounds("token", s) })
				if _, ok := HoverAt([]Program{p}, p.Span(tok.Start, tok.End).Start); ok && tok.Modifiers == 0 {
					t.Fatalf("hover on a non-builtin token %+v in %q", tok, text)
				}
			}
			if e := p.Result.Error; e != nil {
				inBounds("error", p.Span(e.Start, e.End))
			}
		}
	})
}

func TestCorpusProgramsParse(t *testing.T) {
	t.Parallel()

	checked := 0
	scripts := slices.Concat(lintFixtureScripts(t), parserCorpusScripts(t), ownScripts(t))
	for _, script := range scripts {
		tree := parser.Parse(script.text, script.dialect)
		if script.name == ownCorpus && len(tree.Errors) > 0 {
			t.Fatalf("%s has shell syntax errors: %+v", ownCorpus, tree.Errors)
		}
		for _, p := range Find(tree) {
			checked++
			want, invalid := knownInvalid[p.Text]
			switch {
			case p.Result.Error == nil && invalid:
				t.Errorf("%s: %s program %q parses, want %q", script.name, p.Language, p.Text, want)
			case p.Result.Error != nil && p.Result.Error.Message != want:
				t.Errorf("%s: %s program %q: unexpected error %q at %d", script.name, p.Language, p.Text, p.Result.Error.Message, p.Result.Error.Start)
			}
		}
	}
	if checked < minimumCorpusPrograms {
		t.Errorf("checked %d embedded programs in the corpus, want at least %d", checked, minimumCorpusPrograms)
	}
}
