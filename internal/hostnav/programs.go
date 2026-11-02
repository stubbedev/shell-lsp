package hostnav

import (
	"fmt"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/embedded"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type programLink struct {
	flag    Occurrence
	occs    []Occurrence
	binding embedded.Binding
	hops    []Symbol
	refusal string
	program embedded.Language
	taken   func(name string) bool
}

func Programs(file *workspace.File) []embedded.Program {
	return workspace.Memo(file, "embedded.programs", 0, func() []embedded.Program { return embedded.Find(file.Tree) })
}

func (g *Group) programSymbol(file *workspace.File, offset int) (Symbol, bool) {
	for _, variant := range file.Variants() {
		programs := Programs(variant)
		for i := range programs {
			p := &programs[i]
			name, ok := p.VariableAt(offset)
			if !ok {
				b, at := p.BindingAt(offset)
				name, ok = b.Name, at
			}
			if !ok || p.Shadows(name) {
				continue
			}
			if b, bound := p.Binding(name); bound {
				return g.linked(variant, p, b, offset), true
			}
		}
	}
	return Symbol{}, false
}

func (g *Group) linked(file *workspace.File, p *embedded.Program, b embedded.Binding, offset int) Symbol {
	host := func(s parser.Span) parser.Span {
		return parser.Span{Start: file.Doc.SourceOffset(s.Start), End: file.Doc.SourceOffset(s.End)}
	}
	flag := Occurrence{File: file, Span: host(b.Span), Write: true, Declaration: true}
	occs := []Occurrence{flag}
	span := flag.Span
	reads := p.Reads(b.Name)
	for _, read := range reads {
		if read.Start-1 <= offset && offset <= read.End {
			span = host(read)
		}
		occs = append(occs, Occurrence{File: file, Span: host(read)})
	}
	link := &programLink{flag: flag, occs: occs, binding: b, program: p.Language, hops: g.bindingHops(file, b)}
	link.taken = func(name string) bool {
		_, bound := p.Binding(name)
		return bound || len(p.Reads(name)) > 0
	}
	link.refusal = programRefusal(file, p, b.Name, occs)
	return Symbol{Name: b.Name, Kind: model.KindVariable, Span: span, link: link}
}

func programRefusal(file *workspace.File, p *embedded.Program, name string, occs []Occurrence) string {
	if p.NamesArguments() {
		return fmt.Sprintf("this %s program also reads its arguments through `$ARGS`, whose keys are not followed", p.Language)
	}
	text := file.Doc.Source().Text()
	for _, o := range occs {
		if text[o.Span.Start:o.Span.End] != name {
			return fmt.Sprintf("`%s` is written across an antiquotation or an escape here", name)
		}
	}
	return ""
}

func (g *Group) bindingHops(file *workspace.File, b embedded.Binding) []Symbol {
	switch b.Kind {
	case embedded.ValueHole:
		if sym, ok := g.holeSymbol(file, file.Doc.SourceOffset(b.ReadSpan.Start)); ok {
			return []Symbol{sym}
		}
	case embedded.ValueVariable:
		if sym, ok := g.shellHop(file, b.ReadSpan.Start); ok {
			return []Symbol{sym}
		}
		return g.assignedHops(file, b.Read, b.ReadSpan.Start)
	}
	return nil
}

func (g *Group) shellHop(file *workspace.File, offset int) (Symbol, bool) {
	if g.builder != nil {
		return g.nixShellSymbol(file, offset)
	}
	return g.ShellSymbol(offset)
}

func (g *Group) holeSymbol(file *workspace.File, at int) (Symbol, bool) {
	holes := file.Native.Index.Holes
	i := slices.IndexFunc(holes, func(h model.Hole) bool { return h.Span.Start == at })
	if i < 0 {
		return Symbol{}, false
	}
	return g.readAt(file, holes[i].Head)
}

func (g *Group) assignedHops(file *workspace.File, name string, offset int) []Symbol {
	at := file.Doc.SourceOffset(offset)
	list := regions(file)
	i := slices.IndexFunc(list, func(r region) bool { return r.shell.Span.Start <= at && at <= r.shell.Span.End })
	if i < 0 {
		return nil
	}
	scope := list[i].shell.Scope
	var locals []Occurrence
	for _, r := range list {
		if r.shell.Scope != scope {
			continue
		}
		for _, span := range r.defs[name] {
			locals = append(locals, Occurrence{File: file, Span: span, Write: true, Declaration: true})
		}
	}
	if len(locals) == 0 {
		return nil
	}
	out := []Symbol{{Name: name, Kind: model.KindVariable, Span: locals[0].Span, locals: locals, scope: scope}}
	if sym, ok := g.assignedHole(file, name, locals); ok {
		out = append(out, sym)
	}
	return out
}

func (g *Group) assignedHole(file *workspace.File, name string, locals []Occurrence) (Symbol, bool) {
	for _, d := range file.Tree.Definitions {
		if d.Name != name || d.Value == nil || file.Doc.Synthetic(d.NameSpan.Start) {
			continue
		}
		at := file.Doc.SourceOffset(d.NameSpan.Start)
		if !slices.ContainsFunc(locals, func(o Occurrence) bool { return o.Span.Start == at }) {
			continue
		}
		if dollar, ok := soleHole(d.Value); ok {
			return g.holeSymbol(file, file.Doc.SourceOffset(dollar))
		}
	}
	return Symbol{}, false
}

func soleHole(w *syntax.Word) (int, bool) {
	parts := w.Parts
	if len(parts) == 1 {
		if q, quoted := parts[0].(*syntax.DblQuoted); quoted && !q.Dollar {
			parts = q.Parts
		}
	}
	if len(parts) != 1 || !embedded.DynamicValue(parts[0]) {
		return 0, false
	}
	p, isParam := parts[0].(*syntax.ParamExp)
	if !isParam {
		return 0, false
	}
	return parser.Offset(p.Dollar), true
}

func (g *Group) linkDefinitions(sym Symbol) []Location {
	out := []Location{{File: sym.link.flag.File, Span: sym.link.flag.Span}}
	for _, hop := range sym.link.hops {
		out = append(out, g.definitions(hop)...)
	}
	return out
}

func (g *Group) linkOccurrences(sym Symbol) []Occurrence {
	out := slices.Clone(sym.link.occs)
	for _, hop := range sym.link.hops {
		out = append(out, g.Occurrences(hop)...)
	}
	return merge(out)
}

func (g *Group) renameLink(sym Symbol, newName string) ([]Occurrence, error) {
	if !parser.IsName(newName) {
		return nil, fmt.Errorf("%w: %q is not a valid name in a %s program", navigation.ErrInvalidName, newName, sym.link.program)
	}
	if newName == sym.Name {
		return nil, nil
	}
	if sym.link.taken(newName) {
		return nil, fmt.Errorf("%w: this %s program already uses `%s`", navigation.ErrNameConflict, sym.link.program, newName)
	}
	return slices.Clone(sym.link.occs), nil
}

func (g *Group) describeLink(sym Symbol) Hover {
	b := sym.link.binding
	md := fmt.Sprintf("`%s` in this %s program is set by `%s %s`, as `%s`.", sym.Name, sym.link.program, b.Flag, b.Name, b.Text)
	for _, hop := range sym.link.hops {
		md += "\n\n---\n\n" + g.Describe(hop).Markdown
	}
	return Hover{Markdown: md, Span: sym.Span}
}
