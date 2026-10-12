package server

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/perf"
)

const (
	soakCycles      = 16
	soakWarmup      = 4
	soakDocuments   = 4
	soakEdits       = 12
	soakDiskFiles   = 5
	soakScriptLines = 2000
	soakHeapBudget  = 2 << 20
	soakSettle      = 10 * time.Second
)

type soakSession struct {
	t    *testing.T
	c    *testClient
	root string
}

func soakCommand(cycle int) string { return fmt.Sprintf("tool%d", cycle) }

func soakScript(cycle int) string {
	return perf.LargeScript(soakScriptLines) + soakCommand(cycle) + " --flag value\n"
}

func (s soakSession) cycle(n int) {
	s.t.Helper()
	uris := make([]string, soakDocuments)
	for d := range uris {
		uris[d] = fileuri.FromPath(filepath.Join(s.root, fmt.Sprintf("doc%d.sh", d)))
		s.edit(uris[d], n)
	}
	s.disk(n)
	s.folder(n)
	s.c.notify("workspace/didChangeConfiguration", fmt.Sprintf(`{"settings": {"shellLsp": {"lint": {"exclude": ["SC%d"]}}}}`, 2000+n))
	for _, uri := range uris {
		s.c.notify("textDocument/didClose", `{"textDocument": {"uri": "`+uri+`"}}`)
	}
	s.c.call("workspace/symbol", `{"query": "step_1"}`)
	s.c.drain()
}

func (s soakSession) edit(uri string, n int) {
	s.t.Helper()
	text := soakScript(n)
	s.c.notify("textDocument/didOpen", openParams(uri, text))
	for e := range soakEdits {
		s.c.notify("textDocument/didChange", fmt.Sprintf(`{"textDocument": {"uri": %q, "version": %d}, "contentChanges": [{"range": {"start": {"line": 3, "character": 0}, "end": {"line": 3, "character": 0}}, "text": "echo edit %d\n"}]}`, uri, e+2, e))
	}
	doc := `{"textDocument": {"uri": "` + uri + `"}}`
	last := strings.Count(text, "\n") + soakEdits - 1
	s.c.call("textDocument/diagnostic", doc)
	s.c.call("textDocument/semanticTokens/full", doc)
	s.c.call("textDocument/documentSymbol", doc)
	s.c.call("textDocument/codeLens", doc)
	s.c.call("textDocument/hover", positionParams(uri, last, 1, ""))
	s.c.call("textDocument/completion", positionParams(uri, last, len(soakCommand(n))+2, ""))
	id := s.c.start("workspace/symbol", `{"query": ""}`)
	s.c.notify("$/cancelRequest", `{"id": `+id+`}`)
	s.c.awaitResponse(id, "workspace/symbol")
}

func (s soakSession) disk(n int) {
	s.t.Helper()
	dir := filepath.Join(s.root, fmt.Sprintf("disk%d", n))
	if err := os.Mkdir(dir, 0o755); err != nil {
		s.t.Fatal(err)
	}
	var changes []string
	for f := range soakDiskFiles {
		path := filepath.Join(dir, fmt.Sprintf("lib%d.sh", f))
		if err := os.WriteFile(path, []byte(soakScript(n)), 0o600); err != nil {
			s.t.Fatal(err)
		}
		changes = append(changes, fileuri.FromPath(path))
	}
	s.watched(changes, 1)
	s.c.call("workspace/symbol", `{"query": "step_2"}`)
	if err := os.WriteFile(filepath.Join(dir, "lib0.sh"), []byte(soakScript(n+1)), 0o600); err != nil {
		s.t.Fatal(err)
	}
	s.watched(changes[:1], 2)
	s.c.call("workspace/symbol", `{"query": "step_3"}`)
	if err := os.RemoveAll(dir); err != nil {
		s.t.Fatal(err)
	}
	s.watched(changes, 3)
}

func (s soakSession) watched(uris []string, kind int) {
	events := make([]string, len(uris))
	for i, uri := range uris {
		events[i] = fmt.Sprintf(`{"uri": %q, "type": %d}`, uri, kind)
	}
	s.c.notify("workspace/didChangeWatchedFiles", `{"changes": [`+strings.Join(events, ",")+`]}`)
}

func (s soakSession) folder(n int) {
	s.t.Helper()
	dir := s.t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extra.sh"), []byte(soakScript(n)), 0o600); err != nil {
		s.t.Fatal(err)
	}
	folder := fmt.Sprintf(`[{"uri": %q, "name": "extra%d"}]`, fileuri.FromPath(dir), n)
	s.c.notify("workspace/didChangeWorkspaceFolders", `{"event": {"added": `+folder+`, "removed": []}}`)
	s.c.call("workspace/symbol", `{"query": "step_4"}`)
	s.c.notify("workspace/didChangeWorkspaceFolders", `{"event": {"added": [], "removed": `+folder+`}}`)
}

type soakSample struct {
	goroutines  int
	heapInuse   uint64
	heapObjects uint64
}

func sampleMemory() soakSample {
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return soakSample{goroutines: runtime.NumGoroutine(), heapInuse: stats.HeapInuse, heapObjects: stats.HeapObjects}
}

func settled(t *testing.T, srv *Server, done func(soakSample) bool) soakSample {
	t.Helper()
	deadline := time.Now().Add(soakSettle)
	for {
		sample := sampleMemory()
		if srv.idle() && done(sample) {
			return sample
		}
		if time.Now().After(deadline) {
			var dump strings.Builder
			if err := pprof.Lookup("goroutine").WriteTo(&dump, 1); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("server did not settle within %v: %+v, caches %v\n%s", soakSettle, sample, srv.cacheSizes(), dump.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func syncMapLen(m *sync.Map) int {
	n := 0
	m.Range(func(any, any) bool {
		n++
		return true
	})
	return n
}

func (s *Server) cacheSizes() map[string]int {
	s.snapshots.mu.Lock()
	snapshots := len(s.snapshots.entries)
	s.snapshots.mu.Unlock()
	s.semantic.mu.Lock()
	semantic := len(s.semantic.results)
	s.semantic.mu.Unlock()
	s.warming.mu.Lock()
	warming := len(s.warming.running) + len(s.warming.again)
	s.warming.mu.Unlock()
	s.outbound.mu.Lock()
	outbound := len(s.outbound.running) + len(s.outbound.again)
	s.outbound.mu.Unlock()
	pushJobs := 0
	if push, ok := s.delivery.(pushDelivery); ok {
		push.scheduler.mu.Lock()
		pushJobs = len(push.scheduler.jobs)
		push.scheduler.mu.Unlock()
	}
	return map[string]int{
		"documents": len(s.docs.View().Documents()),
		"snapshots": snapshots,
		"semantic":  semantic,
		"warming":   warming,
		"outbound":  outbound,
		"push jobs": pushJobs,
		"cancels":   syncMapLen(&s.cancels),
		"pending":   syncMapLen(&s.pending),
		"progress":  syncMapLen(&s.progress),
	}
}

func (s *Server) idle() bool {
	for _, n := range s.cacheSizes() {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestSoakReleasesEverythingItHeld(t *testing.T) {
	if perf.RaceDetector {
		t.Skip("the race detector's shadow memory makes heap growth meaningless")
	}
	pages := map[string]string{}
	for n := range soakCycles + 1 {
		pages[soakCommand(n)] = stubPage(soakCommand(n))
	}
	root := t.TempDir()
	c, srv := startClient(t, `{"rootUri": "`+fileuri.FromPath(root)+`", "capabilities": {}}`, withPages(t, pages))
	c.notify("initialized", `{}`)
	soak := soakSession{t: t, c: c, root: root}
	for n := range soakWarmup {
		soak.cycle(n)
	}
	baseline := settled(t, srv, func(soakSample) bool { return true })
	for n := soakWarmup; n < soakCycles; n++ {
		soak.cycle(n)
	}
	after := settled(t, srv, func(s soakSample) bool {
		return s.goroutines <= baseline.goroutines && s.heapInuse <= baseline.heapInuse+soakHeapBudget
	})
	t.Logf("after %d cycles: goroutines %d -> %d, heap in use %d -> %d KiB, heap objects %d -> %d",
		soakCycles-soakWarmup, baseline.goroutines, after.goroutines, baseline.heapInuse>>10, after.heapInuse>>10, baseline.heapObjects, after.heapObjects)
}
