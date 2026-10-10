package lint

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestDirectivePlacement(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		script string
		want   []string
	}{
		{"after an opening brace", "#!/bin/sh\n:\n{ # shellcheck disable=SC2086\n  echo $1; }\n", nil},
		{"after then", "#!/bin/sh\n:\nif true; then # shellcheck disable=SC2086\n  echo $1\nfi\n", nil},
		{"after a semicolon applies to the next command", "#!/bin/sh\n:\necho $2; # shellcheck disable=SC2086\necho $1\n", []string{"SC2086 3:6"}},
		{"after a command's words covers its own line", "#!/bin/sh\n:\necho $1 # shellcheck disable=SC2086\necho $2\n", []string{"SC1126 3:9", "SC2086 4:6"}},
		{"after a backslash continuation", "#!/bin/sh\n:\necho $1 \\\n  # shellcheck disable=SC2086\necho $2\n", []string{"SC1126 4:3", "SC2086 5:6"}},
		{"a run before a closer is reported once", "#!/bin/sh\nif true; then\n  :\n  # shellcheck disable=SC2086\n  # note\n  # shellcheck disable=SC2034\nfi\n", []string{"SC1123 4:3"}},
		{"before a closer has no effect", "#!/bin/sh\nfor f in a b; do\n  echo \"$f\"\n  # shellcheck disable=SC2086\ndone\necho $1\n", []string{"SC1123 4:3", "SC2086 6:6"}},
		{"after a case terminator", "#!/bin/sh\ncase $1 in\n  a) echo $2 ;; # shellcheck disable=SC2086\n  b) echo $3 ;;\nesac\n", []string{"SC1124 3:17", "SC2086 3:11", "SC2086 4:11"}},
		{"empty value at the end of the file", "#!/bin/sh\n# shellcheck disable=", nil},
		{"inside a case branch body", "#!/bin/sh\ncase $1 in\n  a)\n    # shellcheck disable=SC2086\n    echo $2 ;;\nesac\n", nil},
	} {
		tree := parser.Parse(tc.script, dialect.Posix)
		var got []string
		for _, f := range Check(tree, nil, Options{}) {
			got = append(got, f.Code.String()+" "+position(tc.script, f.Span.Start))
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s:\n%s\ngot  %v\nwant %v", tc.name, tc.script, got, tc.want)
		}
	}
}
