package server

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func TestPrefetchesSubcommandPages(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	withToolPages(t)(srv)
	fetched := recordPrefetches(t, srv)
	srv.prefetchCommands(parser.Parse("tool -v extra\ntool start web\n", dialect.Bash))

	if got := fetched.await(t, 2); !slices.Equal(got, []string{"tool", "tool-extra"}) {
		t.Errorf("fetched %v, want the command and its documented subcommand page only", got)
	}
}

func TestPrefetchesExternalCommandsOnce(t *testing.T) {
	t.Parallel()

	pages := map[string]string{}
	for _, name := range []string{"helper", "echo", "grep", "sed"} {
		pages[name] = stubPage(name)
	}
	var src strings.Builder
	src.WriteString("helper() { :; }\nhelper\necho hi\nnopage x\ngrep x\ngrep y\nsed s\n")
	for i := range prefetchPerDocument + 10 {
		name := fmt.Sprintf("tool%d", i)
		pages[name] = stubPage(name)
		fmt.Fprintf(&src, "%s\n", name)
	}
	srv := New(jsonrpc.NewConn(nil, nil), "test")
	withPages(t, pages)(srv)
	fetched := recordPrefetches(t, srv)
	srv.prefetchCommands(parser.Parse(src.String(), dialect.Bash))
	srv.prefetchCommands(parser.Parse("grep again\n", dialect.Bash))

	got := fetched.await(t, prefetchPerDocument)
	if len(got) != prefetchPerDocument || got[0] != "grep" || got[1] != "sed" {
		t.Errorf("fetched %d: %v", len(got), got[:min(len(got), 3)])
	}
	for _, unwanted := range []string{"helper", "echo", "nopage"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("prefetched %s, a function, builtin or command without a page: %v", unwanted, got)
		}
	}
}

func TestPrefetchesNestedSubcommandPages(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	withPages(t, map[string]string{"tool": toolPage, "tool-extra": toolExtraPage, "tool-extra-deep": stubPage("tool-extra-deep")})(srv)
	fetched := recordPrefetches(t, srv)
	srv.prefetchCommands(parser.Parse("tool --unknown extra deep\n", dialect.Bash))

	if got := fetched.await(t, 3); !slices.Equal(got, []string{"tool", "tool-extra", "tool-extra-deep"}) {
		t.Errorf("fetched %v, want every page of the nested invocation", got)
	}
}

type prefetches struct {
	mu   sync.Mutex
	list []string
}

func recordPrefetches(t *testing.T, srv *Server) *prefetches {
	t.Helper()
	p := &prefetches{}
	srv.prefetch.fetch = func(_ context.Context, name string) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.list = append(p.list, name)
	}
	srv.prefetch.start(t.Context())
	return p
}

func (p *prefetches) await(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		p.mu.Lock()
		got := len(p.list)
		p.mu.Unlock()
		if got >= n {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("prefetched %d pages, want %d", got, n)
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.list)
}
