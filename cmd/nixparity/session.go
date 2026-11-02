package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const (
	maxRestarts        = 3
	wedgedAfterTimeout = 3
	diagnosticsSilence = 10 * time.Second
	diagnosticsSettle  = 500 * time.Millisecond
	idleSettle         = 3 * time.Second
)

type session struct {
	cfg      serverConfig
	norm     normalizer
	docs     []document
	srv      *server
	info     identity
	opened   []time.Time
	restarts int
	timeouts int
	peakRSS  *int64
	errs     []error
}

type sideRun struct {
	identity identity
	answers  []answer
	restarts int
	peakRSS  *int64
	idleRSS  *int64
}

func measure(ctx context.Context, cfg serverConfig, p plan) (sideRun, error) {
	s := &session{cfg: cfg, norm: normalizer{root: p.root}, docs: p.docs}
	answers, err := s.sweep(ctx, p.queries)
	idle := s.idleRSS(ctx)
	s.retire()
	if err := errors.Join(append(s.errs, err)...); err != nil {
		return sideRun{}, fmt.Errorf("measuring %s: %w", cfg.name, err)
	}
	return sideRun{identity: s.info, answers: answers, restarts: s.restarts, peakRSS: s.peakRSS, idleRSS: idle}, nil
}

func (s *session) idleRSS(ctx context.Context) *int64 {
	if s.srv == nil || s.srv.isDead() {
		return nil
	}
	select {
	case <-time.After(idleSettle):
	case <-ctx.Done():
		return nil
	}
	rss, err := currentTreeRSS(s.srv.cmd.Process.Pid)
	if err != nil {
		if !errors.Is(err, errRSSUnsupported) {
			fmt.Fprintf(os.Stderr, "nixparity: sampling the idle memory of %s: %v\n", s.cfg.name, err)
		}
		return nil
	}
	return &rss
}

func (s *session) sweep(ctx context.Context, queries []query) ([]answer, error) {
	if err := s.launch(ctx); err != nil {
		return nil, err
	}
	s.info = s.srv.info
	published := s.diagnostics(ctx)
	answers := make([]answer, len(queries))
	for i, q := range queries {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("query %d of %d: %w", i+1, len(queries), err)
		}
		if q.feature == featureDiagnostics {
			answers[i] = published[q.file]
			continue
		}
		answers[i] = s.ask(ctx, q)
	}
	return answers, nil
}

func (s *session) launch(ctx context.Context) error {
	srv, err := startServer(ctx, s.cfg, s.norm)
	if err != nil {
		return err
	}
	s.srv = srv
	s.timeouts = 0
	s.opened = make([]time.Time, len(s.docs))
	for i, d := range s.docs {
		s.opened[i] = time.Now()
		item := lsp.TextDocumentItem{URI: d.doc.URI, LanguageID: languageID, Version: 1, Text: d.doc.Text()}
		if err := srv.notify("textDocument/didOpen", lsp.DidOpenTextDocumentParams{TextDocument: item}); err != nil {
			s.retire()
			return fmt.Errorf("opening %s: %w", d.rel, err)
		}
	}
	return nil
}

func (s *session) retire() {
	if s.srv == nil {
		return
	}
	peak, err := s.srv.close()
	s.srv = nil
	if err != nil {
		s.errs = append(s.errs, err)
	}
	if peak != nil && (s.peakRSS == nil || *peak > *s.peakRSS) {
		s.peakRSS = peak
	}
}

func (s *session) live(ctx context.Context) *server {
	if s.srv != nil && !s.srv.isDead() && s.timeouts < wedgedAfterTimeout {
		return s.srv
	}
	s.retire()
	for s.restarts < maxRestarts && ctx.Err() == nil {
		s.restarts++
		err := s.launch(ctx)
		if err == nil {
			return s.srv
		}
		fmt.Fprintf(os.Stderr, "nixparity: restarting %s: %v\n", s.cfg.name, err)
	}
	return nil
}

func (s *session) ask(ctx context.Context, q query) answer {
	srv := s.live(ctx)
	if srv == nil {
		return answer{outcome: outcomeCrashed}
	}
	spec := q.feature.spec()
	if !srv.supports(spec.capability) {
		return answer{outcome: outcomeUnsupported}
	}
	d := s.docs[q.file]
	r := srv.call(ctx, spec.method, requestParams(q, d.doc.URI), s.cfg.timeout)
	if r.state == replyTimedOut {
		s.timeouts++
	} else {
		s.timeouts = 0
	}
	a, err := classify(spec, decodeContext{norm: s.norm, doc: d.doc}, r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nixparity: %s at %s %s: %v\n", s.cfg.name, d.rel, positionKey(q.position), err)
	}
	return a
}

func (s *session) diagnostics(ctx context.Context) []answer {
	rels := make([]string, len(s.docs))
	for i, d := range s.docs {
		rels[i] = d.rel
	}
	s.srv.awaitDiagnostics(ctx, rels, diagnosticsSilence, diagnosticsSettle)
	spec := featureDiagnostics.spec()
	out := make([]answer, len(s.docs))
	for i, d := range s.docs {
		entry, ok := s.srv.publication(d.rel)
		switch {
		case ok:
			a, err := decodeResult(spec, decodeContext{norm: s.norm, doc: d.doc}, reply{result: entry.raw, elapsed: entry.first.Sub(s.opened[i])})
			if err != nil {
				fmt.Fprintf(os.Stderr, "nixparity: %s on %s: %v\n", s.cfg.name, d.rel, err)
			}
			out[i] = a
		case s.srv.isDead():
			out[i] = answer{outcome: outcomeCrashed}
		default:
			out[i] = answer{outcome: outcomeSilent}
		}
	}
	return out
}
