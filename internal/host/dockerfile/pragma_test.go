package dockerfile

import (
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

func TestParsePragma(t *testing.T) {
	t.Parallel()

	codes := func(texts ...string) []lint.Code {
		out := make([]lint.Code, len(texts))
		for i, text := range texts {
			code, err := lint.ParseCode(text)
			if err != nil {
				t.Fatal(err)
			}
			out[i] = code
		}
		return out
	}
	for _, tt := range []struct {
		comment string
		want    Pragma
		ok      bool
	}{
		{"# hadolint ignore=DL3006", Pragma{Kind: PragmaIgnore, Codes: codes("DL3006")}, true},
		{"#hadolint ignore=DL3006,SC2086", Pragma{Kind: PragmaIgnore, Codes: codes("DL3006", "SC2086")}, true},
		{"# hadolint ignore = DL3006 , SC2086", Pragma{Kind: PragmaIgnore, Codes: codes("DL3006", "SC2086")}, true},
		{"#  hadolint\tignore=DL3006 # because", Pragma{Kind: PragmaIgnore, Codes: codes("DL3006")}, true},
		{"# hadolint global ignore=DL3008", Pragma{Kind: PragmaGlobalIgnore, Codes: codes("DL3008")}, true},
		{"# hadolint shell=bash", Pragma{Kind: PragmaShell, Shell: "bash"}, true},
		{"# hadolint shell=/bin/bash -o pipefail", Pragma{}, false},
		{"# hadolint shell=", Pragma{}, false},
		{"# hadolint ignore=DL3006,", Pragma{}, false},
		{"# hadolint ignore=DL3006 reason", Pragma{}, false},
		{"# hadolint ignore=XX1234", Pragma{}, false},
		{"# hadolint", Pragma{}, false},
		{"# hadolintignore=DL3006", Pragma{}, false},
		{"# shellcheck disable=SC2086", Pragma{}, false},
	} {
		got, ok := ParsePragma(tt.comment)
		if ok != tt.ok || ok && (got.Kind != tt.want.Kind || got.Shell != tt.want.Shell || !slices.Equal(got.Codes, tt.want.Codes)) {
			t.Errorf("ParsePragma(%q) = %+v, %v; want %+v, %v", tt.comment, got, ok, tt.want, tt.ok)
		}
	}
}

func TestShellPragmaChoosesTheShellOfLaterRuns(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		text string
		read int
	}{
		{"FROM alpine\nRUN a\n# hadolint shell=pwsh\nRUN b\nRUN c\n", 1},
		{"FROM alpine\n# hadolint shell=pwsh\nRUN a\nSHELL [\"/bin/sh\", \"-c\"]\nRUN b\n", 1},
		{"FROM alpine\nSHELL [\"pwsh\", \"-c\"]\nRUN a\n# hadolint shell=/bin/bash\nRUN b\n", 1},
		{"FROM alpine\nRUN a\n# hadolint shell=pwsh\nFROM alpine\nRUN b\n", 2},
		{"FROM alpine\nRUN echo a \\\n# hadolint shell=pwsh\n  b\nRUN c\n", 2},
	} {
		if got := len(Analyze(tt.text).Index.Shells); got != tt.read {
			t.Errorf("%q: %d RUNs are read as shell, want %d", tt.text, got, tt.read)
		}
	}
}
