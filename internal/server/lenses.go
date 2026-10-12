package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/structure"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const monikerScheme = "shell-lsp"

type CodeLensOptions struct {
	ResolveProvider bool `json:"resolveProvider"`
}

type lensData struct {
	URI      string       `json:"uri"`
	Position lsp.Position `json:"position"`
}

func (d lensData) at() lsp.TextDocumentPositionParams {
	return lsp.TextDocumentPositionParams{TextDocument: lsp.TextDocumentIdentifier{URI: d.URI}, Position: d.Position}
}

func (s *Server) handleCodeLens(_ context.Context, docs *docstore.View, p lsp.CodeLensParams) (any, error) {
	file, err := s.workspaceFile(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	enc := docs.Encoding()
	var spans []parser.Span
	for _, fn := range file.Tree.Functions {
		spans = append(spans, fn.NameSpan)
	}
	seen := map[string]bool{}
	for _, def := range file.Tree.Definitions {
		if def.Within != parser.FileScope || seen[def.Name] || def.Kind == parser.DefCommandEnv {
			continue
		}
		seen[def.Name] = true
		spans = append(spans, def.NameSpan)
	}
	out, err := appendBatsLenses(make([]lsp.CodeLens, 0, len(file.Tree.Tests)+1+len(spans)), file, enc)
	if err != nil {
		return nil, err
	}
	for _, span := range spans {
		r := file.Doc.RangeFor(span.Start, span.End, enc)
		data, err := json.Marshal(lensData{URI: file.URI, Position: r.Start})
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.CodeLens{Range: r, Data: data})
	}
	return out, nil
}

func (s *Server) handleCodeLensResolve(ctx context.Context, docs *docstore.View, lens lsp.CodeLens) (any, error) {
	if lens.Command != nil {
		return lens, nil
	}
	var data lensData
	if err := json.Unmarshal(lens.Data, &data); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "code lens data %s is not from this server: %v", lens.Data, err)
	}
	locations, err := s.referenceLocations(ctx, docs, data.at(), false)
	if err != nil {
		return nil, err
	}
	title := fmt.Sprintf("%d references", len(locations))
	if len(locations) == 1 {
		title = "1 reference"
	}
	args, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	lens.Command = &lsp.Command{Title: title, Command: commandShowReferences, Arguments: []json.RawMessage{args}}
	return lens, nil
}

type callItemData struct {
	Name     string `json:"name"`
	Start    int    `json:"start"`
	TopLevel bool   `json:"topLevel"`
}

func (s *Server) callHierarchyItem(item navigation.CallItem, enc docstore.Encoding) (lsp.CallHierarchyItem, error) {
	data, err := json.Marshal(callItemData{Name: item.Name, Start: item.Span.Start, TopLevel: item.TopLevel})
	if err != nil {
		return lsp.CallHierarchyItem{}, err
	}
	kind := lsp.SymbolFunction
	if item.TopLevel {
		kind = lsp.SymbolFile
	}
	return lsp.CallHierarchyItem{
		Name:           item.Name,
		Kind:           kind,
		Detail:         item.File.Path,
		URI:            item.File.URI,
		Range:          item.File.Doc.RangeFor(item.Span.Start, item.Span.End, enc),
		SelectionRange: item.File.Doc.RangeFor(item.Selection.Start, item.Selection.End, enc),
		Data:           data,
	}, nil
}

func (s *Server) loadURI(docs *docstore.View, uri string) (*workspace.File, error) {
	if file, err := s.workspaceFile(docs, uri); err == nil {
		return file, nil
	}
	path, err := fileuri.ToPath(uri)
	if err != nil {
		return nil, err
	}
	return s.workspace.Load(path)
}

func (s *Server) callItemFromLSP(docs *docstore.View, item lsp.CallHierarchyItem) (navigation.CallItem, error) {
	var data callItemData
	if err := json.Unmarshal(item.Data, &data); err != nil {
		return navigation.CallItem{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "call hierarchy item data %s is not from this server: %v", item.Data, err)
	}
	file, err := s.loadURI(docs, item.URI)
	if err != nil {
		return navigation.CallItem{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	if data.TopLevel {
		return navigation.CallItem{File: file, Name: data.Name, Span: parser.Span{End: len(file.Tree.Text())}, TopLevel: true}, nil
	}
	for _, fn := range file.Tree.Functions {
		if fn.Name == data.Name {
			return navigation.CallItem{File: file, Name: fn.Name, Span: fn.Span, Selection: fn.NameSpan}, nil
		}
	}
	return navigation.CallItem{}, jsonrpc.NewError(jsonrpc.CodeContentModified, "function %s no longer exists in %s", data.Name, item.URI)
}

func (s *Server) handlePrepareCallHierarchy(_ context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil || !inside {
		return nil, err
	}
	item, ok := navigation.PrepareCallHierarchy(here.File, here.offset, s.navigationEnv())
	if !ok {
		return nil, nil
	}
	out, err := s.callHierarchyItem(item, docs.Encoding())
	if err != nil {
		return nil, err
	}
	return []lsp.CallHierarchyItem{out}, nil
}

func (s *Server) handleIncomingCalls(ctx context.Context, docs *docstore.View, p lsp.CallHierarchyCallsParams) (any, error) {
	item, err := s.callItemFromLSP(docs, p.Item)
	if err != nil {
		return nil, err
	}
	calls, err := navigation.IncomingCalls(ctx, item, s.navigationEnv(), s.candidateFiles(docs))
	if err != nil {
		return nil, err
	}
	out := make([]lsp.CallHierarchyIncomingCall, 0, len(calls))
	for _, c := range calls {
		from, err := s.callHierarchyItem(c.Item, docs.Encoding())
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.CallHierarchyIncomingCall{From: from, FromRanges: callRanges(c, docs.Encoding())})
	}
	return out, nil
}

func (s *Server) handleOutgoingCalls(_ context.Context, docs *docstore.View, p lsp.CallHierarchyCallsParams) (any, error) {
	item, err := s.callItemFromLSP(docs, p.Item)
	if err != nil {
		return nil, err
	}
	calls := navigation.OutgoingCalls(item, s.navigationEnv())
	out := make([]lsp.CallHierarchyOutgoingCall, 0, len(calls))
	for _, c := range calls {
		to, err := s.callHierarchyItem(c.Item, docs.Encoding())
		if err != nil {
			return nil, err
		}
		ranges := make([]lsp.Range, len(c.Spans))
		for i, span := range c.Spans {
			ranges[i] = item.File.Doc.RangeFor(span.Start, span.End, docs.Encoding())
		}
		out = append(out, lsp.CallHierarchyOutgoingCall{To: to, FromRanges: ranges})
	}
	return out, nil
}

func callRanges(c navigation.Call, enc docstore.Encoding) []lsp.Range {
	ranges := make([]lsp.Range, len(c.Spans))
	for i, span := range c.Spans {
		ranges[i] = c.Item.File.Doc.RangeFor(span.Start, span.End, enc)
	}
	return ranges
}

func (s *Server) handleInlineValue(_ context.Context, docs *docstore.View, p lsp.InlineValueParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	within := parser.Span{Start: snap.Doc.PositionToOffset(p.Range.Start, snap.encoding), End: snap.Doc.PositionToOffset(p.Range.End, snap.encoding)}
	stopped := snap.Doc.PositionToOffset(p.Context.StoppedLocation.Start, snap.encoding)
	values := navigation.InlineValues(snap.Tree, within, stopped)
	out := make([]lsp.InlineValueVariableLookup, len(values))
	for i, v := range values {
		out[i] = lsp.InlineValueVariableLookup{Range: snap.Doc.RangeFor(v.Span.Start, v.Span.End, snap.encoding), VariableName: v.Name, CaseSensitiveLookup: true}
	}
	return out, nil
}

var monikerKinds = map[navigation.MonikerKind]string{
	navigation.MonikerExport: "export",
	navigation.MonikerImport: "import",
	navigation.MonikerLocal:  "local",
}

func (s *Server) handleMoniker(_ context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil || !inside {
		return nil, err
	}
	m, ok := navigation.MonikerAt(here.File, here.offset, s.navigationEnv())
	if !ok {
		return []lsp.Moniker{}, nil
	}
	unique := "document"
	if m.Workspace {
		unique = "project"
	}
	return []lsp.Moniker{{Scheme: monikerScheme, Identifier: m.Identifier, Unique: unique, Kind: monikerKinds[m.Kind]}}, nil
}

func (s *Server) handleLinkedEditingRange(_ context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil || !inside {
		return nil, err
	}
	spans, ok := structure.LinkedDelimiters(here.Tree, here.offset)
	if !ok {
		return nil, nil
	}
	ranges := make([]lsp.Range, len(spans))
	for i, span := range spans {
		ranges[i] = here.Doc.RangeFor(span.Start, span.End, here.encoding)
	}
	return lsp.LinkedEditingRanges{Ranges: ranges, WordPattern: `[A-Za-z0-9_.-]+`}, nil
}
