package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/shell-lsp/internal/completion"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/manpage"
)

var completionKinds = map[completion.Kind]lsp.CompletionItemKind{
	completion.KindKeyword:  lsp.CompletionKeyword,
	completion.KindBuiltin:  lsp.CompletionMethod,
	completion.KindFunction: lsp.CompletionFunction,
	completion.KindAlias:    lsp.CompletionFunction,
	completion.KindCommand:  lsp.CompletionModule,
	completion.KindVariable: lsp.CompletionVariable,
	completion.KindFile:     lsp.CompletionFile,
	completion.KindFolder:   lsp.CompletionFolder,
	completion.KindFlag:     lsp.CompletionProperty,
	completion.KindOption:   lsp.CompletionValue,
}

type completionData struct {
	Kind   completion.ResolveKind `json:"kind"`
	Name   string                 `json:"name"`
	URI    string                 `json:"uri,omitempty"`
	Offset int                    `json:"offset,omitempty"`
}

type shellEnvironment struct {
	pages       *manpage.Lookup
	executables *executableCache
}

func (e shellEnvironment) Executables() []string { return e.executables.list(os.Getenv("PATH")) }

func (e shellEnvironment) ReadDir(dir string) ([]completion.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]completion.DirEntry, 0, len(entries))
	for _, entry := range entries {
		isDir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, entry.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		out = append(out, completion.DirEntry{Name: entry.Name(), Dir: isDir})
	}
	return out, nil
}

func (e shellEnvironment) Home() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func (e shellEnvironment) EnvironmentNames() []string {
	env := os.Environ()
	names := make([]string, 0, len(env))
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func (e shellEnvironment) Options(ctx context.Context, command string) ([]manpage.Option, error) {
	page, err := e.pages.Page(ctx, command)
	if err != nil {
		return nil, err
	}
	return page.Options(), nil
}

type executableCache struct {
	mu       sync.Mutex
	path     string
	modTimes []time.Time
	names    []string
}

func (c *executableCache) list(path string) []string {
	dirs := filepath.SplitList(path)
	modTimes := make([]time.Time, len(dirs))
	for i, dir := range dirs {
		if info, err := os.Stat(dir); err == nil { //nolint:gosec
			modTimes[i] = info.ModTime()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.names != nil && c.path == path && slices.EqualFunc(c.modTimes, modTimes, time.Time.Equal) {
		return c.names
	}
	names := []string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				if info, err = os.Stat(filepath.Join(dir, entry.Name())); err != nil { //nolint:gosec
					continue
				}
			}
			if !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
				names = append(names, entry.Name())
			}
		}
	}
	slices.Sort(names)
	c.path, c.modTimes, c.names = path, modTimes, slices.Compact(names)
	return c.names
}

func (s *Server) handleCompletion(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	completionParams, err := lsp.DecodeParams[lsp.CompletionParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/completion params: %v", err)
	}
	snap, err := s.snapshot(docs, completionParams.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	workDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	result, err := completion.Complete(ctx, completion.Input{
		Tree:      snap.tree,
		Imports:   s.imports(snap),
		Offset:    snap.doc.PositionToOffset(completionParams.Position, snap.encoding),
		ScriptDir: scriptDir(snap.doc.URI),
		WorkDir:   workDir,
		Snippets:  s.clientCapabilities().TextDocument.Completion.CompletionItem.SnippetSupport,
		Env:       s.environment,
	})
	if err != nil {
		return nil, err
	}
	items := make([]lsp.CompletionItem, 0, len(result.Items))
	for _, it := range result.Items {
		item, err := s.completionItem(snap, it)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return lsp.CompletionList{IsIncomplete: result.Incomplete, Items: items}, nil
}

func (s *Server) completionItem(snap *snapshot, it completion.Item) (lsp.CompletionItem, error) {
	format := lsp.InsertPlainText
	if it.Snippet {
		format = lsp.InsertSnippet
	}
	item := lsp.CompletionItem{
		Label:            it.Label,
		Kind:             completionKinds[it.Kind],
		Detail:           it.Detail,
		SortText:         it.SortText,
		FilterText:       it.InsertText,
		InsertTextFormat: format,
		TextEdit:         &lsp.TextEdit{Range: snap.doc.RangeFor(it.Replace.Start, it.Replace.End, snap.encoding), NewText: it.InsertText},
	}
	if it.Snippet {
		item.FilterText = it.Label
	}
	if it.Documentation != "" {
		item.Documentation = &lsp.MarkupContent{Kind: lsp.MarkupMarkdown, Value: it.Documentation}
	}
	if it.Resolve != completion.ResolveNone {
		data, err := json.Marshal(completionData{Kind: it.Resolve, Name: it.Label, URI: snap.doc.URI, Offset: it.Replace.Start})
		if err != nil {
			return lsp.CompletionItem{}, err
		}
		item.Data = data
	}
	return item, nil
}

func (s *Server) handleCompletionResolve(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	item, err := lsp.DecodeParams[lsp.CompletionItem](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing completionItem/resolve params: %v", err)
	}
	if len(item.Data) == 0 {
		return item, nil
	}
	var data completionData
	if err := json.Unmarshal(item.Data, &data); err != nil || data.Name == "" || data.Kind == completion.ResolveNone {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "completion item data %s is not from this server", item.Data)
	}
	if data.Kind == completion.ResolveCommand {
		doc, err := s.commandDocs.Describe(ctx, data.Name)
		if err != nil && !errors.Is(err, manpage.ErrNoPage) {
			return nil, err
		}
		item.Documentation = &lsp.MarkupContent{Kind: lsp.MarkupMarkdown, Value: doc}
		return item, nil
	}
	snap, err := s.snapshot(docs, data.URI)
	if err != nil {
		return item, nil
	}
	if doc, ok := completion.Documentation(snap.tree, s.imports(snap), data.Kind, data.Name, data.Offset); ok {
		item.Documentation = &lsp.MarkupContent{Kind: lsp.MarkupMarkdown, Value: doc}
	}
	return item, nil
}
