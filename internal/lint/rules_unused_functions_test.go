package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestConventionallySourcedFunctionsAreCommands(t *testing.T) {
	t.Parallel()

	script := "#!/usr/bin/env zsh\nmkcd() { mkdir -p -- \"$1\" && cd -- \"$1\" || return; }\nprint -r -- ready\n"
	for _, tc := range []struct {
		sourced bool
		want    []string
	}{
		{sourced: false, want: []string{"SL2001 2:1"}},
		{sourced: true, want: nil},
	} {
		var got []string
		for _, f := range Check(parser.Parse(script, dialect.Zsh), nil, Options{DialectSource: DialectDetected, SourcedByConvention: tc.sourced}) {
			got = append(got, f.Code.String()+" "+position(script, f.Span.Start))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("sourced by convention %v: got %v, want %v", tc.sourced, got, tc.want)
		}
	}
}
