package server

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/perf"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	keystrokeWarmupRuns = 2
	keystrokeBudgetRuns = 5
)

type keystrokeSource struct {
	name       string
	file       string
	language   string
	text       func() string
	line       int
	maxBytes   uint64
	maxObjects uint64
}

func largeJustfile(recipes int) string {
	var sb strings.Builder
	sb.WriteString("set shell := [\"bash\", \"-cu\"]\n\nprefix := \"/usr\"\n\n")
	for i := range recipes {
		fmt.Fprintf(&sb, "recipe%d arg=\"x\": recipe%d\n    @echo {{prefix}} {{arg}} | grep -v foo\n    cp a b && echo done\n\n", i, max(i-1, 0))
	}
	return sb.String()
}

func largeMakefile(rules int) string {
	var sb strings.Builder
	for i := range rules {
		fmt.Fprintf(&sb, "V%d := $(V%d) x\n", i, max(i-1, 0))
	}
	sb.WriteString("all:")
	for i := range rules {
		fmt.Fprintf(&sb, " t%d", i)
	}
	sb.WriteString("\n")
	for i := range rules {
		fmt.Fprintf(&sb, "t%d:\n\t@echo $(V%d) $@ | grep -v foo\n", i, i)
	}
	return sb.String()
}

func largeShellHeavyNix(scripts int) string {
	var sb strings.Builder
	sb.WriteString("{ pkgs, lib, ... }:\n{\n")
	for i := range scripts {
		fmt.Fprintf(&sb, "  script%d = pkgs.writeShellScript \"s%d\" ''\n    set -eu\n    name=${lib.escapeShellArg \"n%d\"}\n    if [ -n \"$name\" ]; then\n      echo \"$name\" | ${pkgs.gnugrep}/bin/grep -v foo\n    fi\n  '';\n", i, i, i)
	}
	sb.WriteString("}\n")
	return sb.String()
}

func largeDockerfile(steps int) string {
	var sb strings.Builder
	sb.WriteString("FROM alpine:3.20 AS base\n")
	for i := range steps {
		fmt.Fprintf(&sb, "RUN apk add --no-cache pkg%d && echo done%d | grep -v x\n", i, i)
	}
	sb.WriteString("COPY . /app\nCMD [\"sh\"]\n")
	return sb.String()
}

var keystrokeSources = []keystrokeSource{
	{name: "shell", file: "big.sh", language: "shellscript", text: func() string { return perf.LargeScript(5000) }, line: 2, maxBytes: 13 << 20, maxObjects: 150_000},
	{name: "justfile", file: "justfile", language: "just", text: func() string { return largeJustfile(1200) }, line: 4, maxBytes: 32 << 20, maxObjects: 165_000},
	{name: "makefile", file: "Makefile", language: "makefile", text: func() string { return largeMakefile(1500) }, line: 1, maxBytes: 40 << 20, maxObjects: 220_000},
	{name: "dockerfile", file: "Dockerfile", language: "dockerfile", text: func() string { return largeDockerfile(1000) }, line: 1, maxBytes: 18 << 20, maxObjects: 160_000},
	{name: "nix", file: "big.nix", language: "nix", text: func() string { return largeNixFile(5000) }, line: 2, maxBytes: 62 << 20, maxObjects: 230_000},
	{name: "nix-shell", file: "scripts.nix", language: "nix", text: func() string { return largeShellHeavyNix(700) }, line: 2, maxBytes: 16 << 20, maxObjects: 50_000},
	{name: "nix-shell-typing", file: "scripts.nix", language: "nix", text: func() string { return largeShellHeavyNix(700) }, line: 3, maxBytes: 30 << 20, maxObjects: 180_000},
}

type keystrokeSession struct {
	client  *testClient
	uri     string
	doc     string
	line    int
	version int
}

func openKeystrokeSession(tb testing.TB, source keystrokeSource) *keystrokeSession {
	tb.Helper()
	root := tb.TempDir()
	uri := fileuri.FromPath(filepath.Join(root, source.file))
	c, _ := startClient(tb, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openLanguageParams(uri, source.language, source.text()))
	session := &keystrokeSession{client: c, uri: uri, doc: `{"textDocument": {"uri": "` + uri + `"}}`, line: source.line, version: 1}
	c.call("textDocument/diagnostic", session.doc)
	return session
}

func (s *keystrokeSession) press() {
	s.version++
	s.client.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"range": {"start": {"line": %d, "character": 0}, "end": {"line": %d, "character": 0}}, "text": " "}]}`, s.uri, s.version, s.line, s.line))
	s.client.call("textDocument/diagnostic", s.doc)
}

func BenchmarkKeystroke(b *testing.B) {
	for _, source := range keystrokeSources {
		b.Run(source.name, func(b *testing.B) {
			session := openKeystrokeSession(b, source)
			b.ResetTimer()
			for b.Loop() {
				session.press()
			}
		})
	}
}

func medianOf(samples []uint64) uint64 {
	sorted := slices.Sorted(slices.Values(samples))
	return sorted[len(sorted)/2]
}

func TestAKeystrokeStaysInItsAllocationBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation budgets are measured in full test runs")
	}
	for _, source := range keystrokeSources {
		t.Run(source.name, func(t *testing.T) {
			session := openKeystrokeSession(t, source)
			for range keystrokeWarmupRuns {
				session.press()
			}
			bytes, objects := make([]uint64, keystrokeBudgetRuns), make([]uint64, keystrokeBudgetRuns)
			for i := range keystrokeBudgetRuns {
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				session.press()
				runtime.ReadMemStats(&after)
				bytes[i], objects[i] = after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs
			}
			medianBytes, medianObjects := medianOf(bytes), medianOf(objects)
			t.Logf("%d bytes and %d objects a keystroke", medianBytes, medianObjects)
			if medianBytes > source.maxBytes {
				t.Errorf("a keystroke allocated %d bytes, over the budget of %d", medianBytes, source.maxBytes)
			}
			if medianObjects > source.maxObjects {
				t.Errorf("a keystroke allocated %d objects, over the budget of %d", medianObjects, source.maxObjects)
			}
		})
	}
}

func TestQuietFilesCostNoStatPerRequest(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{"lib.sh": "greet() { echo hi; }\n", "main.sh": "source ./lib.sh\ngreet\n"}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stats atomic.Int64
	uri := fileuri.FromPath(filepath.Join(root, "main.sh"))
	c, srv := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {"textDocument": {"diagnostic": {}}}}`, func(s *Server) {
		s.workspace.UseStat(func(path string) (fs.FileInfo, error) {
			stats.Add(1)
			return regularfile.Stat(path)
		})
	})
	if srv.disk == nil {
		t.Skip("no disk watcher on this platform, so requests recheck the disk")
	}
	c.notify("textDocument/didOpen", openParams(uri, files["main.sh"]))
	doc := `{"textDocument": {"uri": "` + uri + `"}}`
	c.call("textDocument/diagnostic", doc)
	definition := positionParams(uri, 1, 1, "")
	c.call("textDocument/definition", definition)
	time.Sleep(2 * workspace.DiskRecheck)
	c.call("textDocument/definition", definition)
	stats.Store(0)
	time.Sleep(2 * workspace.DiskRecheck)
	for version := 2; version < 12; version++ {
		c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"range": {"start": {"line": 1, "character": 0}, "end": {"line": 1, "character": 0}}, "text": " "}]}`, uri, version))
		c.call("textDocument/diagnostic", doc)
		c.call("textDocument/definition", positionParams(uri, 1, version-1, ""))
	}
	if n := stats.Load(); n != 0 {
		t.Errorf("%d stats during ten keystrokes and navigations of quiet files, want none", n)
	}
	if err := os.WriteFile(filepath.Join(root, "lib.sh"), []byte("\ngreet() { echo hello; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	found := decodeResult[[]lsp.Location](t, c.call("textDocument/definition", positionParams(uri, 1, 11, "")))
	if len(found) != 1 || found[0].Range.Start.Line != 1 {
		t.Errorf("definition after an edit outside the editor = %+v, want line 1", found)
	}
}

func TestAQuietEditWarmsTheDocumentAfterAPause(t *testing.T) {
	t.Parallel()

	uri := "file:///w/warm.sh"
	c, srv := startClient(t, `{"capabilities": {"textDocument": {"diagnostic": {}}}}`)
	c.notify("textDocument/didOpen", openParams(uri, "echo hi\n"))
	for version := 2; version < 6; version++ {
		c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"text": "echo %d\n"}]}`, uri, version, version))
	}
	c.call("textDocument/foldingRange", `{"textDocument": {"uri": "`+uri+`"}}`)
	c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": 6}, "contentChanges": [{"text": "echo last\n"}]}`, uri))
	deadline := time.Now().Add(clientTimeout)
	for time.Now().Before(deadline) {
		srv.snapshots.mu.Lock()
		entry := srv.snapshots.entries[uri]
		srv.snapshots.mu.Unlock()
		if entry != nil && entry.doc.Version == 6 {
			select {
			case <-entry.ready:
				return
			case <-time.After(clientTimeout):
				t.Fatal("the warmed snapshot never finished")
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the last edit was never warmed")
}
