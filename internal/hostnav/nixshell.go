package hostnav

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/host"
	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const (
	flakeSelfPrefix = ".#"
	nixCommand      = "nix"
	nixShellCommand = "nix-shell"
	packagesFlag    = "-p"
	maxOriginHops   = 8
)

type shellWord struct {
	span parser.Span
	text string
}

type shellCommand struct {
	span  parser.Span
	name  string
	hole  *model.Hole
	words []shellWord
}

func nixRegions(f *workspace.File, out []region) {
	shells := f.Native.Index.Shells
	holes := f.Native.Index.Holes
	text := f.Doc.Source().Text()
	pieceAt := func(at int) int {
		return slices.IndexFunc(shells, func(s model.Shell) bool { return s.Span.Start <= at && at <= s.Span.End })
	}
	host := func(span parser.Span) parser.Span {
		return parser.Span{Start: f.Doc.SourceOffset(span.Start), End: f.Doc.SourceOffset(span.End)}
	}
	define := func(name string, span parser.Span) {
		if f.Doc.Synthetic(span.Start) {
			return
		}
		at := host(span)
		if i := pieceAt(at.Start); i >= 0 {
			out[i].defs[name] = append(out[i].defs[name], at)
		}
	}
	for _, d := range f.Tree.Definitions {
		if !d.Local {
			define(d.Name, d.NameSpan)
		}
	}
	for _, fn := range f.Tree.Functions {
		define(fn.Name, fn.NameSpan)
	}
	syntax.Walk(f.Tree.File, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		span, ok := f.Tree.NodeSpan(call.Args[0])
		if !ok || f.Doc.Synthetic(span.Start) {
			return true
		}
		at := host(span)
		i := pieceAt(at.End)
		if i < 0 {
			i = pieceAt(at.Start)
		}
		if i < 0 {
			return true
		}
		cmd := shellCommand{span: at}
		if hole, found := holeAt(holes, at); found {
			cmd.hole = &hole
			suffix := strings.Trim(text[min(hole.Span.End, at.End):at.End], `"'`)
			cmd.name = programName(hole.Exe, suffix)
		} else if lit, literal := parser.Literal(call.Args[0]); literal {
			cmd.name = lit
		}
		for _, arg := range call.Args[1:] {
			a, spanned := f.Tree.NodeSpan(arg)
			if !spanned || f.Doc.Synthetic(a.Start) {
				continue
			}
			word := shellWord{span: host(a)}
			if lit, literal := parser.Literal(arg); literal {
				word.text = lit
			}
			cmd.words = append(cmd.words, word)
		}
		out[i].commands = append(out[i].commands, cmd)
		return true
	})
}

func programName(exe, suffix string) string {
	switch {
	case exe != "":
		return exe
	case suffix == "":
		return ""
	}
	return path.Base(suffix)
}

func holeAt(holes []model.Hole, word parser.Span) (model.Hole, bool) {
	i := slices.IndexFunc(holes, func(h model.Hole) bool { return h.Span.Start == word.Start || h.Span.Start == word.Start+1 })
	if i < 0 {
		return model.Hole{}, false
	}
	return holes[i], true
}

func (g *Group) allRegions(module *model.Module, f int) []region {
	file := g.fileOf(module, f)
	if file == nil {
		return nil
	}
	out := regions(file)
	wrapped := g.wrappedOf(module, f)
	if wrapped == nil {
		return out
	}
	for _, variant := range wrapped.Variants() {
		out = slices.Concat(out, regions(variant))
	}
	return out
}

func (g *Group) wrappedOf(module *model.Module, f int) *workspace.File {
	project, linked := module.Links().(*nix.Project)
	if !linked {
		return nil
	}
	shells := project.Shells(f)
	if len(shells) == 0 {
		return nil
	}
	proven := project.Proven(f)
	file := g.fileOf(module, f)
	var key strings.Builder
	key.WriteString("hostnav.wrapped")
	for _, s := range shells {
		fmt.Fprintf(&key, "/%d-%d:%d", s.Span.Start, s.Span.End, s.Scope)
	}
	for _, s := range proven {
		fmt.Fprintf(&key, "/%d=%s", s.Start, s.Writer)
	}
	return workspace.Memo(file, key.String(), 0, func() *workspace.File { return Wrapped(file, proven, shells) })
}

func Wrapped(file *workspace.File, proven []nix.ProvenString, shells []model.Shell) *workspace.File {
	script, ok := host.ProvenScript(file.Doc.Source(), proven, shells)
	if !ok {
		return nil
	}
	variant := func(tree *parser.Tree, detection dialect.Detection) *workspace.File {
		return &workspace.File{Path: file.Path, URI: file.URI, Doc: script.Doc, Detection: detection, Tree: tree, Native: script.Native}
	}
	out := variant(script.Parse(nil), script.Detection)
	for _, v := range script.Variants {
		out.Others = append(out.Others, variant(script.ParseVariant(v, nil), v.Detection))
	}
	return out
}

func (g *Group) wrappedSymbol(offset int) (Symbol, bool) {
	if g.builder == nil {
		return Symbol{}, false
	}
	wrapped := g.wrappedOf(g.Module, g.self)
	if wrapped == nil {
		return Symbol{}, false
	}
	at, inside := wrapped.Doc.Positions(0).Offset(wrapped.Doc.Source().OffsetToPosition(offset, 0))
	if !inside {
		return Symbol{}, false
	}
	for _, variant := range wrapped.Variants() {
		if sym, ok := g.nixShellSymbol(variant, at); ok {
			return sym, true
		}
	}
	return Symbol{}, false
}

func (g *Group) nixShellSymbol(file *workspace.File, offset int) (Symbol, bool) {
	at := file.Doc.SourceOffset(offset)
	if !file.Doc.Synthetic(offset) {
		if hit, ok := g.Index().At(at); ok {
			if sym, found := g.symbolFor(hit, g.self, g.Module); found {
				return sym, true
			}
		}
	}
	list := regions(file)
	i := slices.IndexFunc(list, func(r region) bool { return r.shell.Span.Start <= at && at <= r.shell.Span.End })
	if i < 0 {
		return Symbol{}, false
	}
	r := list[i]
	for _, cmd := range r.commands {
		if sym, ok := g.commandSymbol(file, r, cmd, at); ok {
			return sym, true
		}
	}
	hit, ok := file.Tree.SymbolAt(offset)
	if !ok || file.Doc.Synthetic(hit.Span.Start) {
		return Symbol{}, false
	}
	span := parser.Span{Start: file.Doc.SourceOffset(hit.Span.Start), End: file.Doc.SourceOffset(hit.Span.End)}
	kind := model.KindVariable
	if hit.Kind != parser.HitVariable {
		kind = model.KindFunction
	}
	reach := phaseReach(file, list, r, hit.Name)
	if reach.crossing {
		return Symbol{Name: hit.Name, Kind: kind, Span: span, locals: reach.visible, scope: r.shell.Scope}, true
	}
	if hit.Kind == parser.HitVariable && !r.shadowed[hit.Name] {
		res := g.Module.Resolve(g.self, model.Ref{Name: hit.Name, Kind: model.KindVariable, Scope: r.shell.Scope})
		if len(res.Targets) > 0 {
			return Symbol{Name: hit.Name, Kind: res.Targets[0].Info().Kind, Span: span, id: identityOf(res.Targets[0]), targets: res.Targets, shell: true}, true
		}
	}
	if reach.later != "" {
		return Symbol{Name: hit.Name, Kind: kind, Span: span, doc: reach.later, shell: true, scope: r.shell.Scope}, true
	}
	if hit.Kind == parser.HitVariable && r.shell.Phase > 0 && !r.shadowed[hit.Name] {
		if b, known := nix.BuilderVariable(hit.Name); known {
			return Symbol{Name: hit.Name, Kind: kind, Span: span, Builtin: &b, shell: true, scope: r.shell.Scope}, true
		}
	}
	return Symbol{}, false
}

type reach struct {
	visible  []Occurrence
	crossing bool
	later    string
}

func phaseReach(file *workspace.File, list []region, r region, name string) reach {
	var out reach
	if r.shell.Phase == 0 {
		return out
	}
	for _, other := range list {
		defs := other.defs[name]
		switch {
		case len(defs) == 0:
		case other.shell.Scope != r.shell.Scope || other.shell.Phase == 0:
			if !r.definesInPhase(list, name) {
				out.later = fmt.Sprintf("`%s` is set only by other scripts in this file, and their shell does not reach this one.", name)
			}
		case other.shell.Phase > r.shell.Phase:
			out.later = fmt.Sprintf("`%s` is set in `%s`, which runs after this phase.", name, nix.PhaseName(other.shell.Phase))
		default:
			out.crossing = out.crossing || other.shell.Phase != r.shell.Phase
			for _, span := range defs {
				out.visible = append(out.visible, Occurrence{File: file, Span: span, Write: true, Declaration: true})
			}
		}
	}
	if len(out.visible) > 0 {
		out.later = ""
	}
	return out
}

func (r region) definesInPhase(list []region, name string) bool {
	return slices.ContainsFunc(list, func(other region) bool {
		return other.shell.Scope == r.shell.Scope && other.shell.Phase <= r.shell.Phase && len(other.defs[name]) > 0
	})
}

func (g *Group) undefinedReads(sym Symbol) []Occurrence {
	var out []Occurrence
	for _, other := range regions(g.Self) {
		if other.shell.Scope != sym.scope {
			continue
		}
		for _, n := range other.names {
			if n.name == sym.Name {
				out = append(out, Occurrence{File: g.Self, Span: n.span, Shell: true})
			}
		}
	}
	return merge(out)
}

func (g *Group) localOccurrences(sym Symbol) []Occurrence {
	out := slices.Clone(sym.locals)
	file := sym.locals[0].File
	first := -1
	for _, other := range regions(file) {
		if other.shell.Scope == sym.scope && len(other.defs[sym.Name]) > 0 && (first < 0 || other.shell.Phase < first) {
			first = other.shell.Phase
		}
	}
	for _, other := range regions(file) {
		if other.shell.Scope != sym.scope || other.shell.Phase < first {
			continue
		}
		for _, n := range other.names {
			if n.name == sym.Name {
				out = append(out, Occurrence{File: file, Span: n.span, Shell: true})
			}
		}
		for _, cmd := range other.commands {
			if cmd.hole == nil && cmd.name == sym.Name {
				out = append(out, Occurrence{File: file, Span: cmd.span, Shell: true})
			}
		}
	}
	return merge(out)
}

func (g *Group) commandSymbol(file *workspace.File, r region, cmd shellCommand, at int) (Symbol, bool) {
	if cmd.span.Touches(at) {
		if sym, ok := g.commandTarget(file, r, cmd); ok {
			return sym, true
		}
	}
	for i, word := range cmd.words {
		if word.span.Touches(at) {
			return g.installable(cmd, i)
		}
	}
	return Symbol{}, false
}

func (g *Group) commandTarget(file *workspace.File, r region, cmd shellCommand) (Symbol, bool) {
	if cmd.hole != nil {
		sym, ok := g.readAt(file, cmd.hole.Head)
		label := file.Doc.Source().Text()[cmd.hole.Head.Start:cmd.hole.Head.End]
		if !ok {
			sym = Symbol{Name: label, Kind: model.KindFunction}
		}
		sym.Span = cmd.span
		if cmd.name != "" {
			sym.doc = fmt.Sprintf("Runs the command `%s` of `%s`.", cmd.name, label)
		}
		return sym, ok || cmd.name != ""
	}
	if cmd.name == "" {
		return Symbol{}, false
	}
	for _, input := range r.shell.Inputs {
		sym, known := g.readAt(file, input.Head)
		if input.Name != cmd.name && !g.scriptNamed(sym, known, cmd.name) {
			continue
		}
		if !known {
			sym = Symbol{Name: cmd.name, Kind: model.KindFunction, locals: []Occurrence{{File: file, Span: input.Span, Declaration: true}}, foreign: true}
		}
		sym.Span = cmd.span
		sym.doc = fmt.Sprintf("Found on `PATH` through `%s`.", input.List)
		return sym, true
	}
	return Symbol{}, false
}

func (g *Group) scriptNamed(sym Symbol, known bool, name string) bool {
	if !known {
		return false
	}
	return slices.ContainsFunc(sym.targets, func(t model.Target) bool {
		links := t.Module.Links()
		if links == nil {
			return false
		}
		script, ok := links.Script(t.File, t.Symbol)
		return ok && script.Name == name
	})
}

func (g *Group) readAt(file *workspace.File, head parser.Span) (Symbol, bool) {
	for f, ix := range g.Module.Files {
		other := g.fileOf(g.Module, f)
		if other == nil || other.Path != file.Path {
			continue
		}
		if hit, ok := ix.At(head.Start); ok {
			return g.symbolFor(hit, f, g.Module)
		}
	}
	return Symbol{}, false
}

func (g *Group) installable(cmd shellCommand, i int) (Symbol, bool) {
	links := g.Module.Links()
	word := cmd.words[i]
	if links == nil || cmd.hole != nil {
		return Symbol{}, false
	}
	var name string
	var homes []model.Home
	switch {
	case cmd.name == nixCommand && strings.HasPrefix(word.text, flakeSelfPrefix):
		parts := strings.Split(strings.TrimPrefix(word.text, flakeSelfPrefix), ".")
		name = parts[len(parts)-1]
		homes = links.Installable(parts)
	case cmd.name == nixShellCommand && slices.ContainsFunc(cmd.words[:i], func(w shellWord) bool { return w.text == packagesFlag }) && !strings.HasPrefix(word.text, "-"):
		name = word.text
		homes = links.Package(name)
	default:
		return Symbol{}, false
	}
	targets := g.Module.AtHomes(homes, name)
	if len(targets) == 0 {
		return Symbol{}, false
	}
	return Symbol{Name: name, Kind: targets[0].Info().Kind, Span: word.span, id: identityOf(targets[0]), targets: targets}, true
}

func (g *Group) origin(sym Symbol) Symbol {
	targets := sym.targets
	for range maxOriginHops {
		next, moved := g.hop(targets)
		if !moved {
			break
		}
		targets = next
	}
	if len(targets) == 0 {
		return sym
	}
	sym.targets, sym.id = targets, identityOf(targets[0])
	return sym
}

func (g *Group) hop(targets []model.Target) ([]model.Target, bool) {
	var out []model.Target
	moved := false
	for _, t := range targets {
		s := t.Info()
		ix := t.Module.Files[t.File]
		read := slices.IndexFunc(ix.Refs, func(r model.Ref) bool { return s.Passive && r.Span == s.Selection && r.Name == s.Name })
		if read < 0 {
			out = append(out, t)
			continue
		}
		resolved := t.Module.Resolve(t.File, ix.Refs[read]).Targets
		if len(resolved) == 0 {
			out = append(out, t)
			continue
		}
		out, moved = append(out, resolved...), true
	}
	return out, moved
}

func (g *Group) nixCommandTargets(module *model.Module, f int, cmd shellCommand) []model.Target {
	if cmd.hole == nil {
		return nil
	}
	hit, ok := module.Files[f].At(cmd.hole.Head.Start)
	if !ok || hit.Declaration {
		return nil
	}
	var out []model.Target
	for _, t := range module.Resolve(f, model.Ref{Name: hit.Name, Kind: hit.Kind, Scope: hit.Scope, Qualifier: hit.Qualifier}).Targets {
		if g.isScript(t) {
			out = append(out, t)
		}
	}
	return out
}

func (g *Group) isScript(t model.Target) bool {
	links := t.Module.Links()
	if links == nil {
		return false
	}
	_, ok := links.Script(t.File, t.Symbol)
	return ok
}

func (g *Group) nixShellOccurrences(sym Symbol) []Occurrence {
	module := sym.id.module
	var out []Occurrence
	for f := range module.Files {
		file := g.fileOf(module, f)
		for _, r := range g.allRegions(module, f) {
			if r.shadowed[sym.Name] {
				continue
			}
			wanted := slices.ContainsFunc(r.names, func(n shellName) bool { return n.name == sym.Name })
			if !wanted || !g.resolvesTo(module, f, sym, r.shell.Scope) {
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
