package lint

import (
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
)

func checkSourcing(src, lib string, d dialect.Dialect) []Finding {
	tree := parser.Parse(src, d)
	libTree := parser.Parse(lib, d)
	var sources []Source
	for _, c := range tree.Commands {
		if c.IsSourceBuiltin() {
			sources = append(sources, Source{Command: c.Span, Tree: libTree, Options: shelloptions.EffectOf(libTree)})
		}
	}
	return Check(tree, nil, Options{Profile: ProfileStrict, Sources: sources})
}

func TestBashExtglobFromASourceOnTheSameLineIsNotOnYet(t *testing.T) {
	t.Parallel()

	lib := "shopt -s extglob\n"
	if got := checkSourcing("#!/bin/bash\nsource \"$(dirname \"$0\")/lib.sh\"\nrm -- !(keep).log\n", lib, dialect.Bash); len(got) != 0 {
		t.Errorf("findings with the source on the line before = %v, want none", got)
	}
	got := checkSourcing("#!/bin/bash\nsource \"$(dirname \"$0\")/lib.sh\"; rm -- !(keep).log\n", lib, dialect.Bash)
	if len(got) != 1 || got[0].Code != codePatternNeedsOption || !strings.Contains(got[0].Message, "together with `source \"$(dirname \"$0\")/lib.sh\"`, which runs `shopt -s extglob`, before it") {
		t.Errorf("findings with the source on the same line = %+v, want one SL2016 naming the source command", got)
	}
}
