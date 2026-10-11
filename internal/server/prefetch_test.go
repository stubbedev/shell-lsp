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
	var mu sync.Mutex
	var fetched []string
	srv.prefetch.fetch = func(_ context.Context, name string) {
		mu.Lock()
		defer mu.Unlock()
		fetched = append(fetched, name)
	}
	srv.prefetch.start(t.Context())
	srv.prefetchCommands(parser.Parse("tool -v extra\ntool start web\n", dialect.Bash))

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		got := slices.Clone(fetched)
		mu.Unlock()
		if len(got) >= 2 {
			if !slices.Equal(got, []string{"tool", "tool-extra"}) {
				t.Errorf("fetched %v, want the command and its documented subcommand page only", got)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("fetched %v", got)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPrefetchesExternalCommandsOnce(t *testing.T) {
	t.Parallel()

	srv := New(jsonrpc.NewConn(nil, nil), "test")
	var mu sync.Mutex
	var fetched []string
	srv.prefetch.fetch = func(_ context.Context, name string) {
		mu.Lock()
		defer mu.Unlock()
		fetched = append(fetched, name)
	}
	srv.prefetch.start(t.Context())

	var src strings.Builder
	src.WriteString("helper() { :; }\nhelper\necho hi\ngrep x\ngrep y\nsed s\n")
	for i := range prefetchPerDocument + 10 {
		fmt.Fprintf(&src, "tool%d\n", i)
	}
	srv.prefetchCommands(parser.Parse(src.String(), dialect.Bash))
	srv.prefetchCommands(parser.Parse("grep again\n", dialect.Bash))

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		n := len(fetched)
		mu.Unlock()
		if n >= prefetchPerDocument {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("prefetched %d commands, want %d", n, prefetchPerDocument)
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(fetched) != prefetchPerDocument || fetched[0] != "grep" || fetched[1] != "sed" {
		t.Errorf("fetched %d: %v", len(fetched), fetched[:min(len(fetched), 3)])
	}
	if slices.Contains(fetched, "helper") || slices.Contains(fetched, "echo") {
		t.Errorf("prefetched functions or builtins: %v", fetched)
	}
}
