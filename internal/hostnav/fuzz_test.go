package hostnav

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

func checkSeeds(f *testing.F, corpus string) {
	f.Helper()
	for _, c := range hosttest.Sections(f, filepath.Join("testdata", "check", corpus)) {
		f.Add(c.Text)
	}
}

func skipLoneCarriageReturns(t *testing.T, text string) {
	t.Helper()
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\r") {
		t.Skip("editors treat a lone carriage return as a line break, so the analysis reads other lines than the tool")
	}
}

func FuzzJustChecksAgreeWithJust(f *testing.F) {
	checkSeeds(f, "just.txt")
	f.Fuzz(func(t *testing.T, text string) {
		hosttest.LiveTool(t, "just", hosttest.JustVersion)
		skipLoneCarriageReturns(t, text)
		hosttest.Agree(t, docstore.NormalizeCanonical(text), hosttest.JustVerdict(t, text), findingsAlone(t, "justfile")(text))
	})
}

func FuzzMakeChecksAgreeWithMake(f *testing.F) {
	checkSeeds(f, "make.txt")
	f.Fuzz(func(t *testing.T, text string) {
		hosttest.LiveTool(t, "make", hosttest.MakeVersion)
		skipLoneCarriageReturns(t, text)
		if !hosttest.MakeIsolated(text) {
			t.Skip("make could run a command from this input")
		}
		hosttest.Agree(t, docstore.NormalizeCanonical(text), hosttest.IsolatedMakeVerdict(t, text), findingsAlone(t, "Makefile")(text))
	})
}
