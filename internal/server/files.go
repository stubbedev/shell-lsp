package server

import (
	"context"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/editorconfig"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const watcherRegistration = "shell-lsp/watched-files"

type workspaceFoldersCapability struct {
	Supported           bool `json:"supported"`
	ChangeNotifications bool `json:"changeNotifications"`
}

type fileOperationPattern struct {
	Glob    string `json:"glob"`
	Matches string `json:"matches,omitempty"`
}

type fileOperationFilter struct {
	Pattern fileOperationPattern `json:"pattern"`
}

type fileOperationRegistration struct {
	Filters []fileOperationFilter `json:"filters"`
}

type fileOperationsCapability struct {
	DidCreate  *fileOperationRegistration `json:"didCreate,omitempty"`
	WillRename *fileOperationRegistration `json:"willRename,omitempty"`
	DidRename  *fileOperationRegistration `json:"didRename,omitempty"`
	DidDelete  *fileOperationRegistration `json:"didDelete,omitempty"`
}

type WorkspaceCapabilities struct {
	WorkspaceFolders *workspaceFoldersCapability `json:"workspaceFolders,omitempty"`
	FileOperations   *fileOperationsCapability   `json:"fileOperations,omitempty"`
}

var watchedFileOperations = func() fileOperationRegistration {
	var filters []fileOperationFilter
	for _, glob := range workspace.WatchGlobs() {
		filters = append(filters, fileOperationFilter{Pattern: fileOperationPattern{Glob: glob, Matches: "file"}})
	}
	filters = append(filters, fileOperationFilter{Pattern: fileOperationPattern{Glob: "**/*", Matches: "folder"}})
	return fileOperationRegistration{Filters: filters}
}()

func fileOperations(c *ServerCapabilities) *fileOperationsCapability {
	return ensure(&ensure(&c.Workspace).FileOperations)
}

type fileWatcher struct {
	GlobPattern string `json:"globPattern"`
}

type registration struct {
	ID              string `json:"id"`
	Method          string `json:"method"`
	RegisterOptions any    `json:"registerOptions"`
}

type registrationParams struct {
	Registrations []registration `json:"registrations"`
}

func (s *Server) registerWatchers(ctx context.Context) {
	if !s.clientCapabilities().Workspace.DidChangeWatchedFiles.DynamicRegistration {
		return
	}
	globs := append(workspace.WatchGlobs(), "**/"+editorconfig.FileName)
	for _, name := range shellcheckrc.Names {
		globs = append(globs, "**/"+name)
	}
	watchers := make([]fileWatcher, len(globs))
	for i, glob := range globs {
		watchers[i] = fileWatcher{GlobPattern: glob}
	}
	params := registrationParams{Registrations: []registration{{
		ID:              watcherRegistration,
		Method:          didChangeWatchedFilesMethod,
		RegisterOptions: map[string]any{"watchers": watchers},
	}}}
	if err := s.call(ctx, registerCapabilityMethod, params, nil); err != nil {
		s.noticeOnce("register-watchers", lsp.MessageWarning, "registering file watchers failed; changes made outside the editor are picked up when files are next read: "+err.Error())
		return
	}
	s.workspace.SetWatched(true)
}

func (s *Server) handleFilesChanged(_ context.Context, _ lsp.FileOperationsParams) error {
	s.workspace.InvalidateAll()
	return nil
}

func (s *Server) handleDidRenameFiles(_ context.Context, _ lsp.RenameFilesParams) error {
	s.workspace.InvalidateAll()
	return nil
}

type move struct {
	from, to string
}

func (m move) apply(path string) (string, bool) {
	rel, ok := platform.RelativeWithin(m.from, path)
	if !ok {
		return path, false
	}
	return filepath.Join(m.to, rel), true
}

func (s *Server) handleWillRenameFiles(ctx context.Context, docs *docstore.View, p lsp.RenameFilesParams) (any, error) {
	var moves []move
	for _, f := range p.Files {
		from, errFrom := fileuri.ToPath(f.OldURI)
		to, errTo := fileuri.ToPath(f.NewURI)
		if errFrom != nil || errTo != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "file rename %s -> %s is not between file URIs", f.OldURI, f.NewURI)
		}
		moves = append(moves, move{from: from, to: to})
	}
	moved := func(path string) (string, bool) {
		for _, m := range moves {
			if to, ok := m.apply(path); ok {
				return to, true
			}
		}
		return path, false
	}
	candidates, err := s.candidateFiles(docs)(ctx)
	if err != nil {
		return nil, err
	}
	env := s.navigationEnv()
	var files []fileEdits
	for _, file := range candidates {
		edits := sourceRewrites(file, env, moved)
		if newPath, ok := moved(file.Path); ok && !file.Doc.Projected() {
			if edit, ok := shebangRewrite(file.Tree, file.Path, newPath); ok {
				edits = append(edits, edit)
			}
		}
		if len(edits) > 0 {
			_, open := docs.Document(file.URI)
			files = append(files, fileEdits{doc: file.Doc, versioned: open, edits: edits})
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	return s.multiFileEdit(docs.Encoding(), files)
}

func sourceRewrites(file *workspace.File, env navigation.Env, moved func(string) (string, bool)) []textedit.Edit {
	var edits []textedit.Edit
	newFile, fileMoved := moved(file.Path)
	for _, ref := range navigation.Sources(file, env) {
		if ref.Target == nil {
			continue
		}
		newTarget, targetMoved := moved(ref.Target.Path)
		if !fileMoved && !targetMoved {
			continue
		}
		literal, ok := parser.Literal(ref.File)
		if !ok {
			continue
		}
		raw := file.Tree.Text()[ref.Arg.Start:ref.Arg.End]
		rewritten := rewritePath(literal, filepath.Dir(newFile), newTarget, env.Home)
		if rewritten == literal {
			continue
		}
		edits = append(edits, textedit.Edit{Start: ref.Arg.Start, End: ref.Arg.End, NewText: requote(raw, rewritten)})
	}
	return edits
}

func rewritePath(original, newDir, newTarget, home string) string {
	switch {
	case filepath.IsAbs(original):
		return newTarget
	case strings.HasPrefix(original, "~/") && home != "":
		if rel, ok := platform.RelativeWithin(home, newTarget); ok {
			return "~/" + filepath.ToSlash(rel)
		}
		return newTarget
	}
	rel, err := filepath.Rel(newDir, newTarget)
	if err != nil {
		return newTarget
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(original, "./") && !strings.HasPrefix(rel, "../") {
		rel = "./" + rel
	}
	return rel
}

func requote(raw, value string) string {
	switch {
	case strings.HasPrefix(raw, "'"):
		return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
	case strings.HasPrefix(raw, `"`):
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(value) + `"`
	case strings.ContainsAny(value, " \t'\"$`\\&;|()<>*?[]{}!"):
		return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
	}
	return value
}

func shebangRewrite(tree *parser.Tree, oldPath, newPath string) (textedit.Edit, bool) {
	oldDialect, okOld := dialect.FromFileName(oldPath)
	newDialect, okNew := dialect.FromFileName(newPath)
	if !okOld || !okNew || oldDialect == newDialect {
		return textedit.Edit{}, false
	}
	text := tree.Text()
	firstLine, _, _ := strings.Cut(text, "\n")
	current, ok := dialect.FromShebang(firstLine)
	if !ok || current != oldDialect {
		return textedit.Edit{}, false
	}
	fields := strings.Fields(strings.TrimPrefix(firstLine, "#!"))
	target := 0
	if path.Base(fields[0]) == "env" {
		target = slices.IndexFunc(fields[1:], func(f string) bool { return !strings.HasPrefix(f, "-") }) + 1
	}
	fields[target] = path.Join(path.Dir(fields[target]), newDialect.String())
	return textedit.Edit{Start: 0, End: len(firstLine), NewText: "#!" + strings.Join(fields, " ")}, true
}
