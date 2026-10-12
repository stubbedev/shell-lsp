package navigation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

var (
	ErrNoSymbol      = errors.New("no symbol at this position")
	ErrNotRenamable  = errors.New("symbol cannot be renamed")
	ErrInvalidName   = errors.New("invalid name")
	ErrNameConflict  = errors.New("name already in use")
	variableNameRule = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	functionNameRule = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]*$`)
)

type Symbol struct {
	Name     string
	Function bool
	Local    bool
	Scope    parser.ScopeID
	Path     string
	Span     parser.Span
}

type Occurrence struct {
	File        *workspace.File
	Span        parser.Span
	Write       bool
	Declaration bool
}

func SymbolAt(file *workspace.File, offset int) (Symbol, bool) {
	tree := file.Tree
	for _, fn := range tree.Functions {
		if fn.NameSpan.Covers(offset) {
			return Symbol{Name: fn.Name, Function: true, Path: file.Path, Span: fn.NameSpan}, true
		}
	}
	for _, cmd := range tree.Commands {
		if cmd.NameSpan.Covers(offset) {
			return Symbol{Name: cmd.Name, Function: true, Path: file.Path, Span: cmd.NameSpan}, true
		}
	}
	name, span, ok := variableSpanAt(tree, offset)
	if !ok {
		return Symbol{}, false
	}
	scope, local := tree.Binding(name, tree.ScopeAt(offset))
	return Symbol{Name: name, Local: local, Scope: scope, Path: file.Path, Span: span}, true
}

func variableSpanAt(tree *parser.Tree, offset int) (string, parser.Span, bool) {
	for _, ref := range tree.References {
		if ref.Span.Covers(offset) {
			return ref.Name, ref.Span, true
		}
	}
	for _, def := range tree.Definitions {
		if def.NameSpan.Covers(offset) {
			return def.Name, def.NameSpan, true
		}
	}
	for _, located := range slices.Backward(tree.PathAt(offset)) {
		exp, ok := located.Node.(*syntax.ParamExp)
		if !ok || exp.Param == nil {
			continue
		}
		if span, ok := tree.NodeSpan(exp.Param); ok {
			return exp.Param.Value, span, true
		}
	}
	return "", parser.Span{}, false
}

func Group(ctx context.Context, file *workspace.File, env Env, files WorkspaceFiles) ([]*workspace.File, error) {
	group := append([]*workspace.File{file}, Chain(file, env)...)
	if files == nil {
		return group, nil
	}
	candidates, err := files(ctx)
	if err != nil {
		return nil, err
	}
	members := make(map[string]bool, len(group))
	for _, f := range group {
		members[f.Path] = true
	}
	for _, other := range candidates {
		if members[other.Path] {
			continue
		}
		if slices.ContainsFunc(Chain(other, env), func(f *workspace.File) bool { return f.Path == file.Path }) {
			members[other.Path] = true
			group = append(group, other)
		}
	}
	return group, nil
}

func Occurrences(sym Symbol, group []*workspace.File) []Occurrence {
	var out []Occurrence
	for _, f := range group {
		if sym.Local && f.Path != sym.Path {
			continue
		}
		out = append(out, occurrencesIn(sym, f)...)
	}
	return mergeOccurrences(out)
}

func occurrencesIn(sym Symbol, f *workspace.File) []Occurrence {
	tree := f.Tree
	var out []Occurrence
	if sym.Function {
		for _, fn := range tree.Functions {
			if fn.Name == sym.Name {
				out = append(out, Occurrence{File: f, Span: fn.NameSpan, Write: true, Declaration: true})
			}
		}
		for _, cmd := range tree.Commands {
			if cmd.Name == sym.Name {
				out = append(out, Occurrence{File: f, Span: cmd.NameSpan})
			}
		}
		return out
	}
	matches := func(within parser.ScopeID) bool {
		scope, local := tree.Binding(sym.Name, within)
		return local == sym.Local && (!local || scope == sym.Scope)
	}
	for _, def := range tree.Definitions {
		if def.Name == sym.Name && matches(def.Within) {
			out = append(out, Occurrence{File: f, Span: def.NameSpan, Write: true, Declaration: def.Kind == parser.DefDeclare})
		}
	}
	for _, ref := range tree.References {
		if ref.Name == sym.Name && matches(ref.Within) {
			out = append(out, Occurrence{File: f, Span: ref.Span})
		}
	}
	return out
}

func mergeOccurrences(occs []Occurrence) []Occurrence {
	slices.SortStableFunc(occs, func(a, b Occurrence) int {
		return cmp.Or(cmp.Compare(a.File.Path, b.File.Path), cmp.Compare(a.Span.Start, b.Span.Start))
	})
	var out []Occurrence
	for _, o := range occs {
		if n := len(out); n > 0 && out[n-1].File.Path == o.File.Path && out[n-1].Span == o.Span {
			out[n-1].Write = out[n-1].Write || o.Write
			out[n-1].Declaration = out[n-1].Declaration || o.Declaration
			continue
		}
		out = append(out, o)
	}
	return out
}

func References(ctx context.Context, file *workspace.File, offset int, env Env, files WorkspaceFiles, includeDeclaration bool) ([]Occurrence, error) {
	sym, ok := SymbolAt(file, offset)
	if !ok {
		return nil, nil
	}
	group, err := Group(ctx, file, env, files)
	if err != nil {
		return nil, err
	}
	occs := Occurrences(sym, group)
	if includeDeclaration {
		return occs, nil
	}
	declared := slices.ContainsFunc(occs, func(o Occurrence) bool { return o.Declaration })
	firstWrite := slices.IndexFunc(occs, func(o Occurrence) bool { return o.Write })
	var out []Occurrence
	for i, o := range occs {
		if o.Declaration || (!declared && i == firstWrite) {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

func PrepareRename(ctx context.Context, file *workspace.File, offset int, env Env, files WorkspaceFiles) (Symbol, []Occurrence, error) {
	sym, ok := SymbolAt(file, offset)
	if !ok {
		return Symbol{}, nil, ErrNoSymbol
	}
	group, err := Group(ctx, file, env, files)
	if err != nil {
		return Symbol{}, nil, err
	}
	occs := Occurrences(sym, group)
	if !slices.ContainsFunc(occs, func(o Occurrence) bool { return o.Write }) {
		what := "variable"
		if sym.Function {
			what = "command"
		}
		return Symbol{}, nil, fmt.Errorf("%w: %s %q is not defined in the workspace", ErrNotRenamable, what, sym.Name)
	}
	return sym, occs, nil
}

func Rename(ctx context.Context, file *workspace.File, offset int, newName string, env Env, files WorkspaceFiles) ([]Occurrence, error) {
	sym, occs, err := PrepareRename(ctx, file, offset, env, files)
	if err != nil {
		return nil, err
	}
	rule := variableNameRule
	if sym.Function {
		rule = functionNameRule
	}
	if !rule.MatchString(newName) {
		return nil, fmt.Errorf("%w: %q is not a valid %s name", ErrInvalidName, newName, kindName(sym))
	}
	if newName == sym.Name {
		return nil, nil
	}
	group, err := Group(ctx, file, env, files)
	if err != nil {
		return nil, err
	}
	target := sym
	target.Name = newName
	if conflicts := Occurrences(target, group); slices.ContainsFunc(conflicts, func(o Occurrence) bool { return o.Write }) {
		return nil, fmt.Errorf("%w: %s %q already exists in the same scope", ErrNameConflict, kindName(sym), newName)
	}
	return occs, nil
}

func kindName(sym Symbol) string {
	if sym.Function {
		return "function"
	}
	return "variable"
}
