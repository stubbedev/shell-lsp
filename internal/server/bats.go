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
	Tally   bats.Tally
	Summary string
}

func (r batsRunResult) MarshalJSON() ([]byte, error) {
	fields := make(map[string]any, len(r.Tally)+1)
	for name, n := range r.Tally.Counts() {
		fields[name] = n
	}
	fields["summary"] = r.Summary
	return json.Marshal(fields)
}

type batsRecord struct {
	revision uint64
	failures []placedFailure
}

type failureAnchor struct {
	test     bool
	name     string
	relative int
}

type placedFailure struct {
	bats.Failure
	anchor *failureAnchor
}

func anchorFailure(tree *parser.Tree, f bats.Failure) placedFailure {
	for _, t := range tree.Tests {
		if relative, ok := lineWithin(tree, t.Span, f.Line); ok {
			return placedFailure{Failure: f, anchor: &failureAnchor{test: true, name: t.Name, relative: relative}}
		}
	}
	for _, fn := range tree.Functions {
		if relative, ok := lineWithin(tree, fn.Span, f.Line); ok {
			return placedFailure{Failure: f, anchor: &failureAnchor{name: fn.Name, relative: relative}}
		}
	}
	return placedFailure{Failure: f}
}

func lineWithin(tree *parser.Tree, span parser.Span, line int) (int, bool) {
	first, last := tree.Line(span.Start), tree.Line(max(span.End-1, span.Start))
	if line < first || line > last {
		return 0, false
	}
	return line - first, true
}

func (p placedFailure) on(tree *parser.Tree) (bats.Failure, bool) {
	if p.anchor == nil {
		return p.Failure, true
	}
	var span parser.Span
	found := false
	if p.anchor.test {
		test, ok := namedTest(tree, p.anchor.name)
		span, found = test.Span, ok
	} else {
		fn, ok := tree.FunctionNamed(p.anchor.name)
		span, found = fn.Span, ok
	}
	if !found {
		return bats.Failure{}, false
	}
	f := p.Failure
	f.Line = tree.Line(span.Start) + p.anchor.relative
	return f, true
}

func (r batsRecord) placedOn(tree *parser.Tree) []bats.Failure {
	out := make([]bats.Failure, 0, len(r.failures))
	for _, p := range r.failures {
		if f, ok := p.on(tree); ok {
			out = append(out, f)
		}
	}
	return out
}

type batsResults struct {
	mu       sync.Mutex
	revision uint64
	byURI    map[string]batsRecord
}

func newBatsResults() *batsResults {
	return &batsResults{byURI: map[string]batsRecord{}}
}

func (b *batsResults) record(uri string, selected []string, failures []placedFailure) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var kept []placedFailure
	if selected != nil {
		kept = slices.DeleteFunc(slices.Clone(b.byURI[uri].failures), func(f placedFailure) bool { return bats.Matches(selected, f.Test) })
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
	if !file.Tree.Bats() || len(file.Tree.Tests) == 0 {
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
		title := "▶ Run test"
		if !test.Literal() {
			title = "▶ Run matching tests"
		}
		name := test.Name
		if err := lens(file.Doc.RangeFor(test.Span.Start, test.NameSpan.End, enc), title, batsRunArgs{URI: file.URI, Test: &name}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func namedTest(tree *parser.Tree, name string) (parser.Test, bool) {
	i := slices.IndexFunc(tree.Tests, func(t parser.Test) bool { return t.Name == name })
	if i < 0 {
		return parser.Test{}, false
	}
	return tree.Tests[i], true
}

func testRunning(tree *parser.Tree, resolved string) (parser.Test, bool) {
	i := slices.IndexFunc(tree.Tests, func(t parser.Test) bool { return bats.Matches(t.Segments, resolved) })
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
	var selected []string
	if args.Test != nil {
		test, ok := namedTest(file.Tree, *args.Test)
		if !ok {
			return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s has no @test named %q", file.URI, *args.Test)
		}
		selected = test.Segments
	}
	dir := filepath.Dir(path)
	if root, ok := s.workspace.RootOf(path); ok {
		dir = root
	}
	runner, err := bats.Locate(dir, bats.Environment{Path: s.process.pathDirs(), PathExt: s.process.pathExt(), SystemRoot: s.process.SystemRoot})
	if err != nil {
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "naming %s relative to %s for bats: %v", path, dir, err)
	}
	disk, _, err := workspace.ReadDisk(path)
	if err != nil {
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "reading %s before running it: %v", path, err)
	}
	ran := parser.ParseEdit(nil, string(disk), file.Tree.Dialect, file.Tree.Lang)
	inv := bats.Invocation{Runner: runner, Dir: dir, File: filepath.ToSlash(rel), Test: selected}
	label := "bats " + filepath.Base(path)
	ctx, cancel := context.WithTimeout(ctx, batsTimeout)
	defer cancel()
	progress, ctx, err := s.beginWorkDone(ctx, label)
	if err != nil {
		return batsRunResult{}, err
	}
	outcome := "stopped"
	defer func() { progress.end(outcome) }()
	report, err := yieldSlot(ctx, func() (bats.Report, error) {
		return bats.Run(ctx, inv, func(r bats.Report) { progress.report(batsProgress(r)) })
	})
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%s timed out after %s", inv, batsTimeout)
	case errors.Is(err, context.Canceled):
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestCancelled, "%s was cancelled", inv)
	case err != nil:
		return batsRunResult{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	outcome = report.Summary()
	failures := bats.Failures(report, path, dir, func(name string) (int, bool) {
		test, ok := testRunning(ran, name)
		return ran.Line(test.Span.Start), ok
	})
	placed := make([]placedFailure, len(failures))
	for i, f := range failures {
		placed[i] = anchorFailure(ran, f)
	}
	s.bats.record(file.URI, selected, placed)
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
	if r.Plan == nil {
		return fmt.Sprintf("%d tests", done), 0
	}
	return fmt.Sprintf("%d/%d tests", done, r.Plan.Tests), min(done*100/max(r.Plan.Tests, 1), 100)
}
