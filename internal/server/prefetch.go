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
		pages, err := s.commandPages(tree, cmd)
		if err != nil {
			s.logger.Warn("prefetching manual pages", "command", cmd.Name, "err", err)
		}
		for _, page := range pages {
			if queued == prefetchPerDocument {
				return
			}
			if s.prefetch.enqueue(page) {
				queued++
			}
		}
	}
}

func (s *Server) commandPages(tree *parser.Tree, cmd parser.Command) ([]string, error) {
	wrappers := parser.Wrappers{Bats: tree.Bats(), Programs: true}
	command, args := cmd.Name, parser.ArgumentTexts(cmd.Args)
	var pages []string
	for {
		wrap, wrapped := wrappers.Unwrap(command, args)
		own := args
		if wrapped {
			own = args[:wrap.Command]
		}
		if !shellOnly(tree, command) {
			unloaded, err := s.environment.pages.Unloaded(command, own)
			if err != nil {
				return pages, err
			}
			pages = append(pages, unloaded...)
		}
		if !wrapped || wrap.Command == len(args) {
			return pages, nil
		}
		command, args = args[wrap.Command], args[wrap.Command+1:]
	}
}

func shellOnly(tree *parser.Tree, name string) bool {
	if _, isFunction := tree.FunctionNamed(name); isFunction {
		return true
	}
	_, isBuiltin := shelldocs.Builtin(name)
	return isBuiltin
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
