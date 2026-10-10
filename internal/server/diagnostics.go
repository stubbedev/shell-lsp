package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
)

const diagnosticsDebounce = 200 * time.Millisecond

type shellcheckKey struct {
	version    int32
	generation uint64
	binary     string
	sources    uint64
}

type shellcheckRun struct {
	key     shellcheckKey
	done    chan struct{}
	cancel  context.CancelFunc
	items   []lsp.Diagnostic
	ok      bool
	refresh sync.Once
}

func (r *shellcheckRun) finished() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

type diagnosticsState struct {
	mu      sync.Mutex
	runs    map[string]*shellcheckRun
	partial atomic.Uint64
}

func newDiagnosticsState() *diagnosticsState {
	return &diagnosticsState{runs: map[string]*shellcheckRun{}}
}

func (d *diagnosticsState) start(uri string, key shellcheckKey, launch func(ctx context.Context) ([]lsp.Diagnostic, bool)) *shellcheckRun {
	d.mu.Lock()
	defer d.mu.Unlock()
	if current, ok := d.runs[uri]; ok {
		if current.key == key {
			return current
		}
		current.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &shellcheckRun{key: key, done: make(chan struct{}), cancel: cancel}
	d.runs[uri] = run
	go func() {
		defer cancel()
		run.items, run.ok = launch(ctx)
		close(run.done)
	}()
	return run
}

func (d *diagnosticsState) forget(uri string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if run, ok := d.runs[uri]; ok {
		run.cancel()
		delete(d.runs, uri)
	}
}

type diagnosticReport struct {
	version  int32
	resultID string
	items    []lsp.Diagnostic
	pending  *shellcheckRun
}

type shellcheckPlan struct {
	args    []string
	binary  diagnostics.Binary
	sources uint64
}

func (s *Server) diagnose(ctx context.Context, snap *snapshot, wait bool) (diagnosticReport, error) {
	items := diagnostics.Syntax(snap.tree, snap.doc, snap.encoding)
	plan, planned := s.planShellcheck(snap)
	report := diagnosticReport{
		version:  snap.doc.Version,
		resultID: fmt.Sprintf("%d/%d", snap.doc.Version, snap.generation),
	}
	if planned {
		report.resultID += fmt.Sprintf("/%s/%d", plan.binary.ID, plan.sources)
	}
	if planned {
		run := s.startShellcheck(snap, plan)
		if wait {
			select {
			case <-run.done:
			case <-ctx.Done():
				return diagnosticReport{}, ctx.Err()
			}
		}
		switch {
		case !run.finished():
			report.pending = run
			report.resultID += "/pending-" + strconv.FormatUint(s.diagnostics.partial.Add(1), 10)
		case run.ok:
			items = append(items, run.items...)
		default:
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
	return shellcheckPlan{args: args, binary: binary, sources: s.sourcesStamp(snap)}, true
}

func (s *Server) sourcesStamp(snap *snapshot) uint64 {
	if !slices.ContainsFunc(snap.tree.Commands, navigation.IsSource) {
		return 0
	}
	return s.workspace.Generation()
}

func (s *Server) startShellcheck(snap *snapshot, plan shellcheckPlan) *shellcheckRun {
	uri := snap.doc.URI
	key := shellcheckKey{version: snap.doc.Version, generation: snap.generation, binary: plan.binary.ID, sources: plan.sources}
	return s.diagnostics.start(uri, key, func(ctx context.Context) ([]lsp.Diagnostic, bool) {
		output, err := diagnostics.RunShellcheck(ctx, plan.binary, plan.args, scriptDir(uri), snap.doc.Text())
		if ctx.Err() != nil {
			return nil, false
		}
		if err != nil {
			s.noticeOnce("shellcheck-failed:"+err.Error(), lsp.MessageError, err.Error())
			return nil, false
		}
		items, err := diagnostics.ParseShellcheck(output, snap.doc, snap.encoding)
		if err != nil {
			s.noticeOnce("shellcheck-output:"+err.Error(), lsp.MessageError, err.Error())
			return nil, false
		}
		return items, true
	})
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
	refresh := s.clientCapabilities().Workspace.Diagnostics.RefreshSupport
	report, err := s.diagnose(ctx, snap, !refresh)
	if err != nil {
		return nil, err
	}
	if run := report.pending; run != nil {
		background := context.WithoutCancel(ctx)
		go func() {
			<-run.done
			if run.ok {
				run.refresh.Do(func() { s.requestRefresh(background, "workspace/diagnostic/refresh") })
			}
		}()
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
	report, err := s.diagnose(ctx, snap, false)
	if err != nil {
		return
	}
	s.publish(uri, report)
	if report.pending == nil {
		return
	}
	select {
	case <-report.pending.done:
	case <-ctx.Done():
		return
	}
	if full, err := s.diagnose(ctx, snap, true); err == nil {
		s.publish(uri, full)
	}
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
