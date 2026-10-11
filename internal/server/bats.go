package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/bats"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	commandRunBats = "shellLsp.runBats"
	batsTimeout    = 30 * time.Minute
)

type batsRunArgs struct {
	URI  string  `json:"uri"`
	Test *string `json:"test,omitempty"`
}

type batsRunResult struct {
	bats.Tally
	Summary string `json:"summary"`
}

type batsRecord struct {
	revision uint64
	failures []bats.Failure
}

type batsResults struct {
	mu       sync.Mutex
	revision uint64
	byURI    map[string]batsRecord
}

func newBatsResults() *batsResults {
	return &batsResults{byURI: map[string]batsRecord{}}
}

func (b *batsResults) record(uri string, test *string, failures []bats.Failure) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var kept []bats.Failure
	if test != nil {
		kept = slices.DeleteFunc(slices.Clone(b.byURI[uri].failures), func(f bats.Failure) bool { return f.Test == *test })
	}
	b.revision++
	b.byURI[uri] = batsRecord{revision: b.revision, failures: append(kept, failures...)}
}

func (b *batsResults) of(uri string) batsRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byURI[uri]
}

func appendBatsLenses(out []lsp.CodeLens, file *workspace.File, enc docstore.Encoding) ([]lsp.CodeLens, error) {
	if !file.Tree.Bats || len(file.Tree.Tests) == 0 {
		return out, nil
	}
	lens := func(r lsp.Range, title string, args batsRunArgs) error {
		raw, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("encoding %s arguments for %s: %w", commandRunBats, file.URI, err)
		}
		out = append(out, lsp.CodeLens{Range: r, Command: &lsp.Command{Title: title, Command: commandRunBats, Arguments: []json.RawMessage{raw}}})
		return nil
	}
	if err := lens(lsp.Range{}, "▶ Run file", batsRunArgs{URI: file.URI}); err != nil {
		return nil, err
	}
	for _, test := range file.Tree.Tests {
		if !test.Literal {
			continue
		}
		name := test.Name
		if err := lens(file.Doc.RangeFor(test.Span.Start, test.NameSpan.End, enc), "▶ Run test", batsRunArgs{URI: file.URI, Test: &name}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func runnableTest(tree *parser.Tree, name string) (parser.Test, bool) {
	i := slices.IndexFunc(tree.Tests, func(t parser.Test) bool { return t.Literal && t.Name == name })
	if i < 0 {
		return parser.Test{}, false
	}
	return tree.Tests[i], true
}

func (s *Server) runBats(ctx context.Context, docs *docstore.View, args batsRunArgs) (batsRunResult, error) {
	file, err := s.loadURI(docs, args.URI)
	if err != nil {
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %v", commandRunBats, err)
	}
	path, err := fileuri.ToPath(file.URI)
	if err != nil {
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %v", commandRunBats, err)
	}
	if args.Test != nil {
		if _, ok := runnableTest(file.Tree, *args.Test); !ok {
			return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s has no @test named %q", file.URI, *args.Test)
		}
	}
	dir := filepath.Dir(path)
	if root, ok := s.workspace.RootOf(path); ok {
		dir = root
	}
	executable, err := bats.Locate(dir)
	if err != nil {
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	inv := bats.Invocation{Executable: executable, Dir: dir, File: path, Test: args.Test}
	label := "bats " + filepath.Base(path)
	ctx, cancel := context.WithTimeout(ctx, batsTimeout)
	defer cancel()
	progress, ctx, err := s.beginWorkDone(ctx, label)
	if err != nil {
		return batsRunResult{}, err
	}
	outcome := "stopped"
	defer func() { progress.end(outcome) }()
	report, err := bats.Run(ctx, inv, func(r bats.Report) { progress.report(batsProgress(r)) })
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%s timed out after %s", inv, batsTimeout)
	case errors.Is(err, context.Canceled):
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestCancelled, "%s was cancelled", inv)
	case err != nil:
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	outcome = report.Summary()
	s.bats.record(file.URI, args.Test, bats.Failures(report, path, dir, func(name string) (int, bool) {
		test, ok := runnableTest(file.Tree, name)
		return file.Tree.Line(test.Span.Start), ok
	}))
	s.refreshDiagnosticsOf(context.WithoutCancel(ctx), s.clientCapabilities().Workspace.Diagnostics.RefreshSupport, []string{file.URI})
	kind := lsp.MessageInfo
	if !report.OK() {
		kind = lsp.MessageError
	}
	s.notify("window/showMessage", lsp.ShowMessageParams{Type: kind, Message: label + ": " + outcome})
	return batsRunResult{Tally: report.Tally(), Summary: outcome}, nil
}

func batsProgress(r bats.Report) (string, int) {
	done := len(r.Results)
	if !r.HasPlan {
		return fmt.Sprintf("%d tests", done), 0
	}
	return fmt.Sprintf("%d/%d tests", done, r.Planned), min(done*100/max(r.Planned, 1), 100)
}
