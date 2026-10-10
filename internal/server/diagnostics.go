package server

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parallel"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	diagnosticsDebounce = 200 * time.Millisecond
	lintMemoKey         = "lint.findings/"
)

type diagnosticReport struct {
	version  int32
	resultID string
	items    []lsp.Diagnostic
}

func (s *Server) findings(file *workspace.File, settings config.Settings, generation uint64) ([]lint.Finding, uint64) {
	if !settings.Lint.Enabled {
		return nil, 0
	}
	stamp := s.sourcesStamp(file.Tree)
	key := lintMemoKey + strconv.FormatUint(generation, 10)
	findings := workspace.Memo(file, key, stamp, func() []lint.Finding {
		return lint.Check(file.Tree, s.imports(file), lint.Options{
			Exclude:       settings.Lint.Exclude,
			Optional:      settings.Lint.Optional,
			DialectSource: dialectSource(settings),
			Sources:       s.followedSources(file),
		})
	})
	return findings, stamp
}

func dialectSource(settings config.Settings) lint.DialectSource {
	if settings.Dialect != nil {
		return lint.DialectForced
	}
	return lint.DialectDetected
}

func (s *Server) diagnose(snap *snapshot) diagnosticReport {
	return s.report(snap.file, snap.settings, snap.generation, snap.encoding, "v"+strconv.FormatInt(int64(snap.doc.Version), 10))
}

func (s *Server) diagnoseClosed(file *workspace.File, encoding docstore.Encoding) diagnosticReport {
	settings, generation := s.settingsFor(file.URI)
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(file.Doc.Text()))
	return s.report(file, settings, generation, encoding, "h"+strconv.FormatUint(hash.Sum64(), 16))
}

func (s *Server) report(file *workspace.File, settings config.Settings, generation uint64, encoding docstore.Encoding, identity string) diagnosticReport {
	findings, stamp := s.findings(file, settings, generation)
	items := diagnostics.Syntax(file.Tree, file.Doc, encoding)
	items = append(items, diagnostics.Lint(findings, file.Doc, encoding)...)
	return diagnosticReport{
		version:  file.Doc.Version,
		resultID: fmt.Sprintf("%s/%d/%d", identity, generation, stamp),
		items:    items,
	}
}

func (s *Server) sourcesStamp(tree *parser.Tree) uint64 {
	if !slices.ContainsFunc(tree.Commands, navigation.IsSource) {
		return 0
	}
	return s.workspace.Generation()
}

func (s *Server) followedSources(file *workspace.File) []lint.Source {
	var out []lint.Source
	for _, ref := range navigation.Sources(file, s.navigationEnv()) {
		if ref.Target != nil {
			out = append(out, lint.Source{Command: ref.Command.Span, Tree: ref.Target.Tree})
		}
	}
	return out
}

func (s *Server) handleDocumentDiagnostic(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	diagParams, err := lsp.DecodeParams[lsp.DocumentDiagnosticParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/diagnostic params: %v", err)
	}
	snap, err := s.snapshot(docs, diagParams.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	report := s.diagnose(snap)
	if diagParams.PreviousResultID == report.resultID {
		return lsp.UnchangedDocumentDiagnosticReport{Kind: lsp.ReportUnchanged, ResultID: report.resultID}, nil
	}
	return lsp.FullDocumentDiagnosticReport{Kind: lsp.ReportFull, ResultID: report.resultID, Items: report.items}, nil
}

func (s *Server) handleWorkspaceDiagnostic(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	diagParams, err := lsp.DecodeParams[lsp.WorkspaceDiagnosticParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing workspace/diagnostic params: %v", err)
	}
	previous := make(map[string]string, len(diagParams.PreviousResultIDs))
	for _, p := range diagParams.PreviousResultIDs {
		previous[p.URI] = p.Value
	}
	files, err := s.indexWorkspace(ctx, docs, interactive)
	if err != nil {
		return nil, err
	}
	items := make([]lsp.WorkspaceDocumentDiagnosticReport, len(files))
	parallel.For(interactive.workers(), len(files), func(i int) {
		items[i] = s.workspaceReport(docs, files[i], previous[files[i].URI])
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(items, func(a, b lsp.WorkspaceDocumentDiagnosticReport) int { return strings.Compare(a.URI, b.URI) })
	return lsp.WorkspaceDiagnosticReport{Items: items}, nil
}

func (s *Server) workspaceReport(docs *docstore.View, file *workspace.File, previous string) lsp.WorkspaceDocumentDiagnosticReport {
	var report diagnosticReport
	var version *int32
	if snap, err := s.snapshot(docs, file.URI); err == nil {
		report = s.diagnose(snap)
		version = &report.version
	} else {
		report = s.diagnoseClosed(file, docs.Encoding())
	}
	out := lsp.WorkspaceDocumentDiagnosticReport{Kind: lsp.ReportFull, ResultID: report.resultID, URI: file.URI, Version: version, Items: report.items}
	if previous == report.resultID {
		out.Kind, out.Items = lsp.ReportUnchanged, nil
	}
	return out
}

func (s *Server) publishDiagnostics(ctx context.Context, uri string) {
	snap, err := s.snapshot(s.docs.View(), uri)
	if err != nil || ctx.Err() != nil {
		return
	}
	s.publish(uri, s.diagnose(snap))
}

func (s *Server) publish(uri string, report diagnosticReport) {
	if current, ok := s.docs.View().Document(uri); !ok || current.Version != report.version {
		return
	}
	s.notify("textDocument/publishDiagnostics", lsp.PublishDiagnosticsParams{
		URI:         uri,
		Version:     &report.version,
		Diagnostics: report.items,
	})
}

func (s *Server) noticeOnce(key string, kind lsp.MessageType, message string) {
	if _, seen := s.notices.LoadOrStore(key, struct{}{}); seen {
		return
	}
	s.logger.Warn("client notice", "message", message)
	s.notify("window/logMessage", lsp.LogMessageParams{Type: kind, Message: message})
}

func (s *Server) notify(method string, params any) {
	if err := s.conn.Notify(method, params); err != nil {
		s.logger.Error("sending notification", "method", method, "err", err)
	}
}

type pushScheduler struct {
	delay   time.Duration
	publish func(ctx context.Context, uri string)

	mu     sync.Mutex
	jobs   map[string]*pushJob
	closed bool
	wg     sync.WaitGroup
}

type pushJob struct {
	timer  *time.Timer
	cancel context.CancelFunc
}

func newPushScheduler(delay time.Duration, publish func(context.Context, string)) *pushScheduler {
	return &pushScheduler{delay: delay, publish: publish, jobs: map[string]*pushJob{}}
}

func (p *pushScheduler) schedule(uri string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.stopLocked(uri)
	ctx, cancel := context.WithCancel(context.Background())
	job := &pushJob{cancel: cancel}
	p.wg.Add(1)
	job.timer = time.AfterFunc(p.delay, func() {
		defer p.wg.Done()
		defer cancel()
		p.publish(ctx, uri)
	})
	p.jobs[uri] = job
}

func (p *pushScheduler) cancel(uri string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked(uri)
}

func (p *pushScheduler) stopLocked(uri string) {
	job, ok := p.jobs[uri]
	if !ok {
		return
	}
	job.cancel()
	if job.timer.Stop() {
		p.wg.Done()
	}
	delete(p.jobs, uri)
}

func (p *pushScheduler) close() {
	p.mu.Lock()
	p.closed = true
	for uri := range p.jobs {
		p.stopLocked(uri)
	}
	p.mu.Unlock()
	p.wg.Wait()
}

func scriptDir(uri string) string {
	path, err := fileuri.ToPath(uri)
	if err != nil {
		return ""
	}
	return filepath.Dir(path)
}
