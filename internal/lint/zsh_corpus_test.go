package lint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var zshCorpusFindings = map[string][]string{
	"_deploy": {"SC2034 5:10 targets"},
	"dotzshrc.zsh": {
		"SC2139 58:10 {md,txt}=",
		"SC2164 62:28 cd -- $1",
		"SC2164 65:2 cd ${(j:/:)${(s::)${(l:n::.:)}//./..}}",
		"SC1090 98:9 $rc",
		"SC1090 102:35 ~/.zshrc.local",
	},
	"errors.zsh":     {},
	"expansions.zsh": {},
	"forms.zsh": {
		"SL2016 58:2 (#i)",
		"SL2016 59:13 ##",
		"SC2046 76:13 $(() { print inner })",
		"SC2162 87:1 read",
		"SC2162 89:1 read",
	},
	"plugin.zsh": {},
	"prompt.zsh": {"SC1090 20:9 $plugin"},
	"zshrc.zsh":  {"SC2034 4:1 opts=(a 1 b 2)"},
}

func TestZshCorpusFindings(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("..", "parser", "testdata", "corpus", "zsh", "*"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if filepath.Ext(path) == ".golden" {
			continue
		}
		name := filepath.Base(path)
		seen[name] = true
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		script := string(raw)
		got := []string{}
		for _, f := range Check(parser.Parse(script, dialect.Zsh), nil, Options{DialectSource: DialectDetected}) {
			got = append(got, f.Code.String()+" "+position(script, f.Span.Start)+" "+strings.TrimSpace(script[f.Span.Start:min(f.Span.End, f.Span.Start+40)]))
		}
		want, ok := zshCorpusFindings[name]
		if !ok {
			t.Errorf("%s: no expected findings listed; got %q", name, got)
			continue
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s:\ngot  %q\nwant %q", name, got, want)
		}
	}
	for name := range zshCorpusFindings {
		if !seen[name] {
			t.Errorf("%s is listed but not in the corpus", name)
		}
	}
}
