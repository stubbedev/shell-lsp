package navigation

import (
	"context"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Mode uint8

const (
	ModeDefinition Mode = iota
	ModeDeclaration
	ModeTypeDefinition
	ModeImplementation
)

type Location struct {
	File *workspace.File
	Span parser.Span
}

type WorkspaceFiles func(ctx context.Context) ([]*workspace.File, error)

func Definition(ctx context.Context, file *workspace.File, offset int, env Env, files WorkspaceFiles, mode Mode) ([]Location, error) {
	tree := file.Tree
	if mode != ModeTypeDefinition && mode != ModeImplementation {
		for _, ref := range Sources(file, env) {
			if ref.Names(offset) && ref.Target != nil {
				return []Location{{File: ref.Target}}, nil
			}
		}
	}
	if name, ok := functionAt(tree, offset); ok {
		if mode == ModeTypeDefinition {
			return nil, nil
		}
		return functionLocations(ctx, file, name, env, files, mode)
	}
	if name, ok := variableAt(tree, offset); ok {
		if mode == ModeImplementation {
			return nil, nil
		}
		return variableLocations(file, name, tree.ScopeAt(offset), env, mode), nil
	}
	return nil, nil
}

func functionAt(tree *parser.Tree, offset int) (string, bool) {
	for _, fn := range tree.Functions {
		if fn.NameSpan.Covers(offset) {
			return fn.Name, true
		}
	}
	for _, cmd := range tree.Commands {
		if cmd.NameSpan.Covers(offset) {
			return cmd.Name, true
		}
	}
	return "", false
}

func variableAt(tree *parser.Tree, offset int) (string, bool) {
	name, _, ok := variableSpanAt(tree, offset)
	return name, ok
}

func functionLocations(ctx context.Context, file *workspace.File, name string, env Env, files WorkspaceFiles, mode Mode) ([]Location, error) {
	chain := append([]*workspace.File{file}, Chain(file, env)...)
	var found []Location
	for _, f := range chain {
		found = append(found, functionsIn(f, name)...)
		if len(found) > 0 && mode != ModeImplementation {
			return found, nil
		}
	}
	if files == nil {
		return found, nil
	}
	candidates, err := files(ctx)
	if err != nil {
		return nil, err
	}
	for _, f := range candidates {
		if slices.ContainsFunc(chain, func(c *workspace.File) bool { return c.Path == f.Path }) {
			continue
		}
		found = append(found, functionsIn(f, name)...)
	}
	return found, nil
}

func functionsIn(f *workspace.File, name string) []Location {
	var out []Location
	for _, fn := range f.Tree.Functions {
		if fn.Name == name {
			out = append(out, Location{File: f, Span: fn.NameSpan})
		}
	}
	return out
}

func variableLocations(file *workspace.File, name string, scope parser.ScopeID, env Env, mode Mode) []Location {
	if defs := pick(file.Tree.Visible(name, scope), mode); len(defs) > 0 {
		return locate(file, defs)
	}
	for _, f := range Chain(file, env) {
		if defs := pick(f.Tree.Visible(name, parser.FileScope), mode); len(defs) > 0 {
			return locate(f, defs)
		}
	}
	return nil
}

func pick(defs parser.Definitions, mode Mode) []parser.Definition {
	if defs.Len() == 0 {
		return nil
	}
	switch mode {
	case ModeDeclaration:
		for d := range defs.All() {
			if d.Kind == parser.DefDeclare {
				return []parser.Definition{*d}
			}
		}
	case ModeTypeDefinition:
		for d := range defs.All() {
			if d.Attrs.Has(parser.AttrArray) || d.Attrs.Has(parser.AttrAssoc) {
				return []parser.Definition{*d}
			}
		}
		return nil
	}
	return []parser.Definition{*defs.At(0)}
}

func locate(f *workspace.File, defs []parser.Definition) []Location {
	out := make([]Location, len(defs))
	for i, d := range defs {
		out[i] = Location{File: f, Span: d.NameSpan}
	}
	return out
}
