package server

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/nix/highlight"
	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
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

type placedToken struct {
	r lsp.Range
	t semantic.Token
}

func place(out []placedToken, at docstore.Positions, tokens []semantic.Token) []placedToken {
	for _, t := range tokens {
		r := at.Range(t.Span)
		if r.End.Line == r.Start.Line && r.End.Character > r.Start.Character {
			out = append(out, placedToken{r: r, t: t})
		}
	}
	return out
}

func before(a, b lsp.Position) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Character < b.Character
}

func encodeTokens(placed []placedToken, within *lsp.Range) []uint32 {
	slices.SortStableFunc(placed, func(a, b placedToken) int {
		return cmp.Or(cmp.Compare(a.r.Start.Line, b.r.Start.Line), cmp.Compare(a.r.Start.Character, b.r.Start.Character))
	})
	data := make([]uint32, 0, len(placed)*5)
	var prevLine, prevChar uint32
	for _, p := range placed {
		start, end := p.r.Start, p.r.End
		if within != nil && (!before(within.Start, end) || !before(start, within.End)) {
			continue
		}
		deltaChar := start.Character
		if start.Line == prevLine {
			deltaChar -= prevChar
		}
		data = append(data, start.Line-prevLine, deltaChar, end.Character-start.Character, uint32(p.t.Type), uint32(p.t.Modifiers))
		prevLine, prevChar = start.Line, start.Character
	}
	return data
}

func placeOrdered(at docstore.Positions, tokens []semantic.Token) []placedToken {
	text := at.Document().Text()
	enc := at.Encoding()
	from := 0
	var here lsp.Position
	advance := func(offset int) lsp.Position {
		between := text[from:offset]
		if strings.IndexByte(between, '\n') >= 0 {
			here = at.Position(offset)
		} else {
			here.Character += enc.Units(between)
		}
		from = offset
		return here
	}
	out := make([]placedToken, len(tokens))
	for i, t := range tokens {
		start := advance(t.Span.Start)
		out[i] = placedToken{r: lsp.Range{Start: start, End: advance(t.Span.End)}, t: t}
	}
	return out
}

func (s *Server) placedTokens(snap *snapshot) []placedToken {
	if snap.Native == nil {
		return place(nil, snap.positions(), s.semanticTokens(snap.File))
	}
	source := sourcePositions(snap.File, snap.encoding)
	if snap.Native.Language == host.Nix {
		return placeOrdered(source, s.nixTokens(snap.File))
	}
	shell := slices.DeleteFunc(place(nil, snap.positions(), s.semanticTokens(snap.File)), func(p placedToken) bool {
		_, inside := snap.positions().Offset(p.r.Start)
		return !inside
	})
	return place(shell, source, snap.Native.Index.Tokens)
}

func (s *Server) semanticTokens(file *workspace.File) []semantic.Token {
	return workspace.Memo(file, "semantic.tokens", s.workspace.Generation(), func() []semantic.Token {
		return semantic.Tokens(file.Tree, s.imports(file), embeddedPrograms(file))
	})
}

func (s *Server) nixTokens(file *workspace.File) []semantic.Token {
	return workspace.Memo(file, "semantic.nix", s.workspace.Generation(), func() []semantic.Token {
		tree := syntax.Parse(file.Doc.Source().Text())
		return highlight.Tokens(tree, scope.Resolve(tree), s.hostShellTokens(file))
	})
}

func (s *Server) hostShellTokens(file *workspace.File) []semantic.Token {
	var out []semantic.Token
	for _, variant := range file.Variants() {
		for _, t := range s.semanticTokens(variant) {
			t.Span = parser.Span{Start: file.Doc.SourceOffset(t.Span.Start), End: file.Doc.SourceOffset(t.Span.End)}
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b semantic.Token) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	return out
}

func (s *Server) handleSemanticTokensFull(_ context.Context, docs *docstore.View, p lsp.SemanticTokensParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return s.semantic.remember(p.TextDocument.URI, encodeTokens(s.placedTokens(snap), nil)), nil
}

func (s *Server) handleSemanticTokensDelta(_ context.Context, docs *docstore.View, p lsp.SemanticTokensDeltaParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	data := encodeTokens(s.placedTokens(snap), nil)
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
	return lsp.SemanticTokens{Data: encodeTokens(s.placedTokens(snap), &p.Range)}, nil
}

func (s *Server) handleSelectionRange(_ context.Context, docs *docstore.View, p lsp.SelectionRangeParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	out := make([]lsp.SelectionRange, len(p.Positions))
	source := sourcePositions(snap.File, snap.encoding)
	for i, pos := range p.Positions {
		offset, inside := snap.positions().Offset(pos)
		var ranges []lsp.Range
		at := snap.Doc.Source().PositionToOffset(pos, snap.encoding)
		outer := parser.Span{Start: at, End: at}
		if inside {
			for _, span := range structure.SelectionPath(snap.Tree, offset) {
				ranges = append(ranges, snap.positions().Range(span))
				outer = parser.Span{Start: snap.Doc.SourceOffset(span.Start), End: snap.Doc.SourceOffset(span.End)}
			}
			at = snap.Doc.SourceOffset(offset)
		}
		if snap.Native != nil {
			for _, span := range snap.Native.Enclosing(at) {
				if span.Start <= outer.Start && outer.End <= span.End && span != outer {
					ranges = append(ranges, source.Range(span))
				}
			}
		}
		if len(ranges) == 0 {
			position := source.Position(at)
			ranges = []lsp.Range{{Start: position, End: position}}
		}
		ranges = slices.Compact(ranges)
		var parent *lsp.SelectionRange
		for _, r := range slices.Backward(ranges) {
			parent = &lsp.SelectionRange{Range: r, Parent: parent}
		}
		out[i] = *parent
	}
	return out, nil
}
