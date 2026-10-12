package server

import (
	"context"
	"encoding/json"
	"strconv"
)

type progressCreateParams struct {
	Token string `json:"token"`
}

type progressParams struct {
	Token string        `json:"token"`
	Value progressValue `json:"value"`
}

type progressValue struct {
	Kind        string `json:"kind"`
	Title       string `json:"title,omitempty"`
	Message     string `json:"message,omitempty"`
	Percentage  int    `json:"percentage,omitempty"`
	Cancellable bool   `json:"cancellable,omitempty"`
}

type Progress struct {
	server *Server
	token  string
	ctx    context.Context
	cancel context.CancelFunc
}

func (s *Server) StartProgress(ctx context.Context, title string) (*Progress, error) {
	token := "shell-lsp/progress-" + strconv.FormatInt(s.nextID.Add(1), 10)
	if err := s.call(ctx, progressCreateMethod, progressCreateParams{Token: token}, nil); err != nil {
		return nil, err
	}
	pctx, cancel := context.WithCancel(ctx)
	p := &Progress{server: s, token: token, ctx: pctx, cancel: cancel}
	s.progress.Store(token, cancel)
	return p, p.notify(progressValue{Kind: "begin", Title: title, Cancellable: true})
}

func (p *Progress) Context() context.Context { return p.ctx }

func (p *Progress) Report(message string, percentage int) error {
	return p.notify(progressValue{Kind: "report", Message: message, Percentage: percentage})
}

func (p *Progress) End(message string) error {
	p.server.progress.Delete(p.token)
	p.cancel()
	return p.notify(progressValue{Kind: "end", Message: message})
}

func (p *Progress) notify(value progressValue) error {
	return p.server.conn.Notify(progressMethod, progressParams{Token: p.token, Value: value})
}

type workDone struct {
	server   *Server
	progress *Progress
}

func (s *Server) beginWorkDone(ctx context.Context, title string) (*workDone, context.Context, error) {
	if !s.clientCapabilities().Window.WorkDoneProgress {
		return &workDone{server: s}, ctx, nil
	}
	progress, err := s.StartProgress(ctx, title)
	if err != nil {
		return nil, nil, err
	}
	return &workDone{server: s, progress: progress}, progress.Context(), nil
}

func (w *workDone) report(message string, percentage int) {
	if w.progress == nil {
		return
	}
	if err := w.progress.Report(message, percentage); err != nil {
		w.server.logger.Warn("reporting progress", "err", err)
	}
}

func (w *workDone) end(message string) {
	if w.progress == nil {
		return
	}
	if err := w.progress.End(message); err != nil {
		w.server.logger.Warn("ending progress", "err", err)
	}
}

func (s *Server) handleWorkDoneProgressCancel(_ context.Context, cancelParams progressCreateParams) error {
	if found, ok := s.progress.LoadAndDelete(cancelParams.Token); ok {
		if cancel, ok := found.(context.CancelFunc); ok {
			cancel()
		}
	}
	return nil
}

type logTraceParams struct {
	Message string `json:"message"`
	Verbose string `json:"verbose,omitempty"`
}

func (s *Server) traceIncoming(kind, method string, id, params json.RawMessage) {
	s.mu.Lock()
	trace := s.trace
	s.mu.Unlock()
	if trace == TraceOff || trace == "" {
		return
	}
	message := "Received " + kind + " '" + method + "'."
	if len(id) > 0 {
		message = "Received " + kind + " '" + method + " - (" + string(id) + ")'."
	}
	entry := logTraceParams{Message: message}
	if trace == TraceVerbose && len(params) > 0 {
		entry.Verbose = "Params: " + string(params)
	}
	s.notify(logTraceMethod, entry)
}
