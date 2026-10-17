package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/hostrun"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func (s *Server) hostEnv(docs *docstore.View, language host.Language) hostnav.Env {
	return hostnav.Env{Loader: s.workspace, Home: s.process.Home, Files: s.hostFiles(docs, language)}
}

func (s *Server) hostFiles(docs *docstore.View, language host.Language) navigation.WorkspaceFiles {
	return func(ctx context.Context) ([]*workspace.File, error) {
		paths, err := s.workspace.ShellFiles(ctx)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs.Documents() {
			if l, ok := host.Of(doc.URI, doc.Language); ok && l == language && doc.Local {
				paths = append(paths, doc.Path)
			}
		}
		paths = slices.DeleteFunc(paths, func(path string) bool {
			l, ok := host.FromName(path)
			return !ok || l != language
		})
		slices.Sort(paths)
		return s.workspace.LoadAll(ctx, slices.Compact(paths), interactive.workers(), func(int, int) {})
	}
}

func (s *Server) group(ctx context.Context, docs *docstore.View, file *workspace.File) (*hostnav.Group, error) {
	return hostnav.Build(ctx, file, s.hostEnv(docs, file.Native.Language))
}

func sourcePositions(file *workspace.File, enc docstore.Encoding) docstore.Positions {
	return file.Doc.Source().Positions(enc)
}

func hostOffset(here located, pos lsp.Position) int {
	offset, _ := sourcePositions(here.File, here.encoding).Offset(pos)
	return offset
}

type nativeHit struct {
	group  *hostnav.Group
	symbol hostnav.Symbol
	found  bool
}

func (s *Server) nativeAt(ctx context.Context, docs *docstore.View, here located, pos lsp.Position, inside bool) (nativeHit, error) {
	g, err := s.group(ctx, docs, here.File)
	if err != nil {
		return nativeHit{}, err
	}
	hit := nativeHit{group: g}
	if inside {
		hit.symbol, hit.found = g.ShellSymbol(here.offset)
		return hit, nil
	}
	hit.symbol, hit.found = g.At(hostOffset(here, pos))
	return hit, nil
}

func hostLocations(locations []hostnav.Location, enc docstore.Encoding) []lsp.Location {
	out := make([]lsp.Location, len(locations))
	for i, l := range locations {
		out[i] = lsp.Location{URI: l.File.URI, Range: sourcePositions(l.File, enc).Range(l.Span)}
	}
	return out
}

func hostOccurrences(occs []hostnav.Occurrence, enc docstore.Encoding) []lsp.Location {
	out := make([]lsp.Location, len(occs))
	for i, o := range occs {
		out[i] = lsp.Location{URI: o.File.URI, Range: sourcePositions(o.File, enc).Range(o.Span)}
	}
	return out
}

func (s *Server) nativeDefinition(ctx context.Context, docs *docstore.View, here located, pos lsp.Position, inside bool) ([]lsp.Location, bool, error) {
	if !inside {
		g, err := s.group(ctx, docs, here.File)
		if err != nil {
			return nil, true, err
		}
		return hostLocations(g.Definition(hostOffset(here, pos)), here.encoding), true, nil
	}
	hit, err := s.nativeAt(ctx, docs, here, pos, true)
	if err != nil || !hit.found {
		return nil, err != nil, err
	}
	return hostLocations(hit.group.ShellDefinition(hit.symbol), here.encoding), true, nil
}

func nativeKind(kind model.Kind) lsp.SymbolKind {
	switch kind {
	case model.KindRecipe, model.KindAlias, model.KindFunction, model.KindTarget:
		return lsp.SymbolFunction
	case model.KindVariable:
		return lsp.SymbolVariable
	case model.KindParameter:
		return lsp.SymbolVariable
	case model.KindModule:
		return lsp.SymbolModule
	case model.KindSetting, model.KindAttribute, model.KindKeyword:
		return lsp.SymbolProperty
	}
	panic(fmt.Sprintf("server: host kind %s has no LSP symbol kind", kind))
}

func nativeCompletionKind(kind model.Kind) lsp.CompletionItemKind {
	switch kind {
	case model.KindRecipe, model.KindAlias, model.KindFunction, model.KindTarget:
		return lsp.CompletionFunction
	case model.KindVariable, model.KindParameter:
		return lsp.CompletionVariable
	case model.KindModule:
		return lsp.CompletionModule
	case model.KindSetting, model.KindAttribute:
		return lsp.CompletionProperty
	case model.KindKeyword:
		return lsp.CompletionKeyword
	}
	panic(fmt.Sprintf("server: host kind %s has no LSP completion kind", kind))
}

func (s *Server) nativeCompletion(ctx context.Context, docs *docstore.View, here located, pos lsp.Position) (lsp.CompletionList, error) {
	g, err := s.group(ctx, docs, here.File)
	if err != nil {
		return lsp.CompletionList{}, err
	}
	at := sourcePositions(here.File, here.encoding)
	items := []lsp.CompletionItem{}
	for _, it := range g.Complete(hostOffset(here, pos)) {
		item := lsp.CompletionItem{
			Label:            it.Label,
			Kind:             nativeCompletionKind(it.Kind),
			Detail:           it.Detail,
			InsertTextFormat: lsp.InsertPlainText,
			TextEdit:         &lsp.CompletionEdit{NewText: it.Label, Insert: at.Range(it.Span)},
		}
		if it.Doc != "" {
			item.Documentation = lsp.Markdown(it.Doc)
		}
		items = append(items, item)
	}
	return lsp.CompletionList{Items: items}, nil
}

func (s *Server) nativeSymbols(snap *snapshot) []lsp.DocumentSymbol {
	ix := snap.Native.Index
	at := sourcePositions(snap.File, snap.encoding)
	var build func(parent int) []lsp.DocumentSymbol
	build = func(parent int) []lsp.DocumentSymbol {
		out := []lsp.DocumentSymbol{}
		for _, i := range ix.Children(parent) {
			sym := ix.Symbols[i]
			out = append(out, lsp.DocumentSymbol{
				Name:           sym.Name,
				Detail:         sym.Detail,
				Kind:           nativeKind(sym.Kind),
				Range:          at.Range(sym.Span),
				SelectionRange: at.Range(sym.Selection),
				Children:       build(i),
			})
		}
		return out
	}
	return build(model.NoParent)
}

func flatten(out []lsp.SymbolInformation, uri string, symbols []lsp.DocumentSymbol, container string) []lsp.SymbolInformation {
	for _, sym := range symbols {
		out = append(out, lsp.SymbolInformation{Name: sym.Name, Kind: sym.Kind, Location: lsp.Location{URI: uri, Range: sym.Range}, ContainerName: container})
		out = flatten(out, uri, sym.Children, sym.Name)
	}
	return out
}

func nativeFolds(snap *snapshot) []lsp.FoldingRange {
	at := sourcePositions(snap.File, snap.encoding)
	var out []lsp.FoldingRange
	for _, f := range snap.Native.Index.Folds {
		r := at.Range(f.Span)
		if r.End.Line <= r.Start.Line {
			continue
		}
		fold := lsp.FoldingRange{StartLine: r.Start.Line, EndLine: r.End.Line}
		if f.Comment {
			fold.Kind = lsp.FoldingComment
		}
		out = append(out, fold)
	}
	return out
}

func (s *Server) nativeRename(hit nativeHit, enc docstore.Encoding, docs *docstore.View, newName string) (lsp.WorkspaceEdit, error) {
	occs, err := hit.group.Rename(hit.symbol, newName)
	if err != nil {
		return lsp.WorkspaceEdit{}, renameError(err)
	}
	var files []fileEdits
	index := map[string]int{}
	for _, o := range occs {
		i, ok := index[o.File.URI]
		if !ok {
			_, open := docs.Document(o.File.URI)
			i = len(files)
			index[o.File.URI] = i
			files = append(files, fileEdits{doc: o.File.Doc.Source(), versioned: open})
		}
		files[i].edits = append(files[i].edits, textedit.Edit{Start: o.Span.Start, End: o.Span.End, NewText: newName})
	}
	return s.multiFileEdit(enc, files)
}

func (s *Server) nativeHover(snap *snapshot, h hostnav.Hover) lsp.Hover {
	r := sourcePositions(snap.File, snap.encoding).Range(h.Span)
	return lsp.Hover{Contents: lsp.MarkupContent{Kind: s.hoverFormat(), Value: h.Markdown}, Range: &r}
}

func (s *Server) nativeHoverAt(ctx context.Context, docs *docstore.View, here located, pos lsp.Position, inside bool) (any, bool, error) {
	g, err := s.group(ctx, docs, here.File)
	if err != nil {
		return nil, true, err
	}
	if inside {
		sym, ok := g.ShellSymbol(here.offset)
		if !ok {
			return nil, false, nil
		}
		return s.nativeHover(here.snapshot, g.Describe(sym)), true, nil
	}
	h, ok := g.Hover(hostOffset(here, pos))
	if !ok {
		return nil, true, nil
	}
	return s.nativeHover(here.snapshot, h), true, nil
}

func (s *Server) nativeFindings(file *workspace.File, settings config.Settings) []lint.Finding {
	if !settings.Lint.Enabled {
		return nil
	}
	g, err := s.group(context.Background(), s.docs.View(), file)
	if err != nil {
		s.logger.Warn("analysing the files around a host file", "uri", file.URI, "err", err)
		return nil
	}
	excluded := s.lintSettings(file.Path, settings).Excluded()
	return slices.DeleteFunc(g.Findings(), func(f lint.Finding) bool { return excluded.Has(f.Code) })
}

const recipeTimeout = 30 * time.Minute

type recipeRunArgs struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type recipeRunResult struct {
	ExitCode  int    `json:"exitCode"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

func (s *Server) runRecipeCommand(ctx context.Context, docs *docstore.View, args recipeRunArgs) (any, error) {
	if args.URI == "" || args.Name == "" {
		return nil, &argumentError{errors.New("the argument needs both a uri and a name")}
	}
	file, err := s.loadURI(docs, args.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %v", commandRunRecipe, err)
	}
	if file.Native == nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %s is not a justfile or Makefile", commandRunRecipe, args.URI)
	}
	if !file.Doc.Local {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %s is not a file on disk", commandRunRecipe, args.URI)
	}
	if !slices.ContainsFunc(file.Native.Index.Runnables, func(r model.Runnable) bool { return r.Name == args.Name }) {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %s has no %q that runs without arguments", commandRunRecipe, args.URI, args.Name)
	}
	argv := file.Native.Language.Invocation(file.Doc.Path, args.Name)
	label := argv[0] + " " + args.Name
	ctx, cancel := context.WithTimeout(ctx, recipeTimeout)
	defer cancel()
	progress, ctx, err := s.beginWorkDone(ctx, label)
	if err != nil {
		return nil, err
	}
	outcome := "stopped"
	defer func() { progress.end(outcome) }()
	result, err := yieldSlot(ctx, func() (hostrun.Result, error) { return hostrun.Run(ctx, argv, filepath.Dir(file.Doc.Path)) })
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%s timed out after %s", label, recipeTimeout)
	case errors.Is(err, context.Canceled):
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestCancelled, "%s was cancelled", label)
	case err != nil:
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%v", err)
	}
	outcome = fmt.Sprintf("exit status %d", result.Code)
	s.notify(logMessageMethod, lsp.LogMessageParams{Type: lsp.MessageLog, Message: label + "\n" + result.Output})
	kind := lsp.MessageInfo
	if result.Code != 0 {
		kind = lsp.MessageError
	}
	s.notify(showMessageMethod, lsp.ShowMessageParams{Type: kind, Message: label + ": " + outcome})
	return recipeRunResult{ExitCode: result.Code, Output: result.Output, Truncated: result.Truncated}, nil
}
