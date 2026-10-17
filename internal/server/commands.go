package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/actions"
	"github.com/stubbedev/shell-lsp/internal/docstore"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/lsp"
)

const (
	commandFixAll         = "shellLsp.fixAll"
	commandApplyEdit      = "shellLsp.applyEdit"
	commandShowReferences = "shellLsp.showReferences"
	commandRunBats        = "shellLsp.runBats"
	commandRunRecipe      = "shellLsp.runRecipe"
)

type commandRunner func(s *Server, ctx context.Context, docs *docstore.View, args []json.RawMessage) (any, error)

type command struct {
	name  string
	usage string
	run   commandRunner
}

var commands = []command{
	{commandFixAll, "one document URI", oneArg((*Server).fixAll)},
	{commandApplyEdit, "one WorkspaceEdit", oneArg((*Server).applyEditCommand)},
	{commandShowReferences, "one {uri, position}", oneArg((*Server).showReferences)},
	{commandRunBats, "one {uri, test?}", oneArg((*Server).runBatsCommand)},
	{commandRunRecipe, "one {uri, name}", oneArg((*Server).runRecipeCommand)},
}

var serverCommands = func() []string {
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.name
	}
	return names
}()

type argumentError struct {
	err error
}

func (e *argumentError) Error() string { return e.err.Error() }

func (e *argumentError) Unwrap() error { return e.err }

func oneArg[A any](run func(*Server, context.Context, *docstore.View, A) (any, error)) commandRunner {
	return func(s *Server, ctx context.Context, docs *docstore.View, args []json.RawMessage) (any, error) {
		if len(args) != 1 {
			return nil, &argumentError{fmt.Errorf("got %d arguments", len(args))}
		}
		var arg A
		if err := json.Unmarshal(args[0], &arg); err != nil {
			return nil, &argumentError{err}
		}
		return run(s, ctx, docs, arg)
	}
}

func (s *Server) handleExecuteCommand(ctx context.Context, docs *docstore.View, exec lsp.ExecuteCommandParams) (any, error) {
	i := slices.IndexFunc(commands, func(c command) bool { return c.name == exec.Command })
	if i < 0 {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "unknown command %q; this server provides %s", exec.Command, strings.Join(serverCommands, ", "))
	}
	c := commands[i]
	result, err := c.run(s, ctx, docs, exec.Arguments)
	if bad, ok := errors.AsType[*argumentError](err); ok {
		return nil, jsonrpc.NewError(jsonrpc.CodeInvalidParams, "%s takes %s argument: %v", c.name, c.usage, bad.err)
	}
	return result, err
}

func (s *Server) fixAll(ctx context.Context, docs *docstore.View, uri string) (any, error) {
	snap, err := s.snapshot(docs, uri)
	if err != nil {
		return nil, err
	}
	edits := actions.FixAll(s.lintActionDiagnostics(snap))
	if len(edits) == 0 {
		return nil, nil
	}
	edit, err := s.workspaceEdit(snap.Doc, snap.encoding, edits)
	if err != nil {
		return nil, err
	}
	return nil, s.applyEdit(ctx, commandFixAll, edit)
}

func (s *Server) applyEditCommand(ctx context.Context, _ *docstore.View, edit lsp.WorkspaceEdit) (any, error) {
	return nil, s.applyEdit(ctx, commandApplyEdit, edit)
}

func (s *Server) showReferences(ctx context.Context, docs *docstore.View, data lensData) (any, error) {
	return s.referenceLocations(ctx, docs, data.at(), false)
}

func (s *Server) runBatsCommand(ctx context.Context, docs *docstore.View, args batsRunArgs) (any, error) {
	if args.URI == "" {
		return nil, &argumentError{errors.New("the argument names no uri")}
	}
	return s.runBats(ctx, docs, args)
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
