package hostnav

import (
	"cmp"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

type CallSite struct {
	Peer  Extent
	Name  string
	Kind  model.Kind
	File  *workspace.File
	Spans []parser.Span
}

func (g *Group) callable(kind model.Kind) bool {
	return kind.Namespace() == g.Language.Recursion().Kind.Namespace()
}

func (g *Group) CallTarget(offset int) (Symbol, bool) {
	sym, ok := g.At(offset)
	if !ok || !sym.Defined() || !g.callable(sym.Kind) {
		return Symbol{}, false
	}
	return sym, true
}

func (g *Group) SymbolAt(path string, offset int) (Symbol, bool) {
	for _, module := range g.modules() {
		for f, ix := range module.Files {
			file := g.fileOf(module, f)
			if file == nil || file.Path != path {
				continue
			}
			for i, s := range ix.Symbols {
				if s.Selection.Start == offset && g.callable(s.Kind) {
					t := model.Target{Module: module, File: f, Symbol: i}
					return Symbol{Name: s.Name, Kind: s.Kind, Span: s.Selection, id: identityOf(t), targets: []model.Target{t}}, true
				}
			}
		}
	}
	return Symbol{}, false
}

type Extent struct {
	File      *workspace.File
	Span      parser.Span
	Selection parser.Span
}

func (g *Group) Extent(sym Symbol) Extent {
	if !sym.Defined() {
		panic("hostnav: " + sym.Name + " has no declaration")
	}
	t := sym.targets[0]
	return Extent{File: g.fileOf(t.Module, t.File), Span: t.Info().Span, Selection: t.Info().Selection}
}

type call struct {
	caller model.Target
	callee model.Target
	file   *workspace.File
	span   parser.Span
}

func (g *Group) calls() []call {
	var out []call
	for _, module := range g.modules() {
		for f, ix := range module.Files {
			file := g.fileOf(module, f)
			for _, r := range ix.Refs {
				if r.Owner < 0 || !g.callable(r.Kind) || !g.callable(ix.Symbols[r.Owner].Kind) {
					continue
				}
				for _, t := range module.Resolve(f, r).Targets {
					out = append(out, call{caller: model.Target{Module: module, File: f, Symbol: r.Owner}, callee: t, file: file, span: r.Span})
				}
			}
			for _, region := range regions(file) {
				owner := slices.IndexFunc(ix.Symbols, func(s model.Symbol) bool {
					return g.callable(s.Kind) && s.Span.Start <= region.shell.Span.Start && region.shell.Span.End <= s.Span.End
				})
				if owner < 0 {
					continue
				}
				for _, c := range region.calls {
					for _, t := range module.Resolve(f, c).Targets {
						out = append(out, call{caller: model.Target{Module: module, File: f, Symbol: owner}, callee: t, file: file, span: c.Span})
					}
				}
			}
		}
	}
	return out
}

func (g *Group) symbolOf(t model.Target) Symbol {
	s := t.Info()
	return Symbol{Name: s.Name, Kind: s.Kind, Span: s.Selection, id: identityOf(t), targets: []model.Target{t}}
}

func (g *Group) Incoming(sym Symbol) []CallSite {
	return g.callSites(func(c call) (model.Target, bool) { return c.caller, identityOf(c.callee) == sym.id })
}

func (g *Group) Outgoing(sym Symbol) []CallSite {
	return g.callSites(func(c call) (model.Target, bool) { return c.callee, identityOf(c.caller) == sym.id })
}

func (g *Group) callSites(pick func(call) (model.Target, bool)) []CallSite {
	byPeer := map[identity]*CallSite{}
	var order []identity
	for _, c := range g.calls() {
		peer, ok := pick(c)
		if !ok {
			continue
		}
		id := identityOf(peer)
		site, seen := byPeer[id]
		if !seen {
			s := g.symbolOf(peer)
			site = &CallSite{Peer: g.Extent(s), Name: s.Name, Kind: s.Kind, File: c.file}
			byPeer[id] = site
			order = append(order, id)
		}
		site.Spans = append(site.Spans, c.span)
	}
	out := make([]CallSite, 0, len(order))
	for _, id := range order {
		out = append(out, *byPeer[id])
	}
	slices.SortFunc(out, func(a, b CallSite) int {
		return cmp.Or(cmp.Compare(a.Peer.File.Path, b.Peer.File.Path), cmp.Compare(a.Peer.Selection.Start, b.Peer.Selection.Start))
	})
	return out
}
