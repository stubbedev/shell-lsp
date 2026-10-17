package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
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
	env := hostnav.Env{Loader: s.workspace, Home: s.process.Home}
	env.Graph = func(ctx context.Context) (*hostnav.Graph, error) { return s.graphs.of(ctx, s, docs, language, env) }
	return env
}

func (s *Server) hostFiles(docs *docstore.View, language host.Language) navigation.WorkspaceFiles {
	all := s.candidateFiles(docs)
	return func(ctx context.Context) ([]*workspace.File, error) {
		files, err := all(ctx)
		return slices.DeleteFunc(files, func(f *workspace.File) bool { return f.Native == nil || f.Native.Language != language }), err
	}
}

type hostGraphs struct {
	mu      sync.Mutex
	entries map[host.Language]hostGraph
}

type hostGraph struct {
	stamp string
	graph *hostnav.Graph
}

func (s *Server) hostStamp(docs *docstore.View, language host.Language) string {
	var sb strings.Builder
	sb.WriteString(strconv.FormatUint(s.workspace.Generation(), 10))
	for _, doc := range docs.Documents() {
		if l, ok := host.Of(doc.URI, doc.Language); ok && l == language {
			sb.WriteString(" " + doc.URI + "@" + strconv.FormatInt(int64(doc.Version), 10))
		}
	}
	return sb.String()
}

func (g *hostGraphs) of(ctx context.Context, s *Server, docs *docstore.View, language host.Language, env hostnav.Env) (*hostnav.Graph, error) {
	stamp := s.hostStamp(docs, language)
	g.mu.Lock()
	defer g.mu.Unlock()
	if cached, ok := g.entries[language]; ok && cached.stamp == stamp {
		return cached.graph, nil
	}
	graph, err := hostnav.NewGraph(ctx, language, s.hostFiles(docs, language), env)
	if err != nil {
		return nil, fmt.Errorf("finding the files that include %s files: %w", language, err)
	}
	if g.entries == nil {
		g.entries = map[host.Language]hostGraph{}
	}
	g.entries[language] = hostGraph{stamp: stamp, graph: graph}
	return graph, nil
}

func (s *Server) group(ctx context.Context, docs *docstore.View, h hostnav.Host) (*hostnav.Group, error) {
	return hostnav.Build(ctx, h, s.hostEnv(docs, h.Language()))
}

func sourcePositions(file *workspace.File, enc docstore.Encoding) docstore.Positions {
	return file.Doc.Source().Positions(enc)
}

type nativeHere struct {
	located
	host   hostnav.Host
	source int
	inside bool
}

func nativeLocation(here located, pos lsp.Position, inside bool) (nativeHere, bool) {
	h, ok := hostnav.Of(here.File)
	if !ok {
		return nativeHere{}, false
	}
	return nativeHere{located: here, host: h, source: here.File.Doc.Source().PositionToOffset(pos, here.encoding), inside: inside}, true
}

type nativeHit struct {
	group  *hostnav.Group
	symbol hostnav.Symbol
	found  bool
}

func (s *Server) nativeAt(ctx context.Context, docs *docstore.View, here nativeHere) (nativeHit, error) {
	g, err := s.group(ctx, docs, here.host)
	if err != nil {
		return nativeHit{}, err
	}
	hit := nativeHit{group: g}
	if here.inside {
		hit.symbol, hit.found = g.ShellSymbol(here.offset)
		return hit, nil
	}
	hit.symbol, hit.found = g.At(here.source)
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

func (s *Server) nativeDefinition(ctx context.Context, docs *docstore.View, here nativeHere) ([]lsp.Location, bool, error) {
	if !here.inside {
		g, err := s.group(ctx, docs, here.host)
		if err != nil {
			return nil, true, err
		}
		return hostLocations(g.Definition(here.source), here.encoding), true, nil
	}
	hit, err := s.nativeAt(ctx, docs, here)
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

func (s *Server) nativeCompletion(ctx context.Context, docs *docstore.View, here nativeHere) (lsp.CompletionList, error) {
	g, err := s.group(ctx, docs, here.host)
	if err != nil {
		return lsp.CompletionList{}, err
	}
	at := sourcePositions(here.File, here.encoding)
	items := []lsp.CompletionItem{}
	for _, it := range g.Complete(here.source) {
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

func (s *Server) nativeHoverAt(ctx context.Context, docs *docstore.View, here nativeHere) (any, bool, error) {
	g, err := s.group(ctx, docs, here.host)
	if err != nil {
		return nil, true, err
	}
	if here.inside {
		sym, ok := g.ShellSymbol(here.offset)
		if !ok {
			return nil, false, nil
		}
		return s.nativeHover(here.snapshot, g.Describe(sym)), true, nil
	}
	h, ok := g.Hover(here.source)
	if !ok {
		return nil, true, nil
	}
	return s.nativeHover(here.snapshot, h), true, nil
}

func (s *Server) nativeFindings(h hostnav.Host, settings config.Settings) []lint.Finding {
	if !settings.Lint.Enabled {
		return nil
	}
	file := h.File()
	findings := file.Native.Index.Findings
	if g, err := s.group(context.Background(), s.docs.View(), h); err == nil {
		findings = g.Findings()
	} else {
		s.noticeOnce("hostnav:"+file.URI+":"+err.Error(), lsp.MessageWarning, "Checking "+file.Path+" on its own, without the files it includes: "+err.Error())
	}
	excluded := s.lintSettings(file.Path, settings).Excluded()
	return slices.DeleteFunc(slices.Clone(findings), func(f lint.Finding) bool { return excluded.Has(f.Code) })
}

const recipeTimeout = 30 * time.Minute

type recipeRunArgs struct {
	URI        string           `json:"uri"`
	Name       string           `json:"name"`
	Arguments  []string         `json:"arguments,omitempty"`
	Parameters []model.RunParam `json:"parameters,omitempty"`
}

type recipeListing struct {
	URI        string           `json:"uri"`
	Path       string           `json:"path"`
	Name       string           `json:"name"`
	Tool       string           `json:"tool"`
	Argv       []string         `json:"argv"`
	Cwd        string           `json:"cwd"`
	Parameters []model.RunParam `json:"parameters"`
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
	i := slices.IndexFunc(file.Native.Index.Runnables, func(r model.Runnable) bool { return r.Name == args.Name })
	if i < 0 {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %s has no %q to run", commandRunRecipe, args.URI, args.Name)
	}
	if run := file.Native.Index.Runnables[i]; !run.Accepts(len(args.Arguments)) {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %q takes %s, not %d", commandRunRecipe, args.Name, run.Arity(), len(args.Arguments))
	}
	argv := append(file.Native.Language.Invocation(file.Doc.Path, args.Name), args.Arguments...)
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

func (s *Server) nativeCallHierarchyItem(name string, kind model.Kind, extent hostnav.Extent, enc docstore.Encoding) (lsp.CallHierarchyItem, error) {
	data, err := encodeData(callItemData{Name: name, Start: extent.Selection.Start, Native: true})
	if err != nil {
		return lsp.CallHierarchyItem{}, err
	}
	at := sourcePositions(extent.File, enc)
	return lsp.CallHierarchyItem{Name: name, Kind: nativeKind(kind), Detail: extent.File.Path, URI: extent.File.URI, Range: at.Range(extent.Span), SelectionRange: at.Range(extent.Selection), Data: data}, nil
}

func (s *Server) nativePrepareCalls(ctx context.Context, docs *docstore.View, here nativeHere) (any, error) {
	g, err := s.group(ctx, docs, here.host)
	if err != nil {
		return nil, err
	}
	sym, ok := g.CallTarget(here.source)
	if !ok {
		return nil, nil
	}
	item, err := s.nativeCallHierarchyItem(sym.Name, sym.Kind, g.Extent(sym), here.encoding)
	if err != nil {
		return nil, err
	}
	return []lsp.CallHierarchyItem{item}, nil
}

func (s *Server) nativeCallItem(ctx context.Context, docs *docstore.View, item lsp.CallHierarchyItem) (*hostnav.Group, hostnav.Symbol, bool, error) {
	data, err := decodeData[callItemData]("call hierarchy item", item.Data)
	if err != nil || !data.Native {
		return nil, hostnav.Symbol{}, false, err
	}
	file, err := s.loadURI(docs, item.URI)
	if err != nil {
		return nil, hostnav.Symbol{}, true, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	h, ok := hostnav.Of(file)
	if !ok {
		return nil, hostnav.Symbol{}, true, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s is not a justfile or Makefile", item.URI)
	}
	g, err := s.group(ctx, docs, h)
	if err != nil {
		return nil, hostnav.Symbol{}, true, err
	}
	sym, ok := g.SymbolAt(file.Path, data.Start)
	if !ok {
		return nil, hostnav.Symbol{}, true, jsonrpc.NewError(jsonrpc.CodeContentModified, "%s no longer exists in %s", data.Name, item.URI)
	}
	return g, sym, true, nil
}

func (s *Server) nativeCalls(docs *docstore.View, sites []hostnav.CallSite, incoming bool) (any, error) {
	enc := docs.Encoding()
	if incoming {
		out := make([]lsp.CallHierarchyIncomingCall, 0, len(sites))
		for _, site := range sites {
			from, err := s.nativeCallHierarchyItem(site.Name, site.Kind, site.Peer, enc)
			if err != nil {
				return nil, err
			}
			out = append(out, lsp.CallHierarchyIncomingCall{From: from, FromRanges: sourcePositions(site.File, enc).Ranges(site.Spans)})
		}
		return out, nil
	}
	out := make([]lsp.CallHierarchyOutgoingCall, 0, len(sites))
	for _, site := range sites {
		to, err := s.nativeCallHierarchyItem(site.Name, site.Kind, site.Peer, enc)
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.CallHierarchyOutgoingCall{To: to, FromRanges: sourcePositions(site.File, enc).Ranges(site.Spans)})
	}
	return out, nil
}

func (s *Server) nativeFixes(ctx context.Context, docs *docstore.View, h hostnav.Host, p lsp.CodeActionParams) ([]lsp.CodeAction, error) {
	g, err := s.group(ctx, docs, h)
	if err != nil {
		return nil, err
	}
	file := h.File()
	enc := docs.Encoding()
	source := sourcePositions(file, enc)
	within, _ := source.Span(p.Range)
	caps := s.clientCapabilities().Workspace.WorkspaceEdit
	creates := caps.DocumentChanges && slices.Contains(caps.ResourceOperations, "create")
	_, open := docs.Document(file.URI)
	var out []lsp.CodeAction
	for _, fix := range g.Fixes(within) {
		action := lsp.CodeAction{Title: fix.Title, Kind: lsp.CodeActionKind(actions.QuickFix), IsPreferred: fix.Preferred}
		finding := source.Range(fix.Finding.Span)
		for _, d := range p.Context.Diagnostics {
			if d.Range == finding && d.Code == fix.Finding.Code.String() {
				action.Diagnostics = append(action.Diagnostics, d)
			}
		}
		edit := lsp.WorkspaceEdit{}
		switch {
		case fix.Create != "":
			if !creates {
				continue
			}
			edit.CreateFiles = []lsp.CreateFile{{URI: fileuri.FromPath(fix.Create)}}
		default:
			if edit, err = s.multiFileEdit(enc, []fileEdits{{doc: file.Doc.Source(), versioned: open, edits: fix.Edits}}); err != nil {
				return nil, err
			}
		}
		action.Edit = &edit
		out = append(out, action)
	}
	return out, nil
}

func (s *Server) listRecipesCommand(ctx context.Context, docs *docstore.View) (any, error) {
	out := []recipeListing{}
	for _, language := range []host.Language{host.Just, host.Make} {
		files, err := s.hostFiles(docs, language)(ctx)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !f.Doc.Local {
				continue
			}
			for _, r := range f.Native.Index.Runnables {
				out = append(out, recipeListing{
					URI: f.URI, Path: f.Path, Name: r.Name, Tool: language.Recursion().Command,
					Argv: language.Invocation(f.Path, r.Name), Cwd: filepath.Dir(f.Path), Parameters: r.Params,
				})
			}
		}
	}
	return out, nil
}
