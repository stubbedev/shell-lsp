package hostnav

import (
	"path/filepath"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/hosttest"
)

func checkSeeds(f *testing.F, corpus string) {
	f.Helper()
	for _, c := range hosttest.Sections(f, filepath.Join("testdata", "check", corpus)) {
		f.Add(c.Text)
	}
}

func FuzzJustChecksAgreeWithJust(f *testing.F) {
	checkSeeds(f, "just.txt")
	f.Fuzz(func(t *testing.T, text string) {
		hosttest.LiveTool(t, "just", hosttest.JustVersion)
		hosttest.Agree(t, text, hosttest.JustVerdict(t, text), findingsAlone(t, "justfile")(text))
	})
}

func FuzzMakeChecksAgreeWithMake(f *testing.F) {
	checkSeeds(f, "make.txt")
	f.Fuzz(func(t *testing.T, text string) {
		hosttest.LiveTool(t, "make", hosttest.MakeVersion)
		if !hosttest.MakeIsolated(text) {
			t.Skip("make could run a command from this input")
		}
		hosttest.Agree(t, text, hosttest.IsolatedMakeVerdict(t, text), findingsAlone(t, "Makefile")(text))
	})
}
