package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/diagnostics"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
)

const (
	commandFixAll    = "shellLsp.fixAll"
	commandApplyEdit = "shellLsp.applyEdit"
)

var serverCommands = []string{commandFixAll, commandApplyEdit}

var codeActionKinds = []string{
	string(actions.QuickFix),
	string(actions.RefactorExtract),
	string(actions.RefactorRewrite),
	string(actions.SourceFixAll),
}

type actionData struct {
	URI      string           `json:"uri"`
	Version  int32            `json:"version"`
	Refactor actions.Refactor `json:"refactor"`
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
	in := actions.Input{
		Tree: snap.tree,
		Selection: parser.Span{
			Start: snap.doc.PositionToOffset(actionParams.Range.Start, snap.encoding),
			End:   snap.doc.PositionToOffset(actionParams.Range.End, snap.encoding),
		},
		Diagnostics:    toActionDiagnostics(snap, actionParams.Context.Diagnostics),
		LocalSupported: snap.dialect.Features().Local,
	}
	if wants(only, string(actions.SourceFixAll)) {
		report, err := s.diagnose(ctx, snap)
		if err != nil {
			return nil, err
		}
		in.AllDiagnostics = toActionDiagnostics(snap, report.items)
	}
	caps := s.clientCapabilities()
	literal := caps.TextDocument.CodeAction.CodeActionLiteralSupport != nil
	lazy := slices.Contains(caps.TextDocument.CodeAction.ResolveSupport.Properties, "edit")
	out := []any{}
	for _, a := range actions.Actions(in) {
		if !wants(only, string(a.Kind)) {
			continue
		}
		action, err := s.codeAction(snap, actionParams.Context.Diagnostics, a, lazy)
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

func (s *Server) codeAction(snap *snapshot, contextDiags []lsp.Diagnostic, a actions.Action, lazy bool) (lsp.CodeAction, error) {
	action := lsp.CodeAction{Title: a.Title, Kind: string(a.Kind), IsPreferred: a.Preferred}
	for _, i := range a.Diagnostics {
		action.Diagnostics = append(action.Diagnostics, contextDiags[i])
	}
	if lazy && a.Refactor != nil {
		data, err := json.Marshal(actionData{URI: snap.doc.URI, Version: snap.doc.Version, Refactor: *a.Refactor})
		if err != nil {
			return lsp.CodeAction{}, err
		}
		action.Data = data
		return action, nil
	}
	edit := s.workspaceEdit(snap.doc, snap.encoding, a.Edits)
	action.Edit = &edit
	return action, nil
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
	edits, _, ok := actions.Compute(snap.tree, data.Refactor)
	if !ok {
		return nil, jsonrpc.NewError(jsonrpc.CodeRequestFailed, "the %s refactoring no longer applies", data.Refactor.Kind)
	}
	edit := s.workspaceEdit(snap.doc, snap.encoding, edits)
	action.Edit = &edit
	action.Data = nil
	return action, nil
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
		report, err := s.diagnose(ctx, snap)
		if err != nil {
			return nil, err
		}
		edits := actions.FixAll(toActionDiagnostics(snap, report.items))
		if len(edits) == 0 {
			return nil, nil
		}
		edit = s.workspaceEdit(snap.doc, snap.encoding, edits)
	case commandApplyEdit:
		if len(exec.Arguments) != 1 || json.Unmarshal(exec.Arguments[0], &edit) != nil {
			return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s takes one WorkspaceEdit argument", commandApplyEdit)
		}
	default:
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "unknown command %q; this server provides %s", exec.Command, strings.Join(serverCommands, ", "))
	}
	return nil, s.applyEdit(ctx, exec.Command, edit)
}

func (s *Server) applyEdit(ctx context.Context, label string, edit lsp.WorkspaceEdit) error {
	var result lsp.ApplyWorkspaceEditResult
	if err := s.call(ctx, "workspace/applyEdit", lsp.ApplyWorkspaceEditParams{Label: label, Edit: edit}, &result); err != nil {
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

func (s *Server) workspaceEdit(doc *docstore.Document, enc docstore.Encoding, edits []textedit.Edit) lsp.WorkspaceEdit {
	return s.multiFileEdit(enc, []fileEdits{{doc: doc, versioned: true, edits: edits}})
}

func (s *Server) multiFileEdit(enc docstore.Encoding, files []fileEdits) lsp.WorkspaceEdit {
	documentChanges := s.clientCapabilities().Workspace.WorkspaceEdit.DocumentChanges
	out := lsp.WorkspaceEdit{}
	for _, f := range files {
		lspEdits := make([]lsp.TextEdit, len(f.edits))
		for i, e := range f.edits {
			lspEdits[i] = lsp.TextEdit{Range: f.doc.RangeFor(e.Start, e.End, enc), NewText: e.NewText}
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
	return out
}

func toActionDiagnostics(snap *snapshot, diags []lsp.Diagnostic) []actions.Diagnostic {
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
		if d.Source == diagnostics.ShellcheckSource && len(d.Data) > 0 {
			var fix diagnostics.FixData
			if err := json.Unmarshal(d.Data, &fix); err == nil {
				for _, e := range fix.Edits {
					ad.Fix = append(ad.Fix, textedit.Edit{
						Start:   snap.doc.PositionToOffset(e.Range.Start, snap.encoding),
						End:     snap.doc.PositionToOffset(e.Range.End, snap.encoding),
						NewText: e.NewText,
					})
				}
			}
		}
		out = append(out, ad)
	}
	return out
}

func wants(only []string, kind string) bool {
	if len(only) == 0 {
		return true
	}
	return slices.ContainsFunc(only, func(o string) bool { return kind == o || strings.HasPrefix(kind, o+".") })
}
