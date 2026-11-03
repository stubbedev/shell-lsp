package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"weak"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/config"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/hostnav"
	"github.com/stubbedev/shell-lsp/internal/hostrun"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/nixeval"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/regularfile"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	projectLinkPatience = 100 * time.Millisecond
	nixpkgsInput        = "nixpkgs"
	maxLibraryFile      = 8 << 20
	memoryReleaseQuiet  = 2 * time.Second
	memoryReleaseRest   = 30 * time.Second
	memoryReleaseBurst  = 64 << 20
)

type memoryRelease struct {
	mu        sync.Mutex
	timer     *time.Timer
	resting   bool
	stopped   bool
	released  uint64
	quiet     time.Duration
	rest      time.Duration
	burst     uint64
	allocated func() uint64
	rested    func()
	free      func()
}

func newMemoryRelease(projects *hostnav.Projects) *memoryRelease {
	return &memoryRelease{quiet: memoryReleaseQuiet, rest: memoryReleaseRest, burst: memoryReleaseBurst, allocated: heapAllocated, rested: projects.ReleaseIdle, free: debug.FreeOSMemory}
}

func heapAllocated() uint64 {
	sample := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

func (m *memoryRelease) schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.resting = false
	if m.timer != nil {
		m.timer.Reset(m.quiet)
		return
	}
	m.timer = time.AfterFunc(m.quiet, m.fire)
}

func (m *memoryRelease) fire() {
	allocated := m.allocated()
	m.mu.Lock()
	resting := m.resting
	if m.stopped {
		m.mu.Unlock()
		return
	}
	if !resting && allocated-m.released < m.burst {
		m.resting = true
		m.timer.Reset(m.rest)
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	if resting {
		m.rested()
	}
	m.free()
	released := m.allocated()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released = released
}

func (m *memoryRelease) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	if m.timer != nil {
		m.timer.Stop()
	}
}

func (s *Server) hostEnv(docs *docstore.View, language host.Language) hostnav.Env {
	env := hostnav.Env{Loader: s.workspace, Home: s.process.Home, Exists: func(path string) bool {
		_, err := regularfile.Stat(path)
		return err == nil
	}, Projects: s.projects, Disk: s.disk, Stamp: hostnav.Stamp{Docs: docs.Revision(), Disk: s.workspace.Generation()}, Patience: cmp.Or(s.linkPatience, projectLinkPatience), OnPartial: s.partialAnswer}
	env.Graph = func(ctx context.Context) (*hostnav.Graph, error) { return s.graphs.of(ctx, s, docs, language, env) }
	return env
}

func (s *Server) hostFiles(docs *docstore.View, language host.Language) navigation.WorkspaceFiles {
	all := s.candidates(docs, func(path string) bool {
		named, known := host.FromName(path)
		return !known || named != language
	})
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

func (s *Server) nixGraphStamp(docs *docstore.View) string {
	var sb strings.Builder
	sb.WriteString(strconv.FormatUint(s.workspace.Generation(), 10))
	for _, doc := range docs.Documents() {
		l, known := host.Of(doc.URI, doc.Language)
		if !known || l != host.Nix {
			continue
		}
		snap, err := s.snapshot(docs, doc.URI)
		if err != nil || snap.Native == nil {
			continue
		}
		sb.WriteString(" " + doc.URI)
		for _, inc := range snap.Native.Index.Includes {
			if !inc.Probe && !inc.Dynamic {
				sb.WriteString(" " + inc.Path)
			}
		}
	}
	return sb.String()
}

func (s *Server) graphStamp(docs *docstore.View, language host.Language) string {
	if language == host.Nix {
		return s.nixGraphStamp(docs)
	}
	return s.hostStamp(docs, language)
}

func (g *hostGraphs) of(ctx context.Context, s *Server, docs *docstore.View, language host.Language, env hostnav.Env) (*hostnav.Graph, error) {
	stamp := s.graphStamp(docs, language)
	g.mu.Lock()
	defer g.mu.Unlock()
	if cached, ok := g.entries[language]; ok && cached.stamp == stamp {
		return cached.graph, nil
	}
	graph, err := s.importGraph(ctx, docs, language, env)
	if err != nil {
		return nil, fmt.Errorf("finding the files that include %s files: %w", language, err)
	}
	if g.entries == nil {
		g.entries = map[host.Language]hostGraph{}
	}
	g.entries[language] = hostGraph{stamp: stamp, graph: graph}
	return graph, nil
}

func (s *Server) importGraph(ctx context.Context, docs *docstore.View, language host.Language, env hostnav.Env) (*hostnav.Graph, error) {
	if language != host.Nix {
		return hostnav.NewGraph(ctx, language, s.hostFiles(docs, language), env)
	}
	return hostnav.NewNixGraph(ctx, func(ctx context.Context) ([]string, error) {
		notNix := func(path string) bool { return !nixPath(path) }
		paths, err := s.candidatePaths(ctx, docs, notNix)
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(paths, notNix), nil
	}, env)
}

func (s *Server) partialAnswer() {
	if s.partialShown.Swap(true) {
		return
	}
	s.notify(logMessageMethod, lsp.LogMessageParams{Type: lsp.MessageInfo, Message: "Answering from single files until the project finishes linking; hints and lenses refresh when it does."})
	if !s.projects.Linking() {
		s.linkLanded()
	}
}

func (s *Server) linkLanded() {
	s.workspace.ReleaseStash()
	if !s.partialShown.Swap(false) {
		return
	}
	caps := s.clientCapabilities().Workspace
	ctx := context.Background()
	if caps.InlayHint.RefreshSupport {
		s.requestRefresh(ctx, inlayHintRefreshMethod)
	}
	if caps.CodeLens.RefreshSupport {
		s.requestRefresh(ctx, codeLensRefreshMethod)
	}
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
		locations := hostLocations(g.Definition(here.source), here.encoding)
		if here.host.Language() == host.Nix {
			locations = append(locations, s.nixElsewhere(ctx, docs, here)...)
		}
		return locations, true, nil
	}
	hit, err := s.nativeAt(ctx, docs, here)
	if err != nil || !hit.found {
		return nil, err != nil, err
	}
	return hostLocations(hit.group.ShellDefinition(hit.symbol), here.encoding), true, nil
}

func (s *Server) nixElsewhere(ctx context.Context, docs *docstore.View, here nativeHere) []lsp.Location {
	doc := here.Native.Nix()
	known := s.nixEvaluated(ctx, docs, here.snapshot)
	defer known.done()
	names, _, inNixpkgs := doc.NixpkgsPathAt(here.source)
	switch {
	case inNixpkgs && names[0] == nix.LibName:
		entry, ok := s.nixLibraryEntry(here.File, names[1:])
		if ok && entry.Builtin == "" {
			return declarationLocations(entry.Declaration)
		}
		if ok {
			return nil
		}
		return nixpkgsDeclared(known, names)
	case inNixpkgs:
		if pkg, ok := known.NixpkgsPackage(names); ok && pkg.Declaration.File != "" {
			return declarationLocations(known.realised([]nixeval.Declaration{pkg.Declaration})...)
		}
		return nixpkgsDeclared(known, names)
	}
	return declarationLocations(known.realised(doc.OptionDeclarations(here.source, known))...)
}

func nixpkgsDeclared(known *nixRequest, names []string) []lsp.Location {
	declaration, ok := known.NixpkgsDeclaration(names)
	if !ok {
		return nil
	}
	return declarationLocations(known.realised([]nixeval.Declaration{declaration})...)
}

func declarationLocations(declarations ...nixeval.Declaration) []lsp.Location {
	var out []lsp.Location
	for _, d := range declarations {
		if !filepath.IsAbs(d.File) {
			continue
		}
		at := lsp.Position{Line: uint32(max(d.Line-1, 0)), Character: uint32(max(d.Column-1, 0))} //nolint:gosec // lines and columns of a source file fit
		out = append(out, lsp.Location{URI: fileuri.FromPath(d.File), Range: lsp.Range{Start: at, End: at}})
	}
	return out
}

func (s *Server) nixLibrary(file *workspace.File) (*nix.Library, bool) {
	if !file.Doc.Local {
		return nil, false
	}
	dir, ok := s.nixFlakeDir(file.Path)
	if !ok {
		return nil, false
	}
	source, ok := s.flakeLocks.SourceOf(dir, nixpkgsInput)
	if !ok {
		return nil, false
	}
	s.library.mu.Lock()
	defer s.library.mu.Unlock()
	if s.library.source == source {
		if library := s.library.held.Value(); library != nil {
			return library, true
		}
	}
	library := nix.NewLibrary(source, func(path string) (string, bool) {
		raw, _, err := regularfile.Read(path, maxLibraryFile)
		return string(raw), err == nil
	})
	s.library.source, s.library.held = source, weak.Make(library)
	return library, true
}

type heldLibrary struct {
	mu     sync.Mutex
	source string
	held   weak.Pointer[nix.Library]
}

func (s *Server) nixLibraryEntry(file *workspace.File, names []string) (nix.LibraryEntry, bool) {
	library, ok := s.nixLibrary(file)
	if !ok || len(names) == 0 {
		return nix.LibraryEntry{}, false
	}
	return library.Lookup(names)
}

func (s *Server) nixLibraryHover(here nativeHere) (lsp.Hover, bool) {
	names, span, ok := here.File.Native.Nix().NixpkgsPathAt(here.source)
	if !ok || names[0] != nix.LibName {
		return lsp.Hover{}, false
	}
	entry, ok := s.nixLibraryEntry(here.File, names[1:])
	if !ok {
		return lsp.Hover{}, false
	}
	markdown, ok := nix.LibraryMarkdown(names[1:], entry)
	if !ok {
		return lsp.Hover{}, false
	}
	return s.nativeHover(here.snapshot, hostnav.Hover{Markdown: markdown, Span: span}), true
}

func nativeKind(kind model.Kind) lsp.SymbolKind {
	switch kind {
	case model.KindRecipe, model.KindAlias, model.KindFunction, model.KindTarget:
		return lsp.SymbolFunction
	case model.KindVariable:
		return lsp.SymbolVariable
	case model.KindParameter:
		return lsp.SymbolVariable
	case model.KindModule, model.KindStage:
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
	case model.KindModule, model.KindStage:
		return lsp.CompletionModule
	case model.KindSetting, model.KindAttribute:
		return lsp.CompletionProperty
	case model.KindKeyword:
		return lsp.CompletionKeyword
	}
	panic(fmt.Sprintf("server: host kind %s has no LSP completion kind", kind))
}

func nativeItem(label string, kind model.Kind, detail string, span parser.Span, at docstore.Positions) lsp.CompletionItem {
	return lsp.CompletionItem{
		Label:            label,
		Kind:             nativeCompletionKind(kind),
		Detail:           detail,
		InsertTextFormat: lsp.InsertPlainText,
		TextEdit:         &lsp.CompletionEdit{NewText: label, Insert: at.Range(span)},
	}
}

func (s *Server) nativeCompletion(ctx context.Context, docs *docstore.View, here nativeHere) (lsp.CompletionList, error) {
	if here.host.Language() == host.Nix {
		return s.nixCompletion(ctx, docs, here)
	}
	g, err := s.group(ctx, docs, here.host)
	if err != nil {
		return lsp.CompletionList{}, err
	}
	at := sourcePositions(here.File, here.encoding)
	items := []lsp.CompletionItem{}
	for _, it := range g.Complete(here.source) {
		item := nativeItem(it.Label, it.Kind, it.Detail, it.Span, at)
		if it.Doc != "" {
			item.Documentation = lsp.Markdown(it.Doc)
		}
		items = append(items, item)
	}
	return lsp.CompletionList{Items: items}, nil
}

func (s *Server) nixCompletion(ctx context.Context, docs *docstore.View, here nativeHere) (lsp.CompletionList, error) {
	at := sourcePositions(here.File, here.encoding)
	if items, hosted := hostnav.EmbeddedComplete(here.File, here.source); hosted {
		out := make([]lsp.CompletionItem, 0, len(items))
		for _, it := range items {
			item := nativeItem(it.Label, it.Kind, it.Detail, it.Span, at)
			if it.Doc != "" {
				item.Documentation = lsp.Markdown(it.Doc)
			}
			out = append(out, item)
		}
		return lsp.CompletionList{Items: out}, nil
	}
	known := s.nixEvaluated(ctx, docs, here.snapshot)
	defer known.done()
	text := here.File.Doc.Source().Text()
	cursor, located := nix.Locate(text, here.source, s.nixFiles(docs, here.File))
	if !located {
		return lsp.CompletionList{IsIncomplete: known.incomplete, Items: []lsp.CompletionItem{}}, nil
	}
	found := cursor.Complete(known)
	linking := false
	if req, wanted := cursor.MembersWanted(); wanted && here.Doc.Local {
		g, err := s.group(ctx, docs, here.host)
		if err != nil {
			return lsp.CompletionList{}, err
		}
		linking = g.Linking()
		found = withProjectMembers(found, g.NixMembers(req))
	}
	if parent, word, wanted := cursor.LibraryMembersWanted(); wanted {
		if library, ok := s.nixLibrary(here.File); ok {
			found = withProjectMembers(found, nix.LibraryItems(library.Members(parent), parent, word))
		}
	}
	items := make([]lsp.CompletionItem, 0, len(found))
	for _, it := range found {
		item := nativeItem(it.Label, it.Kind, it.Detail, it.Span, at)
		if it.HasDoc() {
			data, err := encodeData(completionData{Name: it.Label, URI: here.URI, Offset: here.source, Nix: true})
			if err != nil {
				return lsp.CompletionList{}, err
			}
			item.Data = data
		}
		items = append(items, item)
	}
	return lsp.CompletionList{IsIncomplete: known.incomplete || linking, Items: items}, nil
}

func withProjectMembers(found, members []nix.Item) []nix.Item {
	for _, member := range members {
		if !slices.ContainsFunc(found, func(it nix.Item) bool { return it.Label == member.Label }) {
			found = append(found, member)
		}
	}
	return found
}

func (s *Server) resolveNixCompletion(ctx context.Context, docs *docstore.View, item lsp.CompletionItem, data completionData) lsp.CompletionItem {
	snap, err := s.snapshot(docs, data.URI)
	if err != nil {
		return item
	}
	known := s.nixEvaluated(ctx, docs, snap)
	defer known.done()
	if doc, ok := nix.ItemDoc(snap.Doc.Source().Text(), data.Offset, data.Name, s.nixFiles(docs, snap.File), known); ok {
		item.Documentation = lsp.Markdown(doc)
	}
	return item
}

func (s *Server) nixFiles(docs *docstore.View, file *workspace.File) nix.Files {
	return hostnav.NixFiles(s.hostEnv(docs, host.Nix).Loader, file)
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
				Detail:         sym.Description(),
				Kind:           nativeKind(sym.Kind),
				Range:          at.Range(sym.Span),
				SelectionRange: at.Range(sym.Selection),
				Children:       build(int(i)),
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
		files[i].edits = append(files[i].edits, o.Edits(newName)...)
	}
	return s.multiFileEdit(enc, files)
}

func (s *Server) nativeHover(snap *snapshot, h hostnav.Hover) lsp.Hover {
	r := sourcePositions(snap.File, snap.encoding).Range(h.Span)
	return lsp.Hover{Contents: lsp.MarkupContent{Kind: s.hoverFormat(), Value: h.Markdown}, Range: &r}
}

func (s *Server) nativeHoverAt(ctx context.Context, docs *docstore.View, here nativeHere) (any, bool, error) {
	if here.host.Language() == host.Nix && !here.inside {
		known := s.nixEvaluated(ctx, docs, here.snapshot)
		defer known.done()
		if h, ok := here.File.Native.Nix().Hover(here.source, known); ok {
			return s.nativeHover(here.snapshot, hostnav.Hover{Markdown: h.Markdown, Span: h.Span}), true, nil
		}
		if h, ok := s.nixLibraryHover(here); ok {
			return h, true, nil
		}
	}
	g, err := s.group(ctx, docs, here.host)
	if err != nil {
		return nil, true, err
	}
	if here.inside {
		sym, ok := g.ShellSymbol(here.offset)
		if !ok {
			return nil, false, nil
		}
		described := g.Describe(sym)
		described.Markdown += linkingNote(g)
		described.Markdown = s.findingsAbove(here.File, here.File, here.offset, here.settings, here.generation, described.Markdown)
		return s.nativeHover(here.snapshot, described), true, nil
	}
	h, ok := g.Hover(here.source)
	if proven, held := s.nixProvenOffset(here); held {
		if !ok {
			return s.provenShellHover(ctx, here, proven)
		}
		h.Markdown = s.findingsAbove(proven.whole, proven.variant, proven.offset, here.settings, here.generation, h.Markdown)
	}
	if !ok {
		return nil, true, nil
	}
	if here.host.Language() == host.Nix {
		h.Markdown += nixComments(g, here)
	}
	h.Markdown += linkingNote(g)
	return s.nativeHover(here.snapshot, h), true, nil
}

func (s *Server) findingsAbove(whole, variant *workspace.File, offset int, settings config.Settings, generation uint64, markdown string) string {
	return hover.WithFindings(hover.Input{Tree: variant.Tree, Offset: offset, Lint: s.lintOf(whole, settings, generation)}, hover.Result{Markdown: markdown}).Markdown
}

type provenOffset struct {
	whole, variant *workspace.File
	offset         int
}

func (s *Server) nixProvenOffset(here nativeHere) (provenOffset, bool) {
	if here.host.Language() != host.Nix {
		return provenOffset{}, false
	}
	proven := s.nixProvenFile(here.File)
	if proven.wrapped == nil || !here.File.Native.Nix().InProvenString(here.source, proven.proof.On(here.Doc.Source())) {
		return provenOffset{}, false
	}
	wrapped := proven.file()
	if wrapped == nil {
		return provenOffset{}, false
	}
	offset, inside := wrapped.Doc.Positions(here.encoding).Offset(here.File.Doc.Source().OffsetToPosition(here.source, here.encoding))
	if !inside {
		return provenOffset{}, false
	}
	return provenOffset{whole: wrapped, variant: wrapped.At(offset), offset: offset}, true
}

func (s *Server) provenShellHover(ctx context.Context, here nativeHere, proven provenOffset) (any, bool, error) {
	result, ok, err := s.shellHover(ctx, proven.whole, proven.variant, proven.offset, here.settings, here.generation)
	if err != nil || !ok {
		return nil, true, err
	}
	r := proven.whole.Doc.Positions(here.encoding).Range(result.Span)
	return lsp.Hover{Contents: lsp.MarkupContent{Kind: s.hoverFormat(), Value: result.Markdown}, Range: &r}, true, nil
}

func linkingNote(g *hostnav.Group) string {
	if !g.Linking() {
		return ""
	}
	return "\n\n*The rest of the project is still being linked, so this answer covers this file alone.*"
}

func nixComments(g *hostnav.Group, here nativeHere) string {
	sym, ok := g.At(here.source)
	if !ok {
		return ""
	}
	doc := here.Native.Nix()
	var out strings.Builder
	seen := map[string]bool{}
	for _, d := range g.Declarations(sym) {
		comment := doc.CommentAbove(d.Span.Start)
		if comment != "" && !seen[comment] {
			seen[comment] = true
			out.WriteString("\n\n")
			out.WriteString(comment)
		}
	}
	return out.String()
}

func (s *Server) nativeFindings(h hostnav.Host, settings config.Settings, docker hostnav.Hadolint) []lint.Finding {
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
	checks := s.lintSettings(file.Path, settings)
	unused := hostnav.OptionalNixFindings(h, checks)
	locked, err := hostnav.FlakeLockFindings(h, s.flakeLocks, checks)
	if err != nil {
		s.noticeOnce("flake-lock:"+err.Error(), lsp.MessageWarning, "Ignoring the flake.lock next to "+file.Path+": "+err.Error())
	}
	excluded := checks.Excluded()
	return slices.DeleteFunc(slices.Concat(findings, unused, locked, docker.Findings()), func(f lint.Finding) bool {
		return excluded.Has(f.Code) || !settings.Lint.Profile.KeepsHost(f)
	})
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
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %s has nothing to run", commandRunRecipe, args.URI)
	}
	if !file.Doc.Local {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %s is not a file on disk", commandRunRecipe, args.URI)
	}
	if !lockedInputRun(file, args) {
		i := slices.IndexFunc(file.Native.Index.Runnables, func(r model.Runnable) bool { return r.Name == args.Name })
		if i < 0 {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %s has no %q to run", commandRunRecipe, args.URI, args.Name)
		}
		if run := file.Native.Index.Runnables[i]; !run.Accepts(len(args.Arguments)) {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s: %q takes %s, not %d", commandRunRecipe, args.Name, run.Arity(), len(args.Arguments))
		}
	}
	argv := append(file.Native.Language.Invocation(file.Doc.Path, args.Name), args.Arguments...)
	label := file.Native.Language.RunLabel(args.Name, argv)
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
		return nil, hostnav.Symbol{}, true, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s is not a file this server navigates natively", item.URI)
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
	for _, fix := range slices.Concat(g.Fixes(within), nixIgnoreFixes(g, h, within), s.nixProvenFixes(file, within)) {
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
	if wants(p.Context.Only, actions.SourceFlakeInput) {
		update, offered, err := s.nixUpdateInput(h, within.Start)
		if err != nil {
			return nil, err
		}
		if offered {
			out = append(out, update)
		}
	}
	return s.nixWorkspaceIgnores(out, g, h, within, enc)
}

func (s *Server) nixWorkspaceIgnores(out []lsp.CodeAction, g *hostnav.Group, h hostnav.Host, within parser.Span, enc docstore.Encoding) ([]lsp.CodeAction, error) {
	if h.Language() != host.Nix || !h.File().Doc.Local {
		return out, nil
	}
	settings := func() (actions.SettingsFile, bool) { return s.settingsFileFor(h.File().Path) }
	seen := map[lint.Code]bool{}
	for _, f := range g.Findings() {
		if f.Span.End < within.Start || f.Span.Start > within.End || !f.Code.Nix() || f.Code == lint.CodeNixSyntax || seen[f.Code] {
			continue
		}
		seen[f.Code] = true
		change, ok := actions.DisableForWorkspace(settings, f.Code)
		if !ok {
			continue
		}
		file, ok := settings()
		if !ok {
			continue
		}
		title := actions.WorkspaceDisableTitle(f.Code)
		edit, err := s.settingsEdit(enc, title, file, change)
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.CodeAction{Title: title, Kind: lsp.CodeActionKind(actions.QuickFix), Edit: &edit})
	}
	return out, nil
}

func lockedInputRun(file *workspace.File, args recipeRunArgs) bool {
	if file.Native.Language != host.Nix || len(args.Arguments) > 0 || filepath.Base(file.Path) != nix.FlakeFile {
		return false
	}
	doc := file.Native.Nix()
	input, named := strings.CutPrefix(args.Name, nix.InputRun(""))
	return named && doc.DeclaresInput(input)
}

func (s *Server) nixUpdateInput(h hostnav.Host, at int) (lsp.CodeAction, bool, error) {
	file := h.File()
	if h.Language() != host.Nix || !file.Doc.Local || filepath.Base(file.Path) != nix.FlakeFile {
		return lsp.CodeAction{}, false, nil
	}
	input, ok := file.Native.Nix().FlakeInputAt(at)
	if !ok {
		return lsp.CodeAction{}, false, nil
	}
	raw, err := json.Marshal(recipeRunArgs{URI: file.URI, Name: nix.InputRun(input)})
	if err != nil {
		return lsp.CodeAction{}, false, fmt.Errorf("encoding %s arguments for %s: %w", commandRunRecipe, file.URI, err)
	}
	title := "Update input " + input
	return lsp.CodeAction{Title: title, Kind: lsp.CodeActionKind(actions.SourceFlakeInput), Command: &lsp.Command{Title: title, Command: commandRunRecipe, Arguments: []json.RawMessage{raw}}}, true, nil
}

func (s *Server) nixProvenFindings(file *workspace.File) ([]lint.Finding, *workspace.File) {
	wrapped := s.nixProvenFile(file).file()
	if wrapped == nil {
		return nil, nil
	}
	settings, generation := s.settingsFor(file.Doc.Location)
	findings, _ := s.findings(wrapped, settings, generation)
	return findings, wrapped
}

func (s *Server) nixProvenFixes(file *workspace.File, within parser.Span) []hostnav.Fix {
	findings, wrapped := s.nixProvenFindings(file)
	var out []hostnav.Fix
	for _, f := range findings {
		start, end := wrapped.Doc.SourceOffset(f.Span.Start), wrapped.Doc.SourceOffset(f.Span.End)
		if end < within.Start || start > within.End {
			continue
		}
		for _, fix := range f.Fixes {
			if edits, ok := wrapped.Doc.SourceEdits(fix.Edits); ok {
				placed := f
				placed.Span = parser.Span{Start: start, End: end}
				out = append(out, hostnav.Fix{Title: fix.Title, Finding: placed, Edits: edits})
			}
		}
	}
	return out
}

func nixIgnoreFixes(g *hostnav.Group, h hostnav.Host, within parser.Span) []hostnav.Fix {
	if h.Language() != host.Nix {
		return nil
	}
	findings := slices.DeleteFunc(g.Findings(), func(f lint.Finding) bool { return f.Span.End < within.Start || f.Span.Start > within.End })
	var out []hostnav.Fix
	for i, fixes := range h.File().Native.Nix().IgnoreFixes(findings) {
		for _, fix := range fixes {
			out = append(out, hostnav.Fix{Title: fix.Title, Finding: findings[i], Edits: fix.Edits})
		}
	}
	return out
}

func (s *Server) listRecipesCommand(ctx context.Context, docs *docstore.View) (any, error) {
	out := []recipeListing{}
	listed := func(f *workspace.File) bool {
		if f.Native == nil || !f.Doc.Local {
			return true
		}
		language := f.Native.Language
		for _, r := range f.Native.Index.Runnables {
			argv := language.TerminalInvocation(f.Path, r.Name)
			out = append(out, recipeListing{
				URI: f.URI, Path: f.Path, Name: strings.Clone(r.Name), Tool: argv[0],
				Argv: argv, Cwd: filepath.Dir(f.Path), Parameters: append([]model.RunParam{}, r.Params...),
			})
		}
		return len(out) < maxListedRecipes
	}
	for _, doc := range docs.Documents() {
		if _, known := host.Of(doc.URI, doc.Language); !known {
			continue
		}
		file, err := s.workspaceFile(docs, doc.URI)
		if err != nil {
			return nil, err
		}
		listed(file)
	}
	visited, total, err := s.eachDiskFile(ctx, closedFiles(docs, func(path string) bool {
		_, known := host.FromName(path)
		return known
	}), listed)
	if err != nil {
		return nil, err
	}
	s.noticeScanCut("Listing recipes")
	s.noticeIndexLimit("Listing recipes", maxListedRecipes, visited, total)
	slices.SortStableFunc(out, func(a, b recipeListing) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), strings.Compare(a.Name, b.Name))
	})
	return out, nil
}
