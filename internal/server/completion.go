package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/completion"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/manpage"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func completionKind(kind completion.Kind) lsp.CompletionItemKind {
	switch kind {
	case completion.KindKeyword:
		return lsp.CompletionKeyword
	case completion.KindBuiltin:
		return lsp.CompletionMethod
	case completion.KindFunction:
		return lsp.CompletionFunction
	case completion.KindAlias:
		return lsp.CompletionFunction
	case completion.KindCommand:
		return lsp.CompletionModule
	case completion.KindVariable:
		return lsp.CompletionVariable
	case completion.KindFile:
		return lsp.CompletionFile
	case completion.KindFolder:
		return lsp.CompletionFolder
	case completion.KindFlag:
		return lsp.CompletionProperty
	case completion.KindOption:
		return lsp.CompletionValue
	case completion.KindSubcommand:
		return lsp.CompletionMethod
	case completion.KindOperator:
		return lsp.CompletionOperator
	case completion.KindSignal:
		return lsp.CompletionConstant
	case completion.KindValue:
		return lsp.CompletionEnumMember
	case completion.KindKey:
		return lsp.CompletionField
	case completion.KindSnippet:
		return lsp.CompletionSnippet
	}
	panic(fmt.Sprintf("server: completion kind %d has no LSP kind", kind))
}

type completionData struct {
	Kind   completion.ResolveKind `json:"kind"`
	Name   string                 `json:"name"`
	URI    string                 `json:"uri,omitempty"`
	Offset int                    `json:"offset,omitempty"`
}

type shellEnvironment struct {
	process     Process
	pages       *manpage.Lookup
	executables *executableCache
	prefetch    *prefetcher
}

func (e shellEnvironment) Executables() []string {
	return e.executables.list(time.Now())
}

func (e shellEnvironment) ReadDir(dir string) ([]completion.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]completion.DirEntry, 0, len(entries))
	for _, entry := range entries {
		isDir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			if info, err := followedInfo(dir, entry); err == nil {
				isDir = info.IsDir()
			}
		}
		out = append(out, completion.DirEntry{Name: entry.Name(), Dir: isDir})
	}
	return out, nil
}

func (e shellEnvironment) Home() string { return e.process.Home }

func (e shellEnvironment) WorkDir() (string, error) { return e.process.workDir() }

func (e shellEnvironment) EnvironmentNames() []string { return e.process.environmentNames() }

func (e shellEnvironment) Options(ctx context.Context, command string, args []string) ([]manpage.Option, error) {
	return e.pages.Options(ctx, command, args)
}

func (e shellEnvironment) Subcommands(ctx context.Context, command string, args []string) (manpage.Listing, error) {
	listing, err := e.pages.Subcommands(ctx, command, args)
	for _, name := range listing.Unloaded {
		e.prefetch.enqueue(name)
	}
	return listing, err
}

type executableCache struct {
	dirs     []string
	pathExt  []string
	mu       sync.Mutex
	checked  time.Time
	modTimes []time.Time
	names    []string
}

func newExecutableCache(dirs, pathExt []string) *executableCache {
	return &executableCache{dirs: dirs, pathExt: pathExt}
}

func (c *executableCache) list(now time.Time) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.names != nil && now.Sub(c.checked) < workspace.DiskRecheck {
		return c.names
	}
	modTimes := make([]time.Time, len(c.dirs))
	for i, dir := range c.dirs {
		if info, err := os.Stat(dir); err == nil {
			modTimes[i] = info.ModTime()
		}
	}
	c.checked = now
	if c.names != nil && slices.EqualFunc(c.modTimes, modTimes, time.Time.Equal) {
		return c.names
	}
	c.modTimes, c.names = modTimes, scanExecutables(c.dirs, c.pathExt)
	return c.names
}

func scanExecutables(dirs, pathExt []string) []string {
	names := []string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			info, err := followedInfo(dir, entry)
			if err != nil {
				continue
			}
			if name, ok := platform.Native.Executable(entry.Name(), info.Mode(), pathExt); ok {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func followedInfo(dir string, entry fs.DirEntry) (fs.FileInfo, error) {
	if entry.Type()&fs.ModeSymlink != 0 {
		return os.Stat(filepath.Join(dir, entry.Name()))
	}
	return entry.Info()
}

func (s *Server) handleCompletion(ctx context.Context, docs *docstore.View, completionParams lsp.CompletionParams) (any, error) {
	here, inside, err := s.at(docs, completionParams.TextDocumentPositionParams)
	if err != nil {
		return nil, err
	}
	if native, ok := nativeLocation(here, completionParams.Position, inside); ok && !inside {
		return s.nativeCompletion(ctx, docs, native)
	}
	if !inside {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}, nil
	}
	caps := s.clientCapabilities().TextDocument.Completion.CompletionItem
	scriptDir := ""
	if here.Doc.Local {
		scriptDir = filepath.Dir(here.Doc.Path)
	}
	result, err := completion.Complete(ctx, completion.Input{
		Tree:      here.Tree,
		Imports:   s.imports(here.File),
		Offset:    here.offset,
		ScriptDir: scriptDir,
		Snippets:  caps.SnippetSupport && !here.Doc.Projected(),
		Trigger:   completionTrigger(completionParams.Context),
		Env:       s.environment,
	})
	if err != nil {
		return nil, err
	}
	items := make([]lsp.CompletionItem, 0, len(result.Items))
	for _, it := range result.Items {
		item, ok, err := completionItem(here.snapshot, caps, it)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, item)
		}
	}
	return lsp.CompletionList{IsIncomplete: result.Incomplete, Items: items}, nil
}

func completionTrigger(ctx *lsp.CompletionContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.TriggerCharacter
}

func completionItem(snap *snapshot, caps CompletionItemCapabilities, it completion.Item) (lsp.CompletionItem, bool, error) {
	format := lsp.InsertPlainText
	if it.Snippet {
		format = lsp.InsertSnippet
	}
	source := snap.Doc.Source()
	insert, ok := snap.Doc.SourceReplacement(textedit.Edit{Start: it.Replace.Start, End: it.Replace.End, NewText: it.InsertText})
	if !ok {
		return lsp.CompletionItem{}, false, nil
	}
	at := source.Positions(snap.encoding)
	edit := &lsp.CompletionEdit{NewText: insert.NewText, Insert: at.Range(parser.Span{Start: insert.Start, End: insert.End})}
	if caps.InsertReplaceSupport && it.Word.End > it.Replace.End {
		if word, ok := snap.Doc.SourceReplacement(textedit.Edit{Start: it.Word.Start, End: it.Word.End, NewText: it.InsertText}); ok {
			replace := at.Range(parser.Span{Start: word.Start, End: word.End})
			edit.Replace = &replace
		}
	}
	item := lsp.CompletionItem{
		Label:            it.Label,
		Kind:             completionKind(it.Kind),
		Detail:           it.Detail,
		SortText:         it.SortText,
		FilterText:       insert.NewText,
		InsertTextFormat: format,
		TextEdit:         edit,
	}
	if caps.LabelDetailsSupport && (it.LabelDetail != "" || it.Detail != "") {
		item.LabelDetails = &lsp.CompletionItemLabelDetails{Detail: it.LabelDetail, Description: it.Detail}
	}
	switch {
	case it.FilterText != "":
		filter, ok := snap.Doc.SourceReplacement(textedit.Edit{Start: it.Replace.Start, End: it.Replace.End, NewText: it.FilterText})
		if !ok {
			return lsp.CompletionItem{}, false, nil
		}
		item.FilterText = filter.NewText
	case it.Snippet:
		item.FilterText = it.Label
	}
	if it.Documentation != "" {
		item.Documentation = lsp.Markdown(it.Documentation)
	}
	if it.Resolve != completion.ResolveNone {
		data, err := encodeData(completionData{Kind: it.Resolve, Name: it.Label, URI: snap.Doc.URI, Offset: it.Replace.Start})
		if err != nil {
			return lsp.CompletionItem{}, false, err
		}
		item.Data = data
	}
	return item, true, nil
}

func (s *Server) handleCompletionResolve(ctx context.Context, docs *docstore.View, item lsp.CompletionItem) (any, error) {
	if len(item.Data) == 0 {
		return item, nil
	}
	data, err := decodeData[completionData]("completion item", item.Data)
	if err != nil {
		return nil, err
	}
	if data.Name == "" || data.Kind == completion.ResolveNone {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "completion item data %s is not from this server: it names nothing to resolve", item.Data)
	}
	if data.Kind == completion.ResolveCommand {
		doc, err := s.commandDocs.Describe(ctx, data.Name)
		if err != nil && !errors.Is(err, manpage.ErrNoPage) {
			return nil, err
		}
		item.Documentation = lsp.Markdown(doc)
		return item, nil
	}
	snap, err := s.snapshot(docs, data.URI)
	if err != nil {
		return item, nil
	}
	if doc, ok := completion.Documentation(snap.Tree, s.imports(snap.File), data.Kind, data.Name, data.Offset); ok {
		item.Documentation = lsp.Markdown(doc)
	}
	return item, nil
}
