package completion

import (
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

func TestSnippetPreview(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]string{
		"set -eu\n$0":                       "set -eu\n",
		"if ${1:condition}; then\n\t$0\nfi": "if condition; then\n\t\nfi",
		"cleanup() {\n\t${1::}\n}":          "cleanup() {\n\t:\n}",
		"\\$${1:tmp} and \\${0:A:h}":        "$tmp and ${0:A:h}",
		"${2}x${12:y}$":                     "xy$",
		"a ${b} \\\\ c":                     "a ${b} \\ c",
		"${1:outer ${2:inner} \\} end}":     "outer inner } end",
	} {
		if got := snippetPreview(body); got != want {
			t.Errorf("snippetPreview(%q) = %q, want %q", body, got, want)
		}
	}
	for d := range dialect.All.Members() {
		for _, id := range idioms {
			snippetPreview(id.body(d))
		}
	}
	for _, body := range keywordSnippets {
		snippetPreview(body)
	}
}
