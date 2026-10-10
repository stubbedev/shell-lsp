package server

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/shelldocs"
)

const (
	prefetchQueue       = 256
	prefetchPerDocument = 32
)

type prefetcher struct {
	fetch func(ctx context.Context, name string)
	queue chan string
	seen  sync.Map
	once  sync.Once
}

func newPrefetcher(pages *manpage.Lookup) *prefetcher {
	return &prefetcher{
		fetch: func(ctx context.Context, name string) { _, _ = pages.Page(ctx, name) },
		queue: make(chan string, prefetchQueue),
	}
}

func (p *prefetcher) start(ctx context.Context) {
	p.once.Do(func() {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case name := <-p.queue:
					p.fetch(ctx, name)
				}
			}
		}()
	})
}

func (p *prefetcher) enqueue(name string) bool {
	if _, queued := p.seen.LoadOrStore(name, struct{}{}); queued {
		return false
	}
	select {
	case p.queue <- name:
		return true
	default:
		p.seen.Delete(name)
		return false
	}
}

func (s *Server) prefetchCommands(tree *parser.Tree) {
	queued := 0
	for _, cmd := range tree.Commands {
		if queued == prefetchPerDocument {
			return
		}
		if _, isFunction := tree.FunctionNamed(cmd.Name); isFunction {
			continue
		}
		if _, isBuiltin := shelldocs.Builtin(cmd.Name); isBuiltin {
			continue
		}
		if s.prefetch.enqueue(cmd.Name) {
			queued++
		}
	}
}

func (s *Server) warmUp(ctx context.Context) {
	s.prefetch.start(ctx)
	go s.environment.executables.list(os.Getenv("PATH"), time.Now())
}

func (s *Server) indexInBackground(ctx context.Context) {
	if _, err := s.indexWorkspace(ctx, s.docs.View(), background); err != nil && ctx.Err() == nil {
		s.logger.Warn("indexing workspace", "err", err)
	}
}
