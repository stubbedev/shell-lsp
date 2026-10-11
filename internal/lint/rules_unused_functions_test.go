package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestRCFileFunctionsAreCommands(t *testing.T) {
	t.Parallel()

	script := "#!/usr/bin/env zsh\nmkcd() { mkdir -p -- \"$1\" && cd -- \"$1\" || return; }\nprint -r -- ready\n"
	for _, tc := range []struct {
		rcFile bool
		want   []string
	}{
		{rcFile: false, want: []string{"SL2001 2:1"}},
		{rcFile: true, want: nil},
	} {
		var got []string
		for _, f := range Check(parser.Parse(script, dialect.Zsh), nil, Options{DialectSource: DialectDetected, RCFile: tc.rcFile}) {
			got = append(got, f.Code.String()+" "+position(script, f.Span.Start))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("rc file %v: got %v, want %v", tc.rcFile, got, tc.want)
		}
	}
}
