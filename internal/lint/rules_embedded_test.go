package lint

import (
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

type embeddedFinding struct {
	code     string
	severity Severity
	at       string
	message  string
}

func embeddedFindings(src string, opts Options) []embeddedFinding {
	var out []embeddedFinding
	for _, f := range Check(parser.Parse(src, dialect.Bash), nil, opts) {
		if slices.Contains(embeddedSyntaxCodes[:], f.Code) {
			out = append(out, embeddedFinding{f.Code.String(), f.Severity, src[f.Span.Start:f.Span.End], f.Message})
		}
	}
	return out
}

func TestEmbeddedProgramFindings(t *testing.T) {
	t.Parallel()

	src := "#!/bin/bash\n# 😀\nawk '{ print \"x }' f\njq '.a |' f\nsed -e 's/a/b/q' f\nawk '{ print }' f\n"
	want := []embeddedFinding{
		{"SL3001", SeverityError, `"x }`, "awk: unterminated string"},
		{"SL3003", SeverityError, "", "jq: unexpected end of program"},
		{"SL3002", SeverityError, "q", "sed: unknown option to `s`"},
	}
	if got := embeddedFindings(src, Options{}); !slices.Equal(got, want) {
		t.Errorf("findings = %+v\nwant %+v", got, want)
	}
}

func TestEmbeddedProgramFindingsAreConfigurable(t *testing.T) {
	t.Parallel()

	src := "#!/bin/bash\nawk '{' f\n# shellcheck disable=SL3002\nsed 'k' f\njq '(' f\n"
	if got := embeddedFindings(src, Options{}); len(got) != 2 || got[0].code != "SL3001" || got[1].code != "SL3003" {
		t.Errorf("findings with SL3002 disabled for one command = %+v, want SL3001 and SL3003", got)
	}
	if got := embeddedFindings(src, Options{Exclude: []Code{embeddedSyntaxCodes[0], embeddedSyntaxCodes[2]}}); len(got) != 0 {
		t.Errorf("findings with SL3001 and SL3003 excluded = %+v, want none", got)
	}
	fileWide := "#!/bin/bash\n# shellcheck disable=SL3001\nawk '{' f\nawk '(' g\n"
	if got := embeddedFindings(fileWide, Options{}); len(got) != 0 {
		t.Errorf("findings with SL3001 disabled for the file = %+v, want none", got)
	}
}

func TestEmbeddedProgramCodesAreDistinctNativeCodes(t *testing.T) {
	t.Parallel()

	seen := map[Code]bool{}
	for _, code := range embeddedSyntaxCodes {
		if !code.Native() || seen[code] || !strings.HasPrefix(code.String(), "SL3") {
			t.Errorf("embedded syntax code %s is not a distinct SL3xxx code", code)
		}
		seen[code] = true
	}
}
