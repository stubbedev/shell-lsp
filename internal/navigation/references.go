package navigation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/scan"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

var (
	ErrNoSymbol     = errors.New("no symbol at this position")
	ErrNotRenamable = errors.New("symbol cannot be renamed")
	ErrInvalidName  = errors.New("invalid name")
	ErrNameConflict = errors.New("name already in use")
)

const functionNamePunctuation = ".:-"

type Kind uint8

const (
	KindFunction Kind = iota
	KindVariable
)

func (k Kind) String() string {
	switch k {
	case KindFunction:
		return "function"
	case KindVariable:
		return "variable"
	}
	panic(fmt.Sprintf("navigation: unknown symbol kind %d", k))
}

func (k Kind) ValidName(name string) bool {
	switch k {
	case KindFunction:
		return validFunctionName(name)
	case KindVariable:
		return parser.IsName(name)
	}
	panic(fmt.Sprintf("navigation: unknown symbol kind %d", k))
}

func validFunctionName(name string) bool {
	return name != "" && parser.IsName(name[:1]) && scan.All(name, func(c byte) bool {
		return scan.NameChar(c) || strings.IndexByte(functionNamePunctuation, c) >= 0
	})
}

type Symbol struct {
	Name    string
	Kind    Kind
	Binding *parser.ScopeID
	Path    string
	Span    parser.Span
}

func (s Symbol) boundAs(scope parser.ScopeID, local bool) bool {
	if s.Binding == nil {
		return !local
	}
	return local && scope == *s.Binding
}

type Occurrence struct {
	File        *workspace.File
	Span        parser.Span
	Write       bool
	Declaration bool
}

func SymbolAt(file *workspace.File, offset int) (Symbol, bool) {
	tree := file.Tree
	hit, ok := tree.SymbolAt(offset)
	if !ok {
		return Symbol{}, false
	}
	sym := Symbol{Name: hit.Name, Kind: KindFunction, Path: file.Path, Span: hit.Span}
	if hit.Kind != parser.HitVariable {
		return sym, true
	}
	sym.Kind = KindVariable
	if scope, local := tree.Binding(hit.Name, tree.ScopeAt(offset)); local {
		sym.Binding = &scope
	}
	return sym, true
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
		if sym.Binding != nil && f.Path != sym.Path {
			continue
		}
		out = append(out, occurrencesIn(sym, f)...)
	}
	return mergeOccurrences(out)
}

func occurrencesIn(sym Symbol, f *workspace.File) []Occurrence {
	tree := f.Tree
	var out []Occurrence
	if sym.Kind == KindFunction {
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
		return sym.boundAs(tree.Binding(sym.Name, within))
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
		return Symbol{}, nil, fmt.Errorf("%w: %s %q is not defined in the workspace", ErrNotRenamable, sym.Kind, sym.Name)
	}
	return sym, occs, nil
}

func Rename(ctx context.Context, file *workspace.File, offset int, newName string, env Env, files WorkspaceFiles) ([]Occurrence, error) {
	sym, occs, err := PrepareRename(ctx, file, offset, env, files)
	if err != nil {
		return nil, err
	}
	if !sym.Kind.ValidName(newName) {
		return nil, fmt.Errorf("%w: %q is not a valid %s name", ErrInvalidName, newName, sym.Kind)
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
		return nil, fmt.Errorf("%w: %s %q already exists in the same scope", ErrNameConflict, sym.Kind, newName)
	}
	return occs, nil
}
