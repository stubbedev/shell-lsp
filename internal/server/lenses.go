package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/host/model"
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
	at := docs.Positions(file.Doc)
	var ranges []lsp.Range
	for _, fn := range file.Tree.Functions {
		if !file.Doc.Synthetic(fn.NameSpan.Start) {
			ranges = append(ranges, at.Range(fn.NameSpan))
		}
	}
	for def := range globalDefinitions(file.Tree) {
		if !file.Doc.Synthetic(def.NameSpan.Start) {
			ranges = append(ranges, at.Range(def.NameSpan))
		}
	}
	out, err := appendBatsLenses(make([]lsp.CodeLens, 0, len(file.Tree.Tests)+1+len(ranges)), file, at)
	if err != nil {
		return nil, err
	}
	if file.Native != nil {
		if out, err = appendRunLenses(out, file, docs.Encoding(), s.clientCapabilities().Experimental.ShellLsp.PromptsForArguments); err != nil {
			return nil, err
		}
		source := sourcePositions(file, docs.Encoding())
		for _, sym := range file.Native.Index.Symbols {
			if sym.Scope == model.Global {
				ranges = append(ranges, source.Range(sym.Selection))
			}
		}
	}
	for _, r := range ranges {
		data, err := encodeData(lensData{URI: file.URI, Position: r.Start})
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.CodeLens{Range: r, Data: data})
	}
	return out, nil
}

func appendRunLenses(out []lsp.CodeLens, file *workspace.File, enc docstore.Encoding, prompts bool) ([]lsp.CodeLens, error) {
	at := sourcePositions(file, enc)
	for _, r := range file.Native.Index.Runnables {
		args, title := recipeRunArgs{URI: file.URI, Name: r.Name}, "▶ Run"
		if len(r.Params) > 0 {
			if !prompts && r.Required() > 0 {
				continue
			}
			if prompts {
				args.Parameters, title = r.Params, "▶ Run…"
			}
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("encoding %s arguments for %s: %w", commandRunRecipe, file.URI, err)
		}
		out = append(out, lsp.CodeLens{Range: at.Range(r.Span), Command: &lsp.Command{Title: title, Command: commandRunRecipe, Arguments: []json.RawMessage{raw}}})
	}
	return out, nil
}

func (s *Server) handleCodeLensResolve(ctx context.Context, docs *docstore.View, lens lsp.CodeLens) (any, error) {
	if lens.Command != nil {
		return lens, nil
	}
	data, err := decodeData[lensData]("code lens", lens.Data)
	if err != nil {
		return nil, err
	}
	locations, err := s.referenceLocations(ctx, docs, data.at(), false)
	if err != nil {
		return nil, err
	}
	title := fmt.Sprintf("%d references", len(locations))
	if len(locations) == 1 {
		title = "1 reference"
	}
	args, err := encodeData(data)
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
	Native   bool   `json:"native,omitempty"`
}

func (s *Server) callHierarchyItem(item navigation.CallItem, docs *docstore.View) (lsp.CallHierarchyItem, error) {
	data, err := encodeData(callItemData{Name: item.Name, Start: item.Span.Start, TopLevel: item.TopLevel})
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
		Range:          docs.Positions(item.File.Doc).Range(item.Span),
		SelectionRange: docs.Positions(item.File.Doc).Range(item.Selection),
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
	data, err := decodeData[callItemData]("call hierarchy item", item.Data)
	if err != nil {
		return navigation.CallItem{}, err
	}
	file, err := s.loadURI(docs, item.URI)
	if err != nil {
		return navigation.CallItem{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	if data.TopLevel {
		return navigation.CallItem{File: file, Name: data.Name, Span: parser.Span{End: len(file.Tree.Text())}, TopLevel: true}, nil
	}
	for _, fn := range file.Tree.Functions {
		if fn.Name == data.Name && fn.Span.Start == data.Start {
			return navigation.CallItem{File: file, Name: fn.Name, Span: fn.Span, Selection: fn.NameSpan}, nil
		}
	}
	return navigation.CallItem{}, jsonrpc.NewError(jsonrpc.CodeContentModified, "function %s no longer exists in %s", data.Name, item.URI)
}

func (s *Server) handlePrepareCallHierarchy(ctx context.Context, docs *docstore.View, position lsp.TextDocumentPositionParams) (any, error) {
	here, inside, err := s.at(docs, position)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, position.Position, inside); ok && !inside {
		return s.nativePrepareCalls(ctx, docs, native)
	}
	if !inside {
		return nil, nil
	}
	items := navigation.PrepareCallHierarchy(here.File, here.offset, s.navigationEnv())
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]lsp.CallHierarchyItem, 0, len(items))
	for _, item := range items {
		lspItem, err := s.callHierarchyItem(item, docs)
		if err != nil {
			return nil, err
		}
		out = append(out, lspItem)
	}
	return out, nil
}

func (s *Server) handleIncomingCalls(ctx context.Context, docs *docstore.View, p lsp.CallHierarchyCallsParams) (any, error) {
	if g, sym, ok, err := s.nativeCallItem(ctx, docs, p.Item); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return s.nativeCalls(docs, g.Incoming(sym), true)
	}
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
		from, err := s.callHierarchyItem(c.Item, docs)
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.CallHierarchyIncomingCall{From: from, FromRanges: docs.Positions(c.Item.File.Doc).Ranges(c.Spans)})
	}
	return out, nil
}

func (s *Server) handleOutgoingCalls(ctx context.Context, docs *docstore.View, p lsp.CallHierarchyCallsParams) (any, error) {
	if g, sym, ok, err := s.nativeCallItem(ctx, docs, p.Item); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return s.nativeCalls(docs, g.Outgoing(sym), false)
	}
	item, err := s.callItemFromLSP(docs, p.Item)
	if err != nil {
		return nil, err
	}
	calls := navigation.OutgoingCalls(item, s.navigationEnv())
	out := make([]lsp.CallHierarchyOutgoingCall, 0, len(calls))
	for _, c := range calls {
		to, err := s.callHierarchyItem(c.Item, docs)
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.CallHierarchyOutgoingCall{To: to, FromRanges: docs.Positions(item.File.Doc).Ranges(c.Spans)})
	}
	return out, nil
}

func (s *Server) handleInlineValue(_ context.Context, docs *docstore.View, p lsp.InlineValueParams) (any, error) {
	snap, err := s.snapshot(docs, p.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	at := snap.positions()
	within, inShell := at.Span(p.Range)
	stopped, stoppedInShell := at.Offset(p.Context.StoppedLocation.Start)
	if !inShell || !stoppedInShell {
		return []lsp.InlineValueVariableLookup{}, nil
	}
	values := navigation.InlineValues(snap.Tree, within, stopped)
	out := make([]lsp.InlineValueVariableLookup, len(values))
	for i, v := range values {
		out[i] = lsp.InlineValueVariableLookup{Range: at.Range(v.Span), VariableName: v.Name, CaseSensitiveLookup: true}
	}
	return out, nil
}

func monikerKind(kind navigation.MonikerKind) lsp.MonikerKind {
	switch kind {
	case navigation.MonikerExport:
		return lsp.MonikerExport
	case navigation.MonikerImport:
		return lsp.MonikerImport
	case navigation.MonikerLocal:
		return lsp.MonikerLocal
	}
	panic(fmt.Sprintf("server: moniker kind %d has no LSP kind", kind))
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
	unique := lsp.UniqueDocument
	if m.Workspace {
		unique = lsp.UniqueProject
	}
	return []lsp.Moniker{{Scheme: monikerScheme, Identifier: m.Identifier, Unique: unique, Kind: monikerKind(m.Kind)}}, nil
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
	return lsp.LinkedEditingRanges{Ranges: here.positions().Ranges(spans), WordPattern: `[A-Za-z0-9_.-]+`}, nil
}
