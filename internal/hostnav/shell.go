package hostnav

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type shellName struct {
	name string
	span parser.Span
}

type region struct {
	shell    model.Shell
	names    []shellName
	calls    []model.Ref
	shadowed map[string]bool
}

func regions(f *workspace.File) []region {
	return workspace.Memo(f, "hostnav.shell", 0, func() []region { return shellRegions(f) })
}

func shellRegions(f *workspace.File) []region {
	shells := f.Native.Index.Shells
	out := make([]region, len(shells))
	for i, s := range shells {
		out[i] = region{shell: s, shadowed: map[string]bool{}}
	}
	locate := func(span parser.Span) (int, parser.Span, bool) {
		if f.Doc.Synthetic(span.Start) {
			return 0, parser.Span{}, false
		}
		host := parser.Span{Start: f.Doc.SourceOffset(span.Start), End: f.Doc.SourceOffset(span.End)}
		i := slices.IndexFunc(shells, func(s model.Shell) bool { return s.Span.Start <= host.Start && host.End <= s.Span.End })
		return i, host, i >= 0
	}
	for _, d := range f.Tree.Definitions {
		if i, _, ok := locate(d.NameSpan); ok {
			out[i].shadowed[d.Name] = true
		}
	}
	for _, r := range f.Tree.References {
		if i, host, ok := locate(r.Span); ok {
			out[i].names = append(out[i].names, shellName{name: r.Name, span: host})
		}
	}
	recursion := f.Native.Language.Recursion()
	syntax.Walk(f.Tree.File, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		span, ok := f.Tree.NodeSpan(call.Args[0])
		if !ok {
			return true
		}
		host := parser.Span{Start: f.Doc.SourceOffset(span.Start), End: f.Doc.SourceOffset(span.End)}
		i := slices.IndexFunc(shells, func(s model.Shell) bool { return s.Span.Start <= host.Start && host.End <= s.Span.End })
		if i >= 0 && invokes(f, call.Args[0], span, host, recursion) {
			out[i].calls = append(out[i].calls, calls(f, call.Args[1:], recursion, out[i].shell.Scope)...)
		}
		return true
	})
	return out
}

func invokes(f *workspace.File, name *syntax.Word, span, host parser.Span, r host.Recursion) bool {
	if lit, ok := parser.Literal(name); ok && lit == r.Command && !f.Doc.Synthetic(span.Start) {
		return true
	}
	return slices.ContainsFunc(f.Native.Index.Refs, func(ref model.Ref) bool {
		return ref.Kind == r.Ref.Kind && ref.Name == r.Ref.Name && host.Start <= ref.Span.Start && ref.Span.End <= host.End
	})
}

func calls(f *workspace.File, args []*syntax.Word, r host.Recursion, scope model.Scope) []model.Ref {
	var out []model.Ref
	for _, arg := range args {
		lit, ok := parser.Literal(arg)
		if !ok {
			continue
		}
		if slices.ContainsFunc(r.FileFlags, func(flag string) bool { return lit == flag || strings.HasPrefix(lit, flag+"=") }) {
			return nil
		}
		span, ok := f.Tree.NodeSpan(arg)
		if !ok || strings.HasPrefix(lit, "-") || strings.Contains(lit, "=") || f.Doc.Synthetic(span.Start) {
			continue
		}
		ref := model.Ref{Name: lit, Kind: r.Kind, Scope: scope, Args: model.NoArgs, Owner: model.NoSymbol, Soft: true}
		ref.Span = parser.Span{Start: f.Doc.SourceOffset(span.Start), End: f.Doc.SourceOffset(span.End)}
		if r.Separator != "" && strings.Contains(lit, r.Separator) {
			parts := strings.Split(lit, r.Separator)
			ref.Name, ref.Qualifier = parts[len(parts)-1], parts[:len(parts)-1]
			ref.Span.Start = ref.Span.End - len(ref.Name)
		}
		out = append(out, ref)
	}
	return out
}

func (g *Group) shellOccurrences(sym Symbol) []Occurrence {
	if sym.id.ns == g.Language.Recursion().Kind.Namespace() {
		return g.callOccurrences(sym)
	}
	if sym.id.ns != model.NamespaceValues || !sym.Exported() {
		return nil
	}
	module := sym.id.module
	var out []Occurrence
	for f := range module.Files {
		if sym.id.file >= 0 && f != sym.id.file {
			continue
		}
		file := g.fileOf(module, f)
		for _, r := range regions(file) {
			if r.shadowed[sym.Name] || !g.resolvesTo(module, f, sym, r.shell.Scope) {
				continue
			}
			for _, n := range r.names {
				if n.name == sym.Name {
					out = append(out, Occurrence{File: file, Span: n.span, Shell: true})
				}
			}
		}
	}
	return out
}

func (g *Group) resolvesTo(module *model.Module, file int, sym Symbol, scope model.Scope) bool {
	targets := module.Resolve(file, model.Ref{Name: sym.Name, Kind: model.KindVariable, Scope: scope}).Targets
	return slices.ContainsFunc(targets, func(t model.Target) bool { return identityOf(t) == sym.id })
}

func (g *Group) ShellSymbol(offset int) (Symbol, bool) {
	at := g.Self.Doc.SourceOffset(offset)
	for _, r := range regions(g.Self) {
		for _, c := range r.calls {
			if c.Span.Touches(at) {
				return g.symbolFor(model.Hit{Name: c.Name, Kind: c.Kind, Span: c.Span, Scope: c.Scope, Qualifier: c.Qualifier, Symbol: model.NoSymbol}, g.self, g.Module)
			}
		}
	}
	hit, ok := g.Self.Tree.SymbolAt(offset)
	if !ok || hit.Kind != parser.HitVariable || g.Self.Doc.Synthetic(hit.Span.Start) {
		return Symbol{}, false
	}
	host := g.Self.Doc.SourceOffset(hit.Span.Start)
	for _, r := range regions(g.Self) {
		if host < r.shell.Span.Start || host > r.shell.Span.End || r.shadowed[hit.Name] {
			continue
		}
		res := g.Module.Resolve(g.self, model.Ref{Name: hit.Name, Kind: model.KindVariable, Scope: r.shell.Scope})
		if len(res.Targets) == 0 {
			return Symbol{}, false
		}
		sym := Symbol{Name: hit.Name, Kind: res.Targets[0].Info().Kind, Span: parser.Span{Start: host, End: g.Self.Doc.SourceOffset(hit.Span.End)}, id: identityOf(res.Targets[0]), targets: res.Targets}
		if !sym.Exported() {
			return Symbol{}, false
		}
		return sym, true
	}
	return Symbol{}, false
}

func (g *Group) shellUsage() model.Usage {
	used := model.Usage{}
	for f := range g.Module.Files {
		file := g.fileOf(g.Module, f)
		for _, r := range regions(file) {
			for _, n := range r.names {
				if r.shadowed[n.name] {
					continue
				}
				for _, t := range g.Module.Resolve(f, model.Ref{Name: n.name, Kind: model.KindVariable, Scope: r.shell.Scope}).Targets {
					if s := t.Info(); s.Exported || t.Module.Files[t.File].ExportAll {
						used[t] = true
					}
				}
			}
		}
	}
	return used
}

func (g *Group) callOccurrences(sym Symbol) []Occurrence {
	var out []Occurrence
	for _, module := range g.modules() {
		for f := range module.Files {
			file := g.fileOf(module, f)
			for _, r := range regions(file) {
				for _, c := range r.calls {
					if c.Name == sym.Name && slices.ContainsFunc(module.Resolve(f, c).Targets, func(t model.Target) bool { return identityOf(t) == sym.id }) {
						out = append(out, Occurrence{File: file, Span: c.Span, Shell: true})
					}
				}
			}
		}
	}
	return out
}
