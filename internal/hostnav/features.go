package hostnav

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/navigation"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/internal/workspace"
)

func (g *Group) Findings() []lint.Finding {
	checks := g.Language.Checks()
	ix := g.Index()
	if !checks.Active() {
		return slices.Clone(ix.Findings)
	}
	used := g.Module.Usage()
	for t := range g.shellUsage() {
		used[t] = true
	}
	out := slices.Clone(ix.Findings)
	out = append(out, g.Module.Findings(g.selfAt(), checks, used)...)
	for _, inc := range g.missing {
		out = append(out, model.MissingInclude(checks, inc))
	}
	return out
}

type Location struct {
	File *workspace.File
	Span parser.Span
}

func (g *Group) includeAt(offset int) (resolvedInclude, bool) {
	if g.builder != nil {
		return g.projectIncludeAt(offset)
	}
	for _, r := range g.includes {
		if r.include.Span.Touches(offset) {
			return r, true
		}
	}
	return resolvedInclude{}, false
}

func (g *Group) projectIncludeAt(offset int) (resolvedInclude, bool) {
	for _, inc := range g.Index().Includes {
		if !inc.Span.Touches(offset) {
			continue
		}
		r := g.builder.resolveInclude(g.Self, inc)
		return r, r.target != nil || !inc.Probe
	}
	return resolvedInclude{}, false
}

func (g *Group) Definition(offset int) []Location {
	if r, ok := g.includeAt(offset); ok {
		if r.target == nil {
			return nil
		}
		return []Location{{File: r.target}}
	}
	sym, ok := g.At(offset)
	if !ok {
		return nil
	}
	return g.definitions(sym)
}

func (g *Group) definitions(sym Symbol) []Location {
	if sym.link != nil {
		return g.linkDefinitions(sym)
	}
	if sym.lexical != nil {
		return sym.lexical.origins
	}
	if len(sym.locals) > 0 {
		out := make([]Location, len(sym.locals))
		for i, o := range sym.locals {
			out[i] = Location{File: o.File, Span: o.Span}
		}
		return out
	}
	if sym.shell {
		sym = g.origin(sym)
	}
	if sym.Kind == model.KindModule {
		if root := g.moduleRoot(sym); root != nil {
			return []Location{{File: root}}
		}
	}
	declarations := g.Declarations(sym)
	if slices.ContainsFunc(declarations, func(d Occurrence) bool { return d.kind != model.KindParameter }) {
		declarations = slices.DeleteFunc(declarations, func(d Occurrence) bool { return d.kind == model.KindParameter })
	} else if len(declarations) > 1 {
		declarations = g.nearestParameter(declarations)
	}
	if sym.id.module != nil && sym.id.module.Links() != nil {
		declarations = merge(declarations)
	}
	var out []Location
	for _, d := range declarations {
		out = append(out, Location{File: d.File, Span: d.Span})
	}
	return append(out, g.sources(sym)...)
}

func (g *Group) nearestParameter(formals []Occurrence) []Occurrence {
	own := slices.DeleteFunc(slices.Clone(formals), func(d Occurrence) bool { return d.File.Path != g.Self.Path })
	if len(own) > 0 {
		return own[:1]
	}
	return formals[:1]
}

func (g *Group) sources(sym Symbol) []Location {
	if g.builder == nil {
		return nil
	}
	var out []Location
	seen := map[string]bool{}
	for _, t := range sym.targets {
		links := t.Module.Links()
		if links == nil {
			continue
		}
		for _, path := range links.Sources(t.File, t.Symbol) {
			if seen[path] {
				continue
			}
			seen[path] = true
			if file := g.builder.load(path); file != nil {
				out = append(out, Location{File: file})
				break
			}
		}
	}
	return out
}

func (g *Group) moduleRoot(sym Symbol) *workspace.File {
	for _, t := range sym.targets {
		sub := t.Module.Submodules[t.Info().Name]
		if sub != nil && len(sub.Files) > 0 {
			return g.files[sub.Files[0]]
		}
	}
	return nil
}

func (g *Group) ShellDefinition(sym Symbol) []Location { return g.definitions(sym) }

func References(g *Group, sym Symbol, includeDeclaration bool) []Occurrence {
	occs := g.Occurrences(sym)
	if sym.binding != nil {
		occs = merge(slices.Concat(occs, g.Occurrences(*sym.binding)))
	}
	if includeDeclaration {
		return occs
	}
	return slices.DeleteFunc(occs, func(o Occurrence) bool { return o.Declaration })
}

func (g *Group) Highlights(sym Symbol) []Occurrence {
	return slices.DeleteFunc(References(g, sym, true), func(o Occurrence) bool { return o.File.Path != g.Self.Path })
}

func (g *Group) PrepareRename(sym Symbol) error {
	if sym.link != nil {
		if sym.link.refusal != "" {
			return fmt.Errorf("%w: %s", navigation.ErrNotRenamable, sym.link.refusal)
		}
		return nil
	}
	if !sym.Defined() {
		return fmt.Errorf("%w: %s `%s` is built in", navigation.ErrNotRenamable, sym.Kind, sym.Name)
	}
	if sym.lexical != nil {
		return fmt.Errorf("%w: `%s` %s", navigation.ErrNotRenamable, sym.Name, sym.lexical.source)
	}
	if sym.foreign {
		return fmt.Errorf("%w: `%s` is the name of a program, not a binding of this file", navigation.ErrNotRenamable, sym.Name)
	}
	if len(sym.locals) > 0 {
		return nil
	}
	if reason := g.refusal(sym); reason != "" {
		return fmt.Errorf("%w: %s", navigation.ErrNotRenamable, reason)
	}
	if sym.Kind == model.KindModule && g.moduleRoot(sym) != nil && !g.explicitPath(sym) {
		return fmt.Errorf("%w: module `%s` is found by its name; give it an explicit path first", navigation.ErrNotRenamable, sym.Name)
	}
	return nil
}

func (g *Group) refusal(sym Symbol) string {
	t := sym.targets[0]
	if links := t.Module.Links(); links != nil {
		if reason, decided := links.Refusal(t.File, t.Symbol); decided {
			return reason
		}
	}
	if r := renamesOf(sym); r != nil {
		return r.Refusal(t.Symbol)
	}
	return ""
}

func renamesOf(sym Symbol) model.Renames {
	t := sym.targets[0]
	return t.Module.Files[t.File].Renames
}

func (g *Group) explicitPath(sym Symbol) bool {
	for _, t := range sym.targets {
		ix := t.Module.Files[t.File]
		for _, inc := range ix.Includes {
			if inc.Module == sym.Name && inc.Path != sym.Name {
				return true
			}
		}
	}
	return false
}

func (g *Group) Rename(sym Symbol, newName string) ([]Occurrence, error) {
	if err := g.PrepareRename(sym); err != nil {
		return nil, err
	}
	if sym.link != nil {
		return g.renameLink(sym, newName)
	}
	occs := g.Occurrences(sym)
	shell := slices.ContainsFunc(occs, func(o Occurrence) bool { return o.Shell })
	if !g.Language.ValidName(sym.Kind, newName) || shell && sym.Kind != g.Language.Recursion().Kind && !parser.IsName(newName) {
		return nil, fmt.Errorf("%w: %q is not a valid %s name", navigation.ErrInvalidName, newName, sym.Kind)
	}
	if newName == sym.Name {
		return nil, nil
	}
	if len(sym.locals) > 0 {
		if !parser.IsName(newName) {
			return nil, fmt.Errorf("%w: %q is not a valid shell name", navigation.ErrInvalidName, newName)
		}
		return occs, nil
	}
	t := sym.targets[0]
	if clash := t.Module.Resolve(t.File, model.Ref{Name: newName, Kind: sym.Kind, Scope: t.Info().Scope}); len(clash.Targets) > 0 {
		return nil, fmt.Errorf("%w: %s `%s` already exists", navigation.ErrNameConflict, clash.Targets[0].Info().Kind, newName)
	}
	if links := t.Module.Links(); links != nil {
		if reason := links.Conflict(t.File, t.Symbol, newName); reason != "" {
			return nil, fmt.Errorf("%w: %s", navigation.ErrNameConflict, reason)
		}
	}
	if r := renamesOf(sym); r != nil {
		if reason := r.Conflict(t.Symbol, newName); reason != "" {
			return nil, fmt.Errorf("%w: %s", navigation.ErrNameConflict, reason)
		}
	}
	return occs, nil
}

type Hover struct {
	Markdown string
	Span     parser.Span
}

func (g *Group) Hover(offset int) (Hover, bool) {
	if r, ok := g.includeAt(offset); ok {
		if r.target == nil {
			return Hover{Markdown: "`" + r.include.Path + "` is not found", Span: r.include.Span}, true
		}
		return Hover{Markdown: "`" + r.target.Path + "`", Span: r.include.Span}, true
	}
	sym, ok := g.At(offset)
	if !ok {
		return Hover{}, false
	}
	return g.Describe(sym), true
}

func (g *Group) Describe(sym Symbol) Hover {
	if sym.link != nil {
		return g.describeLink(sym)
	}
	if sym.lexical != nil {
		return Hover{Markdown: "`" + sym.Name + "` " + sym.lexical.source, Span: sym.Span}
	}
	fence := g.Language.Fence()
	if b := sym.Builtin; b != nil {
		md := "```" + fence + "\n" + b.Signature + "\n```\n\n" + b.Doc
		if b.Deprecated != "" {
			md += "\n\n**Deprecated:** " + b.Deprecated + "."
		}
		return Hover{Markdown: md, Span: sym.Span}
	}
	var parts []string
	for _, t := range sym.targets {
		s := t.Info()
		md := "```" + fence + "\n" + s.Detail + "\n```"
		if s.Doc != "" {
			md += "\n\n" + s.Doc
		}
		md += "\n\n*" + s.Kind.String() + "*"
		if s.Exported {
			md += ", exported to the shell"
		}
		if file := g.files[t.Module.Files[t.File]]; file != nil && file.Path != g.Self.Path {
			md += " in `" + filepath.Base(file.Path) + "`"
		}
		if links := t.Module.Links(); links != nil {
			if script, ok := links.Script(t.File, t.Symbol); ok && script.Head != "" {
				md += "\n\n```" + script.Fence + "\n" + script.Head + "\n```"
			}
		}
		if project, nixLinks := t.Module.Links().(*nix.Project); nixLinks {
			if locked, ok := project.LockedInput(t.File, t.Symbol); ok {
				md += "\n\n" + locked
			}
		}
		parts = append(parts, md)
	}
	if len(sym.targets) == 0 {
		parts = append(parts, "```sh\n"+sym.Name+"\n```")
	}
	if sym.doc != "" {
		parts = append(parts, sym.doc)
	}
	return Hover{Markdown: strings.Join(parts, "\n\n---\n\n"), Span: sym.Span}
}

type Item struct {
	Label  string
	Kind   model.Kind
	Detail string
	Doc    string
	Span   parser.Span
}

func (g *Group) Complete(offset int) []Item {
	expect, ok := g.Self.Native.Expect(offset)
	if !ok {
		return nil
	}
	module := g.Module
	prefix := g.Self.Doc.Source().Text()[expect.Span.Start:min(offset, expect.Span.End)]
	if i := strings.LastIndex(prefix, "::"); i >= 0 {
		for name := range strings.SplitSeq(prefix[:i], "::") {
			if module = module.Submodules[name]; module == nil {
				return nil
			}
		}
		expect.Span.Start += i + 2
		expect.Scope = model.Global
	}
	var out []Item
	seen := map[string]bool{}
	add := func(item Item) {
		key := item.Kind.String() + "\x00" + item.Label
		if !seen[key] {
			seen[key] = true
			item.Span = expect.Span
			out = append(out, item)
		}
	}
	for _, kind := range expect.Kinds {
		for f, ix := range module.Files {
			for _, s := range ix.Symbols {
				visible := s.Scope == model.Global || f == g.selfAt() && module == g.Module && s.Scope == expect.Scope
				if s.Kind == kind && visible {
					add(Item{Label: s.Name, Kind: s.Kind, Detail: s.Detail, Doc: s.Doc})
				}
			}
		}
		if kind == model.KindModule {
			for name, sub := range module.Submodules {
				if sub != nil {
					add(Item{Label: name, Kind: model.KindModule, Detail: "mod " + name})
				}
			}
		}
		for _, b := range module.Builtins.Of(kind) {
			add(Item{Label: b.Name, Kind: kind, Detail: b.Signature, Doc: b.Doc})
		}
	}
	slices.SortStableFunc(out, func(a, b Item) int { return strings.Compare(a.Label, b.Label) })
	return out
}

type Link struct {
	Span parser.Span
	Path string
}

func (g *Group) Links() []Link {
	var out []Link
	for _, r := range g.includes {
		if r.target != nil {
			out = append(out, Link{Span: r.include.Span, Path: r.target.Path})
		}
	}
	if g.exists == nil {
		return out
	}
	ix := g.Index()
	for _, p := range ix.Paths {
		path := filepath.Join(filepath.Dir(g.Self.Path), filepath.FromSlash(strings.TrimPrefix(p.Path, "/")))
		if g.exists(path) {
			out = append(out, Link{Span: p.Span, Path: path})
		}
	}
	for _, r := range ix.Refs {
		if r.Kind != model.KindTarget || r.Owner < 0 || g.Module.Resolve(g.selfAt(), r).Found() {
			continue
		}
		if path, ok := g.prerequisite(r.Name); ok {
			out = append(out, Link{Span: r.Span, Path: path})
		}
	}
	return out
}

func (g *Group) prerequisite(name string) (string, bool) {
	dir := filepath.Dir(g.Self.Path)
	candidates := []string{name}
	if !filepath.IsAbs(name) {
		var search []model.SearchPath
		for _, ix := range g.Module.Files {
			search = append(search, ix.Search...)
		}
		for _, wanted := range []bool{true, false} {
			for _, s := range search {
				if (s.Pattern != "") != wanted || wanted && !model.MatchesPattern(s.Pattern, name) {
					continue
				}
				for _, d := range s.Dirs {
					candidates = append(candidates, filepath.Join(d, name))
				}
			}
		}
	}
	for _, c := range candidates {
		if !filepath.IsAbs(c) {
			c = filepath.Join(dir, c)
		}
		if g.exists(c) {
			return c, true
		}
	}
	return "", false
}
