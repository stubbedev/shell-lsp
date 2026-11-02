package hostnav

import (
	"cmp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/textedit"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Occurrence struct {
	File        *workspace.File
	Span        parser.Span
	Write       bool
	Declaration bool
	Shell       bool
	Rewrite     *model.Rewrite
	kind        model.Kind
}

func (o Occurrence) Edits(newName string) []textedit.Edit {
	if o.Rewrite == nil {
		return []textedit.Edit{{Start: o.Span.Start, End: o.Span.End, NewText: newName}}
	}
	r := o.Rewrite
	edits := []textedit.Edit{{Start: r.Span.Start, End: r.Span.End, NewText: r.Before + newName + r.After}}
	if r.Remove.End > r.Remove.Start {
		edits = append(edits, textedit.Edit{Start: r.Remove.Start, End: r.Remove.End})
	}
	return edits
}

type identity struct {
	module *model.Module
	ns     model.Namespace
	name   string
	scope  model.Scope
	file   int
}

func identityOf(t model.Target) identity {
	s := t.Info()
	if links := t.Module.Links(); links != nil {
		if shared, ok := links.Shared(t.File, s.Scope, s.Name); ok {
			return identity{module: t.Module, ns: model.NamespaceAttributes, name: s.Name, scope: shared.Scope, file: shared.File}
		}
	}
	id := identity{module: t.Module, ns: s.Kind.Namespace(), name: s.Name, scope: s.Scope, file: -1}
	if s.Scope != model.Global {
		id.file = t.File
	}
	return id
}

type Symbol struct {
	Name    string
	Kind    model.Kind
	Span    parser.Span
	id      identity
	targets []model.Target
	Builtin *model.Builtin
	locals  []Occurrence
	scope   model.Scope
	doc     string
	shell   bool
	foreign bool
	link    *programLink
}

func (s Symbol) Defined() bool { return len(s.targets) > 0 || len(s.locals) > 0 || s.link != nil }

func (s Symbol) Exported() bool {
	return slices.ContainsFunc(s.targets, func(t model.Target) bool {
		return t.Info().Exported || t.Module.Files[t.File].ExportAll && t.Info().Kind.Namespace() == model.NamespaceValues
	})
}

func (g *Group) symbolFor(hit model.Hit, file int, module *model.Module) (Symbol, bool) {
	sym := Symbol{Name: hit.Name, Kind: hit.Kind, Span: hit.Span}
	if hit.Declaration {
		sym.targets = []model.Target{{Module: module, File: file, Symbol: hit.Symbol}}
	} else {
		res := module.Resolve(file, model.Ref{Name: hit.Name, Kind: hit.Kind, Scope: hit.Scope, Qualifier: hit.Qualifier})
		sym.targets, sym.Builtin = res.Targets, res.Builtin
	}
	if len(sym.targets) > 0 {
		sym.id = identityOf(sym.targets[0])
		sym.Kind = sym.targets[0].Info().Kind
		return sym, true
	}
	return sym, sym.Builtin != nil
}

func (g *Group) At(offset int) (Symbol, bool) {
	hit, ok := g.Index().At(offset)
	if !ok {
		return g.wrappedSymbol(offset)
	}
	sym, found := g.symbolFor(hit, g.selfAt(), g.Module)
	if found || hit.Declaration {
		return sym, found
	}
	own, declared := g.Index().DeclarationAt(offset)
	if !declared {
		return Symbol{}, false
	}
	return g.symbolFor(own, g.selfAt(), g.Module)
}

func (g *Group) fileOf(module *model.Module, i int) *workspace.File {
	return g.files[module.Files[i]]
}

func (g *Group) Declarations(sym Symbol) []Occurrence {
	var out []Occurrence
	module := sym.id.module
	if module == nil {
		return nil
	}
	for _, at := range module.SymbolsNamed(sym.id.name) {
		f, i := int(at.File), int(at.Item)
		if identityOf(model.Target{Module: module, File: f, Symbol: i}) == sym.id {
			s := module.Files[f].Symbols[i]
			out = append(out, Occurrence{File: g.fileOf(module, f), Span: s.Selection, Write: true, Declaration: true, Rewrite: s.Rewrite, kind: s.Kind})
		}
	}
	return out
}

func (g *Group) modules() []*model.Module {
	out := []*model.Module{g.Module}
	for _, p := range g.parents {
		out = append(out, p.module)
	}
	seen := map[*model.Module]bool{}
	for i := 0; i < len(out); i++ {
		seen[out[i]] = true
		for _, sub := range out[i].Submodules {
			if sub != nil && !seen[sub] {
				seen[sub] = true
				out = append(out, sub)
			}
		}
	}
	return out
}

func (g *Group) Occurrences(sym Symbol) []Occurrence {
	if sym.link != nil {
		return g.linkOccurrences(sym)
	}
	if sym.shell && !sym.Defined() {
		return g.undefinedReads(sym)
	}
	if !sym.Defined() {
		return nil
	}
	if len(sym.locals) > 0 {
		if sym.foreign {
			return slices.Clone(sym.locals)
		}
		return g.localOccurrences(sym)
	}
	out := g.Declarations(sym)
	for _, module := range g.modules() {
		for _, at := range module.RefsNamed(sym.Name) {
			f := int(at.File)
			r := module.Files[f].Refs[at.Item]
			if module.Links() == nil && r.Kind.Namespace() != sym.id.ns {
				continue
			}
			if slices.ContainsFunc(module.Resolve(f, r).Targets, func(t model.Target) bool { return identityOf(t) == sym.id }) {
				out = append(out, Occurrence{File: g.fileOf(module, f), Span: r.Span, Rewrite: r.Rewrite})
			}
		}
	}
	out = append(out, g.shellOccurrences(sym)...)
	return merge(out)
}

func merge(occs []Occurrence) []Occurrence {
	slices.SortStableFunc(occs, func(a, b Occurrence) int {
		return cmp.Or(cmp.Compare(a.File.Path, b.File.Path), cmp.Compare(a.Span.Start, b.Span.Start))
	})
	var out []Occurrence
	for _, o := range occs {
		if n := len(out); n > 0 && out[n-1].File.Path == o.File.Path && out[n-1].Span == o.Span {
			out[n-1].Write = out[n-1].Write || o.Write
			out[n-1].Declaration = out[n-1].Declaration || o.Declaration
			out[n-1].Rewrite = cmp.Or(out[n-1].Rewrite, o.Rewrite)
			continue
		}
		out = append(out, o)
	}
	return out
}
