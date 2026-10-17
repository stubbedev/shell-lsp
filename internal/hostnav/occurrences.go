package hostnav

import (
	"cmp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type Occurrence struct {
	File        *workspace.File
	Span        parser.Span
	Write       bool
	Declaration bool
	Shell       bool
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
}

func (s Symbol) Defined() bool { return len(s.targets) > 0 }

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
		return Symbol{}, false
	}
	return g.symbolFor(hit, g.self, g.Module)
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
	for f, ix := range module.Files {
		for i, s := range ix.Symbols {
			if identityOf(model.Target{Module: module, File: f, Symbol: i}) == sym.id {
				out = append(out, Occurrence{File: g.fileOf(module, f), Span: s.Selection, Write: true, Declaration: true})
			}
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
	if !sym.Defined() {
		return nil
	}
	out := g.Declarations(sym)
	for _, module := range g.modules() {
		for f, ix := range module.Files {
			for _, r := range ix.Refs {
				if r.Kind.Namespace() != sym.id.ns || r.Name != sym.Name {
					continue
				}
				if slices.ContainsFunc(module.Resolve(f, r).Targets, func(t model.Target) bool { return identityOf(t) == sym.id }) {
					out = append(out, Occurrence{File: g.fileOf(module, f), Span: r.Span})
				}
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
			continue
		}
		out = append(out, o)
	}
	return out
}
