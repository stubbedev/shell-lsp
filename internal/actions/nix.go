package actions

import (
	"fmt"

	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

func nixKind(kind nix.RefactorKind) Kind {
	switch kind {
	case nix.RefactorExtract:
		return RefactorExtract
	case nix.RefactorInline:
		return RefactorInline
	case nix.RefactorRewrite:
		return RefactorRewrite
	}
	panic(fmt.Sprintf("actions: Nix refactoring kind %d has no action kind", kind))
}

func Nix(text string, selection parser.Span) []Action {
	refactorings := nix.Read(text).Refactorings(selection)
	out := make([]Action, len(refactorings))
	for i, r := range refactorings {
		out[i] = Action{Title: r.Title, Kind: nixKind(r.Kind), Edits: r.Edits}
	}
	return out
}
