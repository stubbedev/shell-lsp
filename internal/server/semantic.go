package server

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/semantic"
	"github.com/stubbedev/shell-lsp/internal/structure"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type semanticLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

type semanticFull struct {
	Delta bool `json:"delta"`
}

type SemanticTokensOptions struct {
	Legend semanticLegend `json:"legend"`
	Range  bool           `json:"range"`
	Full   *semanticFull  `json:"full,omitempty"`
}

func semanticTokens(c *ServerCapabilities) *SemanticTokensOptions {
	options := ensure(&c.SemanticTokensProvider)
	options.Legend = semanticLegend{TokenTypes: semantic.TypeNames[:], TokenModifiers: semantic.ModifierNames}
	return options
}

type semanticCache struct {
	open    func(uri string) bool
	mu      sync.Mutex
	results map[string]lsp.SemanticTokens
	next    atomic.Uint64
}

func newSemanticCache(open func(uri string) bool) *semanticCache {
	return &semanticCache{open: open, results: map[string]lsp.SemanticTokens{}}
}

func (c *semanticCache) remember(uri string, data []uint32) lsp.SemanticTokens {
	tokens := lsp.SemanticTokens{ResultID: strconv.FormatUint(c.next.Add(1), 10), Data: data}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open(uri) {
		c.results[uri] = tokens
	}
	return tokens
}

func (c *semanticCache) previous(uri, resultID string) ([]uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, ok := c.results[uri]
	return prev.Data, ok && prev.ResultID == resultID
}

func (c *semanticCache) forget(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.results, uri)
}

func encodeTokens(snap *snapshot, tokens []semantic.Token, within *parser.Span) []uint32 {
	data := make([]uint32, 0, len(tokens)*5)
	var prevLine, prevChar uint32
	for _, t := range tokens {
		if within != nil && (t.Span.End <= within.Start || t.Span.Start >= within.End) {
			continue
		}
		r := snap.positions().Range(t.Span)
		start, end := r.Start, r.End
		if end.Line != start.Line || end.Character <= start.Character {
			continue
		}
		deltaChar := start.Character
		if start.Line == prevLine {
			deltaChar -= prevChar
		}
		data = append(data, start.Line-prevLine, deltaChar, end.Character-start.Character, uint32(t.Type), uint32(t.Modifiers))
		prevLine, prevChar = start.Line, start.Character
	}
	return data
}

func (s *Server) semanticTokens(snap *snapshot) []semantic.Token {
	return workspace.Memo(snap.File, "semantic.tokens", s.workspace.Generation(), func() []semantic.Token {
		return semantic.Tokens(snap.Tree, s.imports(snap.File), embeddedPrograms(snap.File))
	})
}

func (s *Server) handleSemanticTokensFull(_ context.Context, docs *docstore.View, p lsp.SemanticTokensParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return s.semantic.remember(p.TextDocument.URI, encodeTokens(snap, s.semanticTokens(snap), nil)), nil
}

func (s *Server) handleSemanticTokensDelta(_ context.Context, docs *docstore.View, p lsp.SemanticTokensDeltaParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	data := encodeTokens(snap, s.semanticTokens(snap), nil)
	previous, ok := s.semantic.previous(p.TextDocument.URI, p.PreviousResultID)
	current := s.semantic.remember(p.TextDocument.URI, data)
	if !ok {
		return current, nil
	}
	return lsp.SemanticTokensDelta{ResultID: current.ResultID, Edits: tokenEdits(previous, data)}, nil
}

func tokenEdits(before, after []uint32) []lsp.SemanticTokensEdit {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && before[prefix] == after[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix && before[len(before)-1-suffix] == after[len(after)-1-suffix] {
		suffix++
	}
	if prefix == len(before) && prefix == len(after) {
		return []lsp.SemanticTokensEdit{}
	}
	return []lsp.SemanticTokensEdit{{
		Start:       uint32(prefix),
		DeleteCount: uint32(len(before) - prefix - suffix), //nolint:gosec
		Data:        append([]uint32{}, after[prefix:len(after)-suffix]...),
	}}
}

func (s *Server) handleSemanticTokensRange(_ context.Context, docs *docstore.View, p lsp.SemanticTokensRangeParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	within, inShell := snap.positions().Span(p.Range)
	if !inShell {
		return lsp.SemanticTokens{Data: []uint32{}}, nil
	}
	return lsp.SemanticTokens{Data: encodeTokens(snap, s.semanticTokens(snap), &within)}, nil
}

func (s *Server) handleSelectionRange(_ context.Context, docs *docstore.View, p lsp.SelectionRangeParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	out := make([]lsp.SelectionRange, len(p.Positions))
	for i, pos := range p.Positions {
		offset, inside := snap.positions().Offset(pos)
		if !inside {
			source := snap.Doc.Source().Positions(snap.encoding)
			clamped, _ := source.Offset(pos)
			at := source.Position(clamped)
			out[i] = lsp.SelectionRange{Range: lsp.Range{Start: at, End: at}}
			continue
		}
		spans := structure.SelectionPath(snap.Tree, offset)
		var parent *lsp.SelectionRange
		for _, span := range slices.Backward(spans) {
			parent = &lsp.SelectionRange{Range: snap.positions().Range(span), Parent: parent}
		}
		out[i] = *parent
	}
	return out, nil
}
