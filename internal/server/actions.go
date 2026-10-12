package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/fileuri"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/platform"
	"github.com/stubbedev/shell-lsp/internal/shellcheckrc"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

const (
	commandFixAll    = "shellLsp.fixAll"
	commandApplyEdit = "shellLsp.applyEdit"
)

var serverCommands = []string{commandFixAll, commandApplyEdit, commandShowReferences, commandRunBats}

type actionData struct {
	URI      string            `json:"uri"`
	Version  int32             `json:"version"`
	Refactor *actions.Refactor `json:"refactor,omitempty"`
	FixAll   string            `json:"fixAll,omitempty"`
}

func (s *Server) handleCodeAction(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	actionParams, err := lsp.DecodeParams[lsp.CodeActionParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing textDocument/codeAction params: %v", err)
	}
	snap, err := s.snapshot(docs, actionParams.TextDocument.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	only := actionParams.Context.Only
	doc, sources := s.actionDocument(snap)
	in := actions.Input{
		Document: doc,
		Selection: parser.Span{
			Start: snap.doc.PositionToOffset(actionParams.Range.Start, snap.encoding),
			End:   snap.doc.PositionToOffset(actionParams.Range.End, snap.encoding),
		},
		Diagnostics:    s.toActionDiagnostics(snap, actionParams.Context.Diagnostics),
		AllDiagnostics: s.lintActionDiagnostics(snap),
	}
	settings := sync.OnceValues(func() (actions.SettingsFile, bool) { return s.settingsFileFor(snap.file.Path) })
	in.Settings = settings
	caps := s.clientCapabilities()
	literal := caps.TextDocument.CodeAction.CodeActionLiteralSupport != nil
	lazy := slices.Contains(caps.TextDocument.CodeAction.ResolveSupport.Properties, "edit") && !snap.doc.Projected()
	out := []any{}
	for _, a := range actions.Actions(in) {
		if !wants(only, string(a.Kind)) || !expressible(snap, sources, a) {
			continue
		}
		action, err := s.codeAction(docs, snap, sources, settings, actionParams.Context.Diagnostics, a, lazy)
		if err != nil {
			return nil, err
		}
		if literal {
			out = append(out, action)
			continue
		}
		arg, err := json.Marshal(action.Edit)
		if err != nil {
			return nil, err
		}
		out = append(out, lsp.Command{Title: a.Title, Command: commandApplyEdit, Arguments: []json.RawMessage{arg}})
	}
	return out, nil
}

func expressible(snap *snapshot, sources func() actionSources, a actions.Action) bool {
	if !snap.doc.Projected() {
		return true
	}
	if _, ok := snap.doc.SourceEdits(a.Edits); !ok {
		return false
	}
	for _, o := range a.Others {
		target, found := sources().files[o.URI]
		if !found {
			return false
		}
		if _, ok := target.Doc.SourceEdits(o.Edits); !ok {
			return false
		}
	}
	return true
}

func (s *Server) codeAction(docs *docstore.View, snap *snapshot, sources func() actionSources, settings func() (actions.SettingsFile, bool), contextDiags []lsp.Diagnostic, a actions.Action, lazy bool) (lsp.CodeAction, error) {
	action := lsp.CodeAction{Title: a.Title, Kind: string(a.Kind), IsPreferred: a.Preferred}
	for _, i := range a.Diagnostics {
		action.Diagnostics = append(action.Diagnostics, contextDiags[i])
	}
	if lazy && (a.Refactor != nil || a.FixAllCode != "") {
		data, err := json.Marshal(actionData{URI: snap.doc.URI, Version: snap.doc.Version, Refactor: a.Refactor, FixAll: a.FixAllCode})
		if err != nil {
			return lsp.CodeAction{}, err
		}
		action.Data = data
		return action, nil
	}
	if a.Settings != nil {
		file, _ := settings()
		edit, err := s.settingsEdit(snap.encoding, a.Title, file, *a.Settings)
		if err != nil {
			return lsp.CodeAction{}, err
		}
		action.Edit = &edit
		return action, nil
	}
	edit, err := s.changeEdit(docs, snap, sources, a.Title, a.Edits, a.Others)
	if err != nil {
		return lsp.CodeAction{}, err
	}
	action.Edit = &edit
	return action, nil
}

type actionSources struct {
	list  []actions.Source
	files map[string]*workspace.File
}

func (s *Server) actionDocument(snap *snapshot) (actions.Document, func() actionSources) {
	resolve := sync.OnceValue(func() actionSources {
		roots := s.workspace.Roots()
		out := actionSources{files: map[string]*workspace.File{}}
		for _, ref := range navigation.Sources(snap.file, s.navigationEnv()) {
			target := ref.Target
			if target == nil || target.URI == snap.file.URI || !slices.ContainsFunc(roots, func(root string) bool { return platform.Native.Within(target.Path, root) }) {
				continue
			}
			out.files[target.URI] = target
			out.list = append(out.list, actions.Source{URI: target.URI, Name: navigation.RelativeName(snap.file.Path, target.Path), Tree: target.Tree, Command: ref.Command})
		}
		return out
	})
	closure := sync.OnceValues(func() ([]*parser.Tree, bool) {
		files, complete := navigation.Closure(snap.file, s.navigationEnv())
		trees := make([]*parser.Tree, len(files))
		for i, f := range files {
			trees[i] = f.Tree
		}
		return trees, complete
	})
	return actions.Document{Tree: snap.tree, Format: snap.formatStyle().Options(), Sources: func() []actions.Source { return resolve().list }, SourcedTrees: closure}, resolve
}

func (s *Server) changeEdit(docs *docstore.View, snap *snapshot, sources func() actionSources, title string, edits []textedit.Edit, others []actions.FileEdits) (lsp.WorkspaceEdit, error) {
	files := []fileEdits{{doc: snap.doc, versioned: true, edits: edits}}
	for _, o := range others {
		target, ok := sources().files[o.URI]
		if !ok {
			return lsp.WorkspaceEdit{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "refactoring edits %s, which %s does not source", o.URI, snap.doc.URI)
		}
		_, open := docs.Document(o.URI)
		files = append(files, fileEdits{doc: target.Doc, versioned: open, edits: o.Edits})
	}
	edit, err := s.multiFileEdit(snap.encoding, files)
	if err != nil {
		return lsp.WorkspaceEdit{}, err
	}
	if len(others) > 0 {
		s.annotateForConfirmation(&edit, title)
	}
	return edit, nil
}

const crossFileAnnotation = "crossFile"

func (s *Server) annotateForConfirmation(edit *lsp.WorkspaceEdit, label string) {
	caps := s.clientCapabilities().Workspace.WorkspaceEdit
	if caps.ChangeAnnotationSupport == nil || len(edit.DocumentChanges) == 0 {
		return
	}
	edit.ChangeAnnotations = map[string]lsp.ChangeAnnotation{crossFileAnnotation: {Label: label, NeedsConfirmation: true}}
	for i := range edit.CreateFiles {
		edit.CreateFiles[i].AnnotationID = crossFileAnnotation
	}
	for i := range edit.DocumentChanges {
		for j := range edit.DocumentChanges[i].Edits {
			edit.DocumentChanges[i].Edits[j].AnnotationID = crossFileAnnotation
		}
	}
}

func (s *Server) settingsFileFor(path string) (actions.SettingsFile, bool) {
	root, ok := s.workspace.RootOf(path)
	if !ok {
		return actions.SettingsFile{}, false
	}
	rc, found, err := s.shellcheckrc.Nearest(filepath.Dir(path))
	if err != nil {
		return actions.SettingsFile{}, false
	}
	if found && platform.Native.Within(rc.Path, root) {
		return actions.SettingsFile{URI: fileuri.FromPath(rc.Path), Text: rc.Text, Exists: true}, true
	}
	caps := s.clientCapabilities().Workspace.WorkspaceEdit
	if !caps.DocumentChanges || !slices.Contains(caps.ResourceOperations, "create") || !rc.Relocatable() {
		return actions.SettingsFile{}, false
	}
	return actions.SettingsFile{URI: fileuri.FromPath(filepath.Join(root, shellcheckrc.FileName)), Text: rc.Text}, true
}

func (s *Server) settingsEdit(enc docstore.Encoding, title string, file actions.SettingsFile, change actions.SettingsEdit) (lsp.WorkspaceEdit, error) {
	doc := docstore.NewDocument(change.URI, "", 0, file.Text)
	if change.Create {
		doc = docstore.NewDocument(change.URI, "", 0, "")
	}
	edit, err := s.multiFileEdit(enc, []fileEdits{{doc: doc, edits: change.Edits}})
	if err != nil {
		return lsp.WorkspaceEdit{}, err
	}
	if change.Create {
		edit.CreateFiles = []lsp.CreateFile{{URI: change.URI}}
	}
	s.annotateForConfirmation(&edit, title)
	return edit, nil
}

func (s *Server) handleCodeActionResolve(_ context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	action, err := lsp.DecodeParams[lsp.CodeAction](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing codeAction/resolve params: %v", err)
	}
	if action.Edit != nil || len(action.Data) == 0 {
		return action, nil
	}
	var data actionData
	if err := json.Unmarshal(action.Data, &data); err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "code action data %s is not from this server: %v", action.Data, err)
	}
	snap, err := s.snapshot(docs, data.URI)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
	}
	if snap.doc.Version != data.Version {
		return nil, jsonrpc.NewError(jsonrpc.CodeContentModified, "%s changed since the action was offered", data.URI)
	}
	doc, sources := s.actionDocument(snap)
	change, err := s.deferredChange(snap, doc, data)
	if err != nil {
		return nil, err
	}
	edit, err := s.changeEdit(docs, snap, sources, action.Title, change.Edits, change.Others)
	if err != nil {
		return nil, err
	}
	action.Edit = &edit
	action.Data = nil
	return action, nil
}

func (s *Server) deferredChange(snap *snapshot, doc actions.Document, data actionData) (actions.Change, error) {
	switch {
	case data.Refactor != nil:
		change, ok := actions.Compute(doc, *data.Refactor)
		if !ok {
			return actions.Change{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "the %s refactoring no longer applies", data.Refactor.Kind)
		}
		return change, nil
	case data.FixAll != "":
		change, ok := actions.FixAllOf(data.FixAll, s.lintActionDiagnostics(snap))
		if !ok {
			return actions.Change{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "%s no longer has several findings to fix", data.FixAll)
		}
		return change, nil
	}
	return actions.Change{}, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "code action data for %s names nothing to compute", data.URI)
}

func (s *Server) handleExecuteCommand(ctx context.Context, docs *docstore.View, params json.RawMessage) (any, error) {
	exec, err := lsp.DecodeParams[lsp.ExecuteCommandParams](params)
	if err != nil {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "parsing workspace/executeCommand params: %v", err)
	}
	var edit lsp.WorkspaceEdit
	switch exec.Command {
	case commandFixAll:
		var uri string
		if len(exec.Arguments) != 1 || json.Unmarshal(exec.Arguments[0], &uri) != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s takes one document URI argument", commandFixAll)
		}
		snap, err := s.snapshot(docs, uri)
		if err != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%v", err)
		}
		edits := actions.FixAll(s.lintActionDiagnostics(snap))
		if len(edits) == 0 {
			return nil, nil
		}
		edit, err = s.workspaceEdit(snap.doc, snap.encoding, edits)
		if err != nil {
			return nil, err
		}
	case commandApplyEdit:
		if len(exec.Arguments) != 1 || json.Unmarshal(exec.Arguments[0], &edit) != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s takes one WorkspaceEdit argument", commandApplyEdit)
		}
	case commandShowReferences:
		var data lensData
		if len(exec.Arguments) != 1 || json.Unmarshal(exec.Arguments[0], &data) != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s takes one {uri, position} argument", commandShowReferences)
		}
		return s.referenceLocations(ctx, docs, data.URI, data.Position)
	case commandRunBats:
		var args batsRunArgs
		if len(exec.Arguments) != 1 || json.Unmarshal(exec.Arguments[0], &args) != nil || args.URI == "" {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s takes one {uri, test?} argument", commandRunBats)
		}
		return s.runBats(ctx, docs, args)
	default:
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "unknown command %q; this server provides %s", exec.Command, strings.Join(serverCommands, ", "))
	}
	return nil, s.applyEdit(ctx, exec.Command, edit)
}

func (s *Server) applyEdit(ctx context.Context, label string, edit lsp.WorkspaceEdit) error {
	var result lsp.ApplyWorkspaceEditResult
	if err := s.call(ctx, applyEditMethod, lsp.ApplyWorkspaceEditParams{Label: label, Edit: edit}, &result); err != nil {
		return err
	}
	if !result.Applied {
		return jsonrpc.NewError(jsonrpc.CodeRequestFailed, "client did not apply the %s edit: %s", label, result.FailureReason)
	}
	return nil
}

type fileEdits struct {
	doc       *docstore.Document
	versioned bool
	edits     []textedit.Edit
}

func (s *Server) workspaceEdit(doc *docstore.Document, enc docstore.Encoding, edits []textedit.Edit) (lsp.WorkspaceEdit, error) {
	return s.multiFileEdit(enc, []fileEdits{{doc: doc, versioned: true, edits: edits}})
}

func (s *Server) multiFileEdit(enc docstore.Encoding, files []fileEdits) (lsp.WorkspaceEdit, error) {
	documentChanges := s.clientCapabilities().Workspace.WorkspaceEdit.DocumentChanges
	out := lsp.WorkspaceEdit{}
	for _, f := range files {
		lspEdits, ok := f.doc.TextEdits(f.edits, enc)
		if !ok {
			return lsp.WorkspaceEdit{}, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "editing %s: the change cannot be written in the syntax of the file around its shell", f.doc.URI)
		}
		if !documentChanges {
			if out.Changes == nil {
				out.Changes = map[string][]lsp.TextEdit{}
			}
			out.Changes[f.doc.URI] = lspEdits
			continue
		}
		id := lsp.OptionalVersionedTextDocumentIdentifier{URI: f.doc.URI}
		if f.versioned {
			version := f.doc.Version
			id.Version = &version
		}
		out.DocumentChanges = append(out.DocumentChanges, lsp.TextDocumentEdit{TextDocument: id, Edits: lspEdits})
	}
	return out, nil
}

func (s *Server) toActionDiagnostics(snap *snapshot, diags []lsp.Diagnostic) []actions.Diagnostic {
	findings, _ := s.findings(snap.file, snap.settings, snap.generation)
	out := make([]actions.Diagnostic, 0, len(diags))
	for i, d := range diags {
		ad := actions.Diagnostic{
			Index:   i,
			Code:    d.Code,
			Message: d.Message,
			Span: parser.Span{
				Start: snap.doc.PositionToOffset(d.Range.Start, snap.encoding),
				End:   snap.doc.PositionToOffset(d.Range.End, snap.encoding),
			},
		}
		if j := slices.IndexFunc(findings, func(f lint.Finding) bool { return f.Code.String() == d.Code && f.Span == ad.Span }); j >= 0 {
			ad.Fixes = findings[j].Fixes
		}
		out = append(out, ad)
	}
	return out
}

func (s *Server) lintActionDiagnostics(snap *snapshot) []actions.Diagnostic {
	findings, _ := s.findings(snap.file, snap.settings, snap.generation)
	return actions.FindingDiagnostics(findings)
}

func wants(only []string, kind string) bool {
	if len(only) == 0 {
		return true
	}
	return slices.ContainsFunc(only, func(o string) bool { return kind == o || strings.HasPrefix(kind, o+".") })
}
