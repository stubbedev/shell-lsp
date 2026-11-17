package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelloptions"
)

// checkSourcing lints src as if every source command in it was followed to a file whose text is lib.
func checkSourcing(src, lib string, d dialect.Dialect) []Finding {
	tree := parser.Parse(src, d)
	libTree := parser.Parse(lib, d)
	effect := shelloptions.TopLevel(libTree, nil)
	var sources []Source
	var sourced []shelloptions.Sourced
	for _, c := range tree.Commands {
		if c.IsSourceBuiltin() {
			sources = append(sources, Source{Command: c.Span, Tree: libTree})
			sourced = append(sourced, shelloptions.Sourced{Command: c.Span, Effect: effect})
		}
	}
	return Check(tree, nil, Options{Profile: ProfileStrict, Sources: sources, SourcedOptions: sourced})
}

func TestOptionsOfSourcedFilesReachTheScript(t *testing.T) {
	t.Parallel()

	src := "#!/usr/bin/env zsh\nsource \"$(dirname \"$0\")/lib.zsh\"\nif [[ ${1-} == (#i)yes ]]; then\n  print -r -- ${1-} | tr \"[:lower:]\" \"[:upper:]\"\nfi\n"
	var got []string
	for _, f := range checkSourcing(src, "", dialect.Zsh) {
		got = append(got, f.Code.String())
	}
	if want := []string{"SL2016", "SC2312"}; !slices.Equal(got, want) {
		t.Fatalf("findings when the sourced file sets nothing = %v, want %v", got, want)
	}
	if got := checkSourcing(src, "setopt pipe_fail extended_glob\n", dialect.Zsh); len(got) != 0 {
		t.Errorf("findings when the sourced file sets pipe_fail and extended_glob = %v, want none", got)
	}
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
