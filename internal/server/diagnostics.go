package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
)

const diagnosticsDebounce = 200 * time.Millisecond

type diagnosticReport struct {
	version  int32
	resultID string
	items    []lsp.Diagnostic
}

type lintCache struct {
	mu       sync.Mutex
	valid    bool
	stamp    uint64
	findings []lint.Finding
}

func (s *Server) findings(snap *snapshot) ([]lint.Finding, uint64) {
	if !snap.settings.Lint.Enabled {
		return nil, 0
	}
	stamp := s.sourcesStamp(snap)
	snap.lint.mu.Lock()
	defer snap.lint.mu.Unlock()
	if snap.lint.valid && snap.lint.stamp == stamp {
		return snap.lint.findings, stamp
	}
	findings := lint.Check(snap.tree, s.imports(snap), lint.Options{Exclude: snap.settings.Lint.Exclude, Optional: snap.settings.Lint.Optional, ForcedDialect: snap.settings.Dialect != nil})
	snap.lint.valid, snap.lint.stamp, snap.lint.findings = true, stamp, findings
	return findings, stamp
}

func (s *Server) diagnose(snap *snapshot) diagnosticReport {
	findings, stamp := s.findings(snap)
	items := diagnostics.Syntax(snap.tree, snap.doc, snap.encoding)
	items = append(items, diagnostics.Lint(findings, snap.doc, snap.encoding)...)
	return diagnosticReport{
		version:  snap.doc.Version,
		resultID: fmt.Sprintf("%d/%d/%d", snap.doc.Version, snap.generation, stamp),
		items:    items,
	}
}

func (s *Server) sourcesStamp(snap *snapshot) uint64 {
	if !slices.ContainsFunc(snap.tree.Commands, navigation.IsSource) {
		return 0
	}
	return s.workspace.Generation()
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
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}
