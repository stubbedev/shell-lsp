package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const diagnosticsDebounce = 200 * time.Millisecond

type shellcheckKey struct {
	version    int32
	generation uint64
	binary     string
}

type diagnosticsState struct {
	mu         sync.Mutex
	shellcheck map[string]shellcheckResult
	partial    atomic.Uint64
}

type shellcheckResult struct {
	key   shellcheckKey
	items []lsp.Diagnostic
}

func newDiagnosticsState() *diagnosticsState {
	return &diagnosticsState{shellcheck: map[string]shellcheckResult{}}
}

func (d *diagnosticsState) cached(uri string, key shellcheckKey) ([]lsp.Diagnostic, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	result, ok := d.shellcheck[uri]
	if !ok || result.key != key {
		return nil, false
	}
	return result.items, true
}

func (d *diagnosticsState) remember(uri string, key shellcheckKey, items []lsp.Diagnostic) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.shellcheck[uri] = shellcheckResult{key: key, items: items}
}

func (d *diagnosticsState) forget(uri string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.shellcheck, uri)
}

type diagnosticReport struct {
	version  int32
	resultID string
	items    []lsp.Diagnostic
}

type shellcheckPlan struct {
	args   []string
	binary diagnostics.Binary
}

func (s *Server) diagnose(ctx context.Context, snap *snapshot) (diagnosticReport, error) {
	items := diagnostics.Syntax(snap.tree, snap.doc, snap.encoding)
	plan, planned := s.planShellcheck(snap)
	binaryID := "none"
	if planned {
		binaryID = plan.binary.ID
	}
	report := diagnosticReport{
		version:  snap.doc.Version,
		resultID: fmt.Sprintf("%d/%d/%s", snap.doc.Version, snap.generation, binaryID),
	}
	if planned {
		found, complete, err := s.runShellcheck(ctx, snap, plan)
		if err != nil {
			return diagnosticReport{}, err
		}
		items = append(items, found...)
		if !complete {
			report.resultID += "/partial-" + strconv.FormatUint(s.diagnostics.partial.Add(1), 10)
		}
	}
	report.items = items
	return report, nil
}

func (s *Server) planShellcheck(snap *snapshot) (shellcheckPlan, bool) {
	settings := snap.settings.Shellcheck
	if !settings.Enabled {
		return shellcheckPlan{}, false
	}
	args, err := diagnostics.ShellcheckArgs(settings, snap.dialect)
	if errors.Is(err, diagnostics.ErrDialectUnsupported) {
		s.noticeOnce("shellcheck-dialect:"+snap.dialect.String(), lsp.MessageInfo,
			fmt.Sprintf("shellcheck does not support %s; %s documents get syntax diagnostics only", snap.dialect, snap.dialect))
		return shellcheckPlan{}, false
	}
	if err != nil {
		s.noticeOnce("shellcheck-args:"+err.Error(), lsp.MessageError, err.Error())
		return shellcheckPlan{}, false
	}
	binary, err := diagnostics.ResolveBinary(settings.Path)
	if err != nil {
		s.noticeOnce("shellcheck-missing:"+settings.Path, lsp.MessageWarning,
			err.Error()+"; diagnostics are limited to syntax errors until shellcheck is installed or shellLsp.shellcheck.path is set")
		return shellcheckPlan{}, false
	}
	return shellcheckPlan{args: args, binary: binary}, true
}

func (s *Server) runShellcheck(ctx context.Context, snap *snapshot, plan shellcheckPlan) ([]lsp.Diagnostic, bool, error) {
	uri := snap.doc.URI
	key := shellcheckKey{version: snap.doc.Version, generation: snap.generation, binary: plan.binary.ID}
	if items, ok := s.diagnostics.cached(uri, key); ok {
		return items, true, nil
	}
	output, err := diagnostics.RunShellcheck(ctx, plan.binary, plan.args, scriptDir(uri), snap.doc.Text())
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, false, ctxErr
	}
	if err != nil {
		s.noticeOnce("shellcheck-failed:"+err.Error(), lsp.MessageError, err.Error())
		return nil, false, nil
	}
	items, err := diagnostics.ParseShellcheck(output, snap.doc, snap.encoding)
	if err != nil {
		s.noticeOnce("shellcheck-output:"+err.Error(), lsp.MessageError, err.Error())
		return nil, false, nil
	}
	s.diagnostics.remember(uri, key, items)
	return items, true, nil
}

func (s *Server) handleDocumentDiagnostic(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	diagParams, err := lsp.DecodeParams[lsp.DocumentDiagnosticParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/diagnostic params: %v", err)
	}
	snap, err := s.snapshot(docs, diagParams.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	report, err := s.diagnose(ctx, snap)
	if err != nil {
		return nil, err
	}
	if diagParams.PreviousResultID == report.resultID {
		return lsp.UnchangedDocumentDiagnosticReport{Kind: lsp.ReportUnchanged, ResultID: report.resultID}, nil
	}
	return lsp.FullDocumentDiagnosticReport{Kind: lsp.ReportFull, ResultID: report.resultID, Items: report.items}, nil
}

func (s *Server) publishDiagnostics(ctx context.Context, uri string) {
	snap, err := s.snapshot(s.docs.View(), uri)
	if err != nil {
		return
	}
	report, err := s.diagnose(ctx, snap)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			s.logger.Error("computing diagnostics", "uri", uri, "err", err)
		}
		return
	}
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
