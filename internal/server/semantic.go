package server

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
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
	Full   semanticFull   `json:"full"`
}

var semanticTokensOptions = SemanticTokensOptions{
	Legend: semanticLegend{TokenTypes: semantic.TypeNames, TokenModifiers: semantic.ModifierNames},
	Range:  true,
	Full:   semanticFull{Delta: true},
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
		start := snap.doc.OffsetToPosition(t.Span.Start, snap.encoding)
		end := snap.doc.OffsetToPosition(t.Span.End, snap.encoding)
		deltaChar := start.Character
		if start.Line == prevLine {
			deltaChar -= prevChar
		}
		data = append(data, start.Line-prevLine, deltaChar, end.Character-start.Character, uint32(t.Type), uint32(t.Modifiers))
		prevLine, prevChar = start.Line, start.Character
	}
	return data
}

func (s *Server) semanticSnapshot(docs *docstore.View, uri string) (*snapshot, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	return snap, nil
}

func (s *Server) semanticTokens(snap *snapshot) []semantic.Token {
	return workspace.Memo(snap.file, "semantic.tokens", s.workspace.Generation(), func() []semantic.Token {
		return semantic.Tokens(snap.tree, s.imports(snap.file), embeddedPrograms(snap.file))
	})
}

func (s *Server) handleSemanticTokensFull(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	p, err := lsp.DecodeParams[lsp.SemanticTokensParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing semantic tokens params: %v", err)
	}
	snap, err := s.semanticSnapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return s.semantic.remember(p.TextDocument.URI, encodeTokens(snap, s.semanticTokens(snap), nil)), nil
}

func (s *Server) handleSemanticTokensDelta(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	p, err := lsp.DecodeParams[lsp.SemanticTokensDeltaParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing semantic tokens delta params: %v", err)
	}
	snap, err := s.semanticSnapshot(docs, p.TextDocument.URI)
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

func (s *Server) handleSemanticTokensRange(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	p, err := lsp.DecodeParams[lsp.SemanticTokensRangeParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing semantic tokens range params: %v", err)
	}
	snap, err := s.semanticSnapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	within := parser.Span{
		Start: snap.doc.PositionToOffset(p.Range.Start, snap.encoding),
		End:   snap.doc.PositionToOffset(p.Range.End, snap.encoding),
	}
	return lsp.SemanticTokens{Data: encodeTokens(snap, s.semanticTokens(snap), &within)}, nil
}

func (s *Server) handleSelectionRange(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	p, err := lsp.DecodeParams[lsp.SelectionRangeParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/selectionRange params: %v", err)
	}
	snap, err := s.semanticSnapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	out := make([]lsp.SelectionRange, len(p.Positions))
	for i, pos := range p.Positions {
		spans := structure.SelectionPath(snap.tree, snap.doc.PositionToOffset(pos, snap.encoding))
		var parent *lsp.SelectionRange
		for _, span := range slices.Backward(spans) {
			parent = &lsp.SelectionRange{Range: snap.doc.RangeFor(span.Start, span.End, snap.encoding), Parent: parent}
		}
		out[i] = *parent
	}
	return out, nil
}
