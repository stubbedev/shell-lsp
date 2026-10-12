package navigation

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type CallItem struct {
	File      *workspace.File
	Name      string
	Span      parser.Span
	Selection parser.Span
	TopLevel  bool
}

type Call struct {
	Item  CallItem
	Spans []parser.Span
}

func PrepareCallHierarchy(file *workspace.File, offset int, env Env) []CallItem {
	sym, ok := SymbolAt(file, offset)
	if !ok || sym.Kind != KindFunction {
		return nil
	}
	for _, fn := range file.Tree.Functions {
		if fn.NameSpan == sym.Span {
			return []CallItem{functionItem(file, fn)}
		}
	}
	return functionItems(append([]*workspace.File{file}, Chain(file, env)...), sym.Name)
}

func functionItem(file *workspace.File, fn parser.Function) CallItem {
	return CallItem{File: file, Name: fn.Name, Span: fn.Span, Selection: fn.NameSpan}
}

func fileItem(file *workspace.File) CallItem {
	whole := parser.Span{Start: 0, End: len(file.Tree.Text())}
	return CallItem{File: file, Name: filepath.Base(file.Path), Span: whole, Selection: parser.Span{}, TopLevel: true}
}

func IncomingCalls(ctx context.Context, item CallItem, env Env, files WorkspaceFiles) ([]Call, error) {
	group, err := Group(ctx, item.File, env, files)
	if err != nil {
		return nil, err
	}
	var calls []Call
	for _, f := range group {
		byCaller := map[string]*Call{}
		var order []string
		for _, cmd := range f.Tree.Commands {
			if cmd.Name != item.Name {
				continue
			}
			caller := fileItem(f)
			if fn, ok := f.Tree.EnclosingFunction(cmd.Within); ok {
				caller = functionItem(f, fn)
			}
			key := caller.Name
			if _, ok := byCaller[key]; !ok {
				byCaller[key] = &Call{Item: caller}
				order = append(order, key)
			}
			byCaller[key].Spans = append(byCaller[key].Spans, cmd.NameSpan)
		}
		for _, key := range order {
			calls = append(calls, *byCaller[key])
		}
	}
	return calls, nil
}

func OutgoingCalls(item CallItem, env Env) []Call {
	group := append([]*workspace.File{item.File}, Chain(item.File, env)...)
	spans := map[string][]parser.Span{}
	var order []string
	for _, cmd := range item.File.Tree.Commands {
		fn, inFunction := item.File.Tree.EnclosingFunction(cmd.Within)
		if item.TopLevel && inFunction {
			continue
		}
		if !item.TopLevel && (!inFunction || fn.Name != item.Name || !item.Span.Touches(cmd.NameSpan.Start)) {
			continue
		}
		if _, seen := spans[cmd.Name]; !seen {
			order = append(order, cmd.Name)
		}
		spans[cmd.Name] = append(spans[cmd.Name], cmd.NameSpan)
	}
	var calls []Call
	for _, name := range order {
		for _, callee := range functionItems(group, name) {
			calls = append(calls, Call{Item: callee, Spans: spans[name]})
		}
	}
	return calls
}

func functionItems(group []*workspace.File, name string) []CallItem {
	for _, f := range group {
		var items []CallItem
		for _, fn := range f.Tree.Functions {
			if fn.Name == name {
				items = append(items, functionItem(f, fn))
			}
		}
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

type MonikerKind uint8

const (
	MonikerExport MonikerKind = iota
	MonikerImport
	MonikerLocal
)

type Moniker struct {
	Identifier string
	Kind       MonikerKind
	Workspace  bool
}

func MonikerAt(file *workspace.File, offset int, env Env) (Moniker, bool) {
	for _, ref := range Sources(file, env) {
		if ref.Arg.Covers(offset) && ref.Target != nil {
			return Moniker{Identifier: relativeID(ref.Target.Path, env.Roots), Kind: MonikerImport, Workspace: true}, true
		}
	}
	sym, ok := SymbolAt(file, offset)
	if !ok {
		return Moniker{}, false
	}
	if sym.Binding != nil {
		return Moniker{Identifier: fmt.Sprintf("%s#%s/%d", relativeID(file.Path, env.Roots), sym.Name, *sym.Binding), Kind: MonikerLocal}, true
	}
	owner := file
	if sym.Kind == KindFunction {
		if _, defined := file.Tree.FunctionNamed(sym.Name); !defined {
			items := functionItems(Chain(file, env), sym.Name)
			if len(items) == 0 {
				return Moniker{}, false
			}
			owner = items[0].File
		}
		kind := MonikerExport
		if owner != file {
			kind = MonikerImport
		}
		return Moniker{Identifier: relativeID(owner.Path, env.Roots) + "#" + sym.Name, Kind: kind, Workspace: true}, true
	}
	return Moniker{Identifier: "$" + sym.Name, Kind: MonikerExport, Workspace: true}, true
}

func relativeID(path string, roots []string) string {
	for _, root := range roots {
		if rel, ok := relativeTo(root, path); ok {
			return rel
		}
	}
	return filepath.ToSlash(path)
}

type InlineValue struct {
	Span parser.Span
	Name string
}

func InlineValues(tree *parser.Tree, within parser.Span, stopped int) []InlineValue {
	fn, inFunction := tree.EnclosingFunction(tree.ScopeAt(stopped))
	seen := map[parser.Span]bool{}
	var out []InlineValue
	add := func(name string, span parser.Span, at parser.ScopeID) {
		if seen[span] || span.Start > stopped || !within.Touches(span.Start) {
			return
		}
		owner, ownerInFunction := tree.EnclosingFunction(at)
		if ownerInFunction != inFunction || (inFunction && owner.Span != fn.Span) {
			return
		}
		seen[span] = true
		out = append(out, InlineValue{Span: span, Name: name})
	}
	for _, def := range tree.Definitions {
		add(def.Name, def.NameSpan, def.Within)
	}
	for _, ref := range tree.References {
		add(ref.Name, ref.Span, ref.Within)
	}
	slices.SortFunc(out, func(a, b InlineValue) int { return cmp.Compare(a.Span.Start, b.Span.Start) })
	return out
}
